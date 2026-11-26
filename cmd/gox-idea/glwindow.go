package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	skia "github.com/robertpanvip/gox-backend-skia"
	"github.com/robertpanvip/gox-idea/ui"
	sk "github.com/robertpanvip/gox-skia/compat"
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
	blit    *rasterBlit // CPU raster → GL 纹理显示端

	mu     sync.Mutex
	root   *gxscene.Node // 最近一次 Draw/Present 提交的场景树根
	hits   []skia.HitBox // 最近一帧命中矩形（左上原点）
	dirty  bool          // 有新帧待绘制
	frames int           // 已绘制帧数
	opened bool
	closed bool

	// 增量帧（FramePresenter）：App 每次提交带递增 Version 与 Changes。
	// 无 Change 的帧画面不变，Present 不置 dirty，paintNow 跳过整条绘制/blit/swap
	// 链（屏幕保持上帧）——这是帧级短路。
	painted bool // 是否至少绘制过一帧；首帧即使无 Change 也必须绘制（建树期不记录 Change）

	// 像素级脏矩形：桌面 canvas 是跨帧持久的 CPU raster（SurfacesWrapBackendRenderTarget
	// 实际返回 NewCanvasWithDimensions 的 Pixmap），可直接复用 skia 的帧缓存。
	// 有 Change 的帧进 paintNow 后，PaintToCachedScaled 只重画脏区、不清整屏，
	// 大幅降低编辑器逐字输入/光标移动时的光栅化量。resize 重建画布时必须清空。
	frameCache skia.FrameCache

	// 编辑器选区交互状态。GLFW 回调都在主线程（Run 的 WaitEvents 循环里）串行执行，无需加锁。
	scrollEvents   atomic.Int64 // 测试钩子：累计收到的滚轮回调次数，用于区分「事件没到」与「滚动逻辑没生效」
	lastScrollYOff    float64 // 测试钩子：最近一次滚轮回调的 yoff，诊断事件参数是否为 0
	lastScrollBefore  float64 // 测试钩子：滚动转发前后的偏移，定位「转发没生效/被回写」
	lastScrollAfter   float64
	dragging   bool      // 左键按住拖拽选择中
	clickTime  time.Time // 上次按下时刻/位置：双击、三击判定
	clickX     float64
	clickY     float64
	clickCount int

	// WebStorm 三区分隔条拖拽状态（同样只在主线程回调里读写）。
	splitDrag   bool   // 正在拖拽分隔条
	splitAxis   string // "v"=竖直分隔条(调左栏宽)，"h"=水平分隔条(调终端高)
	splitStartX float64
	splitStartY float64
	splitStartW int
	splitStartH int

	// 完整重绘回调：拖拽分隔条改变布局尺寸时，动态子树内部的 height 变化
	// 在增量帧后端不产生有效刷新（同类于结构型 For 的限制），故走一次
	// app.Render 完整重建。由 main.go 在 App 创建后注入。
	rerender func()

	// UI Loop（提案 P2）：桌面主循环接入的任务队列与监测器；
	// openGL() 创建、Run() drain、Close() 停止。
	uiLoop *loop.Queue
	wd     *loop.Watchdog

	// hoverCursor：当前悬停命中框的 cursor 形状（""=默认箭头）。只在主线程回调
	// 里读写；标准光标对象按形状惰性创建、复用（GLFW cursor 是可复用句柄）。
	hoverCursor string
	cursors     map[string]*glfw.Cursor
}

var _ gxapp.Window = (*glWindow)(nil)
var _ gxapp.UIExecutor = (*glWindow)(nil)
var _ gxapp.FramePresenter = (*glWindow)(nil)

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
	// 不开 FRAMEBUFFER_SRGB：CPU 光栅画布输出的已是 sRGB 编码像素，
	// 再经 sRGB 帧缓冲写入转换会二次提亮（暗色 #1e1e1e 被抬成灰 #606060）。
	gl.BlendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA)

	g.win = win
	g.context = sk.GrDirectContextsMakeGLInterface(sk.GrGLMakeNativeInterface())
	g.fonts = make(map[int]sk.Font)
	// CPU raster 显示端：canvas 画到像素缓冲 → 上传纹理 → 全屏 blit。
	var blitErr error
	g.blit, blitErr = newRasterBlit()
	if blitErr != nil {
		glfw.Terminate()
		return fmt.Errorf("newRasterBlit: %w", blitErr)
	}

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
		inputLogf("char %q before: %s", ch, ui.EditorStatus())
		ui.EditorInsert(string(ch))
		inputLogf("char %q after:  %s", ch, ui.EditorStatus())
	})
	win.SetMouseButtonCallback(g.onMouseButton)
	// 滚轮 → 虚拟滚动窗口移动。GLFW yoff 正为向上滚，因此取负号转成"向下为正行数"。
	// 编辑器的受控 scrollTop 由 ui.EditorScrollLines 写入，光标跟随 effect 不参与。
	win.SetScrollCallback(func(w *glfw.Window, xoff, yoff float64) {
		g.scrollEvents.Add(1)
		g.lastScrollYOff = yoff
		g.lastScrollBefore = ui.DebugScrollTop()
		ui.EditorScrollLines(-yoff * 3)
		g.lastScrollAfter = ui.DebugScrollTop()
		g.markDirty()
	})
	// 光标移动：分隔条拖拽调整区域尺寸；否则左键按住时扩展编辑器选区
	// （与 web 端 pointermove 同语义）。
	win.SetCursorPosCallback(func(w *glfw.Window, x, y float64) {
		if w.GetMouseButton(glfw.MouseButtonLeft) != glfw.Press {
			// 悬停：按命中框 cursor 属性切换系统光标（消费 CSS cursor 样式）。
			g.updateHoverCursor(w, float32(x), float32(y))
			return
		}
		if g.splitDrag {
			if g.splitAxis == "v" {
				ui.SetLayoutProjectW(g.splitStartW + int(x-g.splitStartX))
			} else {
				// 终端在下方：向上拖动(y 减小)增大高度，故取负号。
				ui.SetLayoutTerminalH(g.splitStartH - int(y-g.splitStartY))
			}
			// 布局尺寸写在动态子树内部，增量帧不刷新，走完整重绘。
			if g.rerender != nil {
				g.rerender()
			}
			g.markDirty()
			return
		}
		if !g.dragging {
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
	// 光标离开窗口：恢复默认箭头。
	win.SetCursorEnterCallback(func(w *glfw.Window, entered bool) {
		if !entered {
			g.setHoverCursor(w, "")
		}
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
		sk.GrSurfaceOriginBottomLeft, sk.ColorTypeRGBA_8888, cs,
		sk.NewSurfacePropsPixelGeometry(0, sk.PixelGeometryRGB_H),
	)
	if g.blit != nil {
		g.blit.resize(int32(w), int32(h))
	}
	// 新 surface 的 raster 像素是全白的；旧缓存的布局与脏区基于旧尺寸，
	// 必须丢弃，否则下一帧会误判"整帧复用"而在白画布上留下空画面。
	g.frameCache = skia.FrameCache{}
}

// Draw 实现 gxapp.Window：线程安全地把最新场景树提交给主线程的绘制循环。
// 注意：此回调可能来自 signal 写入方 goroutine，绝不能在此调用 GL 函数。
//
// App 检测到本窗口实现 FramePresenter 后会走 Present，不再调 Draw；保留 Draw
// 是为了兼容直接调用（headless/测试/第三方装配）。老路径保守处理：总是置 dirty。
func (g *glWindow) Draw(root *gxscene.Node) error {
	g.mu.Lock()
	g.root = root
	g.dirty = true
	g.mu.Unlock()
	return nil
}

// Present 实现 gxapp.FramePresenter：消费 Host 的增量帧。
//
// 两级增量，叠加生效：
//  1. 帧级短路：无实质变化的帧不进绘制链——不置 dirty，paintNow 跳过绘制、
//     纹理上传与缓冲交换，屏幕保持上一帧（GL 双缓冲语义下安全）。
//  2. 像素级脏矩形：有 Change 的帧进 paintNow 后，PaintToCachedScaled 复用
//     跨帧持久的 CPU raster，只重画 diffLayout 算出的脏区、不清整屏——
//     编辑器逐字输入/光标移动只光栅化一小条。
//
// 桌面 canvas 是跨帧持久的 raster Pixmap（SurfacesWrapBackendRenderTarget 实为
// NewCanvasWithDimensions），不是易失的 GL 默认帧缓冲，故 skia 的帧缓存可直接
// 复用；resize 重建画布时须清空缓存（见 resizeToWindow）。
//
// changed 判定只看 FullSync 与 Changes：Version 每帧都递增（即使无变化），
// 不能作为"有变化"的依据——frame.go 明确：Changes 为空但 Version 前进即表示
// 变更被相等性短路、画面无变化。resize/交互等非帧源通过 markDirty 独立强制
// 重绘，不经过这里，不受短路影响。
func (g *glWindow) Present(frame *gxapp.Frame) error {
	if frame == nil {
		return g.Draw(nil)
	}
	g.mu.Lock()
	g.root = frame.Root
	// 首帧必须绘制（Render 建树期 sink 未开，Changes 为空但树是全新的）；
	// 之后只有真正有变更的帧才置 dirty，无 Change 帧跳过整条绘制链。
	if !g.painted || frame.FullSync || len(frame.Changes) > 0 {
		if frame.FullSync {
			// 丢帧恢复：强制全量，丢弃基于旧画面的脏区缓存。
			g.frameCache = skia.FrameCache{}
		}
		g.dirty = true
	}
	g.mu.Unlock()
	return nil
}

// markDirty 请求重绘一帧（例如窗口尺寸变化）。
func (g *glWindow) markDirty() {
	g.mu.Lock()
	g.dirty = true
	g.mu.Unlock()
}

// ForceRedraw 请求下一帧强制走完整绘制链，即使该帧没有 Host Change。
//
// GX 的标准动画都经 signal/RAF/命令式写回产生 Change，Present 会自动放行；
// 此方法是给"不写 signal、直接改 props 的旁路动画"预留的逃生口——动画系统
// 在每帧起点调用它即可，避免被"无 Change 即短路"吞掉。当前桌面端未接动画，
// 正常路径无需调用。
func (g *glWindow) ForceRedraw() {
	g.markDirty()
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
	// 带缓存的 DPR 绘制：内容未变整帧不动、有变化只重画脏矩形区域。
	// canvas 是跨帧持久的 CPU raster，局部重绘保留脏区外的上帧像素。
	_, hits := skia.PaintToCachedScaled(&g.frameCache, canvas, float32(fx), float32(fy), dpr, g.root, g.fonts)
	g.hits = hits
	// CPU raster → GL 纹理 → 全屏 blit（替代原 Skia GL surface 的直接绘制）。
	if g.blit != nil {
		if pix := canvas.Pixmap(); pix != nil {
			g.blit.upload(int32(fx), int32(fy), pix.Pix)
			g.blit.blit()
		}
	}
	gl.Flush()
	g.win.SwapBuffers()
	g.frames++
	g.painted = true
	// 声明式动画（如 Editor 光标的 blink）是"不写 signal、直接改 props 的旁路动画"：
	// 它不产生 frame.Changes，会被 Present 的"无 Change 即短路"跳过。因此这里在绘制后
	// 检测场景是否仍有活跃动画，有则保持 dirty，让 Run 的 20ms 事件循环持续重绘下一帧
	// （等价于每帧调用 ForceRedraw）。动画消失后自动停帧，不产生空转开销。
	// 与 FramePresenter 的像素级脏矩形叠加：动画帧进来后仍只重画光标那一小条。
	g.dirty = skia.HasActiveAnimation(g.root)
}

// updateHoverCursor 按最近一帧命中框的 cursor 属性切换系统光标（消费 CSS cursor 样式）。
// cursor 是 CSS 继承属性：在所有包含该点的命中框里按面积从小到大找第一个声明了
// cursor 的框（最内层有效值）；都没有则恢复默认箭头。
func (g *glWindow) updateHoverCursor(w *glfw.Window, px, py float32) {
	g.mu.Lock()
	hits := append([]skia.HitBox(nil), g.hits...)
	g.mu.Unlock()
	type cand struct {
		shape string
		area  float32
	}
	var best cand
	for i := range hits {
		if !hits[i].Contains(px, py) || hits[i].Cursor == "" {
			continue
		}
		area := hits[i].W * hits[i].H
		if best.shape == "" || area < best.area {
			best = cand{hits[i].Cursor, area}
		}
	}
	g.setHoverCursor(w, best.shape)
}

// setHoverCursor 切换系统光标形状（""=默认箭头）；相同形状幂等跳过。
// 标准光标对象按形状惰性创建并跨帧复用。
func (g *glWindow) setHoverCursor(w *glfw.Window, shape string) {
	if g.hoverCursor == shape {
		return
	}
	g.hoverCursor = shape
	if g.cursors == nil {
		g.cursors = make(map[string]*glfw.Cursor)
	}
	shape = strings.ToLower(strings.TrimSpace(shape))
	if shape == "" || shape == "default" || shape == "auto" {
		shape = "arrow"
	}
	c, ok := g.cursors[shape]
	if !ok {
		var std glfw.StandardCursor
		switch shape {
		case "pointer", "hand":
			std = glfw.HandCursor
		case "text":
			std = glfw.IBeamCursor
		case "crosshair":
			std = glfw.CrosshairCursor
		case "ew-resize", "col-resize":
			std = glfw.HResizeCursor
		case "ns-resize", "row-resize":
			std = glfw.VResizeCursor
		default:
			std = glfw.ArrowCursor // wait/progress/move 等暂按箭头处理
		}
		c = glfw.CreateStandardCursor(std)
		g.cursors[shape] = c
	}
	w.SetCursor(c)
}

// onMouseButton 把鼠标点击映射到场景节点：命中矩形（面积最小优先）→ 焦点处理 → 冒泡派发 onClick。
func (g *glWindow) onMouseButton(w *glfw.Window, button glfw.MouseButton, action glfw.Action, mods glfw.ModifierKey) {
	if button != glfw.MouseButtonLeft {
		return
	}
	if action == glfw.Release {
		g.dragging = false
		if g.splitDrag {
			g.splitDrag = false
			ui.SetSplitDragging(false)
			g.markDirty()
		}
		return
	}
	if action != glfw.Press {
		return
	}
	cx, cy := w.GetCursorPos()
	px, py := float32(cx), float32(cy) // GLFW 光标坐标已是左上原点，与 painter/hits 一致
	inputLogf("click px=%.1f py=%.1f", px, py)

	// ---- WebStorm 分隔条拖拽：命中 splitter 则进入拖拽态，优先于编辑器选区 ----
	if axis, ok := g.splitterAt(px, py); ok {
		g.splitDrag = true
		g.splitAxis = axis
		g.splitStartX, g.splitStartY = cx, cy
		if axis == "v" {
			g.splitStartW = ui.LayoutProjectW()
		} else {
			g.splitStartH = ui.LayoutTerminalH()
		}
		ui.SetSplitDragging(true)
		g.markDirty()
		return
	}

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
	// 命中选取：登记序倒序取第一个包含点的框。skia 绘制序=父先子后、浮层最后，
	// 因此倒序即浏览器语义的"顶绘制层优先 + 同层最内层优先"——浮层（popover/
	// scrim）压在最上时点得到浮层，点空白命中 scrim 收起菜单；旧"面积最小"
	// 启发式会让点击穿透遮罩，打不开也关不掉浮层。
	bestIdx := -1
	for i := len(hits) - 1; i >= 0; i-- {
		if hits[i].Contains(px, py) {
			bestIdx = i
			break
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

// ---- 输入诊断日志（GOX_INPUT_DEBUG=1 时启用，写 %TEMP%\gox-ide-input.log）----
// 用于定位「点击定位偏移 / 空格插错位置」类报障：记录每次点击的换算输入输出
// 与每次字符输入后的光标状态，区分「逻辑层错」与「显示层错」。

func inputLogf(format string, args ...any) {
	if os.Getenv("GOX_INPUT_DEBUG") == "" {
		return
	}
	p := filepath.Join(os.TempDir(), "gox-ide-input.log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	fmt.Fprintf(f, time.Now().Format("15:04:05.000 ")+" "+format+"\n", args...)
	f.Close()
}

// splitterAt 判断屏幕点是否落在 WebStorm 三区的分隔条上，返回轴向。
// 分隔条在 GX 里用 class="gx-splitter-v"/"gx-splitter-h" 标记，命中框由
// skia 后端随帧生成；这里按 class 精确匹配，避免与编辑器/终端的命中混淆。
func (g *glWindow) splitterAt(px, py float32) (string, bool) {
	g.mu.Lock()
	hits := append([]skia.HitBox(nil), g.hits...)
	g.mu.Unlock()
	for _, h := range hits {
		if h.Node == nil || h.Node.Props == nil || h.W <= 0 || h.H <= 0 {
			continue
		}
		if px < h.X || px >= h.X+h.W || py < h.Y || py >= h.Y+h.H {
			continue
		}
		cls, _ := h.Node.Props["class"].(string)
		switch cls {
		case "gx-splitter-v":
			return "v", true
		case "gx-splitter-h":
			return "h", true
		}
	}
	return "", false
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
			inputLogf("editorHit px=%.1f py=%.1f r.X=%.1f r.Y=%.1f idx=%d relX=%.1f line=%q col=%d",
				px, py, r.X, r.Y, idx, relX, ui.EditorLineSource(idx), ui.PickColumn(idx, relX))
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
	// 单次宿主轮次：微任务预算耗尽（洪水续命）时带积压让出，由下一轮
	// WaitEventsTimeout 继续——紧凑清空会架空 §4.2 的公平性让步。
	g.uiLoop.DrainTurn()
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
	if g.blit != nil {
		g.blit.destroy()
		g.blit = nil
	}
	if g.win != nil {
		g.win.Destroy()
	}
	glfw.Terminate()
	return nil
}

// Config 实现 gxapp.Window。
// DebugSplitters 返回当前帧里分隔条的真实命中框，供 GUI E2E 脚本按实际坐标
// 拖动——避免脚本写死布局常量，分隔条位置随主题/尺寸/项目宽度变化时仍稳定。
// 调用方必须在 UI 线程（glWindow.Post）调用。
func (g *glWindow) DebugSplitters() []map[string]any {
	g.mu.Lock()
	hits := append([]skia.HitBox(nil), g.hits...)
	g.mu.Unlock()
	out := []map[string]any{}
	for _, h := range hits {
		if h.Node == nil || h.Node.Props == nil {
			continue
		}
		cls, _ := h.Node.Props["class"].(string)
		if cls != "gx-splitter-v" && cls != "gx-splitter-h" {
			continue
		}
		out = append(out, map[string]any{
			"axis": cls[len("gx-splitter-"):],
			"x":    h.X,
			"y":    h.Y,
			"w":    h.W,
			"h":    h.H,
		})
	}
	return out
}

// DebugTestidBoxes 导出当前帧所有带 testid 节点的命中框，供图标渲染 E2E
// 断言：每个图标（btn-folder / stripe-search / ...）都应被绘制、有非零尺寸、
// 落在预期区域。像素层再配合截图采样确认不是空白方框。
func (g *glWindow) DebugTestidBoxes() []map[string]any {
	g.mu.Lock()
	hits := append([]skia.HitBox(nil), g.hits...)
	g.mu.Unlock()
	out := []map[string]any{}
	for _, h := range hits {
		if h.Node == nil || h.Node.Props == nil {
			continue
		}
		tid, _ := h.Node.Props["testid"].(string)
		if tid == "" {
			continue
		}
		out = append(out, map[string]any{
			"testid": tid,
			"x":      h.X,
			"y":      h.Y,
			"w":      h.W,
			"h":      h.H,
		})
	}
	return out
}

// DebugWindowSize 供 E2E 快照读取窗口逻辑尺寸（点），用于断言 resize 生效。
func (g *glWindow) DebugWindowSize() (int, int) {
	w, h := g.win.GetSize()
	return w, h
}

func (g *glWindow) Config() gxapp.WindowConfig { return g.cfg }

// Frames 返回已绘制帧数（观测用）。
func (g *glWindow) Frames() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.frames
}
