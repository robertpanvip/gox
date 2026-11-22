// Package rustbackend 是 GX 的 Rust 渲染后端适配层（cgo 消费 Rust 静态库）。
//
// 依赖方向：本包 → GX host（runtime/app, runtime/scene）；host 零改动。
//
// 数据流：
//
//	GX 响应式 → app.Draw(root) → Window.Draw 把场景树序列化成二进制协议 → CABI → Rust 渲染
//	Rust 命中/IME → 事件回调 → goEventBridge → Window.events → Dispatcher → node.DispatchBubble
package rustbackend

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// Window 实现 gxapp.Window，把绘制委托给 Rust 静态库。
//
// 传输采用增量动作流（Zed 式）：首帧发全量快照（Rust 建镜像树），
// 之后每次 Draw 只发 app.TakeChanges() 的增量动作；遇结构动作退化重发快照。
type Window struct {
	handle unsafe.Pointer
	cfg    gxapp.WindowConfig

	app       *gxapp.App      // BindApp 注入，用于取增量动作
	hasBase   bool            // Rust 侧是否已建立基线镜像树
	events    chan inputEvent
	dispatcher func(ev InputEvent)
	mu        sync.Mutex
	closed    bool
}

// 全局注册表：C 回调的 ctx 是注册表 key（uintptr），避免把 Go 指针长期交给 C。
var (
	windows   = map[uintptr]*Window{}
	windowsMu sync.Mutex
	winSeq    uintptr = 1
)

// NewWindow 创建 Rust 后端窗口。
func NewWindow() *Window {
	return &Window{events: make(chan inputEvent, 64)}
}

// Open 实现 gxapp.Window。创建底层 Rust 窗口并注册事件回调。
// wgpu surface 与 winit 事件循环绑定调用线程，故固定 OS 线程。
func (w *Window) Open(cfg gxapp.WindowConfig) error {
	runtime.LockOSThread()
	w.cfg = cfg
	w.handle = cWindowNew(cfg.Width, cfg.Height, cfg.Title)
	if w.handle == nil {
		return fmt.Errorf("rustbackend: gx_window_new failed")
	}
	windowsMu.Lock()
	key := winSeq
	winSeq++
	windows[key] = w
	windowsMu.Unlock()
	cWindowSetEventCb(w.handle, key)
	return nil
}

// BindApp 注入 App 引用以启用增量动作流。Render 之后调用；未绑定则每帧全量。
func (w *Window) BindApp(app *gxapp.App) {
	w.mu.Lock()
	w.app = app
	w.hasBase = false // 重新建基线
	w.mu.Unlock()
	app.EnableChanges()
	app.TakeChanges() // 清掉接线期变更
}

// Draw 实现 gxapp.Window：首帧全量快照，后续增量动作。
func (w *Window) Draw(root *gxscene.Node) error {
	if w.handle == nil || root == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	var data []byte
	var err error
	if w.hasBase && w.app != nil {
		changes := w.app.TakeChanges()
		if len(changes) > 0 {
			if delta, ok := serializeDelta(changes, root, uint32(w.cfg.Width), uint32(w.cfg.Height)); ok {
				data = delta
			}
		}
	}
	// 没有基线 / 无 App / 有结构动作 / 无变化 → 发全量快照（无变化也重发首帧保证不空）
	if data == nil {
		data, err = serializeFrameBin(root, uint32(w.cfg.Width), uint32(w.cfg.Height))
		if err != nil {
			return fmt.Errorf("rustbackend: serialize frame: %w", err)
		}
		w.hasBase = true
	}
	if rc := cWindowDraw(w.handle, data); rc != 0 {
		return fmt.Errorf("rustbackend: gx_window_draw rc=%d", rc)
	}
	return nil
}

// Run 实现 gxapp.Window：goroutine 跑 Rust 事件循环，本线程消费事件并分派。
func (w *Window) Run() error {
	if w.handle == nil {
		return fmt.Errorf("rustbackend: window not opened")
	}
	done := make(chan int, 1)
	go func() { done <- cWindowRun(w.handle) }()
	for {
		select {
		case ev := <-w.events:
			w.mu.Lock()
			fn := w.dispatcher
			w.mu.Unlock()
			if fn != nil {
				fn(InputEvent{
					Kind: ev.Kind, NodeID: ev.NodeID, Event: ev.Event,
					Text: ev.Text, X: ev.X, Y: ev.Y,
				})
			}
		case rc := <-done:
			if rc != 0 {
				return fmt.Errorf("rustbackend: gx_window_run rc=%d", rc)
			}
			return nil
		}
	}
}

// SetDispatcher 注册事件分派器：把 Rust 回传事件映射到场景树节点。
// 典型实现：按 ev.NodeID 找节点 → node.DispatchBubble(ev.Event, ctx)。
func (w *Window) SetDispatcher(fn func(ev InputEvent)) {
	w.mu.Lock()
	w.dispatcher = fn
	w.mu.Unlock()
}

// InputEvent 是导出给业务的事件类型。
type InputEvent struct {
	Kind   string // "click" / "ime_preedit" / "ime_commit" / "focus" / "backspace" / "ime_enabled"
	NodeID string
	Event  string
	Text   string
	X      float32
	Y      float32
}

// Close 实现 gxapp.Window。
func (w *Window) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if w.handle != nil {
		cWindowClose(w.handle)
		w.handle = nil
	}
	runtime.UnlockOSThread()
	return nil
}

// Config 实现 gxapp.Window。
func (w *Window) Config() gxapp.WindowConfig { return w.cfg }

// Version 返回 Rust 后端版本（探测用）。
func Version() string { return cVersion() }
