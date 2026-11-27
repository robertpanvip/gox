//go:build android

package androidbackend

// Window 是 Android 平台的 gxapp.Window / FramePresenter 实现，承接与桌面
// rustbackend.Window 对等的职责：持有场景树、产出帧、管理 surface 与渲染内核。
//
// 平台差异只在事件源：桌面窗口由 Host 主循环 Poll 驱动；Android 的生命周期 /
// size / paint / touch 由 golang.org/x/mobile 的 app.Main 驱动。因此本类型把
// 这些事件以公开方法暴露（OnSurfaceReady / OnSurfaceLost / OnSize / OnPaint /
// OnTouch），由装配层 main.go 转发。装配后与桌面同构：
//
//	handle := gxapp.Use(androidbackend.NewBackend())
//	app, _ := handle.Render(func() any { return ui.Playground() }, cfg)
//	win := handle.WindowOf(app).(*androidbackend.Window)
//	app.SetFrameClock(win.FrameClock())
//
// 编辑器专属的点击/滚动语义不进本包：OnTouch 只做坐标换算与手势阈值判定，
// 命中框经 TouchSink 回调交给上层业务（见 TouchSink）。

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	rustbackend "github.com/robertpanvip/gox-backend-rust"
	skia "github.com/robertpanvip/gox-backend-skia"
	sk "github.com/robertpanvip/gox-skia/compat"
	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
	"github.com/robertpanvip/gox-core/runtime/scheduler"

	"golang.org/x/mobile/event/touch"
	"golang.org/x/mobile/gl"
)

var (
	_ gxapp.Window         = (*Window)(nil)
	_ gxapp.FramePresenter = (*Window)(nil)
	_ scheduler.FrameClock = (*PaintClock)(nil)
)

// TouchSink 是触摸语义回调：本包完成坐标换算（物理→逻辑）与手势阈值判定后，
// 把 tap / drag 交给上层。上层用 Window.Hits() / Window.Root() 自行命中。
type TouchSink interface {
	OnTap(w *Window, px, py float32)
	OnDrag(w *Window, px, py, dy float32) bool // 返回 true 表示已消费（如编辑器滚动）
}

// wakePaint 由装配层注入（a.Send(paint.Event{})）。Post/Draw 标脏后唤醒 paint
// 循环：Android 的 app.Main 停在事件 channel 上，停帧后必须显式 Send。
type wakeFunc func()

type Window struct {
	mu      sync.Mutex
	cfg     gxapp.WindowConfig
	root    *gxscene.Node
	dirty   bool
	painted bool
	opened  bool

	blit *esBlit

	canvas sk.Canvas // 跨帧持久，配合 FrameCache 做脏矩形增量
	physW  int
	physH  int
	dpr    float32

	frameCache skia.FrameCache
	fonts      map[int]sk.Font
	hits       []skia.HitBox

	touchStart  touch.Event
	touchActive bool

	glctx   gl.Context
	visible bool

	clock  *PaintClock
	wake   wakeFunc
	sink   TouchSink
	closed bool
}

// NewWindow 创建窗口实例（时钟在首次 GetFrameClock 时惰性注入 send 回调）。
func NewWindow() *Window {
	return &Window{clock: NewPaintClock(nil)}
}

// ---- gxapp.Window 契约 ----

func (w *Window) Open(cfg gxapp.WindowConfig) error {
	if cfg.Width <= 0 {
		cfg.Width = 1080
	}
	if cfg.Height <= 0 {
		cfg.Height = 720
	}
	w.mu.Lock()
	w.cfg = cfg
	w.fonts = make(map[int]sk.Font)
	w.opened = true
	w.mu.Unlock()
	return nil
}

func (w *Window) Config() gxapp.WindowConfig {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cfg
}

func (w *Window) Draw(root *gxscene.Node) error {
	w.mu.Lock()
	prev := w.dirty
	w.root = root
	w.dirty = true
	w.wakeIfDirtyLocked(prev)
	w.mu.Unlock()
	return nil
}

// Run 是 no-op：Android 事件循环由 app.Main 驱动，不阻塞在 Window 内。
func (w *Window) Run() error { return nil }

func (w *Window) Close() error {
	w.mu.Lock()
	w.opened = false
	w.closed = true
	w.mu.Unlock()
	v2Destroy()
	return nil
}

// Present 实现 gxapp.FramePresenter：与桌面同语义的帧级短路。
func (w *Window) Present(frame *gxapp.Frame) error {
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

func (w *Window) wakeIfDirtyLocked(prev bool) {
	if !prev && w.dirty && w.wake != nil {
		w.wake()
	}
}

// ---- 装配层注入点 ----

// SetWakePaint 注入 paint 事件发送函数（a.Send(paint.Event{})）。
func (w *Window) SetWakePaint(fn func()) {
	w.mu.Lock()
	w.wake = fn
	w.mu.Unlock()
	w.clock.SetSend(fn)
}

// FrameClock 返回统一帧时钟，装配层传给 app.SetFrameClock。
func (w *Window) FrameClock() scheduler.FrameClock { return w.clock }

// Clock 返回具体时钟类型（供装配层 Pending 诊断）。
func (w *Window) Clock() *PaintClock { return w.clock }

// SetTouchSink 注入触摸语义回调（编辑器定位/滚动/点击冒泡由上层实现）。
func (w *Window) SetTouchSink(s TouchSink) {
	w.mu.Lock()
	w.sink = s
	w.mu.Unlock()
}

// ---- 平台事件注入（由 app.Main 转发）----

// OnSurfaceReady 在 lifecycle CrossOn 调用：EGL 就绪，初始化 GLES blit。
// V2 渲染器在首个 paint（surface 已挂载）时懒初始化，见 paintNow。
func (w *Window) OnSurfaceReady(glctx gl.Context) error {
	w.mu.Lock()
	w.glctx = glctx
	w.visible = true
	var err error
	if w.blit == nil {
		w.blit, err = newESBlit(glctx)
		if err == nil && w.canvas.IsNil() && w.physW > 0 {
			w.rebuildCanvasLocked()
		}
	}
	w.mu.Unlock()
	return err
}

// OnSurfaceLost 在 lifecycle CrossOff 调用：销毁渲染器与显示端资源。
func (w *Window) OnSurfaceLost() {
	w.mu.Lock()
	w.visible = false
	w.painted = false
	w.blit = nil
	w.frameCache = skia.FrameCache{}
	w.mu.Unlock()
	v2Destroy()
}

// OnSize 在 size.Event 到达时调用：记录物理像素与 DPR，重建画布。
func (w *Window) OnSize(physW, physH int, dpr float32) {
	if physW <= 0 || physH <= 0 {
		return
	}
	if dpr <= 0 {
		dpr = 1
	}
	w.mu.Lock()
	w.physW, w.physH, w.dpr = physW, physH, dpr
	if w.opened {
		w.rebuildCanvasLocked()
	}
	lw, lh := int(float32(physW)/dpr), int(float32(physH)/dpr)
	w.mu.Unlock()
	if v2Ready() {
		v2Resize(lw, lh)
	}
}

func (w *Window) rebuildCanvasLocked() {
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
	w.frameCache = skia.FrameCache{}
	w.dirty = true
}

// OnPaint 在 paint.Event 回调内调用：先派发帧时钟（Scheduler dirty 门控），
// 再执行实际绘制。返回是否需要调用 a.Publish()（仅 GLES 回退路径需要）。
func (w *Window) OnPaint() bool {
	w.clock.emit()
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.opened || !w.visible || w.root == nil || !w.dirty {
		return false
	}

	// V2 渲染器懒初始化（首个 paint 时 surface 已挂载）。
	if !v2Ready() {
		if nw := nativeWindow(); nw != nil {
			lw, lh := w.logicalSizeLocked()
			if err := v2New(nw, lw, lh); err != nil {
				log.Printf("[androidbackend] V2 初始化失败，回退 GLES blit: %v", err)
			}
		}
	}
	if v2Ready() {
		w.paintV2Locked()
		return false // wgpu 直接 present 到 ANativeWindow，无需 a.Publish
	}

	// 回退路径：skia CPU raster → GLES blit。
	if w.blit == nil || w.canvas.IsNil() {
		return false
	}
	_, hits := skia.PaintToCachedScaled(&w.frameCache, w.canvas,
		float32(w.physW), float32(w.physH), w.dpr, w.root, w.fonts)
	w.hits = hits
	if pix := w.canvas.Pixmap(); pix != nil {
		w.blit.upload(w.physW, w.physH, pix.Pix)
		w.blit.blit()
	}
	w.painted = true
	w.dirty = skia.HasActiveAnimation(w.root)
	return true
}

// paintV2Locked 是 Rust 渲染内核路径：layout（skia，供命中框）→ GRX3 序列化
// → apply/render。帧与 surface 用逻辑尺寸（系统拉伸到物理视口）。
func (w *Window) paintV2Locked() {
	if !w.canvas.IsNil() {
		_, hits := skia.PaintToCachedScaled(&w.frameCache, w.canvas,
			float32(w.physW), float32(w.physH), w.dpr, w.root, w.fonts)
		w.hits = hits
	}
	lw, lh := w.logicalSizeLocked()
	data, err := rustbackend.SerializeFrameBinV3(w.root, uint32(lw), uint32(lh))
	if err != nil {
		log.Printf("[androidbackend] V2 序列化失败: %v", err)
		return
	}
	if err := v2Apply(data); err != nil {
		log.Printf("[androidbackend] V2 apply 失败: %v", err)
	}
	if err := v2Render(); err != nil {
		log.Printf("[androidbackend] V2 render 失败: %v", err)
	}
	w.painted = true
	w.dirty = skia.HasActiveAnimation(w.root)
}

// HasPendingPaint 报告是否有未消费的绘制请求（装配层用于续帧兜底）。
func (w *Window) HasPendingPaint() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.dirty
}

// OnTouch 把 x/mobile 触摸事件换算为逻辑坐标，做手势阈值判定后回调 TouchSink。
// 坐标契约：x/mobile touch 是物理像素，hitbox/编辑器命中是逻辑坐标。
func (w *Window) OnTouch(e touch.Event) {
	w.mu.Lock()
	dpr := w.dpr
	sink := w.sink
	w.mu.Unlock()
	if dpr <= 0 {
		dpr = 1
	}
	e.X, e.Y = e.X/dpr, e.Y/dpr
	switch e.Type {
	case touch.TypeBegin:
		w.mu.Lock()
		w.touchStart, w.touchActive = e, true
		w.mu.Unlock()
	case touch.TypeMove:
		w.mu.Lock()
		start, active := w.touchStart, w.touchActive
		w.mu.Unlock()
		if !active || sink == nil {
			return
		}
		dy := e.Y - start.Y
		if dy < -8 || dy > 8 {
			sink.OnDrag(w, e.X, e.Y, dy)
			w.mu.Lock()
			w.touchStart = e
			w.mu.Unlock()
		}
	case touch.TypeEnd:
		w.mu.Lock()
		start, active := w.touchStart, w.touchActive
		w.touchActive = false
		w.mu.Unlock()
		if active && sink != nil {
			dy := e.Y - start.Y
			if dy < -20 || dy > 20 {
				return // 明显拖动手势，结束时不做点击
			}
			sink.OnTap(w, e.X, e.Y)
		}
	}
}

// ---- 供 TouchSink / 装配层查询 ----

// Hits 返回最近一帧命中框快照（逻辑坐标）。
func (w *Window) Hits() []skia.HitBox {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]skia.HitBox(nil), w.hits...)
}

// Root 返回当前场景树根节点。
func (w *Window) Root() *gxscene.Node {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.root
}

// Dpr / LogicalSize 供坐标换算与视口布局使用。
func (w *Window) Dpr() float32 {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.dpr <= 0 {
		return 1
	}
	return w.dpr
}

func (w *Window) LogicalSize() (int, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.logicalSizeLocked()
}

func (w *Window) logicalSizeLocked() (int, int) {
	if w.physW <= 0 || w.dpr <= 0 {
		return 1080, 720
	}
	return int(float32(w.physW) / w.dpr), int(float32(w.physH) / w.dpr)
}

// EditorRowAt 命中 (px,py) 下的编辑器可视行，返回真实行号（data-line）与
// 相对行首的 x 偏移。列换算交给业务层（ui.PickColumn）——依赖编辑器字符度量，
// 不进能力包。ok=false 表示不在编辑器行内。
func (w *Window) EditorRowAt(px, py float32) (line int, relX float64, ok bool) {
	return editorRowAt(w.Hits(), px, py)
}

// EditorRowAtHits 是 EditorRowAt 的快照版本（调用方已持有 hits）。
func (w *Window) EditorRowAtHits(hits []skia.HitBox, px, py float32) (int, float64, bool) {
	return editorRowAt(hits, px, py)
}

func editorRowAt(hits []skia.HitBox, px, py float32) (int, float64, bool) {
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
		return line, float64(px) - (float64(h.X) + 50), true
	}
	return 0, 0, false
}

// DragScrollAt 通用滚动路由：把拖动增量交给 (px,py) 下 overflow:auto/scroll
// 容器的 scroll-top。编辑器走上层 TouchSink，故这里跳过 gx-editor 类容器。
func (w *Window) DragScrollAt(px, py, delta float32) bool {
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
	maxScroll := w.contentSizeLocked(target, box, hits) - box.H
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

func (w *Window) contentSizeLocked(container *gxscene.Node, box skia.HitBox, hits []skia.HitBox) float32 {
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

// TestElements 导出关键可点元素的逻辑坐标（E2E 测试桥消费）。
func (w *Window) TestElements() string {
	hits := w.Hits()
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
		}
	}
	b.WriteString("}")
	return b.String()
}
