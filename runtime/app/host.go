package gxapp

import (
	"fmt"
	"sync"
)

// Host / Handle：进程级应用宿主与接入层（P4 多窗口首版实现）。
//
// 对应 GX 接入形态（GX 编译产物落到 Use / Handle.Render / Handle.Run 三行）：
//
//	app := gxapp.Use("rust")                                  // gx.use(Backend)
//	app.Render(func() any { return ui.App() }, gxapp.WindowConfig{ // app.render(() => <App/>, {...})
//		Title: "Main Window", Width: 1200, Height: 800,
//	})
//	app.Render(func() any { return ui.Settings() }, gxapp.WindowConfig{
//		Title: "Settings", Width: 600, Height: 400,
//	})
//	app.Run() // 统一主循环：全部窗口退出才返回
//
// 模型（ui-loop-p4-multiwindows.md §3 的首版落地）：
//   - Host 持有窗口集合，每窗一个独立 App（独立场景树 / signal Runtime / 生命周期），
//     关闭任意窗口不影响其它窗口。
//   - 首版各窗口仍由后端自驱事件循环（Host.Run 并行等待全部退出）；"单一 UI Loop
//     + Host 轮询窗口"的目标态见 P4 文档 §4，需要后端改造为一循环多窗口。
//   - 故障隔离域 = 窗口：任一窗口的 Run panic 被 recover 归因到该窗口并上报
//     FaultHandler，不影响其它窗口。
//
// 主线程亲和限制：GLFW 类主线程亲和后端（skia glwindow）的多窗口需 P4 目标态改造；
// 首版多窗口适用于"事件循环不要求主线程"的后端（rustbackend / headless / 测试桩）。

// Fault 是一次窗口级故障的归因记录（Run panic / 关闭失败）。
type Fault struct {
	WindowID  int    // Host 内的窗口序号
	Backend   string // 窗口所属后端名（Use 注入或 OpenBackendWindow 的 cfg.Backend）
	Phase     string // "run" / "close"
	Panic     any    // recover 捕获的 panic 值（nil 表示非 panic 故障）
	Recovered bool   // 恒为 true：Host 已隔离，未向上传播
}

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

// Host 是进程级应用宿主：持有窗口集合，提供统一 Run / Close 与故障隔离。
type Host struct {
	mu      sync.Mutex
	seq     int
	windows []*hostWindow
	fault   func(Fault)
	closed  bool
}

// HostOption 配置 Host。
type HostOption func(*Host)

// WithFaultHandler 注册窗口故障回调（Run panic / 关闭失败归因）。
func WithFaultHandler(fn func(Fault)) HostOption {
	return func(h *Host) { h.fault = fn }
}

// NewHost 创建进程级宿主。
func NewHost(opts ...HostOption) *Host {
	h := &Host{}
	for _, o := range opts {
		o(h)
	}
	return h
}

// OpenWindow 在 Host 下打开一个窗口并创建其独立 App。
//
// 装配顺序（评审 P0）：Open → OpenWith（建 App）→ 若后端实现 UIExecutor 则注入
// （把 Dispatch/RAF 派发到该窗口的事件泵，避免后台 goroutine 跨线程绘制）。
// Render 由调用方完成（Handle.Render 或手动 app.Render），随后 Host 经
// WindowBinder 通知后端启用增量。
func (h *Host) OpenWindow(cfg WindowConfig, win Window) (*App, error) {
	cfg = cfg.normalize()
	entry, err := h.openEntry(cfg, "", win, nil)
	if err != nil {
		return nil, err
	}
	return entry.app, nil
}

// OpenBackendWindow 按 cfg.Backend 从注册表选后端并打开窗口。语义同 OpenBackend，
// 但窗口纳入 Host 管理（可多开）。
func (h *Host) OpenBackendWindow(cfg WindowConfig) (*App, error) {
	cfg = cfg.normalize()
	entry, err := h.openEntry(cfg, cfg.Backend, nil, nil)
	if err != nil {
		return nil, err
	}
	return entry.app, nil
}

// openEntry 公共装配路径：解析后端 → Open → 建 App → 注入 UIExecutor → 登记。
// factory 非空时优先生效（UseFactory / 内部使用）；backend 非空时走注册表；
// 两者都空则用内置 Headless。
func (h *Host) openEntry(cfg WindowConfig, backend string, win Window, factory BackendFactory) (*hostWindow, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, fmt.Errorf("gxapp: Host 已关闭，拒绝新窗口")
	}
	h.seq++
	id := h.seq
	h.mu.Unlock()

	if win == nil {
		if factory != nil {
			w, ferr := factory(cfg)
			if ferr != nil {
				return nil, fmt.Errorf("gxapp: 创建 Backend %q 失败: %w", backend, ferr)
			}
			if w == nil {
				return nil, fmt.Errorf("gxapp: Backend %q 的工厂返回 nil", backend)
			}
			win = w
		} else if backend != "" && backend != "headless" {
			f, ok := lookupBackend(backend)
			if !ok {
				return nil, fmt.Errorf("gxapp: 未注册的 Backend %q（已注册: %v）", backend, Backends())
			}
			w, ferr := f(cfg)
			if ferr != nil {
				return nil, fmt.Errorf("gxapp: 创建 Backend %q 失败: %w", backend, ferr)
			}
			if w == nil {
				return nil, fmt.Errorf("gxapp: Backend %q 的工厂返回 nil", backend)
			}
			win = w
		} else {
			win = &HeadlessWindow{}
		}
	}

	app, err := OpenWith(cfg, win)
	if err != nil {
		return nil, err
	}
	// 后端自驱 UI 线程（rustbackend.Run 的 select 泵 / glWindow 的 GLFW 主循环）：
	// Dispatch/RAF 派发进窗口事件泵，与绘制同线程串行。
	if exec, ok := win.(UIExecutor); ok {
		app.SetUIExecutor(exec)
	}

	entry := &hostWindow{id: id, backend: backend, app: app, win: win}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = app.Close()
		return nil, fmt.Errorf("gxapp: Host 已关闭，窗口已回收")
	}
	h.windows = append(h.windows, entry)
	h.mu.Unlock()
	return entry, nil
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

// Run 启动全部窗口并阻塞，直到所有窗口退出。
//
// 每个窗口在自己的 goroutine 里 app.Run()（后端事件循环自驱）；任一窗口 Run
// panic 被 recover 归因上报 FaultHandler，不传播、不影响其它窗口。返回第一个
// 非 panic 的 Run 错误（如 rustbackend 的协议错误）。
func (h *Host) Run() error {
	h.mu.Lock()
	wins := append([]*hostWindow(nil), h.windows...)
	h.mu.Unlock()
	if len(wins) == 0 {
		return nil
	}
	done := make(chan error, len(wins))
	for _, w := range wins {
		w := w
		go func() {
			err := func() (err error) {
				defer func() {
					if r := recover(); r != nil {
						h.reportFault(w, "run", r)
						err = fmt.Errorf("gxapp: 窗口 %q Run panic（已隔离）: %v", w.win.Config().Title, r)
					}
				}()
				return w.app.Run()
			}()
			done <- err
		}()
	}
	var first error
	for range wins {
		if err := <-done; err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Close 关闭全部窗口并拒绝后续新窗口。幂等。
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

// Handle 是绑定某个后端的窗口组句柄：每次 Render 开一扇新窗，Run 统一等待。
// 非并发安全：Render / Run / Close 应在装配 goroutine（通常 main）串行调用。
type Handle struct {
	h       *Host
	backend string // 注册表后端名；空串 = 使用注入工厂或 headless
	factory BackendFactory
}

// Use 是 GX `gx.use(Backend)` 的 Go 落点：按注册表后端名返回窗口组句柄。
// 后端必须已 Register（或用 "headless" / UseFactory 注入实现）。
func Use(backend string) *Handle {
	return &Handle{h: NewHost(), backend: backend}
}

// UseFactory 用注入工厂构造句柄（未注册后端 / 测试桩 / 第三方 Window 实现）。
func UseFactory(factory BackendFactory) *Handle {
	return &Handle{h: NewHost(), factory: factory}
}

// Host 返回底层宿主（Windows() 快照、故障回调等高级用法）。
func (hd *Handle) Host() *Host { return hd.h }

// Render 打开一个新窗口并渲染根组件（GX: app.render(() => <App/>, cfg)）。
//
// 每次调用 = 一扇独立窗口 + 独立 App（独立场景树 / signal Runtime）。
// 若后端实现 WindowBinder（rustbackend），Render 完成后自动 BindApp 启用增量流。
func (hd *Handle) Render(build func() any, cfg WindowConfig) (*App, error) {
	cfg = cfg.normalize()
	cfg.Backend = hd.backend
	entry, err := hd.h.openEntry(cfg, hd.backend, nil, hd.factory)
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

// Run 启动全部窗口并阻塞，直到所有窗口退出（代理到 Host.Run）。
func (hd *Handle) Run() error { return hd.h.Run() }

// Close 关闭全部窗口（代理到 Host.Close）。
func (hd *Handle) Close() error { return hd.h.Close() }
