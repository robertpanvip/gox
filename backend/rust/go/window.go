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

	hasBase     bool   // Rust 侧是否已成功建立基线镜像树
	lastVersion uint64 // 最近一次成功 Present 的 Frame.Version（丢帧检测）
	events      chan inputEvent
	dispatcher  func(ev InputEvent)
	mu          sync.Mutex
	closed      bool

	// 事件分级（评审 P1）：events 是可靠交付通道；overflow 是可靠事件在
	// events 满时的无界溢出缓冲（不丢），mergeSlot 只保留最新可合并事件。
	// pump 在 postEvent 写入 overflow/mergeSlot 后非阻塞通知 Run 去 drain。
	overflow  []inputEvent
	mergeSlot *inputEvent
	pump      chan struct{}

	// 生命周期（评审 P0）：runDone 在 Run 退出时 close；Close 等待它后再 destroy。
	runDone    chan struct{}
	runOnce    sync.Once
	runStarted bool
	destroyed  bool
	regKey     uintptr // 全局注册表 key，Close 时注销（评审 P1 泄漏）

	// drawFn 仅供测试注入：模拟 cWindowDraw 的返回码与捕获发送字节。
	// 生产环境为 nil，走真实 cgo 调用 cWindowDraw。
	drawFn func(data []byte) int
}

// reliable 报告事件是否必须可靠、有序交付（评审 P1 事件分级）。
// IME 提交/预编辑、键盘、点击、焦点、开关输入法属于可靠事件，不能静默丢弃；
// 光标移动、hover 等高频状态事件可合并，只保留最新（当前 Rust 端在内部边沿
// 检测处理 hover，未回传此类事件，但分级机制为未来扩展预留）。
func (e inputEvent) reliable() bool {
	switch e.Kind {
	case "click", "focus", "ime_enabled", "ime_commit", "ime_preedit", "backspace", "keydown", "keyup":
		return true
	default:
		return false
	}
}

// postEvent 是 Rust 回调（goEventBridge）与 Focus 之外的统一事件投递口。
// 可靠事件绝不丢弃（chan 满则进无界 overflow）；可合并事件覆盖 mergeSlot。
func (w *Window) postEvent(ev inputEvent) {
	if ev.reliable() {
		select {
		case w.events <- ev:
		default:
			w.mu.Lock()
			w.overflow = append(w.overflow, ev)
			w.mu.Unlock()
		}
	} else {
		w.mu.Lock()
		cp := ev
		w.mergeSlot = &cp
		w.mu.Unlock()
	}
	// 非阻塞唤醒 Run 去 drain 溢出/合并槽。
	select {
	case w.pump <- struct{}{}:
	default:
	}
}

// drainPending 把 overflow 与 mergeSlot 搬运到 events 通道供 Run 统一消费。
// 由 Run 在每次 pump 唤醒时调用。
func (w *Window) drainPending() {
	w.mu.Lock()
	ov := w.overflow
	w.overflow = nil
	var merged *inputEvent
	if w.mergeSlot != nil {
		merged = w.mergeSlot
		w.mergeSlot = nil
	}
	w.mu.Unlock()
	for _, ev := range ov {
		w.events <- ev // 这里可能阻塞，但 Run 是唯一消费者，安全
	}
	if merged != nil {
		select {
		case w.events <- *merged:
		default:
			// 极端情况下 events 仍满：合并事件允许覆盖最新一条到 mergeSlot。
			w.mu.Lock()
			w.mergeSlot = merged
			w.mu.Unlock()
		}
	}
}

// 编译期断言：本后端采用 Frame 契约，并实现可选的焦点能力。
var (
	_ gxapp.FramePresenter = (*Window)(nil)
	_ gxapp.Focuser        = (*Window)(nil)
)

// 全局注册表：C 回调的 ctx 是注册表 key（uintptr），避免把 Go 指针长期交给 C。
var (
	windows   = map[uintptr]*Window{}
	windowsMu sync.Mutex
	winSeq    uintptr = 1
)

// NewWindow 创建 Rust 后端窗口。
func NewWindow() *Window {
	return &Window{
		events:  make(chan inputEvent, 64),
		pump:    make(chan struct{}, 1),
		runDone: make(chan struct{}),
	}
}

// parseProtocolVersion 从 gx_window_version() 文本里解析 major.minor。
// 形如 "gxbackend-rust 0.2.0 (binary protocol)" → (0, 2, true)。
// Major 不一致拒绝启动；Minor 允许差异（新后端可兼容旧 Host 的能力子集）。
func parseProtocolVersion(s string) (major, minor int, ok bool) {
	// 找第一段 "数字.数字"。
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, 0, false
	}
	i := start
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		major = major*10 + int(s[i]-'0')
		i++
	}
	if i >= len(s) || s[i] != '.' {
		return 0, 0, false
	}
	i++
	minorStart := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		minor = minor*10 + int(s[i]-'0')
		i++
	}
	if i == minorStart {
		return 0, 0, false
	}
	return major, minor, true
}

// decoratedC 把 WindowConfig.Decorated(*bool) 翻译成 C ABI 三态：
// -1 未设置(后端默认)，0 无边框，1 有边框。
func decoratedC(v *bool) int {
	if v == nil {
		return -1
	}
	if *v {
		return 1
	}
	return 0
}

// 期望的 Wire 协议主版本。Rust runtime 与 Go Adapter 独立成仓后，Major 不一致
// 必须在启动时直接拒绝，而不是靠双方手动同步文档（评审 P2 版本协商）。
const expectedProtocolMajor = 0

// Open 实现 gxapp.Window。创建底层 Rust 窗口并注册事件回调。
//
// 线程模型（评审 P0）：事件循环由 Rust 后端在 gx_window_run 所在线程自管理，
// Go 侧不再 LockOSThread——旧实现锁定的调用线程与 Rust 事件循环线程根本不是同一条，
// 只会白白占用 OS 线程且给出错误的安全感。
func (w *Window) Open(cfg gxapp.WindowConfig) error {
	// Wire 版本协商（评审 P2）：明确解析出不同 Major 时拒绝启动，防止 core 更新
	// 了 GRX2 而用户仍用旧版后端这种难排查的错配。Minor 差异放行（能力向后兼容）。
	// 字符串解析失败时放行——协议版本文本格式脆弱，不应因无法判断而阻塞启动。
	if major, _, ok := parseProtocolVersion(cVersion()); ok && major != expectedProtocolMajor {
		return fmt.Errorf("rustbackend: wire 协议不兼容: got %q (major=%d), want major=%d",
			cVersion(), major, expectedProtocolMajor)
	}
	w.cfg = cfg
	w.handle = cWindowNewEx(cfg.Width, cfg.Height, cfg.Title,
		decoratedC(cfg.Decorated), cfg.Transparent, cfg.AlwaysOnTop, cfg.Opacity)
	if w.handle == nil {
		return fmt.Errorf("rustbackend: gx_window_new failed")
	}
	windowsMu.Lock()
	key := winSeq
	winSeq++
	windows[key] = w
	w.regKey = key
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
	w.lastVersion = 0 // 版本序列随新树重置
	w.mu.Unlock()
	if app != nil {
		app.EnableChanges()
	}
}

// Present 实现 gxapp.FramePresenter：首帧全量快照，后续用 frame.Changes 发增量。
// 后端不再反向访问 App——Changes 由 Host 随帧交付。
//
// 镜像一致性契约（Rust 后端评审 P0/P1）：
//   - 只有 cWindowDraw 成功才推进 hasBase/lastVersion；失败立即回退 hasBase，
//     下一帧用 Root 全量重建，杜绝「Go 以为有基线、Rust 实际没有」的永久错配。
//   - Version 必须严格递增 1；跳号/回退/重复视为丢帧，强制全量同步。
//   - 无变化且已有基线时不发送任何数据（避免空帧触发整棵树重序列化）。
func (w *Window) Present(frame *gxapp.Frame) error {
	if w.handle == nil || frame == nil || frame.Root == nil {
		return nil
	}
	root := frame.Root
	w.mu.Lock()
	defer w.mu.Unlock()

	// 版本连续性：有基线时 Version 必须恰好 +1，否则镜像可能已失步，强制全量重建。
	needFull := false
	if w.hasBase && frame.Version > 0 && frame.Version != w.lastVersion+1 {
		needFull = true
	}
	fullSync := frame.FullSync || needFull || !w.hasBase

	// 无变化且基线完好且无需全量：不发数据。
	// 注意仍要推进 lastVersion——Host 每帧都递增 Version，跳过会让下一帧被误判丢帧。
	if w.hasBase && !fullSync && len(frame.Changes) == 0 {
		w.lastVersion = frame.Version
		return nil
	}

	var data []byte
	var err error
	if !fullSync && len(frame.Changes) > 0 {
		if delta, ok := serializeDelta(frame.Changes, root, uint32(w.cfg.Width), uint32(w.cfg.Height)); ok {
			data = delta
		}
	}
	if data == nil {
		data, err = serializeFrameBin(root, uint32(w.cfg.Width), uint32(w.cfg.Height))
		if err != nil {
			return fmt.Errorf("rustbackend: serialize frame: %w", err)
		}
	}

	// 只有 draw 成功才确认基线；失败回退让下一帧全量重建。
	if rc := w.draw(data); rc != 0 {
		w.hasBase = false
		return fmt.Errorf("rustbackend: gx_window_draw rc=%d", rc)
	}
	w.hasBase = true
	w.lastVersion = frame.Version
	return nil
}

// draw 是 cWindowDraw 的窄封装，仅为测试注入留钩子；生产路径零开销直通 cgo。
func (w *Window) draw(data []byte) int {
	if w.drawFn != nil {
		return w.drawFn(data)
	}
	return cWindowDraw(w.handle, data)
}

// Draw 实现 gxapp.Window 的兼容路径：未走 Frame 契约时每帧发全量快照。
// App 检测到本后端实现 FramePresenter 后会优先调用 Present，故正常路径不会到这里；
// 保留它是为了满足 gxapp.Window 接口（需要一个 Draw 方法）。
func (w *Window) Draw(root *gxscene.Node) error {
	// 兼容路径无 Version/Changes 概念，强制全量快照（与旧行为一致）。
	return w.Present(&gxapp.Frame{Root: root, FullSync: true})
}

// Run 实现 gxapp.Window：goroutine 跑 Rust 事件循环，本线程消费事件并分派。
func (w *Window) Run() error {
	w.mu.Lock()
	if w.handle == nil {
		w.mu.Unlock()
		return fmt.Errorf("rustbackend: window not opened")
	}
	w.runStarted = true
	w.mu.Unlock()
	// runDone 在退出时 close，通知 Close 可以安全 destroy Backend（评审 P0）。
	defer w.runOnce.Do(func() { close(w.runDone) })

	done := make(chan int, 1)
	handle := w.handle
	go func() { done <- cWindowRun(handle) }()
	for {
		select {
		case <-w.pump:
			// 有可靠事件溢出或可合并事件更新，搬运到 events 再消费。
			w.drainPending()
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
			// 事件循环已退出：最后再 drain 一次，避免尾部事件丢失。
			w.drainPending()
			for len(w.events) > 0 {
				ev := <-w.events
				w.mu.Lock()
				fn := w.dispatcher
				w.mu.Unlock()
				if fn != nil {
					fn(InputEvent{
						Kind: ev.Kind, NodeID: ev.NodeID, Event: ev.Event,
						Text: ev.Text, X: ev.X, Y: ev.Y,
					})
				}
			}
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

// Focus 实现 gxapp.Focuser：把 ref 句柄的命令式聚焦意图注入事件通道，
// 走与「点击输入框」完全相同的 focus 事件闭环：
//
//	Focus(id) → w.events → Run → Dispatcher → widgets.Focus(node)
//
// 应用只需在 SetDispatcher 的处理器里对 "focus" 事件调用 widgets.Focus
// （demo 已如此处理原生点击焦点），无需为 ref 另写一套焦点逻辑。Focuser
// 因此不引入第二套焦点状态，也不依赖组件库——事件分派权始终在应用层。
//
// 非阻塞投递：focus 是可靠事件，走统一 postEvent——通道满时进无界溢出队列，
// 不丢弃、不阻塞调用方，与 goEventBridge 的分级策略一致。
func (w *Window) Focus(id gxscene.NodeID) {
	if w.events == nil {
		return // 零值 Window（测试构造）
	}
	w.postEvent(inputEvent{Kind: "focus", NodeID: id.String()})
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
//
// 优雅关闭顺序（评审 P0 生命周期）：
//  1. cWindowClose 只发退出请求（Rust 侧设置 shutdown + 唤醒事件循环），不释放内存；
//  2. 若 Run 正在跑，等待 runDone——确保 Rust 的 gx_window_run 已返回、已 drop 其借用的 Arc；
//  3. cWindowDestroy 释放调用方持有的最后一份 Arc，Backend 此刻才真正析构；
//  4. 从全局注册表注销并清理回调上下文（评审 P1 泄漏）。
//
// 这样 Run 使用 Backend 期间内存始终有效，彻底消除 use-after-free。
func (w *Window) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	handle := w.handle
	w.handle = nil
	started := w.runStarted
	runDone := w.runDone
	key := w.regKey
	destroyed := w.destroyed
	w.mu.Unlock()

	if handle != nil {
		// 1. 请求 Rust 事件循环退出（不释放内存）。
		cWindowClose(handle)
		// 2. 等事件循环退出后再释放，避免 Run/Close 竞争。
		if started {
			<-runDone
		}
		// 3. 释放 Backend（若 run 未启动，没有其它引用，直接释放也安全）。
		if !destroyed {
			cWindowDestroy(handle)
			w.mu.Lock()
			w.destroyed = true
			w.mu.Unlock()
		}
	}

	// 4. 注销全局注册表 + 清理 Rust 侧回调上下文（评审 P1 泄漏）。
	if key != 0 {
		windowsMu.Lock()
		delete(windows, key)
		windowsMu.Unlock()
	}
	return nil
}

// Config 实现 gxapp.Window。
func (w *Window) Config() gxapp.WindowConfig { return w.cfg }

// Version 返回 Rust 后端版本（探测用）。
func Version() string { return cVersion() }
