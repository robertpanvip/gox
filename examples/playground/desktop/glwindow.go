package main

import (
	"fmt"
	"runtime"
	"sync"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	sk "github.com/pekim/go-skia"
	skia "github.com/robertpanvip/gox-backend-skia"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox-playground/ui"
)

func init() {
	// GLFW 与所有 GL 调用必须在主线程：主 goroutine 从此锁定到主线程。
	runtime.LockOSThread()
}

// glWindow 是真实 GUI 适配层：实现 gxapp.Window，把 Skia GL surface 与 host 的
// 窗口协议接起来。布局/绘制协议完全复用可剥离 skia 后端的 skia.PaintTo，
// 事件经 gxscene.Node.DispatchBubble 走 host 统一事件系统。host 零改动。
//
// 线程模型（关键）：GL context 只在主线程 current，而 signal effect 会在写入方
// goroutine 里回调 Draw()。因此 Draw() 只加锁记录 root 并置 dirty；真正的 GL 绘制
// 由主线程 Run() 事件循环执行（paintNow），保证所有 GL 调用都在主线程。
type glWindow struct {
	cfg gxapp.WindowConfig

	win     *glfw.Window
	context sk.GrDirectContext
	surface sk.Surface
	backend sk.GrBackendRenderTarget
	target  sk.GrBackendRenderTarget
	fonts   map[int]sk.Font

	mu     sync.Mutex
	root   *gxscene.Node // 最近一次 Draw 提交的场景树根
	hits   []skia.HitBox // 最近一帧命中矩形（左上原点）
	dirty  bool          // 有新帧待绘制
	frames int           // 已绘制帧数
	opened bool
	closed bool
}

var _ gxapp.Window = (*glWindow)(nil)

func openGL() *glWindow { return &glWindow{} }

// Open 实现 gxapp.Window：创建 GLFW 窗口、GL context 与 Skia GL surface。
func (g *glWindow) Open(cfg gxapp.WindowConfig) error {
	if cfg.Width <= 0 {
		cfg.Width = 800
	}
	if cfg.Height <= 0 {
		cfg.Height = 600
	}
	g.cfg = cfg

	if err := glfw.Init(); err != nil {
		return fmt.Errorf("glfw.Init: %w", err)
	}
	glfw.WindowHint(glfw.Resizable, glfw.True)
	glfw.WindowHint(glfw.ContextVersionMajor, 3)
	glfw.WindowHint(glfw.ContextVersionMinor, 3)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	win, err := glfw.CreateWindow(cfg.Width, cfg.Height, cfg.Title, nil, nil)
	if err != nil {
		glfw.Terminate()
		return fmt.Errorf("glfw.CreateWindow: %w", err)
	}
	win.MakeContextCurrent()
	if err := gl.Init(); err != nil {
		glfw.Terminate()
		return fmt.Errorf("gl.Init: %w", err)
	}
	glfw.SwapInterval(0)
	gl.Enable(gl.BLEND)
	gl.Enable(gl.FRAMEBUFFER_SRGB)
	gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)

	g.win = win
	g.context = sk.GrDirectContextsMakeGLInterface(sk.GrGLMakeNativeInterface())
	g.fonts = make(map[int]sk.Font)

	// 键盘输入统一转发到 GX 编辑器命令（由 editor.gx 用 GX 编写）。
	// 装配层只做"平台按键 → 语义命令"的翻译，不碰编辑逻辑——这正是
	// "一份 GX 代码两端共用"的关键：网页端也调用同一组 GX 命令。
	win.SetKeyCallback(func(w *glfw.Window, key glfw.Key, scancode int, action glfw.Action, mods glfw.ModifierKey) {
		if action != glfw.Press && action != glfw.Repeat {
			return
		}
		if key == glfw.KeyEscape || (key == glfw.KeyQ && mods == glfw.ModControl) {
			w.SetShouldClose(true)
			return
		}
		switch key {
		case glfw.KeyBackspace:
			ui.EditorBackspace()
		case glfw.KeyEnter, glfw.KeyKPEnter:
			ui.EditorNewline()
		case glfw.KeyUp:
			ui.EditorMove(-1, 0)
		case glfw.KeyDown:
			ui.EditorMove(1, 0)
		case glfw.KeyLeft:
			ui.EditorMove(0, -1)
		case glfw.KeyRight:
			ui.EditorMove(0, 1)
		}
	})
	// 可打印字符（含中文，GLFW CharCallback 直接给 Unicode rune）→ GX 编辑器。
	win.SetCharCallback(func(w *glfw.Window, ch rune) {
		ui.EditorInsert(string(ch))
	})
	win.SetMouseButtonCallback(g.onMouseButton)
	win.SetFramebufferSizeCallback(func(w *glfw.Window, width, height int) {
		g.resizeToWindow()
		g.markDirty()
	})

	g.resizeToWindow()
	g.opened = true
	return nil
}

// resizeToWindow 依据当前 framebuffer 尺寸重建 Skia GL surface（仅主线程调用）。
func (g *glWindow) resizeToWindow() {
	if !g.surface.IsNil() {
		g.surface.Unref()
	}
	if !g.target.IsNil() {
		g.target.Delete()
	}
	if !g.backend.IsNil() {
		g.backend.Delete()
	}
	w, h := g.win.GetFramebufferSize()
	gl.Viewport(0, 0, int32(w), int32(h))

	var fbInfo sk.GrGLFramebufferInfo
	fbInfo.SetFBOID(0) // 默认 framebuffer
	fbInfo.SetFormat(gl.RGBA8)
	g.backend = sk.NewGrBackendRenderTargetCopy(
		sk.GrBackendRenderTargetsMakeGL(int32(w), int32(h), 1, 8, fbInfo),
	)
	g.target = sk.NewGrBackendRenderTargetCopy(g.backend)
	var cs sk.ColorSpace
	g.surface = sk.SurfacesWrapBackendRenderTarget(
		g.context.AsGrRecordingContext(), g.backend,
		sk.GrSurfaceOriginBottomLeft, sk.ColorTypeBGRA_8888, cs,
		sk.NewSurfacePropsPixelGeometry(0, sk.PixelGeometryRGB_H),
	)
}

// Draw 实现 gxapp.Window：线程安全地把最新场景树提交给主线程的绘制循环。
// 注意：此回调可能来自 signal 写入方 goroutine，绝不能在此调用 GL 函数。
func (g *glWindow) Draw(root *gxscene.Node) error {
	g.mu.Lock()
	g.root = root
	g.dirty = true
	g.mu.Unlock()
	return nil
}

// markDirty 请求重绘一帧（例如窗口尺寸变化）。
func (g *glWindow) markDirty() {
	g.mu.Lock()
	g.dirty = true
	g.mu.Unlock()
}

// paintNow 在主线程执行真正的 Skia GL 绘制：取最新 root → painter 遍历 → flush → 交换缓冲。
func (g *glWindow) paintNow() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.opened || g.surface.IsNil() || !g.dirty {
		return
	}
	canvas := g.surface.GetCanvas()
	if canvas.IsNil() {
		return
	}
	w, h := g.win.GetFramebufferSize()
	_, hits := skia.PaintTo(canvas, float32(w), float32(h), g.root, g.fonts)
	g.hits = hits
	g.context.FlushAndSubmit(sk.GrSyncCpuYes)
	gl.Flush()
	g.win.SwapBuffers()
	g.frames++
	g.dirty = false
}

// onMouseButton 把鼠标点击映射到场景节点：命中矩形（面积最小优先）→ 焦点处理 → 冒泡派发 onClick。
func (g *glWindow) onMouseButton(w *glfw.Window, button glfw.MouseButton, action glfw.Action, mods glfw.ModifierKey) {
	if action != glfw.Press || button != glfw.MouseButtonLeft {
		return
	}
	g.mu.Lock()
	root, hits := g.root, append([]skia.HitBox(nil), g.hits...)
	g.mu.Unlock()
	if root == nil {
		return
	}
	cx, cy := w.GetCursorPos()
	px, py := float32(cx), float32(cy) // GLFW 光标坐标已是左上原点，与 painter/hits 一致
	// 命中"面积最小"的框：最内层/最具体的元素（如按钮）优先于外层容器。
	bestIdx, bestArea := -1, float32(0)
	for i := range hits {
		if !hits[i].Contains(px, py) {
			continue
		}
		area := hits[i].W * hits[i].H
		if bestIdx < 0 || area < bestArea {
			bestIdx, bestArea = i, area
		}
	}
	if bestIdx >= 0 {
		node := hits[bestIdx].Node
		// 点击任意处即聚焦编辑器（焦点是 editor.gx 里的 sig，由 GX 命令管理）。
		ui.EditorFocus()
		node.DispatchBubble("onClick", gxscene.EventContext{
			Raw: "glfw-mouse",
			Synthetic: &gxscene.SyntheticEvent{
				Type:          gxscene.EvClick,
				NativeName:    "onClick",
				Target:        node,
				CurrentTarget: node,
				X:             float64(px),
				Y:             float64(py),
				Button:        int(button),
			},
		})
	}
}

// Run 实现 gxapp.Window：主线程事件循环，处理 GLFW 事件并按需绘制新帧。
func (g *glWindow) Run() error {
	if !g.opened {
		return nil
	}
	// 首帧：Render() 已在 Run 前调用 Draw 置 dirty，主线程立即绘制。
	g.paintNow()
	for !g.win.ShouldClose() {
		glfw.WaitEventsTimeout(0.02) // 20ms 超时，兼顾事件响应与 signal 异步重绘
		g.paintNow()
	}
	return nil
}

// Close 实现 gxapp.Window：释放所有 GL/Skia 资源（主线程，因 app.Close 在主 goroutine）。
func (g *glWindow) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	g.closed = true
	for _, f := range g.fonts {
		f.Delete()
	}
	g.fonts = nil
	if !g.surface.IsNil() {
		g.surface.Unref()
	}
	if !g.target.IsNil() {
		g.target.Delete()
	}
	if !g.backend.IsNil() {
		g.backend.Delete()
	}
	// GrDirectContext 在本绑定无显式 Release；surface/backend 解引用后由 finalizer 回收。
	if g.win != nil {
		g.win.Destroy()
	}
	glfw.Terminate()
	return nil
}

// Config 实现 gxapp.Window。
func (g *glWindow) Config() gxapp.WindowConfig { return g.cfg }

// Frames 返回已绘制帧数（观测用）。
func (g *glWindow) Frames() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.frames
}
