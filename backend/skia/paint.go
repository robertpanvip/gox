package skia

import (
	"fmt"
	"strings"

	sk "github.com/pekim/go-skia"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
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
	}
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
		if v, ok := propString(n.Props[propWeight]); ok {
			if strings.EqualFold(v, "bold") || v == "700" {
				s.bold = true
			} else {
				s.bold = false
			}
		}
	}
	return s
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
	for _, c := range n.Children {
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
			if r := radiusClamp(s.radius, box.W, h); r > 0 {
				p.canvas.DrawRoundRect(rect, r, r, paint)
			} else {
				p.canvas.DrawRect(rect, paint)
			}
			paint.Delete()
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
		*p.hits = append(*p.hits, hitFrom(n, box))
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
// 样式来自祖先链继承（color/font-size/weight），因为文本节点自身 Props 通常为空。
func (p *painter) paintText(n *gxscene.Node, box layoutBox, st textStyle) {
	if n.Text == "" {
		return
	}
	// 富文本（语法高亮）：一行的分段着色数据挂在 rich-text 属性上。逐段用各自
	// 颜色绘制，段内再按字体回退切 run；落笔 x 累加 widths，与测量口径一致。
	if rt, ok := gxscene.RichTextFromProps(n.Props); ok && len(rt) > 0 {
		p.paintRichText(n, rt, box, st)
		lineH := box.H
		if lineH <= 0 {
			lineH = float32(st.fontSize) * 1.3
		}
		*p.log = append(*p.log, DrawOp{Kind: "text", Name: n.Text, X: box.X, Y: box.Y, W: box.W, H: lineH, ARGB: st.color})
		if p.hits != nil {
			hb := hitFrom(n, box)
			hb.H = lineH
			*p.hits = append(*p.hits, hb)
		}
		return
	}
	if p.shouldDraw(box) {
		paint := sk.NewPaint()
		paint.SetColor(sk.Color(st.color))
		paint.SetAntiAlias(true)
		baseline := box.Y + float32(st.fontSize)
		// 按字体分段绘制：go-skia 不做自动字体回退，中文等缺字需用 CJK fallback 字体。
		// 每段的落笔 x = 前面各段实测宽度之和，与 skiaFontMeasurer 的宽度累加口径一致。
		x := box.X
		for _, r := range splitRuns(n.Text, st.fontSize, st.bold) {
			if r.text == "" || r.font.IsNil() {
				continue
			}
			p.canvas.DrawString(r.text, x, baseline, r.font, paint)
			x += r.font.MeasureText([]byte(r.text), uint32(len(r.text)), sk.TextEncodingUTF8, nil)
		}
		paint.Delete()
	}

	lineH := box.H
	if lineH <= 0 {
		lineH = float32(st.fontSize) * 1.3
	}
	*p.log = append(*p.log, DrawOp{Kind: "text", Name: n.Text, X: box.X, Y: box.Y, W: box.W, H: lineH, ARGB: st.color})
	if p.hits != nil {
		hb := hitFrom(n, box)
		hb.H = lineH
		*p.hits = append(*p.hits, hb)
	}
}

// paintRichText 逐段绘制富文本：每个 RichSpan 用其自身颜色，段内按字体回退切 run。
// 颜色为空的段沿用继承色 st.color。落笔 x 全程累加，与 skiaMeasureFunc(纯文本) 一致。
func (p *painter) paintRichText(n *gxscene.Node, rt gxscene.RichText, box layoutBox, st textStyle) {
	if !p.shouldDraw(box) {
		return
	}
	baseline := box.Y + float32(st.fontSize)
	x := box.X
	paint := sk.NewPaint()
	defer paint.Delete()
	paint.SetAntiAlias(true)
	for _, span := range rt {
		if span.Text == "" {
			continue
		}
		col := st.color
		if span.Color != "" {
			if c, ok := parseColor(span.Color); ok {
				col = c
			}
		}
		paint.SetColor(sk.Color(col))
		for _, r := range splitRuns(span.Text, st.fontSize, st.bold) {
			if r.text == "" || r.font.IsNil() {
				continue
			}
			p.canvas.DrawString(r.text, x, baseline, r.font, paint)
			x += r.font.MeasureText([]byte(r.text), uint32(len(r.text)), sk.TextEncodingUTF8, nil)
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
