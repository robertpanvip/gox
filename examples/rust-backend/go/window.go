// Package rustbackend 是 GX 的 Rust 渲染后端适配层（cgo 消费 Rust 静态库）。
//
// 依赖方向：本包 → GX host（runtime/app, runtime/scene）；host 零改动。
//
// 数据流：
//
//	GX 响应式 → Host 提交 Frame{Root,Changes,Version} → Window.Present 序列化成二进制协议 → CABI → Rust 渲染
//	Rust 命中/IME → 事件回调 → goEventBridge → Window.events → Dispatcher → node.DispatchBubble
//
// 增量由 Host 经 Frame 契约主动交付（见 gxapp.FramePresenter），后端零反向依赖。
package rustbackend

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// Window 实现 gxapp.Window + gxapp.FramePresenter，把绘制委托给 Rust 静态库。
//
// 传输采用增量动作流（Zed 式）：首帧发全量快照（Rust 建镜像树），
// 之后每帧只发 Frame.Changes 里的增量动作；遇结构动作退化重发快照。
//
// 增量由 Host 通过 Frame 契约随帧交付（Present），后端不再反向持有 App 取
// Changes——这正是 Host 架构评审 P0「增量提交通道」要求的接入方式。
type Window struct {
	handle unsafe.Pointer
	cfg    gxapp.WindowConfig

	hasBase   bool            // Rust 侧是否已建立基线镜像树
	events    chan inputEvent
	dispatcher func(ev InputEvent)
	mu        sync.Mutex
	closed    bool
}

// 编译期断言：本后端采用 Frame 契约。
var _ gxapp.FramePresenter = (*Window)(nil)

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

// BindApp 启用增量动作流（兼容旧 demo 调用）。Render 之后调用。
//
// 旧实现在此保存 App 引用并从中 TakeChanges；采用 Frame 契约后不再需要——Host 会把
// Changes 随 Frame 主动推给 Present。这里只负责打开变更记录并重置基线。参数保留以
// 维持 demo 源码兼容（调用方可传 nil）。
func (w *Window) BindApp(app *gxapp.App) {
	w.mu.Lock()
	w.hasBase = false // 重新建基线
	w.mu.Unlock()
	if app != nil {
		app.EnableChanges()
	}
}

// Present 实现 gxapp.FramePresenter：首帧全量快照，后续用 frame.Changes 发增量。
// 后端不再反向访问 App——Changes 由 Host 随帧交付，Version 可用于去重/排序。
func (w *Window) Present(frame *gxapp.Frame) error {
	if w.handle == nil || frame == nil || frame.Root == nil {
		return nil
	}
	root := frame.Root
	w.mu.Lock()
	defer w.mu.Unlock()

	var data []byte
	var err error
	if w.hasBase && len(frame.Changes) > 0 {
		if delta, ok := serializeDelta(frame.Changes, root, uint32(w.cfg.Width), uint32(w.cfg.Height)); ok {
			data = delta
		}
	}
	// 没有基线 / 无变化 / 有结构动作退化 → 发全量快照。
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

// Draw 实现 gxapp.Window 的兼容路径：未走 Frame 契约时每帧发全量快照。
// App 检测到本后端实现 FramePresenter 后会优先调用 Present，故正常路径不会到这里；
// 保留它是为了满足 gxapp.Window 接口（需要一个 Draw 方法）。
func (w *Window) Draw(root *gxscene.Node) error {
	return w.Present(&gxapp.Frame{Root: root})
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
