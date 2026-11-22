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
}

// HitBox 是一个节点在最近一帧中的屏幕矩形，供事件命中测试使用。
// 它由 painter 在绘制同帧时记录，坐标与 Skia canvas 一致（左上角原点）。
type HitBox struct {
	Node *gxscene.Node
	X, Y float32
	W, H float32
}

// Contains 报告屏幕点 (px,py) 是否落在该矩形内。
func (b HitBox) Contains(px, py float32) bool {
	return px >= b.X && px <= b.X+b.W && py >= b.Y && py <= b.Y+b.H
}

// paintFrame 为一帧做布局并绘制整棵树。入口（Window.Draw / PaintTo）调用它。
func (p *painter) paintFrame(root *gxscene.Node, availW, availH float32) {
	if root == nil {
		return
	}
	// 两阶段第一步：Yoga 布局，算出每个节点的绝对矩形。
	p.layout = computeLayout(root, availW, availH, skiaFontMeasurer())
	// 两阶段第二步：按矩形逐个调用 Skia 绘制原语。
	p.paintTree(root, defaultTextStyle())
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
		for _, c := range n.Children {
			p.paintTree(c, eff)
		}
	default:
		eff := inheritedStyle(n, inherited)
		for _, c := range n.Children {
			p.paintTree(c, eff)
		}
	}
}

// paintElement 按 Yoga 给出的矩形画元素背景，并记录命中矩形。
func (p *painter) paintElement(n *gxscene.Node, box layoutBox) {
	s := parseStyle(n.Props)
	h := maxF(box.H, 1)
	rect := sk.RectMakeXYWH(box.X, box.Y, box.W, h)
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
		*p.log = append(*p.log, DrawOp{Kind: "rect", Name: n.Kind, X: box.X, Y: box.Y, W: box.W, H: box.H, ARGB: s.bg})
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
	if p.hits != nil {
		*p.hits = append(*p.hits, HitBox{Node: n, X: box.X, Y: box.Y, W: box.W, H: box.H})
	}
}

// paintText 画文本节点：位置/尺寸由 Yoga 的 measure 回调给出，文字基线按字号定位。
// 样式来自祖先链继承（color/font-size/weight），因为文本节点自身 Props 通常为空。
func (p *painter) paintText(n *gxscene.Node, box layoutBox, st textStyle) {
	if n.Text == "" {
		return
	}
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

	lineH := box.H
	if lineH <= 0 {
		lineH = float32(st.fontSize) * 1.3
	}
	*p.log = append(*p.log, DrawOp{Kind: "text", Name: n.Text, X: box.X, Y: box.Y, W: box.W, H: lineH, ARGB: st.color})
	if p.hits != nil {
		*p.hits = append(*p.hits, HitBox{Node: n, X: box.X, Y: box.Y, W: box.W, H: lineH})
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
