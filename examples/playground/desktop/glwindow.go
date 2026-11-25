package main

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	sk "github.com/pekim/go-skia"
	skia "github.com/robertpanvip/gox-backend-skia"
	"github.com/robertpanvip/gox-playground/ui"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	"github.com/robertpanvip/gox/runtime/loop"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
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

	// 编辑器选区交互状态。GLFW 回调都在主线程（Run 的 WaitEvents 循环里）串行执行，无需加锁。
	dragging   bool      // 左键按住拖拽选择中
	clickTime  time.Time // 上次按下时刻/位置：双击、三击判定
	clickX     float64
	clickY     float64
	clickCount int

	// UI Loop（提案 P2）：桌面主循环接入的任务队列与监测器；
	// openGL() 创建、Run() drain、Close() 停止。
	uiLoop *loop.Queue
	wd     *loop.Watchdog
}

var _ gxapp.Window = (*glWindow)(nil)
var _ gxapp.UIExecutor = (*glWindow)(nil)

// glfwWokeReady：glfw.Init 完成后唤醒才有意义（事件系统就位后 PostEmptyEvent
// 才能打断 WaitEventsTimeout）。
var glfwWokeReady atomic.Bool

func openGL() *glWindow {
	g := &glWindow{}
	// 进程级共享 UI Loop（提案决议 7）：任务只入队、不在调用栈执行，
	// 统一由 Run() 主循环 drain——即 runtime.LockOSThread 绑定的 GLFW 线程。
	g.uiLoop = loop.New(loop.Config{Wake: func() {
		if glfwWokeReady.Load() {
			glfw.PostEmptyEvent()
		}
	}})
	return g
}

// Post 实现 gxapp.UIExecutor：共享 Loop 即桌面端 UI 任务投递上下文（提案 §6.1，
// UIExecutor 语义未变，仅落点换成 Loop 队列）。
func (g *glWindow) Post(fn func()) error {
	if g.uiLoop == nil {
		return loop.ErrClosed
	}
	return g.uiLoop.Post(fn)
}

// reportSlow：Watchdog 诊断输出；只报告不恢复（提案 §10.1）。
func reportSlow(ev loop.WatchEvent) {
	fmt.Fprintf(os.Stderr, "[gox loop] 慢任务 level=%v kind=%v id=%d elapsed=%v\n", ev.Level, ev.Task.Kind, ev.Task.ID, ev.Elapsed)
	if ev.Stack != "" {
		fmt.Fprintln(os.Stderr, ev.Stack)
	}
}

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
	glfwWokeReady.Store(true)
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
		// 与网页端对齐的快捷键：Ctrl+Enter 运行、Ctrl+Z 撤销、Ctrl+Shift+Z /
		// Ctrl+Y 重做。这些是带修饰键的组合，可打印字符不会进 CharCallback。
		ctrl := mods&glfw.ModControl != 0
		if ctrl && key == glfw.KeyEnter {
			ui.TriggerRun()
			return
		}
		if ctrl && key == glfw.KeyZ {
			if mods&glfw.ModShift != 0 {
				ui.EditorRedo()
			} else {
				ui.EditorUndo()
			}
			return
		}
		if ctrl && key == glfw.KeyY {
			ui.EditorRedo()
			return
		}
		shift := mods&glfw.ModShift != 0
		// 选区快捷键：与 web 装配层同源语义，编辑逻辑全部在 editor.gx（桌面零新逻辑）。
		// 剪贴板用本绑定的无窗口参数签名（glfw clipboard.go 已核对）。
		if ctrl && key == glfw.KeyA {
			ui.EditorSelectAll()
			return
		}
		if ctrl && key == glfw.KeyC {
			if sel := ui.EditorSelectedText(); sel != "" {
				glfw.SetClipboardString(sel)
			}
			return
		}
		if ctrl && key == glfw.KeyX {
			if sel := ui.EditorSelectedText(); sel != "" {
				glfw.SetClipboardString(sel)
				ui.EditorDeleteSelection()
			}
			return
		}
		if ctrl && key == glfw.KeyV {
			if s := glfw.GetClipboardString(); s != "" {
				ui.EditorPaste(s)
			}
			return
		}
		switch key {
		case glfw.KeyBackspace:
			ui.EditorBackspace()
		case glfw.KeyEnter, glfw.KeyKPEnter:
			ui.EditorNewline()
		case glfw.KeyUp:
			ui.EditorMoveEx(-1, 0, shift)
		case glfw.KeyDown:
			ui.EditorMoveEx(1, 0, shift)
		case glfw.KeyLeft:
			if ctrl {
				ui.EditorMoveWord(-1, shift)
			} else {
				ui.EditorMoveEx(0, -1, shift)
			}
		case glfw.KeyRight:
			if ctrl {
				ui.EditorMoveWord(1, shift)
			} else {
				ui.EditorMoveEx(0, 1, shift)
			}
		case glfw.KeyHome:
			ui.EditorKeyHome(shift)
		case glfw.KeyEnd:
			ui.EditorKeyEnd(shift)
		case glfw.KeyDelete:
			ui.EditorDeleteForward()
		}
	})
	// 可打印字符（含中文，GLFW CharCallback 直接给 Unicode rune）→ GX 编辑器。
	win.SetCharCallback(func(w *glfw.Window, ch rune) {
		// Ctrl 组合在个别平台会给出控制字符（如 Ctrl+C 的 \x03），过滤掉避免灌进源码；
		// \t 保留（Tab 插入缩进是既有行为），可打印字符与 CJK（>=0x80）正常插入。
		if (ch < 0x20 && ch != '\t') || ch == 0x7f {
			return
		}
		ui.EditorInsert(string(ch))
	})
	win.SetMouseButtonCallback(g.onMouseButton)
	// 拖动选择：左键按住移动时持续扩展选区（与 web 端 pointermove 同语义）。
	win.SetCursorPosCallback(func(w *glfw.Window, x, y float64) {
		if !g.dragging || w.GetMouseButton(glfw.MouseButtonLeft) != glfw.Press {
			return
		}
		if line, col, ok := g.editorHit(float32(x), float32(y)); ok {
			ui.EditorExtendTo(line, col)
		}
	})
	win.SetFramebufferSizeCallback(func(w *glfw.Window, width, height int) {
		g.resizeToWindow()
		g.markDirty()
	})

	g.resizeToWindow()
	g.wd = loop.NewWatchdog(g.uiLoop, loop.WatchConfig{Report: reportSlow})
	g.wd.Start()
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
	// 高 DPI：framebuffer 物理像素 / 窗口逻辑点 = DPR，布局用逻辑点、绘制按 DPR 放大。
	fx, fy := g.win.GetFramebufferSize()
	wx, wy := g.win.GetSize()
	dpr := float32(1)
	if wx > 0 && wy > 0 && fx > 0 && fy > 0 {
		dpr = float32(fx) / float32(wx)
		if dpr <= 0 {
			dpr = 1
		}
	}
	_, hits := skia.PaintToScaled(canvas, float32(fx), float32(fy), dpr, g.root, g.fonts)
	g.hits = hits
	g.context.FlushAndSubmit(sk.GrSyncCpuYes)
	gl.Flush()
	g.win.SwapBuffers()
	g.frames++
	g.dirty = false
}

// onMouseButton 把鼠标点击映射到场景节点：命中矩形（面积最小优先）→ 焦点处理 → 冒泡派发 onClick。
func (g *glWindow) onMouseButton(w *glfw.Window, button glfw.MouseButton, action glfw.Action, mods glfw.ModifierKey) {
	if button != glfw.MouseButtonLeft {
		return
	}
	if action == glfw.Release {
		g.dragging = false
		return
	}
	if action != glfw.Press {
		return
	}
	cx, cy := w.GetCursorPos()
	px, py := float32(cx), float32(cy) // GLFW 光标坐标已是左上原点，与 painter/hits 一致
	// ---- 编辑器选区交互：定点 / Shift 扩展 / 双击选词 / 三击选行 / 拖拽起点 ----
	// GLFW 没有 dblclick 事件，用"400ms 内同点位连续按下"计数模拟 detail。
	if line, col, ok := g.editorHit(px, py); ok {
		shift := mods&glfw.ModShift != 0
		now := time.Now()
		if !shift && now.Sub(g.clickTime) < 400*time.Millisecond &&
			math.Abs(cx-g.clickX) < 6 && math.Abs(cy-g.clickY) < 6 {
			g.clickCount++
		} else {
			g.clickCount = 1
		}
		g.clickTime, g.clickX, g.clickY = now, cx, cy
		switch {
		case shift:
			ui.EditorExtendTo(line, col)
		case g.clickCount == 2:
			ui.EditorSelectWord(line, col)
		case g.clickCount >= 3:
			ui.EditorSelectLine(line)
			g.clickCount = 1 // 四击不再重复选行
		default:
			ui.EditorSetSelect(line, col)
			g.dragging = true
		}
	}
	g.mu.Lock()
	root, hits := g.root, append([]skia.HitBox(nil), g.hits...)
	g.mu.Unlock()
	if root == nil {
		return
	}
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

// editorHit 把屏幕点换算成编辑器 (行, 列)。行取 .gx-editor-row 命中框（可见行
// 按 y 排序后序号即文档行号——固定 24 行里超出内容高度的行高为 0，天然不参与命中）；
// 列换算交给 ui.PickColumn，与 caret 渲染共用同一套字符度量。行内前 50 逻辑点是
// 行号槽（42）+ 间距（8），点击行号区等价列 0（PickColumn 对负数返回 0）。
func (g *glWindow) editorHit(px, py float32) (int, int, bool) {
	g.mu.Lock()
	hits := append([]skia.HitBox(nil), g.hits...)
	g.mu.Unlock()
	var rows []skia.HitBox
	for i := range hits {
		h := hits[i]
		if h.Node == nil || h.Node.Props == nil || h.H <= 0 {
			continue
		}
		if cls, _ := h.Node.Props["class"].(string); strings.Contains(cls, "gx-editor-row") {
			rows = append(rows, h)
		}
	}
	if len(rows) == 0 {
		return 0, 0, false
	}
	sort.Slice(rows, func(a, b int) bool { return rows[a].Y < rows[b].Y })
	for idx, r := range rows {
		if py >= r.Y && py < r.Y+22 {
			relX := float64(px) - (float64(r.X) + 50)
			return idx, ui.PickColumn(idx, relX), true
		}
	}
	return 0, 0, false
}

// drainLoop：提案 §4.1 宿主节奏——每执行一个宏任务即过一次微任务检查点；
// 预算未清空（Ran>0 或 Remaining>0）继续消耗，直到无可执行任务。只在
// GLFW 主线程调用，天然满足线程亲和性；不启动第二个并行事件循环。
func (g *glWindow) drainLoop() {
	if g.uiLoop == nil {
		return
	}
	for {
		ran := g.uiLoop.RunOneMacro()
		rep := g.uiLoop.RunMicrotasks()
		if !ran && rep.Ran == 0 && rep.Remaining == 0 {
			return
		}
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
		glfw.WaitEventsTimeout(0.02) // 20ms 超时；Loop.Wake 经 PostEmptyEvent 即时打断
		g.drainLoop()
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
	if g.wd != nil {
		g.wd.Stop()
	}
	if g.uiLoop != nil {
		_ = g.uiLoop.Stop() // 先拒绝新任务；未执行任务丢弃并计数（提案 §8）
	}
	glfwWokeReady.Store(false)
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
