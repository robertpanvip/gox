package skia

import (
	"fmt"
	"sync"
	"time"

	sk "github.com/robertpanvip/gox-skia/compat"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// Window 实现 gxapp.Window：一个基于纯 Go Skia 渲染后端
// （github.com/richardwilkes/canvas，经 gox-skia/compat 适配）的离屏光栅后端。
//
// 它不依赖 GLFW / 窗口系统：每帧 Draw 把场景树遍历成一组绘制指令，逐条调用
// 真实 Skia 绘制 API 画到一块 raster canvas。同时保留一份「绘制指令记录」
// （DrawLog）用于无窗口环境下的可验证性——证明场景树确实被正确布局/分派。
//
// 真实 GUI 窗口集成只需把内部 canvas 换成 GrDirectContext 包装的 GL surface
// （见 README「接入真实窗口」），协议面与绘制遍历逻辑完全不变。
//
// 并发：Skia canvas 非并发安全，Draw 用互斥锁串行化。
type Window struct {
	mu     sync.Mutex
	cfg    gxapp.WindowConfig
	canvas sk.Canvas
	width  int
	height int
	frames int
	closed bool

	// fonts 按字号缓存，避免每帧构造字体（Close 时统一释放）。
	fonts map[int]sk.Font

	// log 记录最近一帧的绘制指令，供测试/调试断言（可选观测面，不属于 host 协议）。
	log DrawLog
	// hits 记录最近一帧每个节点的屏幕矩形，供真实窗口后端命中测试。
	hits []HitBox
	// anim 是 transition 插值引擎；跨帧持有动画状态。
	anim *Animator
	// cache 跨帧持有布局结果/内容快照，实现"无变化不重算 + 脏矩形局部重绘"。
	cache FrameCache

	// 焦点目标：Skia 是离屏光栅后端，没有平台控件，Focuser 记录最近一次
	// 命令式聚焦的 NodeID，由 GUI 集成层经 FocusedID/FocusHandler 消费。
	focusID      gxscene.NodeID
	hasFocus     bool
	focusHandler func(gxscene.NodeID)
}

var (
	_ gxapp.Window  = (*Window)(nil) // 满足 host 后端契约
	_ gxapp.Focuser = (*Window)(nil) // 可选焦点能力：记录焦点目标供 GUI 集成层消费
)

// NewWindow 创建一个 Skia 后端实例。零配置，Open 时按 WindowConfig 分配画布。
// 返回具体 *Window 而非 gxapp.Window，便于调用方访问 DrawLog 等后端特有 API。
func NewWindow() *Window { return &Window{} }

// Open 按配置创建 Skia raster 画布。Backend 提示字段被忽略（本后端就是 skia）。
func (w *Window) Open(cfg gxapp.WindowConfig) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return fmt.Errorf("skia: 已关闭的窗口不能重新 Open")
	}
	if cfg.Width <= 0 {
		cfg.Width = 800
	}
	if cfg.Height <= 0 {
		cfg.Height = 600
	}
	cfg.Backend = "skia"

	canvas := sk.NewCanvasWithDimensions(int32(cfg.Width), int32(cfg.Height), sk.NewSurfaceProps())
	if canvas.IsNil() {
		return fmt.Errorf("skia: 创建 %dx%d raster canvas 失败", cfg.Width, cfg.Height)
	}
	// 初始清成白色，避免未绘制区域是未定义像素。
	canvas.Clear(sk.ColorSetARGB(255, 255, 255, 255))

	if w.canvas.IsNil() == false {
		w.canvas.Delete()
	}
	w.canvas = canvas
	w.width, w.height = cfg.Width, cfg.Height
	w.cfg = cfg
	w.fonts = make(map[int]sk.Font)
	w.log = nil
	w.anim = NewAnimator()
	w.cache = FrameCache{} // 新画布：丢弃上一帧缓存
	return nil
}

// Draw 把当前场景树绘制一帧：布局 → 逐条调用真实 Skia 绘制 API。
// 经帧缓存编排：内容无变化时整帧复用，有变化时只重绘脏矩形区域。
func (w *Window) Draw(root *gxscene.Node) error {
	w.mu.Lock()
	if w.canvas.IsNil() {
		w.mu.Unlock()
		return fmt.Errorf("skia: 未 Open，无法 Draw")
	}
	w.log = nil
	w.hits = nil
	if root == nil {
		w.mu.Unlock()
		return nil
	}

	// transition + animation：应用本帧动画覆盖（临时写 props），绘制后恢复，避免
	// 污染 host 树。快照在 apply 之后拍——动画值随时间变化会自然触发重绘，不会
	// 误命中缓存。animation 后于 transition 应用、先于其恢复：周期动画（blink）
	// 对 opacity 的覆盖优先于过渡插值，与 CSS 中 animation 优先于 transition 一致。
	now := time.Now()
	restoreAnim := applyAnimations(root, now)
	var restoreTrans func()
	if w.anim != nil {
		restoreTrans = w.anim.apply(root, now)
	}
	w.log, w.hits = renderCached(&w.cache, w.canvas, root, float32(w.width), float32(w.height), w.fonts)
	if restoreTrans != nil {
		restoreTrans()
	}
	restoreAnim()
	w.frames++
	// 快照回流数据，锁外投递：PublishMeasured 的回调会写 signal → 触发重绘，
	// 若在锁内执行会重入 Draw 自锁（InlineDispatcher 同步执行）。
	measured := append([]gxscene.Measured(nil), w.cache.lastMeasured...)
	w.mu.Unlock()

	gxscene.PublishMeasured(measured)
	return nil
}

// Run 是 headless 后端：无事件循环，立即返回。
// 真实窗口后端在此驱动事件循环（命中测试→Node.Dispatch→signal 写入→host 自动重绘），
// 该职责完全在后端侧——host 的 Window 契约里没有事件方法，这正是解耦的体现。
func (w *Window) Run() error { return nil }

// Close 释放 Skia 资源。
func (w *Window) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	for _, f := range w.fonts {
		f.Delete()
	}
	w.fonts = nil
	if w.canvas.IsNil() == false {
		w.canvas.Delete()
	}
	w.closed = true
	return nil
}

// Config 返回实际生效的配置。
func (w *Window) Config() gxapp.WindowConfig {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cfg
}

// Frames 返回已绘制帧数（可观测指标，便于验证响应式重绘触发）。
func (w *Window) Frames() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.frames
}

// DrawLog 返回最近一帧的绘制指令记录（观测面，非 host 协议）。
// 调用方拿到的是副本，可安全读取。
func (w *Window) DrawLog() DrawLog {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append(DrawLog(nil), w.log...)
}

// Hits 返回最近一帧的命中矩形副本（观测面，非 host 协议）。
// 真实窗口后端据此把鼠标坐标映射到场景节点，再调用 Node.Dispatch 分发事件。
func (w *Window) Hits() []HitBox {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]HitBox(nil), w.hits...)
}

// HitTest 返回覆盖屏幕点 (x, y) 的最上层节点（后绘制者优先）。
// 命中矩形由最近一帧 Draw 记录；无命中返回 nil。
func (w *Window) HitTest(x, y float32) *gxscene.Node {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := len(w.hits) - 1; i >= 0; i-- {
		if w.hits[i].Contains(x, y) {
			return w.hits[i].Node
		}
	}
	return nil
}

// Focus 实现 gxapp.Focuser：记录 ref 句柄请求聚焦的节点，并通知集成层。
// Skia 本身是离屏光栅后端、没有平台控件，本方法不持有第二套组件焦点状态；
// 真实窗口集成层通过 FocusHandler 或 FocusedID 拿到焦点意图，按所用窗口
// 系统把键盘焦点落到原生输入框。回调在锁外执行，避免重入 Draw 自锁。
func (w *Window) Focus(id gxscene.NodeID) {
	w.mu.Lock()
	w.focusID, w.hasFocus = id, true
	h := w.focusHandler
	w.mu.Unlock()
	if h != nil {
		h(id)
	}
}

// FocusedID 返回最近一次 Focus 的目标节点；ok=false 表示尚未聚焦。
func (w *Window) FocusedID() (gxscene.NodeID, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.focusID, w.hasFocus
}

// SetFocusHandler 注册焦点意图回调，供 GUI 集成层把焦点转发给原生控件。
// 传 nil 清空。未注册时 Focus 仍会记录目标，可由 FocusedID 查询。
func (w *Window) SetFocusHandler(fn func(gxscene.NodeID)) {
	w.mu.Lock()
	w.focusHandler = fn
	w.mu.Unlock()
}

// PaintTo 用本后端一致的 painter 把场景树画到任意 Skia canvas（供真实窗口复用布局/绘制协议）。
// width/height 为像素尺寸；返回本帧命中矩形。fonts 由调用方长期持有以复用字体缓存。
// 无缓存全量路径；需要帧缓存的真实窗口请用 PaintToCached。
//
// 单位：width/height 是"逻辑点"（pt）尺寸，与组件里的字号/间距同一坐标系。
// 高 DPI 渲染请用 PaintToScaled——本函数等价于 PaintToScaled(..., 1)。
func PaintTo(canvas sk.Canvas, width, height float32, root *gxscene.Node, fonts map[int]sk.Font) (DrawLog, []HitBox) {
	var log DrawLog
	var hits []HitBox
	if canvas.IsNil() || root == nil {
		return log, hits
	}
	p := &painter{canvas: canvas, width: width, height: height, fonts: fonts, log: &log, hits: &hits}
	restore := applyAnimations(root, time.Now())
	canvas.Clear(sk.ColorSetARGB(255, 255, 255, 255))
	p.paintFrame(root, width, height)
	restore()
	// 回流实测尺寸（无窗口锁，可同步投递）。
	gxscene.PublishMeasured(measuredFromLayout(p.layout))
	return log, hits
}

// PaintToScaled 是支持设备像素比（DPR）的高清绘制入口，供真实 GUI 窗口使用。
//
// 坐标模型（对齐 Apple 的 point 体系）：
//   - 组件里所有尺寸（字号 13、控件高 28、圆角 6……）都是"逻辑点"(pt)；
//   - physW/physH 是 framebuffer 的物理像素尺寸 = 逻辑尺寸 × dpr；
//   - 布局在逻辑坐标系进行（availW=physW/dpr, availH=physH/dpr），所以一个
//     13pt 的字号在 2x 屏上仍占 13pt 的逻辑宽度，不会变小；
//   - 绘制前把 canvas 缩放 dpr 倍，Skia 会用物理像素光栅化 glyph 与描边，
//     达到 Retina 清晰度（13pt → 26 物理像素）。
//
// 返回的 HitBox 坐标是逻辑点（与 paintFrame 布局一致），因此调用方用 GLFW
// 的窗口坐标（也是逻辑点）即可直接命中，无需再除 dpr。
//
// dpr<=0 时按 1 处理（退化为 PaintTo）。save/restore 成对，不污染调用方 canvas 状态。
func PaintToScaled(canvas sk.Canvas, physW, physH, dpr float32, root *gxscene.Node, fonts map[int]sk.Font) (DrawLog, []HitBox) {
	if dpr <= 0 {
		dpr = 1
	}
	if canvas.IsNil() || root == nil {
		return nil, nil
	}
	logicalW, logicalH := physW/dpr, physH/dpr
	var log DrawLog
	var hits []HitBox
	p := &painter{canvas: canvas, width: logicalW, height: logicalH, fonts: fonts, log: &log, hits: &hits}
	restore := applyAnimations(root, time.Now())
	// Clear 作用于整块 framebuffer，不受 canvas 变换影响，先清再缩放。
	canvas.Clear(sk.ColorSetARGB(255, 255, 255, 255))
	canvas.Save()
	canvas.Scale(dpr, dpr)
	p.paintFrame(root, logicalW, logicalH)
	canvas.Restore()
	restore()
	gxscene.PublishMeasured(measuredFromLayout(p.layout))
	return log, hits
}

// PaintToCached 是带帧缓存的绘制入口，供真实 GUI 窗口（glWindow 等）使用。
// cache 由调用方跨帧持有（通常嵌在窗口结构里）；窗口尺寸变化或重建画布时传新 &FrameCache{}。
// 内容无变化时画布完全不动并返回上帧 log/hits；有变化时只重绘脏矩形区域。
func PaintToCached(cache *FrameCache, canvas sk.Canvas, width, height float32, root *gxscene.Node, fonts map[int]sk.Font) (DrawLog, []HitBox) {
	if canvas.IsNil() || root == nil {
		return nil, nil
	}
	if cache == nil {
		return PaintTo(canvas, width, height, root, fonts)
	}
	// 动画覆盖在 renderCached 之前拍快照，保证 blink 的 opacity 变化进入 props 签名，
	// 不被整帧复用误判为「无变化」；绘制后恢复，不污染常驻树。
	restore := applyAnimations(root, time.Now())
	log, hits := renderCached(cache, canvas, root, width, height, fonts)
	restore()
	// 回流实测尺寸（无窗口锁，可同步投递）。
	gxscene.PublishMeasured(cache.lastMeasured)
	return log, hits
}

// PaintToCachedScaled 是支持设备像素比（DPR）的带缓存绘制入口，供真实 GUI 窗口
// （如桌面 glWindow）在 HiDPI 屏上做像素级脏矩形重绘。
//
// 与 PaintToScaled 同样的坐标模型：组件尺寸是逻辑点，physW/physH 是物理像素，
// 布局在逻辑坐标系进行，绘制前把 canvas 缩放 dpr 倍，用物理像素光栅化。
//
// 契约（与 PaintToCached 不同）：本入口假定调用方**已经确认本帧有变化**（桌面端
// 由 Present 用 Frame.Changes 判定，无变化帧在更上层短路、不会走到这里）。因此
// 内部跳过 O(N) 快照比对，直接布局+算几何脏区+局部重绘：
//   - 几何变化（光标/选区/滚动/尺寸）：脏区小，只重画脏区，收益显著；
//   - 纯文本/颜色变化（脏区为 0）：退化为全量重画，但因省了快照，不会比
//     PaintToScaled 更慢。
//
// 关键点（正确性）：richardwilkes/canvas 的 Clear 填充"当前 clip"（设备坐标），
// 不受 CTM 影响——Save 后的 Scale 不改变 clip，所以全屏 Clear 仍覆盖整块
// framebuffer；而 ClipRect 会被当前 matrix 映射——逻辑坐标脏区经 Scale(dpr) 后
// 自动落到物理像素，局部重绘位置正确。
//
// cache 由调用方跨帧持有；画布尺寸/DPR 变化时必须传新 &FrameCache{}（旧缓存的
// 布局与脏区基于旧尺寸，继续复用会留下残影）。dpr<=0 时按 1 处理。
func PaintToCachedScaled(cache *FrameCache, canvas sk.Canvas, physW, physH, dpr float32, root *gxscene.Node, fonts map[int]sk.Font) (DrawLog, []HitBox) {
	if dpr <= 0 {
		dpr = 1
	}
	if canvas.IsNil() || root == nil {
		return nil, nil
	}
	if cache == nil {
		return PaintToScaled(canvas, physW, physH, dpr, root, fonts)
	}
	logicalW, logicalH := physW/dpr, physH/dpr
	// 声明式动画（blink）按本帧时钟采样 opacity：本入口由 Present 判有 Change 后调用，
	// 内部跳过快照比对（knownChange），故动画值无需进签名，只在绘制期临时覆盖、
	// 绘制后恢复。桌面光标的持续重绘由 glWindow 依据 HasActiveAnimation 维持。
	restore := applyAnimations(root, time.Now())
	canvas.Save()
	canvas.Scale(dpr, dpr)
	log, hits := renderCachedKnownChange(cache, canvas, root, logicalW, logicalH, fonts)
	canvas.Restore()
	restore()
	// 回流实测尺寸（逻辑坐标，与 PaintToScaled 一致）。
	gxscene.PublishMeasured(cache.lastMeasured)
	return log, hits
}
