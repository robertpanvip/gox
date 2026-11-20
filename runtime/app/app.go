// Package gxapp 是 GX 的应用级入口：把一棵已渲染的常驻场景树挂到「窗口」上，
// 类比 ReactDOM.render(element, container) —— 这里对应 gxapp.Open(view, config)。
//
// 分层：
//   - gxscene 负责 JSX → opcode → 常驻场景树 + 细粒度更新（渲染核心）
//   - signal 负责响应式依赖追踪
//   - gxapp 负责「应用外壳」：窗口配置、窗口后端抽象、生命周期、signal 变化时自动重绘
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
)

// WindowConfig 是窗口配置（open 的第二个参数）。零值字段由各 Window 后端给默认值。
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

// App 是 open 返回的应用句柄，持有场景树实例、窗口后端与配置。
type App struct {
	inst            *gxscene.Instance
	win             Window
	cfg             WindowConfig
	closed          bool
	reactivePriming bool                  // 聚合 effect 首次执行只登记依赖，不重绘（Open 已画首帧）
	reactive        []gxscene.Computation // BindReactive 建立的响应式句柄，Close 时清理
}

// Open 把场景树挂到默认 Headless 窗口并首次绘制。
//
//	view := <div id="app">hello</div>
//	app := gxapp.Open(view, gxapp.WindowConfig{Title:"Demo", Width:800, Height:600})
//	defer app.Close()
//	app.Run()
//
// 等价用法：OpenWith(view, cfg, &gxapp.HeadlessWindow{Out: os.Stdout})。
func Open(inst *gxscene.Instance, cfg WindowConfig) (*App, error) {
	return OpenWith(inst, cfg, &HeadlessWindow{Out: os.Stdout})
}

// OpenWith 同 Open，但允许注入自定义窗口后端（Skia / wgpu / GLFW ...）。
func OpenWith(inst *gxscene.Instance, cfg WindowConfig, win Window) (*App, error) {
	if inst == nil {
		return nil, fmt.Errorf("gxapp: Open 的第一个参数（场景树实例）不能为 nil")
	}
	if win == nil {
		return nil, fmt.Errorf("gxapp: 窗口后端不能为 nil")
	}
	cfg = cfg.normalize()
	if err := win.Open(cfg); err != nil {
		return nil, fmt.Errorf("gxapp: 打开窗口失败: %w", err)
	}
	a := &App{inst: inst, win: win, cfg: cfg}
	if err := a.Draw(); err != nil {
		return nil, err
	}
	return a, nil
}

// Draw 立即把当前场景树绘制一帧到窗口。
func (a *App) Draw() error {
	if a.closed {
		return nil
	}
	return a.win.Draw(a.inst.Root())
}

// Update 触发场景树细粒度更新（转发 Instance.Update），成功后重绘一帧。
// 返回本次实际写入的节点数（与 Instance.Update 一致）。
func (a *App) Update() int {
	n := a.inst.Update()
	_ = a.Draw()
	return n
}

// BindReactive 把 signal 接到场景树，并在 signal 变化时自动重绘窗口。
//
// 采用「单一聚合 effect」而非 Instance.BindReactive 的逐插槽 effect：
// Instance.Update() 会递归读取整棵树的叶子 getter（从而登记全部依赖），
// 并用 equalValue 裁剪，只写真正变化的节点；随后 Draw 一帧。于是：
//
//   - 一次 signal 写入 = 一次 Update + 一帧重绘，不会出现「某个插槽已更新、
//     另一个还没更新」的中间态画面（逐插槽 effect 会产生这种闪烁）；
//   - 写入次数仍精确（equalValue 裁剪），只是多做了几次便宜的 getter 求值；
//   - 这正是 ReactDOM.render 式的批处理语义，适合应用入口。
//
// 若需要「精确命中单插槽、完全不求值其他 getter」的极致性能，可用
// 底层 Instance.BindReactive（但不会产生窗口帧）。
//
//	app.BindReactive(signal.Effect)   // signal 变化 → 更新 + 自动重绘一帧
func (a *App) BindReactive(effect func(func()) gxscene.Computation) {
	if effect == nil || a.closed {
		return
	}
	a.reactivePriming = true
	if c := effect(func() {
		_ = a.inst.Update() // 递归读所有 getter 登记依赖 + 裁剪写入
		if a.reactivePriming {
			// 首次执行（接线时立即触发）只登记依赖；Open 已画首帧，避免重复绘制。
			a.reactivePriming = false
			return
		}
		if !a.closed {
			_ = a.Draw() // 一批更新完成，画一帧
		}
	}); c != nil {
		a.reactive = append(a.reactive, c)
	}
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
	for _, c := range a.reactive {
		c.Dispose()
	}
	a.reactive = nil
	return a.win.Close()
}

// Instance 返回底层场景树实例（供 SetPart / SetGetter 等高级用法）。
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
