package main

// androidWindow —— GX Playground 的 Android 窗口适配层。
//
// 与桌面端（glwindow.go）同一架构：CPU raster 绘制 → GL 纹理上传 → 全屏 blit。
// 差异只在两端：
//   - 窗口/事件：GLFW → golang.org/x/mobile（NativeActivity + EGL），事件由
//     main.go 的 app.Main 循环驱动，本层只提供 gxapp.Window/FramePresenter/
//     UIExecutor 的实现与 paint/touch 处理函数；
//   - GL：桌面 OpenGL 3.3 core → OpenGL ES 2.0（blit_es.go）。
//
// 业务代码零改动：ui.Playground()、编辑器命令（Editor*）、示例切换、Run 解释
// 全部与桌面/网页共用（"一份 GX 代码多端"的验证场景）。
//
// 已知限制（可行性演示版）：无软键盘（文本编辑需接入 Android IME）、
// 拖动滚动只覆盖编辑器区域。

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	sk "github.com/robertpanvip/gox-skia/compat"
	skia "github.com/robertpanvip/gox-backend-skia"
	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
	"github.com/robertpanvip/gox-core/runtime/loop"
	"github.com/robertpanvip/gox-playground/ui"

	"golang.org/x/mobile/event/size"
	"golang.org/x/mobile/event/touch"
	"golang.org/x/mobile/gl"
)

var _ gxapp.Window = (*androidWindow)(nil)
var _ gxapp.FramePresenter = (*androidWindow)(nil)
var _ gxapp.UIExecutor = (*androidWindow)(nil)

type androidWindow struct {
	mu     sync.Mutex
	cfg    gxapp.WindowConfig
	root   *gxscene.Node
	dirty  bool
	painted bool
	opened bool

	uiLoop *loop.Queue
	blit   *esBlit

	// CPU raster 画布（跨帧持久，配合 FrameCache 做脏矩形增量绘制）。
	canvas sk.Canvas
	physW  int
	physH  int
	dpr    float32

	frameCache skia.FrameCache
	fonts      map[int]sk.Font
	hits       []skia.HitBox

	// 触摸拖动（编辑器滚动）状态。
	touchStart  touch.Event
	touchActive bool

	glctx gl.Context // EGL 上下文，lifecycle StageVisible 期间有效
	visible bool
}

func newAndroidWindow() *androidWindow {
	return &androidWindow{
		uiLoop: loop.New(loop.Config{Wake: func() { /* Android 下由 paint 循环 drain，无需跨线程唤醒 */ }}),
	}
}

// Post 实现 gxapp.UIExecutor：UI 任务进共享 Loop 队列，paint 循环里 drain。
func (w *androidWindow) Post(fn func()) error {
	if w.uiLoop == nil {
		return loop.ErrClosed
	}
	return w.uiLoop.Post(fn)
}

func (w *androidWindow) drainLoop() {
	if w.uiLoop != nil {
		w.uiLoop.DrainTurn()
	}
}

// Open 实现 gxapp.Window：Android 下窗口由 NativeActivity 托管，这里只做
// 尺寸兜底与显示端初始化（GL 调用要求在 paint 事件内——首次 paint 时才创建
// blit，见 ensureGL）。Open 可能在 GL 可用前被 OpenWith 调用，所以 GL 初始化
// 延迟到 ensureGL。
func (w *androidWindow) Open(cfg gxapp.WindowConfig) error {
	if cfg.Width <= 0 {
		cfg.Width = 1080
	}
	if cfg.Height <= 0 {
		cfg.Height = 720
	}
	w.cfg = cfg
	w.fonts = make(map[int]sk.Font)
	w.opened = true
	return nil
}

func (w *androidWindow) Config() gxapp.WindowConfig { return w.cfg }

func (w *androidWindow) Close() error {
	w.mu.Lock()
	w.opened = false
	w.mu.Unlock()
	return nil
}

// Run 实现 gxapp.Window：Android 的事件循环由 app.Main 驱动（main.go），
// 这里阻塞等待 App.Close 的退出信号即可（在独立 goroutine 里被 gxapp.Run 调用）。
func (w *androidWindow) Run() error {
	select {}
}

// resize 由 main.go 的 size.Event 调用：记录物理像素与 DPR，重建画布。
func (w *androidWindow) resize(e size.Event) {
	if e.WidthPx <= 0 || e.HeightPx <= 0 {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.physW, w.physH = e.WidthPx, e.HeightPx
	w.dpr = e.PixelsPerPt
	if w.dpr <= 0 {
		w.dpr = 1
	}
	if w.opened {
		w.rebuildCanvasLocked()
	}
}

func (w *androidWindow) rebuildCanvasLocked() {
	if !w.canvas.IsNil() {
		w.canvas.Delete()
	}
	if w.physW <= 0 || w.physH <= 0 {
		return
	}
	w.canvas = sk.NewCanvasWithDimensions(int32(w.physW), int32(w.physH), sk.NewSurfaceProps())
	if w.blit != nil {
		w.blit.resize(w.physW, w.physH)
	}
	// 布局与脏区缓存基于旧尺寸，必须丢弃。
	w.frameCache = skia.FrameCache{}
	w.dirty = true
}

// ensureGL 在 lifecycle StageVisible 起点调用（EGL 上下文就绪）。
func (w *androidWindow) ensureGL(glctx gl.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.glctx = glctx
	w.visible = true
	if w.blit != nil {
		return nil
	}
	b, err := newESBlit(glctx)
	if err != nil {
		return err
	}
	w.blit = b
	// size.Event 可能先于首次可见到达：此时补建画布。
	if w.canvas.IsNil() && w.physW > 0 {
		w.rebuildCanvasLocked()
	}
	return nil
}

// suspendGL 在 StageVisible 结束时调用：GL 上下文即将失效，显示端随之停摆。
func (w *androidWindow) suspendGL() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.visible = false
	w.painted = false
	w.blit = nil // EGL 上下文随之失效，显示端资源整体废弃
	w.frameCache = skia.FrameCache{}
}

// Draw 实现 gxapp.Window（兼容路径，App 实际走 Present）。
func (w *androidWindow) Draw(root *gxscene.Node) error {
	w.mu.Lock()
	w.root = root
	w.dirty = true
	w.mu.Unlock()
	return nil
}

// Present 实现 gxapp.FramePresenter：与桌面端同语义的帧级短路。
func (w *androidWindow) Present(frame *gxapp.Frame) error {
	if frame == nil {
		return w.Draw(nil)
	}
	w.mu.Lock()
	w.root = frame.Root
	if !w.painted || frame.FullSync || len(frame.Changes) > 0 {
		if frame.FullSync {
			w.frameCache = skia.FrameCache{}
		}
		w.dirty = true
	}
	w.mu.Unlock()
	return nil
}

// paintNow 在 GL 线程（paint 事件回调内）执行绘制：CPU raster → 纹理上传 → blit。
func (w *androidWindow) paintNow() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.opened || !w.visible || w.blit == nil || w.canvas.IsNil() || !w.dirty {
		return
	}
	canvas := w.canvas
	_, hits := skia.PaintToCachedScaled(&w.frameCache, canvas,
		float32(w.physW), float32(w.physH), w.dpr, w.root, w.fonts)
	w.hits = hits
	if pix := canvas.Pixmap(); pix != nil {
		w.blit.upload(w.physW, w.physH, pix.Pix)
		w.blit.blit()
	}
	w.painted = true
	// 声明式动画（光标 blink）不产生 frame.Changes：保持 dirty 让主循环续帧。
	w.dirty = skia.HasActiveAnimation(w.root)
}

func (w *androidWindow) hasPendingPaint() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.dirty
}

// onTouch 把触摸事件映射为桌面端等价的交互：点按 = 鼠标左键点击（命中矩形
// 冒泡派发 onClick + 编辑器定位），编辑器内拖动 = 虚拟滚动。
func (w *androidWindow) onTouch(e touch.Event) {
	switch e.Type {
	case touch.TypeBegin:
		w.mu.Lock()
		w.touchStart, w.touchActive = e, true
		w.mu.Unlock()
	case touch.TypeMove:
		w.mu.Lock()
		start := w.touchStart
		w.mu.Unlock()
		if !w.touchActive {
			return
		}
		dy := e.Y - start.Y
		if dy < -8 || dy > 8 {
			// 编辑器内拖动 → 虚拟滚动（1 逻辑点 ≈ 0.2 行）；行容器滚动交给
			// 编辑器自身的受控 scrollTop。
			if _, _, ok := w.editorHit(e.X, e.Y); ok {
				ui.EditorScrollLines(float64(-dy / 5))
				w.mu.Lock()
				w.touchStart = e // 增量式：每次以最新触点为基准
				w.mu.Unlock()
			}
		}
	case touch.TypeEnd:
		w.mu.Lock()
		w.touchActive = false
		w.mu.Unlock()
		w.onTap(e.X, e.Y)
	}
}

// onTap 等价桌面端 onMouseButton(Press)：编辑器行定位 + 命中矩形冒泡 onClick。
func (w *androidWindow) onTap(px, py float32) {
	if line, col, ok := w.editorHit(px, py); ok {
		ui.EditorFocus()
		ui.EditorSetSelect(line, col)
	}
	w.mu.Lock()
	root, hits := w.root, append([]skia.HitBox(nil), w.hits...)
	w.mu.Unlock()
	if root == nil {
		return
	}
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
		ui.EditorFocus()
		node.DispatchBubble("onClick", gxscene.EventContext{
			Raw: "android-touch",
			Synthetic: &gxscene.SyntheticEvent{
				Type:          gxscene.EvClick,
				NativeName:    "onClick",
				Target:        node,
				CurrentTarget: node,
				X:             float64(px),
				Y:             float64(py),
			},
		})
	}
}

// editorHit 与桌面端同源：行号取行节点 data-line（真实文档行号），列换算走
// ui.PickColumn 与 caret 渲染共用同一套字符度量。
func (w *androidWindow) editorHit(px, py float32) (int, int, bool) {
	w.mu.Lock()
	hits := append([]skia.HitBox(nil), w.hits...)
	w.mu.Unlock()
	for i := range hits {
		h := hits[i]
		if h.Node == nil || h.Node.Props == nil || h.H <= 0 {
			continue
		}
		if cls, _ := h.Node.Props["class"].(string); !strings.Contains(cls, "gx-editor-row") {
			continue
		}
		if py < h.Y || py >= h.Y+h.H || px < h.X {
			continue
		}
		line := 0
		if s, ok := h.Node.Props["data-line"].(string); ok {
			if n, err := strconv.Atoi(s); err == nil {
				line = n
			}
		}
		relX := float64(px) - (float64(h.X) + 50)
		return line, ui.PickColumn(line, relX), true
	}
	return 0, 0, false
}

// logicalSize 返回当前窗口的逻辑尺寸（DPR 折算后），供 OpenWith 配置。
func (w *androidWindow) logicalSize() (int, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.physW <= 0 || w.dpr <= 0 {
		return 1080, 720
	}
	return int(float32(w.physW) / w.dpr), int(float32(w.physH) / w.dpr)
}

func (w *androidWindow) paintErr() error {
	return fmt.Errorf("android window closed")
}
