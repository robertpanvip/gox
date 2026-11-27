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
	"encoding/json"
	"fmt"
	"sync"
	"time"
	"unsafe"

	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
	"github.com/robertpanvip/gox-core/runtime/scheduler"
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
	grx3        bool   // 后端支持 GRX3（协议 minor>=3）：NodeID u64 直传编码
	events      chan inputEvent
	dispatcher  func(ev InputEvent)
	mu          sync.Mutex
	closed      bool

	// 事件分级（评审 P1）：events 是可靠交付通道；overflow 是可靠事件在
	// events 满时的无界溢出缓冲（不丢），mergeSlot 只保留最新可合并事件。
	// pump 回调线程写入 overflow/mergeSlot 后，Poll 每轮搬运消费。
	overflow  []inputEvent
	mergeSlot *inputEvent
	pump      chan struct{}

	// 生命周期（P4 目标态）：窗口由 Host 主循环 Poll 驱动，无自驱 Run。
	// pumped 记录是否泵过（Close 决定是否需要让 Rust 事件循环退出）；
	// closeRequested 在 pump 返回 Exit（用户点关闭 / 请求关闭）后置位。
	pumped         bool
	closeRequested bool
	destroyed      bool
	regKey         uintptr // 全局注册表 key，Close 时注销（评审 P1 泄漏）

	// 统一帧时钟（BACKEND_ARCHITECTURE_V2 阶段 1）：Signal 更新合并成一帧，
	// Host 主循环每轮 Poll 派发绘制（App.SetFrameClock 时启用）。wakeHook 是
	// Host 注入的空闲打断函数（gxapp.IdleWaker）：RequestFrame 即时唤醒主循环。
	frameClock *scheduler.LoopClock
	wakeHook   func()

	// UIExecutor（宿主提案 §6.1）：Post 把绘制回调/微任务投递到 Run 事件泵
	// goroutine 执行——Run 的 select 循环就是桌面端「UI 线程」。Run 未启动时
	// 任务按 FIFO 排队等待（与 skia 装配层 loop.Queue 语义一致）；Close 后拒绝。
	// postCh 容量 1 只做唤醒信号（postQ 本身无界，不丢任务）。
	postQ  []func()
	postCh chan struct{}

	// drawFn 仅供测试注入：模拟 cWindowDraw 的返回码与捕获发送字节。
	// 生产环境为 nil，走真实 cgo 调用 cWindowDraw。
	drawFn func(data []byte) int
}

// reliable 报告事件是否必须可靠、有序交付（评审 P1 事件分级）。
// IME 提交/预编辑、键盘、指针按下/抬起、滚轮、点击、焦点属于可靠事件，
// 不能静默丢弃（丢失一次滚轮/按下都会让翻译层状态机错乱）；
// pointer_move 是高频状态事件，可合并只保留最新——拖拽/选区扩展只需
// 最新位置，丢中间值无语义影响。
func (e inputEvent) reliable() bool {
	switch e.Kind {
	case "click", "focus", "ime_enabled", "ime_commit", "ime_preedit", "backspace",
		"keydown", "keyup", "key", "pointer_down", "pointer_up", "wheel":
		return true
	default:
		return false // pointer_move 及未来新增的高频状态事件 → 合并槽
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

// drainPending 把 overflow 与 mergeSlot 搬运到 events 通道供 Poll 消费。
// **非阻塞**：events 放不下的可靠事件保序留在 overflow（不丢）、可合并事件
// 放回 mergeSlot——Poll 是唯一消费者且与搬运同线程串行，先消费腾出空间、
// 高频事件流下逐轮追平，绝不卡住 Host 主循环。
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

	keep := -1 // 第一个放不下的可靠事件下标（其后整体保序保留）
	for i := range ov {
		select {
		case w.events <- ov[i]:
		default:
			keep = i
		}
		if keep >= 0 {
			break
		}
	}
	mergedKept := false
	if merged != nil {
		select {
		case w.events <- *merged:
		default:
			mergedKept = true
		}
	}
	w.mu.Lock()
	if keep >= 0 {
		// 保序放回：未搬完的可靠事件在前，接上搬运期间新到的溢出。
		w.overflow = append(ov[keep:], w.overflow...)
	}
	if mergedKept && w.mergeSlot == nil {
		w.mergeSlot = merged
	}
	w.mu.Unlock()
}

// consumeEvents 非阻塞消费 events 通道，逐条派发给 dispatcher（Host 主循环线程
// 执行——与绘制同线程串行，符合单一 UI Loop 的线程契约）。
func (w *Window) consumeEvents() {
	for {
		var ev inputEvent
		select {
		case ev = <-w.events:
		default:
			return
		}
		w.mu.Lock()
		fn := w.dispatcher
		w.mu.Unlock()
		if fn != nil {
			fn(InputEvent{
				Kind: ev.Kind, NodeID: ev.NodeID, Event: ev.Event,
				Text: ev.Text, X: ev.X, Y: ev.Y, Mods: ev.Mods,
			})
		}
	}
}

// 编译期断言：Frame 契约 + 焦点能力 + P4 目标态的 Host 轮询驱动与唤醒注入。
var (
	_ gxapp.FramePresenter = (*Window)(nil)
	_ gxapp.Focuser        = (*Window)(nil)
	_ gxapp.HostedWindow   = (*Window)(nil)
	_ gxapp.IdleWaker      = (*Window)(nil)
)

// 全局注册表：C 回调的 ctx 是注册表 key（uintptr），避免把 Go 指针长期交给 C。
var (
	windows   = map[uintptr]*Window{}
	windowsMu sync.Mutex
	winSeq    uintptr = 1
)

// NewWindow 创建 Rust 后端窗口。
func NewWindow() *Window {
	// GRX3 能力探测（协议 minor>=3）：NodeID u64 直传编码。旧后端（探测失败
	// 或版本不满足）自动走 GRX1/GRX2 字符串编码，双版本兼容。
	_, minor, ok := parseProtocolVersion(cVersion())
	grx3 := ok && minor >= 3
	return &Window{
		events: make(chan inputEvent, 64),
		pump:   make(chan struct{}, 1),
		postCh: make(chan struct{}, 1),
		grx3:   grx3,
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
	if major, minor, ok := parseProtocolVersion(cVersion()); ok && major != expectedProtocolMajor {
		// minor>=3 → 后端支持 GRX3（NodeID u64 直传）。旧后端自动走 GRX1/GRX2。
		_ = minor
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

	// 过载恢复（BACKEND_ARCHITECTURE_V2 §六.2）：Rust FrameQueue 过载背压丢弃
	// 了未消费增量帧——镜像停在更早版本，但 Go 的 Version 看起来仍连续，
	// 下面的跳号检查发现不了。必须显式查询（读后清零）并强制本帧全量重建。
	if w.drawFn == nil && cWindowTakeFullSync(w.handle) == 1 {
		frame.FullSync = true
	}

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
		// GRX3 优先（id u64 直传）；无法编码（非数字 id 等）回退全量快照。
		var delta []byte
		var ok bool
		if w.grx3 {
			delta, ok = serializeDeltaV3(frame.Changes, root, uint32(w.cfg.Width), uint32(w.cfg.Height))
		} else {
			delta, ok = serializeDelta(frame.Changes, root, uint32(w.cfg.Width), uint32(w.cfg.Height))
		}
		if ok {
			data = delta
		}
	}
	if data == nil {
		if w.grx3 {
			data, err = serializeFrameBinV3(root, uint32(w.cfg.Width), uint32(w.cfg.Height))
		} else {
			data, err = serializeFrameBin(root, uint32(w.cfg.Width), uint32(w.cfg.Height))
		}
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

// Run 实现 gxapp.Window：P4 目标态下退化为 no-op——窗口由 Host 主循环经
// Poll 驱动（winit pump_events 嵌入泵），不再有自驱阻塞事件循环。
// 直接 gxapp.Open + app.Run 的旧单窗口路径对本后端不再可用：请走
// gxapp.Use(Backend) → Handle.Render / Handle.Run 的 Host 接入。
func (w *Window) Run() error { return nil }

// Poll 实现 gxapp.HostedWindow：非阻塞泵一轮 winit 事件，随后消费回调线程
// 投递的事件并派发。错误/panic 由 Host 归因熔断本窗口。
func (w *Window) Poll() error {
	w.mu.Lock()
	handle := w.handle
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return nil // 已关闭：Host 主循环随后把它逐出，Poll 保持幂等安全
	}
	if handle == nil {
		return fmt.Errorf("rustbackend: window not opened")
	}
	rc := cWindowPoll(handle)
	if rc == 2 {
		return fmt.Errorf("rustbackend: gx_window_pump 内部错误（panic）")
	}
	w.mu.Lock()
	w.pumped = true
	if rc == 0 {
		w.closeRequested = true
	}
	w.mu.Unlock()
	// 泵期间 Rust 回调线程投递的事件：先消费腾空间，再搬运溢出/合并槽，
	// 最后把追平的新事件一并派发（顺序：可靠事件 FIFO 不乱）。
	w.consumeEvents()
	w.drainPending()
	w.consumeEvents()
	w.pollFrameClock()
	return nil
}

// ShouldClose 实现 gxapp.HostedWindow：pump 返回 Exit（用户点了关闭按钮 /
// gx_window_close 请求退出）或窗口已关闭时为 true，Host 据此 CloseWindow。
func (w *Window) ShouldClose() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closeRequested || w.closed
}

// SetDispatcher 注册事件分派器：把 Rust 回传事件映射到场景树节点。
// 典型实现：按 ev.NodeID 找节点 → node.DispatchBubble(ev.Event, ctx)。
func (w *Window) SetDispatcher(fn func(ev InputEvent)) {
	w.mu.Lock()
	w.dispatcher = fn
	w.mu.Unlock()
}

// Post 实现 gxapp.UIExecutor：把任务投递到 Run 事件泵 goroutine 执行。
//
// 语义与 skia 装配层的 loop.Queue 一致：任务只入队、绝不在调用栈执行，
// 统一由 Run 的 select 循环 drain——保证绘制回调、后台 Dispatch 与 Rust
// 事件处理天然串行（UI 线程亲和性）。Run 未启动时任务排队等待；Close 后
// 拒绝。postCh 仅做非阻塞唤醒信号（与 pump 互不干扰），postQ 本身无界不丢。
func (w *Window) Post(fn func()) error {
	if fn == nil {
		return fmt.Errorf("rustbackend: Post 回调不能为 nil")
	}
	w.mu.Lock()
	if w.postCh == nil {
		w.mu.Unlock()
		return fmt.Errorf("rustbackend: 窗口未初始化（须经 NewWindow 构造）")
	}
	if w.closed {
		w.mu.Unlock()
		return fmt.Errorf("rustbackend: 窗口已关闭，拒绝新任务")
	}
	w.postQ = append(w.postQ, fn)
	w.mu.Unlock()
	select {
	case w.postCh <- struct{}{}:
	default: // 已有唤醒信号在途，无需重复
	}
	return nil
}

// drainPosts 执行当前排队的全部 Post 任务（FIFO）。只在 Run 事件泵 goroutine
// 调用（drain 期间新到的任务追加到 postQ，由下一轮循环取走，不在持锁态执行
// 回调——回调可能写 signal 触发再入队）。
func (w *Window) drainPosts() {
	for {
		w.mu.Lock()
		if len(w.postQ) == 0 {
			w.mu.Unlock()
			return
		}
		fns := w.postQ
		w.postQ = nil
		w.mu.Unlock()
		for _, fn := range fns {
			fn()
		}
	}
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
	Kind   string // "click" / "pointer_down" / "pointer_up" / "pointer_move" / "wheel" / "key" / "ime_preedit" / "ime_commit" / "focus" / "backspace" / "ime_enabled"
	NodeID string
	Event  string  // click: 事件名；pointer: "left"；key: "down"/"repeat"
	Text   string  // ime 文本 / key: 键名（"Left"/"Home"/"a"…）
	X      float32 // 点击/指针坐标；wheel: 行增量
	Y      float32
	Mods   string // 修饰键（key/pointer）：空串或 "ctrl+shift+alt+super" 的子串
}

// FrameClock 返回统一帧时钟（BACKEND_ARCHITECTURE_V2 阶段 1）：惰性创建。
// wake 经 SetWakeHook 注入的 Host 空闲打断即时唤醒主循环（RequestFrame 不受
// idleWait 轮询间隔量化）；宿主在 Run 事件泵每轮 Poll 派发绘制（App.SetFrameClock
// 接线后 Signal 更新合并成一帧、无变化零绘制）。必须在 Run 前接线生效。
func (w *Window) FrameClock() scheduler.FrameClock {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.frameClock == nil {
		w.frameClock = scheduler.NewLoopClock(
			w.wakeHost, // wake：即时打断 Host 主循环空闲等待
			func() time.Duration { return time.Duration(time.Now().UnixNano()) },
		)
	}
	return w.frameClock
}

// SetWakeHook 实现 gxapp.IdleWaker（P4）：Host 装配窗口时注入主循环空闲打断
// 函数（h.wakeUp，非阻塞、容量 1 合并）。fn 为 nil 表示解除注入。
func (w *Window) SetWakeHook(fn func()) {
	w.mu.Lock()
	w.wakeHook = fn
	w.mu.Unlock()
}

// wakeHost 是帧时钟的 wake 注入点：优先走 Host 注入的 wakeHook（即时打断
// idleWait）；未注入（窗口未纳入 Host / 独立使用）时退化为非阻塞 pump 信号
// ——P4 前 pump 由自驱 Run 消费，P4 目标态无读者时为空操作。任何路径都不阻塞：
// 历史上此处的阻塞发送曾在 pump 无读者时把 UI 线程永久卡死（fpsbench 第二帧
// 死锁），wake 必须保持「只提示、不等待」语义。
func (w *Window) wakeHost() {
	w.mu.Lock()
	hook := w.wakeHook
	w.mu.Unlock()
	if hook != nil {
		hook()
		return
	}
	select {
	case w.pump <- struct{}{}:
	default:
	}
}

// pollFrameClock 在 Run 事件泵每轮调用：帧时钟 pending 则派发一次 OnFrame
// （App 内部 drawNowErr → Present → Rust 渲染）。未接线 SetFrameClock 时零开销。
func (w *Window) pollFrameClock() {
	w.mu.Lock()
	clock := w.frameClock
	w.mu.Unlock()
	if clock != nil {
		clock.Poll()
	}
}

// DebugWindowSize 供 GUI E2E 快照读取窗口逻辑尺寸（点）。
// V2 后端窗口不可 resize（winit with_resizable(false)），配置尺寸即实际尺寸。
func (w *Window) DebugWindowSize() (int, int) {
	cfg := w.Config()
	return cfg.Width, cfg.Height
}

// DebugAllBoxes 导出最近一帧全部命中框（x/y/w/h + label/testid/kind），供
// E2E 按文案动态定位（与旧 skia glwindow 同语义）。数据来自 Rust 渲染端
// gx_window_debug_info（镜像树反查节点文本），未泵过事件循环时返回 nil。
func (w *Window) DebugAllBoxes() []map[string]any {
	w.mu.Lock()
	opened := w.handle != nil && w.pumped
	w.mu.Unlock()
	if !opened {
		return nil
	}
	raw := cDebugInfo()
	if raw == "" {
		return nil
	}
	var info struct {
		WinW  float32          `json:"win_w"`
		WinH  float32          `json:"win_h"`
		Boxes []map[string]any `json:"boxes"`
	}
	if json.Unmarshal([]byte(raw), &info) != nil {
		return nil
	}
	return info.Boxes
}

// RequestClose 请求 Rust 事件循环退出（线程安全、非阻塞）。下一轮 Poll 返回
// Exit → ShouldClose=true，Host 主循环随即 CloseWindow。供后台 goroutine
// （定时自动关窗、外部信号）调用：与 Close 的区别：不等待、不销毁——只请求。
func (w *Window) RequestClose() {
	w.mu.Lock()
	handle := w.handle
	closed := w.closed
	w.mu.Unlock()
	if handle == nil || closed {
		return
	}
	cWindowClose(handle)
}

// VSyncMode 是 present/vsync 模式（P4.5 VSyncControl，高刷专项）。
type VSyncMode int

const (
	// VSyncFifo 默认：帧节奏由 swapchain 背压对齐屏幕刷新率（144Hz 屏 → 144fps），
	// 不撕裂、不空转烧 CPU。
	VSyncFifo VSyncMode = 0
	// VSyncMailbox 三缓冲替换：低延迟且不撕裂；多数 X11 驱动不支持（SetVSync 报错）。
	VSyncMailbox VSyncMode = 1
	// VSyncImmediate 关 vsync 立即上屏：可能撕裂，240fps 压测 / 极限低延迟专用。
	VSyncImmediate VSyncMode = 2
)

// SetVSync 运行时切换 present 模式（VSyncControl）。窗口已开（渲染器已创建）
// 立即 reconfigure 生效；未开窗则记录请求，首窗 resumed 时应用。
// surface 不支持该模式返回错误（保持原值）——144Hz 目标态常用：
// Fifo 跑满刷新率；压测/低延迟切 Immediate 观察 GPU 上限。
func (w *Window) SetVSync(mode VSyncMode) error {
	if mode < VSyncFifo || mode > VSyncImmediate {
		return fmt.Errorf("rustbackend: 未知的 VSyncMode %d", mode)
	}
	w.mu.Lock()
	handle := w.handle
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return fmt.Errorf("rustbackend: 窗口已关闭")
	}
	if handle == nil {
		// 未开窗：Rust 侧 Backend 已存在（handle 在 Open 才有）——这里只能先拒绝，
		// 开窗后再设置（与 RequestClose 同语义：句柄未就绪不静默）。
		return fmt.Errorf("rustbackend: 窗口未打开（先经 Host.Render/OpenWindow）")
	}
	if rc := cWindowSetVSync(handle, int(mode)); rc != 0 {
		return fmt.Errorf("rustbackend: surface 不支持 VSyncMode %d（rc=%d）", mode, rc)
	}
	return nil
}

// MousePassthroughMode 鼠标穿透模式。
type MousePassthroughMode int

const (
	// PassthroughOff 正常接收鼠标（恢复全窗口输入形状）。
	PassthroughOff MousePassthroughMode = iota
	// PassthroughWhole 整窗级：窗口不接收任何鼠标事件，点击落到下层窗口。
	PassthroughWhole
	// PassthroughRegion 区域级：只有本帧「有内容」的区域（不透明背景/图片/
	// canvas/video/边框 + 可交互命中框）接收鼠标，其余透明区点击落到下层
	// 窗口（与旧 skia glwindow 按 DrawLog 逐帧重设 XShape 同语义）。输入形状
	// 随帧自动更新（内容矩形变更时才调 XShape）。
	PassthroughRegion
)

// SetMousePassthroughMode 设置鼠标穿透模式（区域级为推荐形态）。
//
// 窗口未打开时拒绝（不静默）——与 SetVSync 同语义：句柄未就绪先经
// Host.Render/OpenWindow 开窗再设置。区域级 X11 经 XShape ShapeInput 实现，
// Wayland 与 Windows/macOS 无区域级 API 返回错误（可用整窗级回退）；
// 整窗级三端全覆盖（X11 XShape / Win 扩展样式 / mac ignoresMouseEvents）。
func (w *Window) SetMousePassthroughMode(mode MousePassthroughMode) error {
	if mode < PassthroughOff || mode > PassthroughRegion {
		return fmt.Errorf("rustbackend: 未知的 MousePassthroughMode %d", mode)
	}
	w.mu.Lock()
	handle := w.handle
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return fmt.Errorf("rustbackend: 窗口已关闭")
	}
	if handle == nil {
		return fmt.Errorf("rustbackend: 窗口未打开（先经 Host.Render/OpenWindow）")
	}
	if rc := cWindowSetMousePassthrough(handle, int(mode)); rc != 0 {
		return fmt.Errorf("rustbackend: SetMousePassthroughMode(%d) 失败（rc=%d，平台不支持或系统库缺失）", mode, rc)
	}
	return nil
}

// SetMousePassthrough 整窗级穿透开关（SetMousePassthroughMode 的布尔便利形式）。
func (w *Window) SetMousePassthrough(enabled bool) error {
	if enabled {
		return w.SetMousePassthroughMode(PassthroughWhole)
	}
	return w.SetMousePassthroughMode(PassthroughOff)
}

// VSync 返回当前请求的 vsync 模式（诊断用）。
func (w *Window) VSync() VSyncMode {
	w.mu.Lock()
	handle := w.handle
	w.mu.Unlock()
	if handle == nil {
		return VSyncFifo
	}
	return VSyncMode(cWindowGetVSync(handle))
}

// Close 实现 gxapp.Window。
//
// 优雅关闭顺序（P4 目标态生命周期）：
//  1. cWindowClose 只发退出请求（Rust 侧设置 shutdown + 唤醒事件循环），不释放内存；
//  2. 若本窗口被泵过，再非阻塞泵几轮让 Rust 观测 Exit、清空渲染器槽
//     （surface/设备与事件循环同生命周期，必须在 destroy 前释放）；
//  3. cWindowDestroy 释放调用方持有的强引用（Rust 侧兜底清空 app/el/renderer）；
//  4. 从全局注册表注销并清理回调上下文（评审 P1 泄漏）。
func (w *Window) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	handle := w.handle
	w.handle = nil
	pumped := w.pumped
	key := w.regKey
	w.mu.Unlock()

	if handle != nil {
		// 1. 请求 Rust 事件循环退出（不释放内存）。
		cWindowClose(handle)
		// 2. 泵过的窗口：非阻塞泵到退出（通常 1-2 轮），让渲染器槽先释放。
		if pumped {
			for i := 0; i < 8; i++ {
				if cWindowPoll(handle) != 1 {
					break
				}
			}
		}
		// 3. 释放 Backend（若未泵过，没有 GPU 资源，Rust destroy 兜底清理）。
		if !w.destroyed {
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
