// Package gxapp 是 GX 的应用级入口：先创建「窗口外壳」，再把一棵 JSX 场景树
// 渲染进去。类比 ReactDOM.createRoot(container).render(element) —— 这里对应
// gxapp.open(config) 得到 App，再 app.render(jsx)。
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
	win             Window
	cfg             WindowConfig
	inst            *gxscene.Instance // render 前为 nil
	closed          bool
	rendered        bool
	reactivePriming bool                  // 聚合 effect 首次执行只登记依赖，不重绘（Render 已画首帧）
	reactive        []gxscene.Computation // 内部 bindReactive 建立的聚合句柄，Close 时清理
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

// Render 把一棵 JSX 场景树挂到窗口：画首帧，并在内部完成响应式接线
// （signal 变化 → 精确更新 + 自动重绘，含条件分支 / keyed 列表的结构重绘）。
// 调用方无需再手动接线 bindReactive。
//
// 重复调用会先清理上一棵树的订阅与 Owner，再渲染新树。
func (a *App) Render(inst *gxscene.Instance) error {
	if a.closed {
		return fmt.Errorf("gxapp: 应用已关闭，无法 Render")
	}
	if inst == nil {
		return fmt.Errorf("gxapp: Render 的场景树实例不能为 nil")
	}
	if a.rendered {
		// 重渲染：先停掉旧树的订阅与结构 effect，避免泄漏到新帧。
		a.disposeReactive()
		if a.inst != nil {
			a.inst.Dispose()
		}
	}
	a.inst = inst
	a.rendered = true
	if err := a.Draw(); err != nil {
		return err
	}
	// 响应式接线完全封装在 app 内部：signal 写入即更新 + 重绘一帧。
	a.bindReactive()
	return nil
}

// bindReactive 建立「单一聚合 effect」把 signal 接到场景树，并负责窗口重绘。
//
// Instance.Update() 会递归读取整棵树的叶子 getter（从而登记全部依赖），
// 并用 equalValue 裁剪，只写真正变化的节点；随后 Draw 一帧。于是：
//
//   - 一次 signal 写入 = 一次 Update + 一帧重绘，不会出现「某个插槽已更新、
//     另一个还没更新」的中间态画面；
//   - 写入次数仍精确（equalValue 裁剪），只是多做了几次便宜的 getter 求值；
//   - 这正是 ReactDOM.render 式的批处理语义，适合应用入口。
//
// 动态结构插槽（条件分支 / keyed 列表）由 scene 层独立 effect 直接改写场景树，
// 不经过上面的聚合 Update。这里为它们单独接线，并用「重绘包装」的 effect：
// selector/each 变化时重建子树后，顺带画一帧，避免结构变了却没刷新窗口。
func (a *App) bindReactive() {
	if a.inst == nil || a.closed {
		return
	}
	a.reactivePriming = true
	if c := signal.Effect(func() {
		_ = a.inst.Update() // 递归读所有叶子 getter 登记依赖 + 裁剪写入
		if a.reactivePriming {
			// 首次执行（接线时立即触发）只登记依赖；Render 已画首帧，避免重复绘制。
			a.reactivePriming = false
			return
		}
		if !a.closed {
			_ = a.Draw() // 一批更新完成，画一帧
		}
	}); c != nil {
		a.reactive = append(a.reactive, c)
	}

	structuralPriming := true
	structuralEffect := func(f func()) gxscene.Computation {
		return signal.Effect(func() {
			f() // Resolve：读 selector/each（登记依赖）+ 重建/复用子树
			if structuralPriming {
				structuralPriming = false
				return // 首次接线由 Render 画过首帧
			}
			if !a.closed {
				_ = a.Draw()
			}
		})
	}
	a.inst.BindStructural(structuralEffect)
}

// disposeReactive 清理内部聚合 effect；结构插槽句柄由 inst.Dispose 负责。
func (a *App) disposeReactive() {
	for _, c := range a.reactive {
		c.Dispose()
	}
	a.reactive = nil
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
	// 先停掉响应式订阅，避免关闭过程中 signal 写入触发已关闭窗口的重绘。
	a.disposeReactive()
	// 清理 scene 层为动态结构插槽（Branch/For）建立的 effect 与元素 Owner。
	if a.inst != nil {
		a.inst.Dispose()
	}
	return a.win.Close()
}

// Instance 返回底层场景树实例（Render 之前为 nil；供 SetPart / SetGetter 等高级用法）。
func (a *App) Instance() *gxscene.Instance { return a.inst }

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
