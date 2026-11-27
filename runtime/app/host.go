package gxapp

import (
	"fmt"
	"sync"
	"time"

	"github.com/robertpanvip/gox/runtime/loop"
)

// Host / Handle：进程级应用宿主与接入层（P4 目标态，ui-loop-p4-multiwindows.md §3-§5）。
//
// 对应 GX 接入形态（GX 编译产物落到 Use / Handle.Render / Handle.Run 三行）：
//
//	app := gxapp.Use(rb.NewBackend())                        // gx.use(Backend)：接口注入
//	app.Render(func() any { return ui.App() }, gxapp.WindowConfig{ // app.render(() => <App/>, {...})
//		Title: "Main Window", Width: 1200, Height: 800,
//	})
//	app.Render(func() any { return ui.Settings() }, gxapp.WindowConfig{
//		Title: "Settings", Width: 600, Height: 400,
//	})
//	app.Run() // 唯一主循环：全部窗口退出才返回
//
// 后端可插拔模型：gxapp 只定义 Backend 接口形状（backend.go），具体后端实现
// 它并注入 Use——唯一接入路径，不做字符串分发、没有工厂/注册表便利层，
// 核心不依赖任何具体后端。
//
// 目标态模型（三条不变量，§3）：
//   - 一个 Loop：Host 持有进程内唯一的 *loop.Queue；所有窗口的 Dispatch / RAF /
//     帧重绘任务全部汇入同一队列，串行执行，响应式核心单线程假设成立。
//   - 一个主循环：Host.Run() 是唯一阻塞循环，逐轮「Poll 事件 → DrainTurn 任务 →
//     收 ShouldClose」；窗口后端不再各自 Run()，而是实现 HostedWindow 被 Host 轮询
//     （Window.Run 退化为 no-op，不再有自驱回退路径）。
//   - 故障隔离域 = 窗口：任务 panic（窗口级 guard）与 Poll panic/错误都被 recover
//     归因上报 FaultHandler，只熔断该窗口，其余窗口继续被主循环驱动。
//
// 线程契约：Host.Run 所在 goroutine 就是 UI 线程——所有窗口的事件、任务、绘制
// 在同一线程串行执行；后台 goroutine 经 app.Dispatch 把更新投递进共享 Loop。

// Fault 是一次窗口级故障的归因记录（Host 已 recover，未向上传播）。
type Fault struct {
	WindowID  int    // Host 内的窗口序号；Host.Go 的裸故障为 0
	Backend   string // 窗口所属后端标识（Backend.Name()）
	Phase     string // "poll"（取事件）/ "task"（UI 任务）/ "close"（关闭）/ "go"（受保护 goroutine）
	Panic     any    // recover 捕获的 panic 值（nil 表示非 panic 故障，如 Poll 返回错误）
	Recovered bool   // 恒为 true：Host 已隔离，未向上传播
}

// 默认空闲等待：主循环无任务无事件时的轮询间隔。Loop 有任务入队时 Wake
// 会立即打断等待，因此这只影响「平台事件到达但 Loop 无任务」的感知延迟。
const defaultIdleWait = 16 * time.Millisecond

// WindowBinder 是后端可选扩展：Host 装配完一个窗口并 Render 后通知后端
// 启用增量流（rustbackend 的 BindApp 约定在 Render 之后调用）。
type WindowBinder interface {
	BindApp(app *App)
}

// hostWindow 是 Host 管理的一个窗口条目。
type hostWindow struct {
	id      int
	backend string
	app     *App
	win     Window
}

// Host 是进程级应用宿主：持有唯一 UI Loop 与全部窗口，提供统一主循环与故障隔离。
type Host struct {
	mu       sync.Mutex
	seq      int
	windows  []*hostWindow
	fault    func(Fault)
	closed   bool
	inited   map[Backend]bool // 已 Init 的 BackendInitializer（幂等去重）
	shut     map[Backend]bool // 已 Shutdown 的后端（幂等去重）
	loopq    *loop.Queue      // 唯一 UI Loop（WithLoop 可复用外部实例）
	wake     chan struct{}    // Loop.Wake → 主循环空闲等待的即时打断
	idleWait time.Duration    // 主循环空闲轮询间隔
}

// HostOption 配置 Host。
type HostOption func(*Host)

// WithFaultHandler 注册窗口故障回调（Poll/任务 panic、关闭失败归因）。
func WithFaultHandler(fn func(Fault)) HostOption {
	return func(h *Host) { h.fault = fn }
}

// WithLoop 复用外部 UI Loop（缺省自建唯一实例）。
func WithLoop(q *loop.Queue) HostOption {
	return func(h *Host) { h.loopq = q }
}

// WithIdleWait 调整主循环空闲轮询间隔（默认 16ms）。
func WithIdleWait(d time.Duration) HostOption {
	return func(h *Host) { h.idleWait = d }
}

// NewHost 创建进程级宿主（自建唯一 UI Loop；Wake 接主循环空闲打断）。
func NewHost(opts ...HostOption) *Host {
	h := &Host{idleWait: defaultIdleWait, wake: make(chan struct{}, 1)}
	for _, o := range opts {
		o(h)
	}
	if h.loopq == nil {
		h.loopq = loop.New(loop.Config{Wake: h.wakeUp})
	}
	return h
}

// wakeUp 非阻塞唤醒主循环（Loop 首个任务入队时触发；容量 1 合并高频唤醒）。
func (h *Host) wakeUp() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// Loop 返回宿主唯一的 UI Loop（诊断/高级用法：Watchdog 联动、外部任务投递）。
func (h *Host) Loop() *loop.Queue { return h.loopq }

// Go 以受保护方式在后台 goroutine 执行 fn：panic 被 recover 归因上报
// FaultHandler（Phase="go"），不会带崩进程。Go 运行时无法 recover 用户
// 自己 `go func()` 的裸 panic——需要跨 goroutine 执行时请统一走本入口。
func (h *Host) Go(fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				h.reportFault(&hostWindow{}, "go", r)
			}
		}()
		fn()
	}()
}

// OpenWindow 在 Host 下打开一个窗口并创建其独立 App。
//
// 装配顺序（评审 P0）：Open → OpenWith（建 App）→ 注入共享 Loop 投递器
// （Dispatch/RAF/帧重绘全部汇入 Host 唯一 UI Loop）。win 必须实现
// HostedWindow（P4 目标态：窗口由 Host 主循环轮询，不再自驱 Run）。
func (h *Host) OpenWindow(cfg WindowConfig, win Window) (*App, error) {
	cfg = cfg.normalize()
	entry, err := h.openEntry(cfg, nil, win)
	if err != nil {
		return nil, err
	}
	return entry.app, nil
}

// OpenBackendWindow 按 cfg.Backend 从注册表选后端并打开窗口。语义同 OpenBackend，
// 但窗口纳入 Host 管理（可多开）。注册表是接口注入之上的可选便利层。
func (h *Host) OpenBackendWindow(cfg WindowConfig) (*App, error) {
	cfg = cfg.normalize()
	b, err := backendFromRegistry(cfg.Backend)
	if err != nil {
		return nil, err
	}
	entry, err := h.openEntry(cfg, b, nil)
	if err != nil {
		return nil, err
	}
	return entry.app, nil
}

// backendFromRegistry 按注册表名查后端并包装为 Backend 接口值。
func backendFromRegistry(name string) (Backend, error) {
	if name == "" || name == "headless" {
		return nil, nil // nil Backend → openEntry 走内置 Headless
	}
	f, ok := lookupBackend(name)
	if !ok {
		return nil, fmt.Errorf("gxapp: 未注册的 Backend %q（已注册: %v）", name, Backends())
	}
	return registryBackend{name: name, factory: f}, nil
}

// openEntry 公共装配路径：backend.NewWindow → Open → 建 App → 注入共享 Loop 投递器。
// backend 为 nil 时用内置 Headless（无头测试 / 纯逻辑验收）。
func (h *Host) openEntry(cfg WindowConfig, backend Backend, win Window) (*hostWindow, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, fmt.Errorf("gxapp: Host 已关闭，拒绝新窗口")
	}
	h.seq++
	id := h.seq
	h.mu.Unlock()

	if backend != nil {
		if err := h.initBackend(backend); err != nil {
			return nil, err
		}
	}
	if win == nil {
		if backend == nil {
			win = &HeadlessWindow{}
		} else {
			w, werr := backend.NewWindow(cfg)
			if werr != nil {
				return nil, fmt.Errorf("gxapp: 创建 Backend %q 窗口失败: %w", backend.Name(), werr)
			}
			if w == nil {
				return nil, fmt.Errorf("gxapp: Backend %q 的 NewWindow 返回 nil", backend.Name())
			}
			win = w
		}
	}
	backendName := ""
	if backend != nil {
		backendName = backend.Name()
	}

	// P4 目标态硬约束：Host 管理的窗口必须可被主循环轮询。不实现 HostedWindow
	// 的后端（仍在自驱 Run）直接拒绝接入——没有静默降级路径。
	hw, ok := win.(HostedWindow)
	if !ok {
		return nil, fmt.Errorf("gxapp: Backend %q 的窗口未实现 gxapp.HostedWindow（P4 目标态：窗口由 Host 主循环 Poll 驱动，不再自驱 Run）", backendName)
	}
	_ = hw
	app, err := OpenWith(cfg, win)
	if err != nil {
		return nil, err
	}

	entry := &hostWindow{id: id, backend: backendName, app: app, win: win}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = app.Close()
		return nil, fmt.Errorf("gxapp: Host 已关闭，窗口已回收")
	}
	h.windows = append(h.windows, entry)
	h.mu.Unlock()

	// 单一 UI Loop：窗口的全部 UI 任务（Dispatch / RAF / 帧重绘）经共享 Loop
	// 投递，由 Host.Run 的 DrainTurn 串行执行；外层包窗口级 guard 归因熔断。
	app.SetUIExecutor(hostUIExecutor{h: h, w: entry})
	return entry, nil
}

// hostUIExecutor 把 Host 共享 Loop 适配为 UIExecutor：窗口任务汇入唯一 UI Loop。
type hostUIExecutor struct {
	h *Host
	w *hostWindow
}

func (e hostUIExecutor) Post(fn func()) error {
	return e.h.postWindowTask(e.w, fn)
}

// postWindowTask 把窗口 UI 任务投递进共享 Loop，外层包窗口级 guard：
// 任务 panic 归因到该窗口并熔断（其余窗口不受影响），不污染 Loop 的
// 全局 OnTaskPanic 兜底语义（guard 先行 recover，Loop 观察不到该 panic）。
func (h *Host) postWindowTask(w *hostWindow, fn func()) error {
	return h.loopq.Post(func() {
		defer func() {
			if r := recover(); r != nil {
				h.reportFault(w, "task", r)
				_ = h.CloseWindow(w.app)
			}
		}()
		fn()
	})
}

// CloseWindow 关闭并移除一个窗口（按 App 句柄定位）。幂等；重复/未知句柄返回 nil。
// 只关这一个窗口，其它窗口不受影响（P4 不变量：故障/关闭隔离域 = 窗口）。
func (h *Host) CloseWindow(app *App) error {
	if app == nil {
		return nil
	}
	h.mu.Lock()
	idx := -1
	var target *hostWindow
	for i, w := range h.windows {
		if w.app == app {
			idx, target = i, w
			break
		}
	}
	if idx < 0 {
		h.mu.Unlock()
		return nil // 已被移除（重复 Close）或非本 Host 的窗口
	}
	h.windows = append(h.windows[:idx], h.windows[idx+1:]...)
	h.mu.Unlock()
	return target.app.Close()
}

// Windows 返回当前存活窗口的 App 快照。
func (h *Host) Windows() []*App {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*App, 0, len(h.windows))
	for _, w := range h.windows {
		out = append(out, w.app)
	}
	return out
}

// Run 启动 Host 唯一主循环并阻塞，直到所有窗口关闭。
//
// 每轮（ui-loop-p4-multiwindows.md §5.1 的目标态节奏）：
//  1. 逐窗 Poll：非阻塞取本窗口待处理事件（panic/错误 → 归因 + 熔断该窗口）；
//  2. DrainTurn：执行共享 Loop 里的全部 UI 任务——后台 Dispatch、响应式更新
//     触发的帧重绘（dispatcher sink → drawNowErr → Present/Draw）都在这里串行完成；
//  3. 收 ShouldClose：请求关闭的窗口逐个 CloseWindow。
//
// 空闲时等待 Loop.Wake（任务入队即时打断）或 idleWait 超时（感知平台事件）。
// 全部窗口关闭（或 Host.Close）后返回 nil；本路径没有自驱窗口，不存在并行
// Run 的错误汇总语义。
func (h *Host) Run() error {
	for {
		h.mu.Lock()
		wins := append([]*hostWindow(nil), h.windows...)
		h.mu.Unlock()
		if len(wins) == 0 {
			return nil
		}

		for _, w := range wins {
			h.pollWindow(w)
		}
		h.loopq.DrainTurn()
		for _, w := range wins {
			if w.win.(HostedWindow).ShouldClose() {
				_ = h.CloseWindow(w.app)
			}
		}

		select {
		case <-h.wake:
		case <-time.After(h.idleWait):
		}
	}
}

// pollWindow 取一轮窗口事件。panic 或错误都归因上报并熔断该窗口
// （CloseWindow 移除后主循环不再轮询它），其余窗口不受影响。
func (h *Host) pollWindow(w *hostWindow) {
	panicked := false
	err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				h.reportFault(w, "poll", r)
				err = fmt.Errorf("gxapp: 窗口 %q Poll panic（已隔离）: %v", w.win.Config().Title, r)
			}
		}()
		return w.win.(HostedWindow).Poll()
	}()
	if err != nil {
		if !panicked {
			h.reportFault(w, "poll", nil)
		}
		_ = h.CloseWindow(w.app)
	}
}

// initBackend 探测后端是否实现 BackendInitializer，是则幂等 Init 一次
// （首个窗口创建前的进程级钩子：字体库预热 / 协议握手 / 全局资源）。
func (h *Host) initBackend(b Backend) error {
	ini, ok := b.(BackendInitializer)
	if !ok {
		return nil
	}
	h.mu.Lock()
	need := !h.inited[b]
	if need {
		if h.inited == nil {
			h.inited = map[Backend]bool{}
		}
		h.inited[b] = true
	}
	h.mu.Unlock()
	if !need {
		return nil
	}
	return ini.InitBackend()
}

// shutdownBackends 对已 Init 的后端逐个 Shutdown（幂等，错误收集为日志级忽略）。
func (h *Host) shutdownBackends() {
	h.mu.Lock()
	inited := h.inited
	h.inited = nil
	h.mu.Unlock()
	for b := range inited {
		if ini, ok := b.(BackendInitializer); ok {
			ini.ShutdownBackend()
		}
	}
}

// WindowOf 返回某窗口的 Window 实例（未知 App 返回 nil）。装配层需要调用
// 后端专属扩展（如 rustbackend 的 SetDispatcher）时用它取回并自行断言类型。
func (h *Host) WindowOf(app *App) Window {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, w := range h.windows {
		if w.app == app {
			return w.win
		}
	}
	return nil
}

// Close 关闭全部窗口、停掉 UI Loop 并拒绝后续新窗口。幂等。
func (h *Host) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	wins := h.windows
	h.windows = nil
	h.mu.Unlock()
	var first error
	for _, w := range wins {
		if err := w.app.Close(); err != nil {
			h.reportFault(w, "close", nil)
			if first == nil {
				first = err
			}
		}
	}
	h.shutdownBackends()
	_ = h.loopq.Stop() // 丢弃未执行任务（Dropped 计数）；正在执行的任务自然返回
	return first
}

// reportFault 归因上报窗口故障（FaultHandler 未设置时忽略）。
func (h *Host) reportFault(w *hostWindow, phase string, p any) {
	if h.fault == nil {
		return
	}
	h.mu.Lock()
	backend := w.backend
	h.mu.Unlock()
	h.fault(Fault{WindowID: w.id, Backend: backend, Phase: phase, Panic: p, Recovered: true})
}

// ---- 接入层：gx.use(Backend) 的 Go 落点 ----

// Handle 是绑定某个后端的窗口组句柄：每次 Render 开一扇新窗，Run 统一主循环。
// 非并发安全：Render / Run / Close 应在装配 goroutine（通常 main）串行调用。
type Handle struct {
	h       *Host
	backend Backend // nil = 内置 Headless（无头测试）
}

// Use 是 GX `gx.use(Backend)` 的 Go 落点：注入后端接口实现，返回窗口组句柄。
//
// 后端实现形状（gxapp.Backend，见 backend.go）：
//
//	type RustBackend struct{}
//	func (RustBackend) Name() string { return "rust" }
//	func (RustBackend) NewWindow(cfg gxapp.WindowConfig) (gxapp.Window, error) {
//	    return NewWindow(), nil
//	}
//
// 传 nil 等价内置 headless（无头测试）。
func Use(backend Backend) *Handle {
	return &Handle{h: NewHost(), backend: backend}
}

// Host 返回底层宿主（Windows() 快照、Loop()、故障回调等高级用法）。
func (hd *Handle) Host() *Host { return hd.h }

// Render 打开一个新窗口并渲染根组件（GX: app.render(() => <App/>, cfg)）。
//
// 每次调用 = 一扇独立窗口 + 独立 App（独立场景树 / signal Runtime）。
// 若后端实现 WindowBinder（rustbackend），Render 完成后自动 BindApp 启用增量流。
func (hd *Handle) Render(build func() any, cfg WindowConfig) (*App, error) {
	cfg = cfg.normalize()
	entry, err := hd.h.openEntry(cfg, hd.backend, nil)
	if err != nil {
		return nil, err
	}
	if err := entry.app.Render(build); err != nil {
		_ = hd.h.CloseWindow(entry.app)
		return nil, fmt.Errorf("gxapp: 渲染窗口 %q 失败: %w", cfg.Title, err)
	}
	if binder, ok := entry.win.(WindowBinder); ok {
		binder.BindApp(entry.app)
	}
	return entry.app, nil
}

// WindowOf 返回某窗口的 Window 实例（代理到 Host.WindowOf）。
func (hd *Handle) WindowOf(app *App) Window { return hd.h.WindowOf(app) }

// Run 启动唯一主循环并阻塞，直到所有窗口关闭（代理到 Host.Run）。
func (hd *Handle) Run() error { return hd.h.Run() }

// Close 关闭全部窗口（代理到 Host.Close）。
func (hd *Handle) Close() error { return hd.h.Close() }
