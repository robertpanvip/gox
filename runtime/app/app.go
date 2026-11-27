// Package gxapp 是 GX 的应用级入口：先创建「窗口外壳」，再把一棵 JSX 场景树
// 渲染进去。类比 ReactDOM.createRoot(container).render(element) —— 这里对应
// gxapp.open(config) 得到 App，再 app.render(() => jsx)。
//
// 分层：
//   - gxscene 负责 JSX → opcode → 常驻场景树 + 细粒度更新（渲染核心）
//   - signal 负责响应式依赖追踪
//   - gxapp 负责「应用外壳」：窗口配置、窗口后端抽象、生命周期，以及把
//     signal 变化翻译成「更新场景树 + 重绘窗口」的调度（bindReactive 封装在内部）
//
// gxapp 不重新实现渲染或响应式：它包装 Instance 与 effect 原语，
// 让 signal 写入在更新场景树后顺带触发一次窗口重绘。Skia / wgpu / GLFW
// 等真实窗口后端只需实现 Window 接口，app 层与编译器均无需改动。
package gxapp

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/scheduler"
	"github.com/robertpanvip/gox/runtime/signal"
)

// WindowConfig 是窗口配置（open 的唯一参数）。零值字段由各 Window 后端给默认值。
type WindowConfig struct {
	Title      string
	Width      int
	Height     int
	X          int // 窗口左上角横坐标；0 表示由后端决定
	Y          int // 窗口左上角纵坐标；0 表示由后端决定
	Resizable  bool
	Fullscreen bool

	// ---- 窗口外观（无边框 / 透明 / 置顶 / 图标）----
	//
	// Decorated 控制系统边框（标题栏/关闭按钮）。nil 表示未设置，由后端决定默认
	// （通常为 true）；显式 false 得到无边框窗口。用指针是为了区分"未设置"与
	// "明确不要边框"——普通 bool 零值无法区分，会把所有不设置的调用都变成无边框。
	Decorated *bool
	// Transparent 请求透明framebuffer：配合 Decorated=false 与场景里的透明背景
	// 可做无背景悬浮面板。需要后端与合成器支持（GLFW: TRANSPARENT_FRAMEBUFFER，
	// winit: with_transparent）；不支持时后端静默忽略。
	Transparent bool
	// AlwaysOnTop 请求窗口置顶（GLFW Floating / winit always-on-top）。
	AlwaysOnTop bool
	// Opacity 是窗口整体不透明度 0..1；<=0 或 >1 表示不设置（用后端默认 1）。
	// 注意：这是窗口级 opacity（X11/Wayland 支持度不一），与 Transparent（framebuffer
	// alpha 通道）是两种机制，可分别使用。
	Opacity float32
	// IconPath 是窗口图标图片路径（png）。为空表示用后端默认图标。
	// 后端负责解码；解码失败静默忽略，不影响窗口创建。
	IconPath string

	// MousePassthrough 请求"透明区不拦截鼠标"：窗口仅在没有场景内容的区域
	// 让鼠标事件穿透到后面的窗口（点击后面应用），有内容的节点仍正常接收点击。
	// 典型用途：无背景悬浮面板 / 桌面挂件——配合 Decorated=false + Transparent=true。
	// 需要后端支持按命中矩形动态设置窗口输入形状（Linux/X11: XShape ShapeInput；
	// macOS/Windows: 对应区域命中机制）。不支持的后端静默忽略，行为退化为普通窗口。
	// 默认 false（窗口整体拦截鼠标），与既有行为一致。
	MousePassthrough bool

	// Backend 是后端提示（"headless" / "skia" / "wgpu" ...）。
	// Open 忽略它（固定 Headless）；OpenWith 由调用方按此字段挑选 Window 实现。
	Backend string
}

// NoDecorations 是构造 *bool(false) 的便捷函数，便于业务写 Decorated: app.NoDecorations() 得到无边框窗口。
func NoDecorations() *bool { v := false; return &v }

// WithDecorations 返回 true 指针，用于显式开启边框。
func WithDecorations() *bool { v := true; return &v }

// DefaultConfig 返回一份带常用默认值的配置（800×600、可调整、标题 "Gox"）。
func DefaultConfig() WindowConfig {
	return WindowConfig{
		Title:     "Gox",
		Width:     800,
		Height:    600,
		Resizable: true,
		Backend:   "headless",
	}
}

// normalize 把零值尺寸补成默认值，避免后端拿到 0×0 窗口。
func (c WindowConfig) normalize() WindowConfig {
	if c.Width <= 0 {
		c.Width = 800
	}
	if c.Height <= 0 {
		c.Height = 600
	}
	if c.Backend == "" {
		c.Backend = "headless"
	}
	return c
}

// Window 是窗口后端抽象。真实 GUI 后端（Skia / wgpu / GLFW）实现此接口即可接入。
type Window interface {
	// Open 按配置创建/打开窗口。
	Open(cfg WindowConfig) error
	// Draw 把当前场景树绘制到窗口一帧。
	Draw(root *gxscene.Node) error
	// Run 进入事件循环，直到窗口关闭。Headless 实现立即返回。
	Run() error
	// Close 关闭窗口并释放资源。
	Close() error
	// Config 返回实际生效的配置。
	Config() WindowConfig
}

// App 是 open 返回的应用句柄，持有窗口后端与配置；render 之后再持有场景树实例。
type App struct {
	win           Window
	cfg           WindowConfig
	inst          *gxscene.Instance    // render 前为 nil
	owner         *signal.Owner        // render 建树的根 Owner：组件内 effect/computed/cleanup 归属它
	runtime       *signal.Runtime      // 本 App 专属的 UI 任务运行时（多 App 隔离）
	globalLease   *signal.GlobalLease  // 对默认 Runtime 的兼容桥接租约（GX Signal.safe）
	dispatcher    FrameDispatcher      // 帧重绘派发器（默认 InlineDispatcher）
	frameClock    scheduler.FrameClock // 可选：平台帧时钟（BACKEND_ARCHITECTURE_V2 阶段 1）
	frameSched    *scheduler.Scheduler // 可选：统一帧调度器（启用后重绘走 clock 对齐 vsync）
	unFlush       func()               // 注销帧边界 hook（T1.2）
	drawPending   atomic.Bool          // 一批 effect 内是否发生需要重绘的变化（预留给 Dispatcher 精确化；并发写用原子）
	lastDrawErr   error                // 最近一次 Draw 的错误（T2.2）
	errHook       func(error)          // 可选：Draw 错误回调
	commit        *commitGate          // 串行化 Scene Mutation 与 Backend Draw（Host 评审 P0）
	stateMu       sync.Mutex           // 保护 errHook / lastDrawErr / frameVersion / needsFullSync 的并发读写
	closed        atomic.Bool          // 并发安全的关闭标记（Run/Close/Draw 契约）
	closeOnce     sync.Once            // 保证 Close 幂等且只释放一次资源
	closeErr      error                // 保存首次 Close 的结果，供并发/重复调用一致返回（评审 P2）
	frameVersion  uint64               // 单调递增的提交序号（Frame 契约）
	needsFullSync bool                 // 上次 Present 失败后置位：下帧要求后端全量重建（Frame 契约）
	rendered      bool

	// RAF（requestAnimationFrame）调度状态，见 capabilities.go：
	//   inRAF      drain 回调执行期间置位，抑制其中的重绘请求以避免同步递归；
	//   rafPending 已预约的下一帧尚未执行，去抖防止动画循环堆积帧。
	inRAF      atomic.Bool
	rafPending atomic.Bool

	// ref Element 句柄的异步布局读取：事件回调登记，帧后统一回填（见 capabilities.go）。
	pendingMu       sync.Mutex
	pendingMeasures []measureReq
}

// Open 按配置创建默认 Headless 窗口，返回应用句柄。不接收 JSX——JSX 交给 Render。
//
//	app, err := gxapp.Open(gxapp.WindowConfig{Title: "Demo", Width: 800, Height: 600})
//	if err != nil { panic(err) }
//	defer app.Close()
//	app.Render(view)   // view 是 JSX 编译出的 *gxscene.Instance
//	app.Run()
//
// 等价用法：OpenWith(cfg, &gxapp.HeadlessWindow{Out: os.Stdout})。
func Open(cfg WindowConfig) (*App, error) {
	return OpenWith(cfg, &HeadlessWindow{Out: os.Stdout})
}

// OpenWith 同 Open，但允许注入自定义窗口后端（Skia / wgpu / GLFW ...）。
func OpenWith(cfg WindowConfig, win Window) (*App, error) {
	if win == nil {
		return nil, fmt.Errorf("gxapp: 窗口后端不能为 nil")
	}
	cfg = cfg.normalize()
	if err := win.Open(cfg); err != nil {
		return nil, fmt.Errorf("gxapp: 打开窗口失败: %w", err)
	}
	// 回读后端实际生效的配置：窗口后端可能因平台限制调整尺寸/全屏等值（Host 评审
	// P1：App.Config() 与窗口实际配置可能不一致）。以窗口回报为准。
	if actual := win.Config(); actual.Width > 0 && actual.Height > 0 {
		cfg = actual.normalize()
	}
	app := &App{win: win, cfg: cfg, commit: newCommitGate(), runtime: signal.NewRuntime()}
	app.setDispatcher(&InlineDispatcher{})
	return app, nil
}

// SetDispatcher 替换帧重绘派发器（T2.1）。必须在 Render 之前调用——Render 接线时
// 会把派发器绑定到真正的 Draw sink。nil 恢复默认 InlineDispatcher。
//
// 真实 GUI 后端用 QueueDispatcher 把重绘派发到 UI 线程，避免后台 goroutine 写
// signal 时在非 UI 线程调用 Window.Draw。
func (a *App) SetDispatcher(d FrameDispatcher) {
	if a == nil {
		return
	}
	a.setDispatcher(d)
}

func (a *App) setDispatcher(d FrameDispatcher) {
	if d == nil {
		d = &InlineDispatcher{}
	}
	a.dispatcher = d
	// sink 在 render 前 inst 为 nil，drawNow 会安全 no-op；Render 后仍指向同一闭包。
	d.SetSink(a.drawNow)
}

// SetUIExecutor 用后端提供的 UIExecutor 替换派发器（Host 评审 P0：线程契约）。
// 后端实现 Post 保证 fn 在正确的 UI 线程/事件循环执行；若需要锁定 OS Thread，
// 由后端事件循环负责，Host 不猜测线程、也不比较 goroutine id。
// 等价于 SetDispatcher(NewUIExecutorDispatcher(exec))。exec 为 nil 时恢复默认
// InlineDispatcher。须在 Render 之前调用。
func (a *App) SetUIExecutor(exec UIExecutor) {
	if a == nil {
		return
	}
	if exec == nil {
		a.setDispatcher(nil)
		a.detachUITaskPoster()
		return
	}
	a.setDispatcher(NewUIExecutorDispatcher(exec))

	// UI 线程亲和守卫（仅 -tags gxthreadcheck 生效，默认构建为空操作）：
	// 记录当前 goroutine 为合法 UI 线程，之后任何后台野写信号都会在结构变更点
	// 打印带栈诊断，把偶发的 map 竞态提前变成可读报告。
	signal.BindUIThread()

	// 严格单线程响应式：把 UIExecutor 注入本 App 专属的 signal.Runtime，使后台
	// goroutine 调用的 app.Dispatch(fn) 能把更新闭包投递到本 App 的 UI 线程执行。
	// 投递器与关闭状态不再全局共享——多 App 互不覆盖、互不误伤（评审 P0）。
	a.bindUITaskPoster(exec.Post)
}

// bindUITaskPoster 把后端 UIExecutor.Post 适配为 signal.TaskPoster 并注入本
// App 的 Runtime，同时安装兼容桥接（供 GX Signal.safe 使用）。重复调用以最后
// 一次为准；Close 时释放租约并关闭本 App 的 Runtime。
func (a *App) bindUITaskPoster(post func(func()) error) {
	if post == nil {
		return
	}
	poster := signal.TaskPoster(func(fn func()) bool {
		return post(fn) == nil
	})
	a.runtime.SetPoster(poster)
	if a.globalLease != nil {
		a.globalLease.Release()
	}
	a.globalLease = signal.InstallGlobalPost(poster)
}

// detachUITaskPoster 撤销本 App 的投递器注入（SetUIExecutor(nil) 时）。
// 此后 Dispatch 返回 ErrUIRuntimeNotInitialized，不再在任何线程静默执行。
func (a *App) detachUITaskPoster() {
	a.runtime.SetPoster(nil)
	if a.globalLease != nil {
		a.globalLease.Release()
		a.globalLease = nil
	}
}

// Dispatch 把更新闭包投递到本 App 的 UI Runtime 执行——后台 goroutine 修改
// 响应式状态的推荐入口。异步投递，调用后立即返回，不等待 fn 执行。
//
// 返回非 nil 错误（未初始化 / 已关闭 / 被拒绝）表示 fn **未被执行**，调用方
// 据此处理结果。GX 里可用 `app.dispatch(() => {...})`。
func (a *App) Dispatch(fn func()) error {
	if a == nil {
		return signal.ErrUIRuntimeNotInitialized
	}
	return a.runtime.Dispatch(fn)
}

// Runtime 返回本 App 专属的 UI 任务运行时（诊断/高级用法）。
func (a *App) Runtime() *signal.Runtime { return a.runtime }

// drawNow 是派发器最终执行的绘制动作：真正调用窗口 Draw 并记录错误（T2.2）。
// 它是 drawNowErr 的无返回值适配（作为 FrameDispatcher 的 sink）。
func (a *App) drawNow() { _ = a.drawNowErr() }

// drawNowErr 执行一帧绘制并返回本次真实错误。
//
// 绘制在 commit gate 内执行——与所有 Scene Mutation 互斥（Host 评审 P0）：后端
// 遍历 Node 树期间不会有 effect 并发写入。gate 基于 goid 可重入，且用 defer
// 释放，后端 Present/Draw 抛 panic 也不会把提交门永久锁死（P1）。
//
// Frame 契约（Host 评审 P0）：FramePresenter 后端用 peek 取增量，只有 Present
// 成功才 TakeChanges 确认消费；失败则保留缓冲并在下一帧置 FullSync，让后端
// 用 Root 全量重建，杜绝「Changes 已清空但后端没收到」的永久丢帧。
// gate 内二次检查 closed/inst，并用局部 inst 引用，避免与 Close 并发时空解引用。
func (a *App) drawNowErr() error {
	if a.closed.Load() {
		return nil
	}
	a.commit.lock()
	defer a.commit.unlock()
	if a.closed.Load() || a.inst == nil {
		return nil
	}
	inst := a.inst
	presenter, useFrame := a.win.(FramePresenter)
	root := inst.Root()

	// RAF：帧首先行所有已登记的「下一帧」回调（el.requestAnimationFrame）。
	// 放在 frame 快照之前——回调里命令式写回的 Change 能进本帧增量，本帧即可见。
	a.drainRAF(inst)

	a.stateMu.Lock()
	a.frameVersion++
	frame := &Frame{Root: root, Version: a.frameVersion, FullSync: a.needsFullSync}
	if useFrame {
		frame.Changes = inst.Changes()
	}
	a.stateMu.Unlock()

	var err error
	if useFrame {
		err = presenter.Present(frame)
	} else {
		err = a.win.Draw(root)
	}

	closedAfter := a.closed.Load()
	a.stateMu.Lock()
	if err != nil {
		a.lastDrawErr = err
		if useFrame && !closedAfter {
			a.needsFullSync = true
		}
	} else {
		a.lastDrawErr = nil
		if useFrame && !closedAfter {
			inst.TakeChanges()
			a.needsFullSync = false
		}
	}
	hook := a.errHook
	a.stateMu.Unlock()
	if err != nil && hook != nil {
		hook(err)
	}
	// 帧后回填异步布局读取（ref 的 el.Measure / App.Measure）：本帧布局已就绪，
	// 用最新结果回调。回调在 commit gate 内执行（gate 可重入），写 signal 安全；
	// 真正重绘仍由帧边界统一派发。
	if err == nil {
		a.flushMeasures()
	}
	// RAF 续帧：若上面的回调登记了下一帧，异步预约一帧形成动画循环。
	// 放在 commit gate 释放后？不能——defer 在函数末尾才释放；schedule 内部
	// 用 goroutine 派发，会等本帧 unlock 后才真正 Draw，故此处安全。
	a.scheduleRAFNext(inst)
	return err
}

// FrameVersion 返回最近一次提交的帧序号（Frame 契约的可观测指标）。
func (a *App) FrameVersion() uint64 {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.frameVersion
}

// DebugDumpStructural dump 当前场景树全部结构插槽的接线状态（E2E 诊断用）。
func (a *App) DebugDumpStructural() string {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.inst == nil {
		return "inst=nil"
	}
	return a.inst.DebugDumpStructural(0)
}

// DebugRootSinkID 返回根实例变更 sink 的 id（E2E 诊断：与孤立 sink id 对比）。
func (a *App) DebugRootSinkID() int64 {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	if a.inst == nil {
		return -1
	}
	return a.inst.DebugRootSinkID()
}

// SetErrorHandler 设置 Draw 错误回调（T2.2）。err 为 nil 时不调用。
func (a *App) SetErrorHandler(fn func(error)) {
	a.stateMu.Lock()
	a.errHook = fn
	a.stateMu.Unlock()
}

// LastDrawError 返回最近一次 Draw 的错误（未出错为 nil）。
func (a *App) LastDrawError() error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	return a.lastDrawErr
}

// Render 在一棵根 Owner 下运行 build 构建并挂载 JSX 场景树：画首帧，并在内部
// 完成响应式接线（signal 变化 → 精确更新 + 自动重绘，含条件分支 / keyed 列表的结构重绘）。
//
// build 是「建树函数」而非已建好的树——这是 SolidJS 风格的关键：组件内的
// Signal.effect / Signal.computed / Signal.onCleanup / Signal.onMount 在建树时创建，
// 归属本次 Render 建立的根 Owner；应用关闭或重复 Render 时随 Owner 级联销毁，
// 不会残留永不停止的 Effect。调用方无需再手动接线 bindReactive。
//
// GX 用法（零参箭头即 func() any，故直接写）：
//
//	app.render(() => <div id="app">{count}</div>)
//
// 重复调用会先销毁上一棵树的 Owner（含组件副作用）与订阅，再渲染新树。
func (a *App) Render(build func() any) (err error) {
	if a.closed.Load() {
		return fmt.Errorf("gxapp: 应用已关闭，无法 Render")
	}
	if build == nil {
		return fmt.Errorf("gxapp: Render 的建树函数不能为 nil")
	}

	// 整个生命周期转换（拆旧树 → 建树 → 首帧 → 接线）在 commit gate 内，与
	// Close / Update / 后台 draw 互斥（Host 评审 P0：Close 必须与 Render 互斥）。
	// build 是建树期用户代码，本质就是 Scene Mutation，允许在锁内执行；gate
	// 基于 goid 可重入，内部 drawNow/teardownTree/effect 首执不会自锁。
	a.commit.lock()
	defer a.commit.unlock()
	// gate 内二次检查：等待提交门期间可能已被 Close 关闭。
	if a.closed.Load() {
		return fmt.Errorf("gxapp: 应用已关闭，无法 Render")
	}

	reRender := a.rendered
	if reRender {
		// 重渲染：先销毁旧树的 Owner（级联组件 effect/cleanup）与订阅，避免泄漏到新帧。
		a.teardownTree()
		// 新树拥有全新的 Node ID 空间；后端旧镜像不再可用，下帧强制全量重建。
		a.stateMu.Lock()
		a.needsFullSync = true
		a.stateMu.Unlock()
	}

	// 建树失败（build 未返回实例等）时统一清理本次创建的 Owner：
	// CreateRoot 内组件可能已登记 effect/computed/cleanup，若直接返回 error 而
	// 不 Dispose，这些响应式资源会永久泄漏。defer 覆盖所有失败出口（Host 评审 P1）。
	ok := false
	defer func() {
		if !ok {
			a.teardownTree()
			a.rendered = false
		}
	}()

	// 在根 Owner 下建树：组件函数内创建的 effect/computed/cleanup 自动归属它。
	var inst *gxscene.Instance
	a.owner = signal.CreateRoot(func(o *signal.Owner) {
		v := build()
		if i, ok := v.(*gxscene.Instance); ok {
			inst = i
		}
		if inst != nil {
			a.inst = inst
			// ref Element 句柄的宿主能力（Focuser/Measurer）注入共享 sink。
			// 必须在首帧 Draw 前接好回填钩子：首帧布局就绪后如有 pending 读取可回填。
			a.installElementCaps(inst)
			a.drawNow() // 首帧：同步绘制，此时组件的建树期副作用已登记
			// 响应式接线也放在同一 Owner 内：各 effect 随 Owner 一起销毁。
			a.bindReactive()
			// FramePresenter 后端自动开启增量记录（Host 评审 P0）：后端实现
			// Present 即自动生效，无需再手动调用 EnableChanges。放在接线之后，
			// 使首帧（用 Root 建基线）与接线期首次 SetPart 不混入增量流。
			// 老 Draw 路径不开启，行为与零开销承诺不变。
			if _, useFrame := a.win.(FramePresenter); useFrame {
				inst.EnableChanges()
			}
		}
	})
	if inst == nil {
		return fmt.Errorf("gxapp: 建树函数未返回场景树实例（*gxscene.Instance）")
	}
	a.rendered = true
	ok = true
	return nil
}

// teardownTree 销毁当前场景树及其 Owner（含组件副作用）与订阅。
//
// 整个销毁过程在 commit gate 内执行（Host 评审 P0）：Owner.Dispose 会同步执行
// onCleanup（用户代码，可能写 signal 触发嵌套 effect），且 inst.Dispose 释放结构
// 插槽；这些操作不能与正在进行的 Draw/Scene Mutation 并发。gate 可重入，故从
// 已持 gate 的 Render/Close 调用不会自锁。
//
// 销毁期异常隔离（评审 P1-02）：owner.Dispose（用户 cleanup，可能 panic）与
// inst.Dispose（结构插槽释放）分阶段单点捕获——owner 清理异常不得阻断场景树
// 插槽释放。全部释放完毕后统一上报（单异常保真 re-panic），调用方决定记录或
// 继续传播。
func (a *App) teardownTree() {
	a.commit.lock()
	defer a.commit.unlock()
	if a.unFlush != nil {
		a.unFlush() // 注销帧边界 hook，避免旧闭包在重渲染后继续触发重绘
		a.unFlush = nil
	}
	a.drawPending.Store(false)
	var caught []any
	if a.owner != nil {
		caught = append(caught, runFuncSafely(func() {
			a.owner.Dispose() // 级联销毁组件内 effect/computed、Bind 建立的订阅，执行 onCleanup
		})...)
		a.owner = nil
	}
	if a.inst != nil {
		caught = append(caught, runFuncSafely(func() {
			a.inst.Dispose() // 结构插槽 controller / 元素 Owner
		})...)
		a.inst = nil
	}
	repanicDisposeErrors(caught)
}

// bindReactive 把 signal 接到整棵场景树，负责精确更新与窗口重绘（T1.1/T1.2）：
//
//  1. 叶子插槽：BindLeafEffects 为每个叶子 getter 建独立 effect——signal 变化只
//     精确重求值依赖它的那个 getter 并 SetPart，不遍历全树、不重建子树；
//  2. 结构插槽：BindStructural 递归整棵树接条件分支 / keyed 列表，selector/each
//     变化时 Resolve 重建/复用子树；
//  3. 帧合并：两路 effect 都通过 a.reactiveEffect 包装——每次重跑只置 drawPending
//     标志；signal 的 AfterEffects 帧边界在一批 effect 全部跑完后统一 Draw 一次，
//     保证「一帧一致性」且 N 个 signal 变化也只画一帧。
//
// 接线期各 effect 首次执行不经过帧边界（signal.Effect 首次直接调度），不会触发
// 重绘；Render 已画首帧。批处理（signal.Batch）与嵌套写都由帧边界的 depth 去重。
//
// 包装 effect 经 bindCtx.effect 一路传给 For/Branch 的动态子树，使列表项 / 分支
// 内的叶子变化同样触发重绘——这是插槽级模型下替代「聚合 Update 递归全树」的关键。
// 所有 effect 挂在 a.owner 下，teardown 时随 Owner 级联销毁。
func (a *App) bindReactive() {
	if a.inst == nil || a.closed.Load() {
		return
	}
	// 帧边界 hook：一批 effect 跑完后统一画一帧。
	//
	// 为何不依赖 drawPending 标志：首帧期（applyAll）就建立的 Branch/For 子树用的是
	// 裸 signal.Effect 接线（那时 ctx 尚未注入），其叶子变化不经过包装 effect，无法
	// 置标志。改为「帧边界触发即重绘」——帧边界只在 signal 写入真正调度了 effect 时
	// 触发（写相同值被 DeepEqual 短路、无关 signal 无订阅者都不触发），因此不会多画
	// 无效帧，同时覆盖所有层级的叶子（含动态子树首帧项）。
	a.unFlush = signal.AfterEffects(func() {
		if a.closed.Load() {
			return
		}
		a.drawPending.Store(false)
		// 交给统一派发口决定在哪执行 Draw：未启用 FrameClock 时由 FrameDispatcher
		// 派发（Inline 同步 / UIExecutor 投 UI 线程）；启用后走 Scheduler 合并到
		// 下一平台帧时机（vsync 对齐，同周期多次更新只画一帧）。
		a.requestDrawFrame()
	})

	// wrap 是所有叶子/结构 effect 的统一入口。effect body 里的 SetPart/graft 是
	// Scene Mutation，必须在 commit gate 内执行——与 drawNow 里的 win.Draw 互斥，
	// 消除「后台改树 + UI 遍历」的数据竞争（Host 评审 P0）。gate 基于 goid 可重入，
	// 故 effect body 内同步写 signal 触发的嵌套 effect 不会自锁。
	wrap := func(body func()) signal.Computation {
		return signal.Effect(func() {
			func() {
				a.commit.lock()
				defer a.commit.unlock() // body() 里 getter/结构更新/用户代码 panic 也要释放提交门
				body()
			}()
			a.requestDraw() // 出锁后再标记重绘，避免在锁内触发帧边界回调
		})
	}
	// 两路接线共用同一包装 effect：结构通过 bindCtx.effect 传给 For/Branch 子树。
	a.inst.BindStructural(wrap, a.requestDraw)
	a.inst.BindLeafEffects(wrap)
	// 接线期 effect 首次执行会置位；清空以免接线后无变化时多画一帧。
	a.drawPending.Store(false)
}

// requestDraw 标记本轮需要重绘。由响应式 effect 与结构 onDirty 调用；真正绘制
// 延迟到 signal 的 AfterEffects 帧边界执行，从而把同批多次变化合并为一帧。
func (a *App) requestDraw() {
	if !a.closed.Load() {
		a.drawPending.Store(true)
	}
}

// Draw 请求把当前场景树绘制一帧到窗口。所有绘制入口统一经过派发器（Host 评审 P1）：
// InlineDispatcher 下同步完成（headless/测试），UIExecutorDispatcher 下投递到 UI 线程。
// Render 之前 no-op。
//
// 返回值语义（Host 评审 P1：Draw 错误 returns 不准确）：本方法只是「请求一帧」，
// 对异步派发器而言绘制尚未发生，因此返回的是「最近一次绘制的错误」，不保证属于
// 本次请求。需要本次同步绘制的确定错误请用 DrawNow。错误也可用 SetErrorHandler
// 与 LastDrawError 观察。
func (a *App) Draw() error {
	if a.closed.Load() {
		return nil
	}
	a.requestDrawFrame()
	return a.LastDrawError()
}

// DrawNow 同步绘制一帧并返回本次绘制的真实错误（Host 评审 P1）。
//
// 与 Draw 的区别：Draw 经派发器（异步后端可能稍后才绘制），返回最近一次错误；
// DrawNow 绕过派发器、立即在当前 goroutine 的 commit gate 内执行一帧，返回的 err
// 就是本次 Present/Draw 的结果。适用于需要同步确认绘制结果的场景（测试、快照导出、
// 错误重试）。注意：真实 GUI 后端应优先用 Draw（经 UIExecutor 投递到 UI 线程），
// 随意在非 UI 线程调 DrawNow 可能违反后端线程要求。
func (a *App) DrawNow() error {
	if a.closed.Load() {
		return nil
	}
	return a.drawNowErr()
}

// Update 触发场景树细粒度更新（转发 Instance.Update），成功后请求重绘一帧。
// 返回本次实际写入的节点数（与 Instance.Update 一致）。
//
// inst.Update() 会重算 getter 并写回节点，属于 Scene Mutation，因此在 commit gate
// 内执行——与窗口绘制互斥（Host 评审 P0）。重绘统一交给派发器，不再直接调 drawNow。
func (a *App) Update() int {
	if a.closed.Load() {
		return 0
	}
	a.commit.lock()
	// gate 内二次检查：等待提交门期间可能已被 Close 销毁树（Host 评审 P0）。
	if a.closed.Load() || a.inst == nil {
		a.commit.unlock()
		return 0
	}
	n := a.inst.Update()
	a.commit.unlock()
	a.requestDrawFrame()
	return n
}

// Run 进入窗口事件循环，直到窗口关闭。Headless 后端立即返回 nil。
func (a *App) Run() error {
	if a.closed.Load() {
		return nil
	}
	return a.win.Run()
}

// Close 关闭窗口。可重复调用（幂等），且并发调用安全（Host 评审 P1）：
// closeOnce 保证响应式树销毁、派发器关闭、窗口关闭只执行一次；closed 用原子标记，
// 使并发的 Run/Draw/Update 能立即观察到关闭状态并 no-op。
//
// 评审 P2：底层 win.Close() 的错误不再被吞掉，而是保存到 closeErr 并返回。
// sync.Once 保证首次执行的写入对后续所有 Do 调用可见（Go 内存模型），因此并发/
// 重复的 Close 调用会得到一致的结果，而不是时而 error 时而 nil。
//
// 销毁期异常隔离（评审 P1-02）：closeOnce 与 panic 的组合曾是最危险的生命周期
// 风险——teardownTree 里某个用户 cleanup panic 会让 Do 的首次执行中断，且 Once
// 已标记完成，dispatcher / 窗口永远不会再被关闭，closeErr 也不会记录。现在四个
// 阶段（Runtime/租约、响应式树、派发器、窗口）各自单点捕获收集，窗口关闭是最终
// 兜底动作，任何前置阶段 panic 都不阻断它；全部资源释放完毕后统一上报：closeErr
// 记录 win.Close 的原生错误，收集到的 panic 按单异常保真 / 多异常汇总 re-panic。
func (a *App) Close() error {
	a.closeOnce.Do(func() {
		a.closed.Store(true)
		var caught []any
		// 阶段 1：关闭本 App 的 UI 任务投递。此后后台 goroutine 的 app.Dispatch
		// 更新一律被拒绝，不会在已销毁的响应式树 / 窗口上执行闭包。仅关闭本 App
		// 的 Runtime，其它 App 的投递不受影响（评审 P0：多 App 隔离）。兼容桥接
		// 租约同时释放，只有最后一个 App 关闭才关闭默认 Runtime。
		caught = append(caught, runFuncSafely(func() {
			a.runtime.Close()
			if a.globalLease != nil {
				a.globalLease.Release()
				a.globalLease = nil
			}
		})...)
		// 阶段 2：先销毁整棵响应式树：停订阅、执行组件 onCleanup，避免关闭过程中
		// signal 写入触发已关闭窗口的重绘。内部已隔离 owner/inst 两段，这里整体
		// 再捕获一层，保证 panic 不跳过阶段 3/4。
		caught = append(caught, runFuncSafely(a.teardownTree)...)
		// 阶段 3：关闭派发器与帧调度器。
		caught = append(caught, runFuncSafely(func() {
			if a.dispatcher != nil {
				a.dispatcher.Close()
			}
			a.closeFrameClock()
		})...)
		// 阶段 4：窗口关闭兜底——必须最后执行，任何前置 panic 都不阻断。
		caught = append(caught, runFuncSafely(func() {
			a.closeErr = a.win.Close()
		})...)
		// 全部资源已释放，统一上报收集到的 panic（win.Close 的原生错误走 closeErr）。
		repanicDisposeErrors(caught)
	})
	return a.closeErr
}

// Instance 返回底层场景树实例（Render 之前为 nil；供 SetPart / SetGetter 等高级用法）。
func (a *App) Instance() *gxscene.Instance { return a.inst }

// EnableChanges 打开增量变更记录。Render 之后调用；之后每次更新产生的变更可被
// Changes/TakeChanges 读取。不调用时完全退回全量 Draw 行为（零开销）。
func (a *App) EnableChanges() {
	if a != nil && a.inst != nil {
		a.inst.EnableChanges()
	}
}

// SetChildDiffer 注入自定义子节点 diff 策略，覆盖 host 内建算法（nil 恢复内建）。
// 后端若已有更高效的 diff，可用它直接产出协议 Change，避免 host 再跑一遍。
func (a *App) SetChildDiffer(d gxscene.ChildDiffer) {
	if a != nil && a.inst != nil {
		a.inst.SetChildDiffer(d)
	}
}

// Changes 返回自上次 TakeChanges 以来的增量变更（副本，不清空）。
// 每条变更用 Node.ID 定位，后端可选消费做增量重绘，也可忽略继续全量 Draw。
func (a *App) Changes() []gxscene.Change {
	if a == nil || a.inst == nil {
		return nil
	}
	return a.inst.Changes()
}

// TakeChanges 取走并清空当前所有增量变更。后端每帧调用一次即可拿到增量补丁。
func (a *App) TakeChanges() []gxscene.Change {
	if a == nil || a.inst == nil {
		return nil
	}
	return a.inst.TakeChanges()
}

// Window 返回窗口后端（供特定后端的额外 API 使用）。
func (a *App) Window() Window { return a.win }

// Config 返回实际生效的窗口配置。
func (a *App) Config() WindowConfig { return a.cfg }

// ---------- Headless 窗口后端 ----------

// HeadlessWindow 是无 GUI 的窗口后端：把场景树用 TextBackend 绘制到 io.Writer。
// 用于测试、CI、以及 Skia / wgpu 后端接入前的占位实现。
type HeadlessWindow struct {
	Out    io.Writer // nil 表示丢弃输出（os.Stdout 由 Open 显式传入）
	cfg    WindowConfig
	opened bool
	frames int // 已绘制帧数（可观测指标）
}

// Open 实现 Window。
func (h *HeadlessWindow) Open(cfg WindowConfig) error {
	h.cfg = cfg
	h.opened = true
	return nil
}

// Draw 实现 Window：用 TextBackend 把场景树绘制成文本写入 Out。
func (h *HeadlessWindow) Draw(root *gxscene.Node) error {
	if !h.opened {
		return fmt.Errorf("gxapp: 窗口未打开")
	}
	h.frames++
	if h.Out == nil {
		return nil
	}
	_, err := io.WriteString(h.Out, (gxscene.TextBackend{}).Paint(root))
	return err
}

// Run 实现 Window：Headless 无事件循环，立即返回。
// HostedWindow 路径下不再被调用（窗口由 Host 主循环轮询驱动）。
func (h *HeadlessWindow) Run() error { return nil }

// Poll 实现 HostedWindow：无事件源，no-op。
func (h *HeadlessWindow) Poll() error { return nil }

// ShouldClose 实现 HostedWindow：Headless 从不主动请求关闭（用 app.Close / Host.Close 终止）。
func (h *HeadlessWindow) ShouldClose() bool { return false }

// 编译期断言：内置 Headless 窗口参与 Host 轮询主循环。
var _ HostedWindow = (*HeadlessWindow)(nil)

// Close 实现 Window。
func (h *HeadlessWindow) Close() error {
	h.opened = false
	return nil
}

// Config 实现 Window。
func (h *HeadlessWindow) Config() WindowConfig { return h.cfg }

// Frames 返回已绘制帧数（测试/观测用）。
func (h *HeadlessWindow) Frames() int { return h.frames }

// RenderText 便捷函数：用 Headless 后端把场景树绘制成字符串（不打印）。
// 便于测试与不需要窗口生命周期的场景。
func RenderText(inst *gxscene.Instance, cfg ...WindowConfig) (string, error) {
	c := DefaultConfig()
	if len(cfg) > 0 {
		c = cfg[0].normalize()
	}
	var b strings.Builder
	win := &HeadlessWindow{Out: &b}
	if err := win.Open(c); err != nil {
		return "", err
	}
	if err := win.Draw(inst.Root()); err != nil {
		return "", err
	}
	return b.String(), nil
}
