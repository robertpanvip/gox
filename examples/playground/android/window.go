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
	"log"
	"strconv"
	"strings"
	"sync"

	sk "github.com/robertpanvip/gox-skia/compat"
	skia "github.com/robertpanvip/gox-backend-skia"
	rustbackend "github.com/robertpanvip/gox-backend-rust"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/loop"
	"github.com/robertpanvip/gox-playground/ui"

	"golang.org/x/mobile/event/size"
	"golang.org/x/mobile/event/touch"
	"golang.org/x/mobile/gl"
)

var _ gxapp.Window = (*androidWindow)(nil)
var _ gxapp.FramePresenter = (*androidWindow)(nil)
var _ gxapp.UIExecutor = (*androidWindow)(nil)

// wakePaint 由 main.go 注入（a.Send(paint.Event{})）：Present/Post 置脏后
// 唤醒 paint 循环。桌面端由 GLFW/GTK 的事件循环天然驱动；Android 的
// app.Main 阻塞在事件 channel 上，停帧后必须显式 Send 才能继续渲染。
var wakePaint func()

func androidSetWake(fn func()) { wakePaint = fn }

// wakeIfDirtyLocked 在 dirty 由 false→true 转变时发出 paint 请求，
// 避免重复 Send 堆积事件（paintNow 对非脏帧会短路，但仍有开销）。
func (w *androidWindow) wakeIfDirtyLocked(prev bool) {
	if !prev && w.dirty && wakePaint != nil {
		go wakePaint()
	}
}

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
		uiLoop: loop.New(loop.Config{Wake: func() {
			// UIExecutor.Post 排队后唤醒 paint 循环去 drain，否则停帧期间
			// 排队的 UI 任务永远没人执行。
			if wakePaint != nil {
				go wakePaint()
			}
		}}),
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
	// x/mobile 的 PixelsPerPt 以 72dpi 为基准（pt 语义）；GX/桌面/网页统一 CSS
	// px 语义（160dpi 基准，即 Android densityDpi/160）。换算保证布局密度与
	// 桌面端一致：440dpi 设备上 PixelsPerPt=6.11 → dpr=2.75。
	w.dpr = e.PixelsPerPt * 72 / 160
	if w.dpr <= 0 {
		w.dpr = 1
	}
	if w.opened {
		w.rebuildCanvasLocked()
	}
	// V2：surface 尺寸变化同步渲染器（渲染目标=逻辑尺寸，系统拉伸）。
	if v2Ready() {
		v2Resize(int(float32(w.physW)/w.dpr), int(float32(w.physH)/w.dpr))
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
	v2Destroy() // V2：surface 失效即销毁渲染器，重建走 set_window/new
	w.blit = nil // EGL 上下文随之失效，显示端资源整体废弃
	w.frameCache = skia.FrameCache{}
}

// Draw 实现 gxapp.Window（兼容路径，App 实际走 Present）。
func (w *androidWindow) Draw(root *gxscene.Node) error {
	w.mu.Lock()
	prev := w.dirty
	w.root = root
	w.dirty = true
	w.wakeIfDirtyLocked(prev)
	w.mu.Unlock()
	return nil
}

// Present 实现 gxapp.FramePresenter：与桌面端同语义的帧级短路。
func (w *androidWindow) Present(frame *gxapp.Frame) error {
	if frame == nil {
		return w.Draw(nil)
	}
	w.mu.Lock()
	prev := w.dirty
	w.root = frame.Root
	if !w.painted || frame.FullSync || len(frame.Changes) > 0 {
		if frame.FullSync {
			w.frameCache = skia.FrameCache{}
		}
		w.dirty = true
		w.wakeIfDirtyLocked(prev)
	}
	w.mu.Unlock()
	return nil
}

// paintNow 在 UI 线程（paint 事件回调内）执行绘制。
// V2 路径（rust 渲染内核已就绪）：skia layout 只取命中框（E2E 测试桥），
// GRX3 指令流 → gx_android_apply → gx_android_render（wgpu 直通 present）；
// V2 未就绪（wgpu 初始化失败）回退旧 skia raster + GLES blit。
func (w *androidWindow) paintNow() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.opened || !w.visible || w.root == nil || !w.dirty {
		return
	}

	// V2：渲染器懒初始化（首个 paint 时 surface 已挂载）。
	if !v2Ready() {
		if nw := getNativeWindow(); nw != nil {
			lw, lh := w.logicalSize()
			if err := v2New(nw, lw, lh); err != nil {
				log.Printf("V2 渲染器初始化失败，回退 GLES blit: %v", err)
			}
		}
	}

	if v2Ready() {
		w.paintNowV2()
		return
	}

	// 旧路径：skia CPU raster → GLES blit（回退）。
	if w.blit == nil || w.canvas.IsNil() {
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
	w.dirty = skia.HasActiveAnimation(w.root)
}

// paintNowV2 是 rust 渲染内核路径：layout（skia，供 E2E 命中框）→ GRX3 序列化
// → Rust apply/render。帧与 surface 均用逻辑尺寸（ANativeWindow 由系统拉伸到
// 物理视口；dpr 精细缩放待后续 Renderer scale 支持）。
func (w *androidWindow) paintNowV2() {
	canvas := w.canvas
	if !canvas.IsNil() {
		_, hits := skia.PaintToCachedScaled(&w.frameCache, canvas,
			float32(w.physW), float32(w.physH), w.dpr, w.root, w.fonts)
		w.hits = hits
	}
	lw, lh := w.logicalSize()
	data, err := rustbackend.SerializeFrameBinV3(w.root, uint32(lw), uint32(lh))
	if err != nil {
		log.Printf("V2 序列化失败: %v", err)
		return
	}
	if err := v2Apply(data); err != nil {
		log.Printf("V2 apply 失败: %v", err)
	}
	if err := v2Render(); err != nil {
		log.Printf("V2 render 失败: %v", err)
	}
	w.painted = true
	// V2 present 后 buffer 已交给系统：dirty 语义与旧路径一致（动画续帧）。
	w.dirty = skia.HasActiveAnimation(w.root)
}

func (w *androidWindow) hasPendingPaint() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.dirty
}

// onTouch 把触摸事件映射为桌面端等价的交互：点按 = 鼠标左键点击（命中矩形
// 冒泡派发 onClick + 编辑器定位），编辑器内拖动 = 虚拟滚动。
//
// 坐标契约：x/mobile 的 touch 事件是物理像素，而 hitbox/编辑器命中都是逻辑
// 坐标（PaintToCachedScaled 在 Scale(dpr) 内生成）——必须先除以 dpr，与桌面端
// GLFW 的窗口逻辑坐标一致。
func (w *androidWindow) onTouch(e touch.Event) {
	w.mu.Lock()
	dpr := w.dpr
	if dpr <= 0 {
		dpr = 1
	}
	w.mu.Unlock()
	e.X, e.Y = e.X/dpr, e.Y/dpr
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
			w.mu.Lock()
			hits := append([]skia.HitBox(nil), w.hits...)
			w.mu.Unlock()
			// 增量式：每次以最新触点为基准，delta 与命中检测共用逻辑坐标。
			if _, _, ok := w.editorHitAt(hits, e.X, e.Y); ok {
				// 编辑器内拖动 → 虚拟滚动（1 逻辑点 ≈ 1/22 行）。
				ui.EditorScrollLines(float64(-dy / 22))
			} else if !w.dragScrollAt(e.X, e.Y, dy) {
				// 编辑器外无可滚动容器：忽略（紧凑布局外层 overflow hidden）。
			}
			w.mu.Lock()
			w.touchStart = e
			w.mu.Unlock()
		}
	case touch.TypeEnd:
		w.mu.Lock()
		startPos := w.touchStart
		wasActive := w.touchActive
		w.touchActive = false
		w.mu.Unlock()
		if wasActive {
			// 明显的拖动手势（>20 逻辑点）视为滚动，结束时不做点击定位，
			// 否则 EditorSetSelect 会把拖动滚出来的视口又拉回去。
			dy := e.Y - startPos.Y
			if dy < -20 || dy > 20 {
				return
			}
			w.onTap(e.X, e.Y)
		}
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

// ---- 滚动路由（与桌面端 glwindow.wheelScrollAt 同语义）----
// 拖动点下的 overflow:auto/scroll 容器沿祖先链滚动 scroll-top：
// 编辑器走 EditorScrollLines（编辑逻辑层），其余容器（示例列表等）在此处理。

// dragScrollAt 把拖动增量路由到 (px,py) 下的可滚动容器。delta>0 表示内容
// 向下滚。返回是否找到并滚动了容器。
func (w *androidWindow) dragScrollAt(px, py, delta float32) bool {
	w.mu.Lock()
	hits, root := w.hits, w.root
	w.mu.Unlock()
	if root == nil {
		return false
	}
	var target *gxscene.Node
	var box skia.HitBox
	for i := range hits {
		h := hits[i]
		if h.Node == nil || !h.Contains(px, py) {
			continue
		}
		for n := h.Node; n != nil; n = n.Parent() {
			ov, _ := n.Props["overflow"].(string)
			ov = strings.ToLower(strings.TrimSpace(ov))
			if ov != "auto" && ov != "scroll" {
				continue
			}
			// 编辑器拖动走 EditorScrollLines（编辑逻辑层），不在此处理。
			if cls, _ := n.Props["class"].(string); strings.Contains(cls, "gx-editor") {
				continue
			}
			target, box = n, h
			break
		}
		if target != nil {
			break
		}
	}
	if target == nil {
		return false
	}
	w.mu.Lock()
	cur := propFloat(target.Props["scroll-top"])
	maxScroll := w.contentSize(target, box, hits) - box.H
	next := cur + delta
	if next < 0 {
		next = 0
	}
	if next > maxScroll {
		next = maxScroll
	}
	if next != cur {
		target.Props["scroll-top"] = float64(next)
	}
	w.mu.Unlock()
	return true
}

// contentSize 估算容器的内容总高度：取该容器子树内所有命中框的最大底边
// （命中框坐标已含当前 scroll 偏移，加回 curScroll 得内容高度）。
// 与桌面端 glwindow.contentSize 同源。
func (w *androidWindow) contentSize(container *gxscene.Node, box skia.HitBox, hits []skia.HitBox) float32 {
	sub := map[*gxscene.Node]bool{}
	var walk func(*gxscene.Node)
	walk = func(n *gxscene.Node) {
		sub[n] = true
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(container)
	cur := propFloat(container.Props["scroll-top"])
	maxBottom := box.Y + box.H
	for _, h := range hits {
		if h.Node != nil && sub[h.Node] && h.Y+h.H > maxBottom {
			maxBottom = h.Y + h.H
		}
	}
	return maxBottom - box.Y + cur
}

// propFloat 从节点属性读数值（scroll-top 可能是 float64/int/string）。
func propFloat(v any) float32 {
	switch x := v.(type) {
	case float64:
		return float32(x)
	case float32:
		return x
	case int:
		return float32(x)
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(x), 32)
		return float32(f)
	}
	return 0
}

// editorHit 与桌面端同源：行号取行节点 data-line（真实文档行号），列换算走
// ui.PickColumn 与 caret 渲染共用同一套字符度量。
func (w *androidWindow) editorHit(px, py float32) (int, int, bool) {
	w.mu.Lock()
	hits := append([]skia.HitBox(nil), w.hits...)
	w.mu.Unlock()
	return w.editorHitAt(hits, px, py)
}

// editorHitAt 是 editorHit 的纯函数版本（调用方已持有 hits 快照）。
func (w *androidWindow) editorHitAt(hits []skia.HitBox, px, py float32) (int, int, bool) {
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

// Dpr 返回当前 DPR（E2E 测试桥：逻辑坐标 ↔ 物理点击坐标换算用）。
func (w *androidWindow) Dpr() float32 {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.dpr <= 0 {
		return 1
	}
	return w.dpr
}

// TestElements 导出关键可点元素的逻辑坐标（E2E 测试桥消费）：
// class 命中 gx-sample-row / gx-vk-key / gx-tool-btn 的节点，按其
// data-index / data-key / data-action 属性生成 key → "x y w h" 映射。
func (w *androidWindow) TestElements() string {
	w.mu.Lock()
	hits := append([]skia.HitBox(nil), w.hits...)
	w.mu.Unlock()
	var b strings.Builder
	b.WriteString("{")
	first := true
	add := func(k string, h skia.HitBox) {
		if !first {
			b.WriteString(",")
		}
		first = false
		b.WriteString(strconv.Quote(k))
		b.WriteString(fmt.Sprintf(":\"%d %d %d %d\"", int(h.X), int(h.Y), int(h.W), int(h.H)))
	}
	var firstEditor, lastEditor = -1, -1
	for i := range hits {
		h := hits[i]
		if h.Node == nil || h.Node.Props == nil {
			continue
		}
		cls, _ := h.Node.Props["class"].(string)
		switch {
		case strings.Contains(cls, "gx-sample-row"):
			idx, _ := h.Node.Props["data-index"].(string)
			add("sample-"+idx, h)
		case strings.Contains(cls, "gx-vk-key"):
			key, _ := h.Node.Props["data-key"].(string)
			add("key-"+key, h)
		case strings.Contains(cls, "gx-tool-btn"):
			act, _ := h.Node.Props["data-action"].(string)
			add("tool-"+act, h)
		case strings.Contains(cls, "gx-editor-row"):
			// 编辑器可视行首尾：换算拖动滚动区域用。
			if firstEditor < 0 {
				firstEditor = i
			}
			lastEditor = i
		}
	}
	if firstEditor >= 0 && lastEditor >= 0 {
		add("editor-top", hits[firstEditor])
		add("editor-bottom", hits[lastEditor])
	}
	b.WriteString("}")
	return b.String()
}

func (w *androidWindow) paintErr() error {
	return fmt.Errorf("android window closed")
}
