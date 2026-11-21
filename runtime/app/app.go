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

	gxscene "github.com/robertpanvip/gox/runtime/scene"
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
	// Backend 是后端提示（"headless" / "skia" / "wgpu" ...）。
	// Open 忽略它（固定 Headless）；OpenWith 由调用方按此字段挑选 Window 实现。
	Backend string
}

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
	win      Window
	cfg      WindowConfig
	inst     *gxscene.Instance // render 前为 nil
	owner    *signal.Owner     // render 建树的根 Owner：组件内 effect/computed/cleanup 归属它
	closed   bool
	rendered bool
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
	return &App{win: win, cfg: cfg}, nil
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
func (a *App) Render(build func() any) error {
	if a.closed {
		return fmt.Errorf("gxapp: 应用已关闭，无法 Render")
	}
	if build == nil {
		return fmt.Errorf("gxapp: Render 的建树函数不能为 nil")
	}
	if a.rendered {
		// 重渲染：先销毁旧树的 Owner（级联组件 effect/cleanup）与订阅，避免泄漏到新帧。
		a.teardownTree()
	}

	// 在根 Owner 下建树：组件函数内创建的 effect/computed/cleanup 自动归属它。
	var inst *gxscene.Instance
	a.owner = signal.CreateRoot(func(o *signal.Owner) {
		v := build()
		if i, ok := v.(*gxscene.Instance); ok {
			inst = i
		}
		if inst != nil {
			a.inst = inst
			_ = a.Draw() // 首帧：此时组件的建树期副作用已登记
			// 响应式接线也放在同一 Owner 内：聚合 effect 与结构 effect 随 Owner 一起销毁。
			a.bindReactive()
		}
	})
	a.rendered = true
	if inst == nil {
		return fmt.Errorf("gxapp: 建树函数未返回场景树实例（*gxscene.Instance）")
	}
	return nil
}

// teardownTree 销毁当前场景树及其 Owner（含组件副作用）与订阅。
func (a *App) teardownTree() {
	if a.owner != nil {
		a.owner.Dispose() // 级联销毁组件内 effect/computed、Bind 建立的订阅，执行 onCleanup
		a.owner = nil
	}
	if a.inst != nil {
		a.inst.Dispose() // 结构插槽 controller / 元素 Owner
		a.inst = nil
	}
}

// bindReactive 把 signal 接到整棵场景树，并负责窗口重绘，分两路：
//
//  1. 叶子插槽：一个聚合 effect 调 Instance.Update() 读全部叶子 getter（含组件
//     子树）并裁剪写入——保证一帧内叶子状态一致，只画一帧（ReactDOM 式批处理）；
//  2. 结构插槽（条件分支 / keyed 列表）：BindStructural 递归整棵实例树（含组件内
//     与嵌套分支），selector/each 变化时 Resolve 重建/复用子树并触发 onDirty 重绘。
//
// 接线期（各 effect 首次立即执行）产生的重绘全部静音——Render 已画过首帧。
//
// 结构插槽的静音已下沉到 scene 的 bindCtx.silent：BindStructural 接线期整体静音，
// 且 For/Branch 动态构建子树期间也静音（批量新增 N 项不再画 N 帧）。app 层只需：
//   - 叶子聚合 effect 用独立 priming 跳过首次重绘（它不走 bindCtx）；
//   - 结构插槽把 Draw 作为 onDirty 交给 BindStructural。
//
// 两路 effect 都挂在 a.owner 下，teardown 时随 Owner 级联销毁。
func (a *App) bindReactive() {
	if a.inst == nil || a.closed {
		return
	}
	// 叶子聚合 effect：一帧一致性。首次执行只登记依赖，不重绘。
	leafPriming := true
	signal.Effect(func() {
		_ = a.inst.Update()
		if leafPriming {
			leafPriming = false
			return
		}
		if !a.closed {
			_ = a.Draw()
		}
	})
	// 结构插槽：递归接线（接线期静音由 BindStructural 内部负责），变化后重绘。
	a.inst.BindStructural(signal.Effect, func() {
		if !a.closed {
			_ = a.Draw()
		}
	})
}

// Draw 立即把当前场景树绘制一帧到窗口。Render 之前调用为 no-op。
func (a *App) Draw() error {
	if a.closed || a.inst == nil {
		return nil
	}
	return a.win.Draw(a.inst.Root())
}

// Update 触发场景树细粒度更新（转发 Instance.Update），成功后重绘一帧。
// 返回本次实际写入的节点数（与 Instance.Update 一致）。
func (a *App) Update() int {
	if a.closed || a.inst == nil {
		return 0
	}
	n := a.inst.Update()
	_ = a.Draw()
	return n
}

// Run 进入窗口事件循环，直到窗口关闭。Headless 后端立即返回 nil。
func (a *App) Run() error {
	if a.closed {
		return nil
	}
	return a.win.Run()
}

// Close 关闭窗口。可重复调用（幂等）。
func (a *App) Close() error {
	if a.closed {
		return nil
	}
	a.closed = true
	// 先销毁整棵响应式树：停订阅、执行组件 onCleanup，避免关闭过程中 signal 写入
	// 触发已关闭窗口的重绘。
	a.teardownTree()
	return a.win.Close()
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
func (h *HeadlessWindow) Run() error { return nil }

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
