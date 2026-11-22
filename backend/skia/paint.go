package skia

import (
	"fmt"

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
// 布局策略刻意保持简单（纵向流式），因为布局不属于 host 协议、完全由后端决定；
// 这里只求证明"节点被正确分派、属性被解释、绘制原语被调用"。真实后端可替换布局器。
type painter struct {
	canvas sk.Canvas
	width  float32
	height float32
	fonts  map[int]sk.Font
	log    *DrawLog
}

// paintTree 绘制一个节点并返回它占用的高度。
func (p *painter) paintTree(n *gxscene.Node, x, y, availW, availH float32) float32 {
	if n == nil {
		return 0
	}
	switch n.Type {
	case gxscene.NodeFragment:
		// 透明包装：不画自己，纵向透传 children。
		*p.log = append(*p.log, DrawOp{Kind: "fragment", Name: n.Kind, X: x, Y: y})
		return p.paintChildren(n, x, y, availW, availH)
	case gxscene.NodeText:
		return p.paintText(n, x, y, availW)
	case gxscene.NodeDiv:
		return p.paintElement(n, x, y, availW, availH)
	default:
		return p.paintChildren(n, x, y, availW, availH)
	}
}

// paintElement 画一个元素节点：先按样式画背景矩形，再纵向排布 children。
func (p *painter) paintElement(n *gxscene.Node, x, y, availW, availH float32) float32 {
	s := parseStyle(n.Props)
	w := s.width
	if w <= 0 {
		w = availW
	}
	h := s.height // 0 表示按内容高度

	// 背景矩形（真实 Skia 调用）。
	if s.hasBg {
		paint := sk.NewPaint()
		paint.SetColor(sk.Color(s.bg))
		p.canvas.DrawRect(sk.RectMakeXYWH(x, y, w, maxF(h, 1)), paint)
		paint.Delete()
		*p.log = append(*p.log, DrawOp{Kind: "rect", Name: n.Kind, X: x, Y: y, W: w, H: h, ARGB: s.bg})
	}

	// 子元素纵向流式布局。
	childY := y + s.padding
	used := p.paintChildrenOffset(n, x+s.padding, childY, w-2*s.padding, availH)
	if h <= 0 {
		h = used + 2*s.padding
	}
	return h
}

// paintChildren 从当前位置纵向排布所有 children，返回总高度。
func (p *painter) paintChildren(n *gxscene.Node, x, y, availW, availH float32) float32 {
	return p.paintChildrenOffset(n, x, y, availW, availH)
}

func (p *painter) paintChildrenOffset(n *gxscene.Node, x, y, availW, availH float32) float32 {
	curY := y
	for _, c := range n.Children {
		h := p.paintTree(c, x, curY, availW, availH-(curY-y))
		curY += h
	}
	return curY - y
}

// paintText 画文本节点：解析样式、取字体、调用 Skia DrawString。
func (p *painter) paintText(n *gxscene.Node, x, y, availW float32) float32 {
	if n.Text == "" {
		return 0
	}
	s := parseStyle(n.Props)
	font := p.fontFor(s.fontSize, s.bold)
	paint := sk.NewPaint()
	paint.SetColor(sk.Color(s.color))
	baseline := y + float32(s.fontSize)
	p.canvas.DrawString(n.Text, x, baseline, font, paint)
	paint.Delete()

	lineH := float32(s.fontSize) * 1.3
	*p.log = append(*p.log, DrawOp{Kind: "text", Name: n.Text, X: x, Y: y, W: float32(len(n.Text)) * float32(s.fontSize) * 0.5, H: lineH, ARGB: s.color})
	return lineH
}

// fontFor 按字号+字重取缓存字体（未命中则构造并缓存）。
func (p *painter) fontFor(size int, bold bool) sk.Font {
	// 字重用 key 的偏移区分，避免额外结构。
	key := size
	if bold {
		key = -size
	}
	if f, ok := p.fonts[key]; ok && f.IsNil() == false {
		return f
	}
	style := sk.FontStyleNormal()
	if bold {
		style = sk.FontStyleBold()
	}
	tf := sk.FontMgrDefault().MatchFamilyStyle("sans-serif", style)
	f := sk.NewFontTypefaceSize(tf, float32(size))
	p.fonts[key] = f
	return f
}

func maxF(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
