package skia

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	sk "github.com/robertpanvip/gox-skia/compat"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// DrawOp 是一条被记录下来的绘制指令（观测面，非 host 协议）。
// 它让无窗口环境也能断言"场景树被正确遍历与分派"，与真实 Skia 调用一一对应。
type DrawOp struct {
	Kind string  // "rect" / "text" / "fragment"
	Name string  // 元素 Kind（div/span/...）或文本内容
	X, Y float32 // 绘制起点
	W, H float32 // 尺寸（text 时 W/H 为估算）
	ARGB uint32  // 颜色
}

func (o DrawOp) String() string {
	return fmt.Sprintf("%s %q @%.0f,%.0f %.0fx%.0f #%08x", o.Kind, o.Name, o.X, o.Y, o.W, o.H, o.ARGB)
}

// DrawLog 是一帧的绘制指令序列。
type DrawLog []DrawOp

// Find 返回匹配 kind 且名字包含 substr 的指令数量，便于测试精确断言。
func (l DrawLog) Find(kind, substr string) int {
	n := 0
	for _, op := range l {
		if op.Kind == kind && (substr == "" || contains(op.Name, substr)) {
			n++
		}
	}
	return n
}

func contains(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// painter 把 gxscene.Node 树渲染到 Skia canvas。
// 布局由 Yoga 3.x 在 computeLayout 中完成：先把场景树映射成 flex 节点树算出
// 每个节点的绝对矩形，再交给 painter 按矩形逐个调用 Skia 绘制原语。布局完全属于
// 后端表现层，host 与核心运行时对 Yoga 零感知。
type painter struct {
	canvas sk.Canvas
	width  float32
	height float32
	// fonts 仅为兼容 PaintTo 的对外签名而保留：字体现由 font.go 的进程级缓存
	// 统一解析（含 CJK fallback），不再从本 map 取字体。调用方仍可持有/释放它。
	fonts map[int]sk.Font
	log   *DrawLog
	// hits 非 nil 时记录每个已绘制节点的屏幕矩形，供真实窗口后端做命中测试。
	// 离屏后端不设置它，因此零开销、零行为变化。
	hits   *[]HitBox
	layout layoutCache // 本帧的布局结果，由 paintFrame 在绘制前一次性算出
	// clip 非 nil 时表示当前只绘制该脏区：不相交的节点跳过 Skia 调用（log/hits 仍记录）。
	// nil 表示全量绘制。由 renderCached 的脏矩形局部重绘路径设置。
	clip *dirtyRect
}

// shouldDraw 报告节点的绘制矩形是否与当前脏区相交。nil clip 恒为 true。
// 阴影/描边的绘制可能超出 box，检查时按 2px 容差外扩，避免边缘残影。
func (p *painter) shouldDraw(box layoutBox) bool {
	if p.clip == nil {
		return true
	}
	r := boxToRect(box)
	const pad float32 = 2
	r = dirtyRect{r.x - pad, r.y - pad, r.w + 2*pad, r.h + 2*pad}
	return r.w > 0 && r.h > 0 && r.intersects(*p.clip) && (!box.HasClip || dirtyRect{box.ClipX, box.ClipY, box.ClipW, box.ClipH}.intersects(*p.clip))
}

// HitBox 是一个节点在最近一帧中的屏幕矩形，供事件命中测试使用。
// 它由 painter 在绘制同帧时记录，坐标与 Skia canvas 一致（左上角原点）。
// Clip 非零盒表示该节点受祖先 overflow 裁剪（坐标已含滚动偏移）。
type HitBox struct {
	Node         *gxscene.Node
	X, Y         float32
	W, H         float32
	ClipX, ClipY float32
	ClipW, ClipH float32
	HasClip      bool
	// cursor 属性值（""/default/pointer/text/wait...）：真实窗口集成层据此
	// 把系统光标切换为对应形状（消费 Web 端 cursor:xxx 样式）。
	Cursor string
}

// Contains 报告屏幕点 (px,py) 是否落在该矩形内（含祖先裁剪检查）。
func (b HitBox) Contains(px, py float32) bool {
	if px < b.X || px > b.X+b.W || py < b.Y || py > b.Y+b.H {
		return false
	}
	if b.HasClip && (px < b.ClipX || px > b.ClipX+b.ClipW || py < b.ClipY || py > b.ClipY+b.ClipH) {
		return false
	}
	return true
}

func hitFrom(n *gxscene.Node, box layoutBox) HitBox {
	return HitBox{
		Node: n, X: box.X, Y: box.Y, W: box.W, H: box.H,
		ClipX: box.ClipX, ClipY: box.ClipY, ClipW: box.ClipW, ClipH: box.ClipH,
		HasClip: box.HasClip,
		Cursor:  cursorOf(n.Props),
	}
}

// hitAppend 把节点登记进命中矩形列表；pointer-events:none 的节点自身跳过
// （仍绘制，后代节点照常登记——与 CSS 命中语义一致）。
func (p *painter) hitAppend(n *gxscene.Node, box layoutBox) {
	if p.hits == nil || pointerEventsNone(n.Props) {
		return
	}
	*p.hits = append(*p.hits, hitFrom(n, box))
}

// paintFrame 为一帧做布局并绘制整棵树。入口（Window.Draw / PaintTo）调用它。
// 这是无缓存的全量路径，保留给不需要帧缓存的直接调用方与测试。
func (p *painter) paintFrame(root *gxscene.Node, availW, availH float32) {
	if root == nil {
		return
	}
	// 两阶段第一步：Yoga 布局，算出每个节点的绝对矩形。
	p.layout = computeLayout(root, availW, availH, skiaFontMeasurer())
	p.paintWithLayout(root)
}

// paintWithLayout 仅按 p.layout 绘制整棵树（不再重算布局）。
// 供帧缓存路径复用：布局已在外部算好，这里只做绘制 + 浮层补画。
func (p *painter) paintWithLayout(root *gxscene.Node) {
	if root == nil {
		return
	}
	// video 节点：确保解码器与当前树同步（src/playing 变化重建，消失的清理）。
	// key 未变时是廉价的树遍历，多次调用（脏区分遍）无副作用。
	videos.syncFromTree(root)
	// 主树：按矩形逐个调用 Skia 绘制原语。
	p.paintTree(root, defaultTextStyle())
	// 浮层：按"普通内容 → 遮罩 → 浮层"补画，保证层级正确。
	p.paintOverlays(root, defaultTextStyle())
}

// paintOverlays 分两层绘制：先所有遮罩，再所有浮层（多个浮层按树序，后者在上）。
func (p *painter) paintOverlays(root *gxscene.Node, inherited textStyle) {
	var overlays []*gxscene.Node
	collectOverlays(root, &overlays)
	// 遮罩先画（拦截点击但视觉透明/半透明）。
	for _, ov := range overlays {
		if isScrim(ov) {
			p.paintOverlayTree(ov, inheritedStyle(ov, inherited))
		}
	}
	// 内容浮层（popover/modal/portal）后画，保证盖在遮罩和普通内容之上。
	// 按树序绘制，后出现的浮层在上；portal 常作为最外层全局浮层放在树末尾。
	for _, ov := range overlays {
		if isContentOverlay(ov) {
			p.paintOverlayTree(ov, inheritedStyle(ov, inherited))
		}
	}
}

// paintOverlayTree 绘制一棵浮层子树（复用 paintTree 的遍历，但浮层根本身参与绘制）。
func (p *painter) paintOverlayTree(n *gxscene.Node, inherited textStyle) {
	if n == nil {
		return
	}
	box := p.layout[n]
	switch n.Type {
	case gxscene.NodeFragment:
		eff := inheritedStyle(n, inherited)
		for _, c := range n.Children {
			p.paintOverlayTree(c, eff)
		}
	case gxscene.NodeText:
		p.paintText(n, box, inheritedStyle(n, inherited))
	case gxscene.NodeDiv:
		p.paintElement(n, box)
		eff := inheritedStyle(n, inherited)
		p.paintChildrenClipped(n, box, eff, true)
	}
}

// textStyle 是可继承给文本子节点的样式（颜色/字号/字重）。
// CSS 语义下这些属性写在父元素上、由文本继承；场景树里文本节点 Props 常为空，
// 因此绘制时必须从祖先链继承，否则文字会退化成默认黑色小字。
type textStyle struct {
	color    uint32
	fontSize int
	bold     bool
	family   string // 已解析的本机字体族名（空串=系统默认）
	// text-align（CSS 继承属性）：0=left/start、1=center、2=right/end。
	align int
	// line-height：factor>0 按字号倍数，px>0 按绝对像素；两者都 0 表示未设置
	//（回落 1.3 倍的历史行高）。
	lineHeightFactor float32
	lineHeightPx     float32
	// white-space 原始值（空串=默认逐字节渲染，与 pre 等效）。
	whiteSpace string
}

// lineH 返回以 fontSize 折算后的行盒高度（像素）。
func (st textStyle) lineH() float32 {
	switch {
	case st.lineHeightFactor > 0:
		return float32(st.fontSize) * st.lineHeightFactor
	case st.lineHeightPx > 0:
		return st.lineHeightPx
	default:
		return float32(st.fontSize) * 1.3
	}
}

// alignOffset 计算在宽 w 的行盒里按对齐方式落笔的水平偏移：
// 内容宽 tw 小于盒宽时 center/right 生效，否则 0（与 CSS text-align 一致）。
func alignOffset(align int, w, tw float32) float32 {
	if tw <= 0 || tw >= w {
		return 0
	}
	switch align {
	case 1:
		return (w - tw) / 2
	case 2:
		return w - tw
	default:
		return 0
	}
}

// inheritedStyle 从节点自身 Props 覆盖继承样式，返回本节点有效的文本样式。
// 仅当节点显式设置了对应属性时才覆盖（用 parseStyle 的默认值判断较麻烦，
// 这里直接检查原始 props 里是否存在该键）。
func inheritedStyle(n *gxscene.Node, parent textStyle) textStyle {
	s := parent
	if n != nil && n.Props != nil {
		if _, ok := n.Props[propColor]; ok {
			if v, ok := propString(n.Props[propColor]); ok {
				if c, ok := parseColor(v); ok {
					s.color = c
				}
			}
		}
		if v, ok := propNum(n.Props[propFontSize]); ok && v > 0 {
			s.fontSize = int(v)
		}
		if v, ok := propString(n.Props[propFontFamily]); ok && v != "" {
			s.family = pickFontFamily(v)
		}
		// 字重：font-weight（CSS 标准）优先，weight（仓库历史约定）兜底；
		// 数值 100..900 按 >=600 为粗体，与 Web 语义一致。
		if _, ok := n.Props[propFontWeight]; ok {
			s.bold = weightIsBoldFromProps(n.Props)
		}
		if _, ok := n.Props[propWeight]; ok {
			s.bold = weightIsBoldFromProps(n.Props)
		}
		if v, ok := propString(n.Props[propTextAlign]); ok {
			s.align = textAlignOf(v)
		}
		if v, ok := propString(n.Props[propLineHeight]); ok && strings.TrimSpace(v) != "" {
			if f, px := lineHeightOf(v); f > 0 || px > 0 {
				s.lineHeightFactor, s.lineHeightPx = f, px
			}
		}
		if v, ok := propString(n.Props[propWhiteSpace]); ok {
			s.whiteSpace = v
		}
	}
	return s
}

// richTextStyle 构造富文本/渐变文字用的 textStyle，关键在于带上 family：
// 编辑器代码行声明了 font-family（等宽栈），若漏传 family，splitRuns 会退回
// sans-serif 用比例字体绘制，而测量端 MeasureEditorAdvances 用 pickFontFamily
// 解析出的等宽字体测宽——两端字体不一致，导致 caret 渲染与点击命中（PickColumn）
// 系统性错位（点 <div 的 v 后空格却插到 div 前面）。用 inheritedStyle 从节点
// 自身 font-family prop 解析出族名，与测量端同口径。
func richTextStyle(n *gxscene.Node, color uint32, fontSize int, bold bool) textStyle {
	return inheritedStyle(n, textStyle{color: color, fontSize: fontSize, bold: bold})
}

// defaultTextStyle 是根节点的继承起点（黑字 / 16px / 常规）。
func defaultTextStyle() textStyle {
	d := defaultStyle()
	return textStyle{color: d.color, fontSize: d.fontSize, bold: d.bold}
}

// paintTree 递归绘制节点，坐标从本帧 layout cache 读取（Yoga 已算好）。
// inherited 是从父节点继承来的文本样式。
func (p *painter) paintTree(n *gxscene.Node, inherited textStyle) {
	if n == nil {
		return
	}
	// 浮层/遮罩由 paintOverlays 在最后统一绘制，主树遍历跳过它们以保证层级。
	if isOverlay(n) {
		return
	}
	box := p.layout[n]
	switch n.Type {
	case gxscene.NodeFragment:
		// 透明包装：不画自己，只递归 children。
		*p.log = append(*p.log, DrawOp{Kind: "fragment", Name: n.Kind, X: box.X, Y: box.Y})
		eff := inheritedStyle(n, inherited)
		for _, c := range n.Children {
			p.paintTree(c, eff)
		}
	case gxscene.NodeText:
		p.paintText(n, box, inheritedStyle(n, inherited))
	case gxscene.NodeDiv:
		p.paintElement(n, box)
		eff := inheritedStyle(n, inherited)
		p.paintChildrenClipped(n, box, eff, false)
	default:
		eff := inheritedStyle(n, inherited)
		p.paintChildrenClipped(n, box, eff, false)
	}
}

// paintChildrenClipped 绘制节点的 children；若节点是 overflow 容器，用 canvas
// Save/ClipRect/Restore 把子树绘制裁剪到容器矩形。overlay=true 时用对应的浮动遍历。
func (p *painter) paintChildrenClipped(n *gxscene.Node, box layoutBox, eff textStyle, overlay bool) {
	s := parseStyle(n.Props)
	clipping := n.Type == gxscene.NodeDiv && s.overflow != overflowVisible
	if clipping {
		p.canvas.Save()
		clip := sk.RectMakeXYWH(box.X, box.Y, box.W, maxF(box.H, 1))
		p.canvas.ClipRect(clip, sk.ClipOpIntersect, false)
	}
	// z-index：按值稳定排序绘制顺序（负值垫底、正值置顶，同值保持文档序）。
	// 布局仍按文档流（Yoga 不感知），只重排绘制/命中优先级——与 CSS 近似语义一致。
	children := n.Children
	if len(children) > 1 {
		hasZ := false
		for _, c := range children {
			if parseStyle(c.Props).zIndex != 0 {
				hasZ = true
				break
			}
		}
		if hasZ {
			sorted := make([]*gxscene.Node, len(children))
			copy(sorted, children)
			sort.SliceStable(sorted, func(i, j int) bool {
				return parseStyle(sorted[i].Props).zIndex < parseStyle(sorted[j].Props).zIndex
			})
			children = sorted
		}
	}
	for _, c := range children {
		// 父节点带 rich-text 或用渐变给文字着色时，分段着色已由 paintElement 一次绘制；
		// 跳过纯文本子节点，避免普通单色文字覆盖在上面（同时抵消正确着色）。
		// 普通渐变背景节点不跳过——其文本子节点应正常绘制在渐变背景之上。
		if (isRichText(n) || isGradientText(n)) && (c.Kind == "#text" || c.Type == gxscene.NodeText) {
			continue
		}
		if overlay {
			p.paintOverlayTree(c, eff)
		} else {
			p.paintTree(c, eff)
		}
	}
	if clipping {
		p.canvas.Restore()
	}
}

// isRichText 报告节点是否携带富文本分段着色数据。
func isRichText(n *gxscene.Node) bool {
	if n == nil {
		return false
	}
	rt, ok := gxscene.RichTextFromProps(n.Props)
	return ok && len(rt) > 0
}

// gradientBg 返回节点 bg/background 属性里的 CSS 渐变字符串（若存在）。
// 编辑器的语法高亮以此作为统一着色来源：Web 端 CSS background-clip:text 消费，
// Skia 端在此解析回 RichText 分段绘制。非渐变值返回空串。
func gradientBg(n *gxscene.Node) string {
	if n == nil {
		return ""
	}
	for _, k := range []string{propBg, propBgAlt} {
		if v, ok := n.Props[k].(string); ok && strings.HasPrefix(v, "linear-gradient(") {
			return v
		}
	}
	return ""
}

// isGradientText 报告节点是否用渐变给文字着色（编辑器代码行）。
// 这类节点的 bg=linear-gradient 是逐 token 颜色编码，映射到文字而非画成背景。
// 用显式的 gradient-text 属性标记，避免与普通渐变背景 div 混淆。
func isGradientText(n *gxscene.Node) bool {
	if n == nil {
		return false
	}
	v, ok := n.Props["gradient-text"]
	if !ok {
		return false
	}
	s, _ := v.(string)
	return s == "1" || s == "true"
}

// nodeText 返回节点下第一个非空文本子节点的文本，作为渐变着色的落笔内容。
func nodeText(n *gxscene.Node) string {
	if n == nil {
		return ""
	}
	for _, c := range n.Children {
		if c.Text != "" {
			return c.Text
		}
	}
	return ""
}

// paintElement 按 Yoga 给出的矩形画元素背景，并记录命中矩形。
func (p *painter) paintElement(n *gxscene.Node, box layoutBox) {
	s := parseStyle(n.Props)
	draw := p.shouldDraw(box)
	if draw {
		h := maxF(box.H, 1)
		rect := sk.RectMakeXYWH(box.X, box.Y, box.W, h)
		// box-shadow：在背景之前画多层半透明圆角矩形近似（与 Rust 端同算法）。
		if s.hasShadow {
			p.paintShadow(s.shadow, box, s.radius)
		}
		if s.hasBg {
			paint := sk.NewPaint()
			paint.SetColor(sk.Color(s.bg))
			paint.SetAntiAlias(true)
			// opacity 作用于元素整体（对齐 Web CSS opacity 与 Rust）：声明式动画
			// （blink）和静态 opacity prop 都在 parseStyle 后汇聚到 s.opacity。
			// caret 的方波闪烁即靠此 alpha 在 0/1 间阶跃。
			if a := s.opacity; a >= 0 && a < 1 {
				paint.SetAlpha(uint32(a * 255))
			}
			if r := radiusClamp(s.radius, box.W, h); r > 0 {
				p.canvas.DrawRoundRect(rect, r, r, paint)
			} else {
				p.canvas.DrawRect(rect, paint)
			}
			paint.Delete()
		}
		// 通用 CSS 渐变背景：所有 div 的 bg=linear-gradient(...) 都在此绘制。
		// 带 gradient-text 标记的节点（编辑器代码行）例外——它们的渐变映射到文字，
		// 不在背景绘制，交由下方着色逻辑处理。
		if grad := gradientBg(n); grad != "" && !isGradientText(n) {
			if lg := parseLinearGradient(grad); lg != nil {
				// 原生 shader 优先（一次构造、一次绘制）；构造失败回退 CPU 条带近似。
				if !p.paintGradientBackgroundShader(lg, box, s.radius) {
					p.paintGradientBackground(lg, box, s.radius)
				}
				*p.log = append(*p.log, DrawOp{Kind: "gradient-bg", Name: n.Kind, X: box.X, Y: box.Y, W: box.W, H: box.H, ARGB: s.bg})
			}
		}
		// img 原语：<img src="..."> 在背景之后、描边之前绘制。src 由 runtime/imgutil
		// 解析（data URI / 本地路径 / http），object-fit 控制映射，border-radius 裁剪。
		// 解码结果按 src 缓存，响应式重绘不重复解码（见 image.go）。
		if n.Kind == "img" {
			if src, _ := n.Props["src"].(string); src != "" {
				paintImage(p, n, box, src, s)
			}
		}
		// canvas 原语：命令式绘制指令走 Props["canvas-ops"]（runtime/canvas 协议），
		// 在背景之后解释执行，坐标相对本节点布局盒。
		if n.Kind == "canvas" {
			paintCanvas(p, n, box)
		}
		// video 原语：后端内部 ffmpeg 解码，画当前帧（无帧时画 poster 占位）。
		if n.Kind == "video" {
			paintVideo(p, n, box, s)
		}
		// 语法高亮着色来源（两端统一为 CSS linear-gradient）：
		//   - Web 端由 background-clip:text 原生消费渐变；
		//   - Skia 端在此把渐变解析回 RichText 分段绘制。
		// 兼容仍用 rich-text 属性的旧节点（其它示例可能使用）。
		// 其纯文本子节点会在 paintChildrenClipped 跳过，避免单色文字覆盖正确着色。
		st := richTextStyle(n, s.color, s.fontSize, s.bold)
		var coloredText string
		if rt, ok := gxscene.RichTextFromProps(n.Props); ok && len(rt) > 0 {
			p.paintRichText(n, rt, box, st)
			coloredText = rt.Plain()
		} else if grad := gradientBg(n); grad != "" && isGradientText(n) {
			// 仅编辑器代码行（gradient-text 标记）把渐变映射到文字；
			// 普通 div 的渐变已在上方画成背景，此处不处理。
			txt := nodeText(n)
			// 原生 shader 优先：整段文字共享一份渐变 shader，按字形 canvas 坐标取色，
			// 零 Go 侧拆色分配；构造失败回退到 RichText 分段路径。
			if !p.paintGradientTextShader(grad, txt, box, st) {
				if rt := cachedGradientRich(grad, txt); len(rt) > 0 {
					p.paintRichText(n, rt, box, st)
					coloredText = rt.Plain()
				}
			} else {
				coloredText = txt
			}
		}
		if coloredText != "" {
			*p.log = append(*p.log, DrawOp{Kind: "rich-text", Name: coloredText, X: box.X, Y: box.Y, W: box.W, H: box.H, ARGB: s.color})
		}
		// 描边：用 stroke 样式沿矩形（或圆角矩形）边缘绘制。
		if s.hasBorder && s.borderWidth > 0 {
			paint := sk.NewPaint()
			paint.SetColor(sk.Color(s.border))
			paint.SetStyle(sk.PaintStyleStroke)
			paint.SetStrokeWidth(s.borderWidth)
			paint.SetAntiAlias(true)
			if r := radiusClamp(s.radius, box.W, h); r > 0 {
				p.canvas.DrawRoundRect(rect, r, r, paint)
			} else {
				p.canvas.DrawRect(rect, paint)
			}
			paint.Delete()
		}
	}
	// log/hits 始终记录完整（不受脏区裁剪影响），保证对调用方/命中测试透明。
	if s.hasBg {
		*p.log = append(*p.log, DrawOp{Kind: "rect", Name: n.Kind, X: box.X, Y: box.Y, W: box.W, H: box.H, ARGB: s.bg})
	}
	if p.hits != nil {
		p.hitAppend(n, box)
	}
}

// paintShadow 用多层半透明圆角矩形近似 box-shadow。
// 从最大扩张（spread+blur）到 0 逐层绘制，alpha 沿高斯近似衰减，
// 得到柔和阴影。与 Rust 端 paint_shapes 里的算法一致，保证跨平台观感相同。
func (p *painter) paintShadow(sh boxShadow, box layoutBox, radius float32) {
	if sh.blur <= 0 && sh.spread <= 0 && sh.offsetX == 0 && sh.offsetY == 0 {
		return
	}
	baseA := float32(sh.color >> 24 & 0xff)
	if baseA <= 0 {
		baseA = 90
	}
	const layers = 6
	for i := layers; i >= 1; i-- {
		// 扩张量：外层大、内层小；最终层为 spread。
		t := float32(i) / layers
		expand := sh.spread + sh.blur*t
		x := box.X + sh.offsetX - expand
		y := box.Y + sh.offsetY - expand
		w := box.W + 2*expand
		h := maxF(box.H, 1) + 2*expand
		if w <= 0 || h <= 0 {
			continue
		}
		// alpha：越靠外越淡（近似高斯）；每层贡献 baseA/(layers) 左右。
		a := baseA * (1.0 - t) / float32(layers) * 1.8
		if a < 0 {
			a = 0
		} else if a > 255 {
			a = 255
		}
		paint := sk.NewPaint()
		paint.SetColor(argbColor(uint32(a), sh.color>>16&0xff, sh.color>>8&0xff, sh.color&0xff))
		paint.SetAntiAlias(true)
		r := radius + expand
		clipR := radiusClamp(r, w, h)
		rect := sk.RectMakeXYWH(x, y, w, h)
		if clipR > 0 {
			p.canvas.DrawRoundRect(rect, clipR, clipR, paint)
		} else {
			p.canvas.DrawRect(rect, paint)
		}
		paint.Delete()
	}
	*p.log = append(*p.log, DrawOp{Kind: "shadow", Name: "box-shadow", X: box.X, Y: box.Y})
}

func argbColor(a, r, g, b uint32) sk.Color { return sk.Color(argb(a, r, g, b)) }

// paintText 画文本节点：位置/尺寸由 Yoga 的 measure 回调给出，文字基线按字号定位。
// 样式来自祖先链继承（color/font-size/weight/text-align/line-height/white-space），
// 因为文本节点自身 Props 通常为空。
func (p *painter) paintText(n *gxscene.Node, box layoutBox, st textStyle) {
	if n.Text == "" {
		return
	}
	// white-space:normal/nowrap 折叠空白（CSS 语义）；默认保持逐字节渲染，
	// 编辑器代码行的缩进依赖原样空格。
	text := n.Text
	if whiteSpaceCollapses(st.whiteSpace) {
		text = collapseWhitespace(text)
	}
	// 富文本（语法高亮）：一行的分段着色数据挂在 rich-text 属性上。逐段用各自
	// 颜色绘制，段内再按字体回退切 run；落笔 x 累加 widths，与测量口径一致。
	if rt, ok := gxscene.RichTextFromProps(n.Props); ok && len(rt) > 0 {
		p.paintRichText(n, rt, box, st)
		lineH := box.H
		if lineH <= 0 {
			lineH = st.lineH()
		}
		*p.log = append(*p.log, DrawOp{Kind: "text", Name: n.Text, X: box.X, Y: box.Y, W: box.W, H: lineH, ARGB: st.color})
		p.hitAppend(n, box)
		return
	}
	// dx 是 text-align 的落笔偏移；shouldDraw 为 false（脏区外）时保持 0，
	// 日志/hits 仍按盒坐标记录（与历史行为一致）。
	var dx float32
	if p.shouldDraw(box) {
		paint := sk.NewPaint()
		paint.SetColor(sk.Color(st.color))
		paint.SetAntiAlias(true)
		// 行高：显式 line-height 时基线在行盒内垂直居中（默认 1.3 倍行为不变）。
		lineH := st.lineH()
		baseline := box.Y + float32(st.fontSize) + (lineH-float32(st.fontSize)*1.3)/2

		// white-space:pre 文本可能含换行（终端输出、多行文本）：按 \n 逐行落笔，
		// 行距 lineH。测量端（skiaMeasureFunc）以同样的行数 × 行高计高。
		lines := []string{text}
		if !whiteSpaceCollapses(st.whiteSpace) && strings.Contains(text, "\n") {
			lines = strings.Split(text, "\n")
		}

	// selection 标量协议（三端统一）：纯文本节点可挂 selection-range="s:e"，
	// 配合 &::selection 子表对 [s,e) rune 区间画背景、覆盖前景色。
	selRange := gxscene.SelectionRangeFromProps(n.Props)
	sel := gxscene.SelectionFromProps(n.Props)
	var selBg, selFg uint32
	hasSelBg, hasSelFg := false, false
	if selRange.Valid() {
		if sel.Bg != "" {
			if c, ok := parseColor(sel.Bg); ok { selBg, hasSelBg = c, true }
		}
		if sel.Color != "" {
			if c, ok := parseColor(sel.Color); ok { selFg, hasSelFg = c, true }
		}
	}

	// 按字体分段绘制：go-skia 不做自动字体回退，中文等缺字需用 CJK fallback 字体。
	// 先量出每段的 x 范围与 rune 区间，供选区背景定位；再落笔文字。
	type seg struct {
		text string
		font sk.Font
		x0   float32
		w    float32
		r0   int
		r1   int
	}
	// text-align：盒宽大于内容宽时按 center/right 平移全部段（含选区背景）。
	contentW := float32(0)
	for _, lineText := range lines {
		if lw := textRunWidth(lineText, st); lw > contentW {
			contentW = lw
		}
	}
	dx = alignOffset(st.align, box.W, contentW)

	// 逐行落笔：每行独立切 run；选区 rune 区间按全文本累计（编辑器行是单行，
	// 多行时按行内 rune 数顺延，与 selection-range 的全文本 rune 语义一致）。
	runePos := 0
	for li, lineText := range lines {
		lineBaseline := baseline + float32(li)*lineH
		var segs []seg
		x := box.X
		for _, r := range splitRuns(lineText, st.fontSize, st.bold, st.family) {
			if r.text == "" || r.font.IsNil() {
				runePos += utf8.RuneCountInString(r.text)
				continue
			}
			w := r.font.MeasureText([]byte(r.text), uint32(len(r.text)), sk.TextEncodingUTF8, nil)
			segs = append(segs, seg{text: r.text, font: r.font, x0: x, w: w, r0: runePos, r1: runePos + utf8.RuneCountInString(r.text)})
			x += w
			runePos += utf8.RuneCountInString(r.text)
		}

		// 选区背景（先画，被文字覆盖）。
		if hasSelBg {
			for _, s := range segs {
				b0, b1 := maxInt(s.r0, selRange.Start), minInt(s.r1, selRange.End)
				if b1 <= b0 || s.w <= 0 {
					continue
				}
				// 按 rune 在该段内的占比估算像素区间（等宽假设与测量口径足够接近）。
				frac0, frac1 := float32(b0-s.r0)/float32(s.r1-s.r0), float32(b1-s.r0)/float32(s.r1-s.r0)
				rx, rw := s.x0+dx+s.w*frac0, s.w*(frac1-frac0)
				bgPaint := sk.NewPaint()
				bgPaint.SetColor(sk.Color(selBg))
				bgPaint.SetAntiAlias(true)
				p.canvas.DrawRect(sk.RectMakeXYWH(rx, box.Y+float32(li)*lineH, rw, lineH), bgPaint)
				bgPaint.Delete()
				*p.log = append(*p.log, DrawOp{Kind: "selection", Name: text[b0Bytes(text, b0):b0Bytes(text, b1)], X: rx, Y: box.Y + float32(li)*lineH, W: rw, H: lineH, ARGB: selBg})
			}
		}
		// 文字：与选区区间相交的 run 用选区前景色，其余用继承色。
		// 字符级精细前景由富文本 Selected 路径处理；这里是标量协议的 run 级近似。
		for _, s := range segs {
			inSel := hasSelFg && s.r1 > selRange.Start && s.r0 < selRange.End
			if inSel {
				paint.SetColor(sk.Color(selFg))
			} else {
				paint.SetColor(sk.Color(st.color))
			}
			p.canvas.DrawString(s.text, s.x0+dx, lineBaseline, s.font, paint)
		}
	}
	paint.Delete()
}

	lineH := box.H
	if lineH <= 0 {
		lineH = st.lineH()
	}
	// X 记录实际落笔起点（text-align 偏移后的位置；dx=0 时与历史行为一致）。
	*p.log = append(*p.log, DrawOp{Kind: "text", Name: text, X: box.X + dx, Y: box.Y, W: box.W, H: lineH, ARGB: st.color})
	p.hitAppend(n, box)
}

func maxInt(a, b int) int { if a > b { return a }; return b }
func minInt(a, b int) int { if a < b { return a }; return b }

// textRunWidth 按 splitRuns 分段累加一行文本的绘制宽度（与 paintText 落笔同口径）。
func textRunWidth(text string, st textStyle) float32 {
	var w float32
	for _, r := range splitRuns(text, st.fontSize, st.bold, st.family) {
		if r.text == "" || r.font.IsNil() {
			continue
		}
		w += r.font.MeasureText([]byte(r.text), uint32(len(r.text)), sk.TextEncodingUTF8, nil)
	}
	return w
}

// b0Bytes 把 rune 索引换算成字节索引，用于切片与日志文本。
func b0Bytes(s string, runeIdx int) int {
	if runeIdx <= 0 { return 0 }
	i := 0
	for pos := range s {
		if i == runeIdx { return pos }
		i++
	}
	return len(s)
}

type richSeg struct {
	text  string
	col   uint32
	sel   bool
	x0    float32
	width float32
	runs  []run
}

// paintRichText 逐段绘制富文本：每个 RichSpan 用其自身颜色，段内按字体回退切 run。
// 颜色为空的段沿用继承色 st.color。落笔 x 全程累加，与 skiaMeasureFunc(纯文本) 一致。
//
// selection 协议（三端统一）：节点 Props 里的 &::selection 子表给出选区配色，
// 带 Selected 标记的段画一层选区背景矩形，前景改用选区 color（缺省沿用语法色）。
// 分两遍绘制：先量出所有段的 x 范围并画选区背景，再逐段画文字，保证文字压在背景之上。
func (p *painter) paintRichText(n *gxscene.Node, rt gxscene.RichText, box layoutBox, st textStyle) {
	if !p.shouldDraw(box) {
		return
	}
	sel := gxscene.SelectionFromProps(n.Props)
	selRange := gxscene.SelectionRangeFromProps(n.Props)
	var selBg, selFg uint32
	hasSelBg, hasSelFg := false, false
	if sel.Bg != "" {
		if c, ok := parseColor(sel.Bg); ok {
			selBg, hasSelBg = c, true
		}
	}
	if sel.Color != "" {
		if c, ok := parseColor(sel.Color); ok {
			selFg, hasSelFg = c, true
		}
	}
	baseline := box.Y + float32(st.fontSize)
	lineH := float32(st.fontSize) * 1.35

	// 第一遍：切 run、量宽、定段色与 x 范围。
	// 段的选中态来源有二：RichSpan.Selected，或标量 selection-range 与 rune 区间相交。
	segs := make([]richSeg, 0, len(rt))
	x := box.X
	runePos := 0
	for _, span := range rt {
		if span.Text == "" {
			continue
		}
		spanRunes := utf8.RuneCountInString(span.Text)
		isSel := span.Selected || (selRange.Valid() && runePos < selRange.End && runePos+spanRunes > selRange.Start)
		col := st.color
		if span.Color != "" {
			if c, ok := parseColor(span.Color); ok {
				col = c
			}
		}
		if isSel && hasSelFg {
			col = selFg
		}
		runs := splitRuns(span.Text, st.fontSize, st.bold, st.family)
		segStart := x
		for _, r := range runs {
			if r.text == "" || r.font.IsNil() {
				continue
			}
			x += r.font.MeasureText([]byte(r.text), uint32(len(r.text)), sk.TextEncodingUTF8, nil)
		}
		segs = append(segs, richSeg{text: span.Text, col: col, sel: isSel, x0: segStart, width: x - segStart, runs: runs})
		runePos += spanRunes
	}
	// text-align：盒宽大于内容宽时按 center/right 平移整行（编辑器行盒与内容
	// 等宽，偏移自然为 0，不影响既有渲染）。
	dx := alignOffset(st.align, box.W, x-box.X)

	// 选区背景：先画所有选中段，保证被后续文字覆盖。
	if hasSelBg {
		for _, seg := range segs {
			if !seg.sel || seg.width <= 0 {
				continue
			}
			bgPaint := sk.NewPaint()
			bgPaint.SetColor(sk.Color(selBg))
			bgPaint.SetAntiAlias(true)
			p.canvas.DrawRect(sk.RectMakeXYWH(seg.x0+dx, box.Y, seg.width, lineH), bgPaint)
			bgPaint.Delete()
			*p.log = append(*p.log, DrawOp{Kind: "selection", Name: seg.text, X: seg.x0+dx, Y: box.Y, W: seg.width, H: lineH, ARGB: selBg})
		}
	}

	// 第二遍：逐段落笔文字（x 从段起点重新累加）。
	paint := sk.NewPaint()
	defer paint.Delete()
	paint.SetAntiAlias(true)
	for _, seg := range segs {
		segX := seg.x0 + dx
		for _, r := range seg.runs {
			if r.text == "" || r.font.IsNil() {
				continue
			}
			paint.SetColor(sk.Color(seg.col))
			p.canvas.DrawString(r.text, segX, baseline, r.font, paint)
			segX += r.font.MeasureText([]byte(r.text), uint32(len(r.text)), sk.TextEncodingUTF8, nil)
		}
	}
}

func maxF(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// radiusClamp 把圆角半径限制在短边一半以内（避免圆角重叠产生伪影）。
func radiusClamp(r, w, h float32) float32 {
	if r <= 0 {
		return 0
	}
	short := minF(w, h)
	if short < 0 {
		short = 0
	}
	if r > short/2 {
		return short / 2
	}
	return r
}

func minF(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
