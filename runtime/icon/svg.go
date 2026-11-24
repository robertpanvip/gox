package icon

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/robertpanvip/gox/runtime/canvas"
)

// ParseOptions 控制 SVG 解析。
type ParseOptions struct {
	// Size 是目标图标尺寸（px）。viewBox 内容等比缩放并居中到 [0,Size]²。
	// <=0 时使用 SVG 的 width/height 或 viewBox 尺寸。
	Size float32
	// Color 非空时覆盖所有 fill/stroke（图标常用单色）。支持 "currentColor"
	// 语义：元素的 fill="currentColor" 或未指定但需着色的场景用此值。
	Color string
	// StrokeWidth 非空时覆盖描边宽度（0 表示用 SVG 原值）。
	StrokeWidth float32
}

// viewBox 解析自 <svg viewBox="minx miny w h">。
type viewBox struct{ x, y, w, h float32 }

// Parse 把 SVG 文本解析成后端无关的 canvas.Op 矢量指令。
// 结果坐标已烘焙进目标尺寸变换，后端直接绘制即可（零改动、全程矢量）。
func Parse(svg string, opt ParseOptions) ([]canvas.Op, error) {
	dec := xml.NewDecoder(strings.NewReader(svg))
	var root svgElem
	// 找到第一个 <svg> 元素（跳过 XML 声明/注释/前导文本）。
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil, fmt.Errorf("icon: 未找到 <svg> 根元素")
		}
		if err != nil {
			return nil, fmt.Errorf("icon: XML 解析: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "svg" {
			root = toElem(se)
			break
		}
	}

	vb, err := root.viewBox()
	if err != nil {
		return nil, err
	}
	size := opt.Size
	if size <= 0 {
		// 优先用 width/height，其次 viewBox 尺寸。
		if w, ok := root.numAttr("width"); ok && w > 0 {
			size = w
		} else if vb.w > 0 {
			size = vb.w
		} else {
			size = 24
		}
	}
	if vb.w <= 0 || vb.h <= 0 {
		vb = viewBox{0, 0, size, size}
	}
	scale := size / max(vb.w, vb.h)
	// xMidYMid meet：内容等比缩放后居中。
	offX := (size - vb.w*scale) / 2
	offY := (size - vb.h*scale) / 2
	t := transform{
		s:  scale,
		tx: offX - vb.x*scale,
		ty: offY - vb.y*scale,
	}

	ctx := &parseCtx{
		t:        t,
		color:    strings.TrimSpace(opt.Color),
		strokeW:  opt.StrokeWidth,
		baseFill: attrOf(root, "fill"),
	}

	var ops []canvas.Op
	// 根元素自身样式作为默认继承值。
	ctx.fill = ctx.baseFill
	ctx.stroke = attrOf(root, "stroke")

	// 解析根元素的直接子元素（递归处理 g）。
	depth := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		switch el := tok.(type) {
		case xml.StartElement:
			depth++
			e := toElem(el)
			ops = append(ops, ctx.parseElem(e)...)
			if el.Name.Local == "g" {
				ctx.push(e)
			}
		case xml.EndElement:
			if _el := el.Name.Local; _el == "g" && depth > 0 {
				ctx.pop()
			}
			depth--
		}
	}
	if len(ops) == 0 {
		return nil, fmt.Errorf("icon: SVG 中未找到可绘制图元")
	}
	return ops, nil
}

// parseCtx 维护解析中的样式继承栈。
type parseCtx struct {
	t                       transform
	color                   string // 全局覆盖色（非空时）
	strokeW                 float32
	baseFill, baseStroke    string
	fill, stroke            string
	swStack                 []float32
	fillStack, strokeStack  []string
}

func (c *parseCtx) push(e svgElem) {
	c.fillStack = append(c.fillStack, c.fill)
	c.strokeStack = append(c.strokeStack, c.stroke)
	if f, ok := e.attr("fill"); ok {
		c.fill = f
	}
	if s, ok := e.attr("stroke"); ok {
		c.stroke = s
	}
}

func (c *parseCtx) pop() {
	if n := len(c.fillStack); n > 0 {
		c.fill = c.fillStack[n-1]
		c.fillStack = c.fillStack[:n-1]
	}
	if n := len(c.strokeStack); n > 0 {
		c.stroke = c.strokeStack[n-1]
		c.strokeStack = c.strokeStack[:n-1]
	}
}

// resolveColor 计算最终颜色：全局覆盖色优先，currentColor 替换为覆盖色，
// none 保持 none，缺省用继承值。
func (c *parseCtx) resolveColor(v, inherited string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		v = inherited
	}
	if v == "none" || v == "transparent" {
		return ""
	}
	if v == "currentColor" || c.color != "" {
		if c.color != "" {
			return c.color
		}
		return inherited
	}
	return v
}

func (c *parseCtx) parseElem(e svgElem) []canvas.Op {
	switch e.name {
	case "path":
		d := attrOf(e, "d")
		if d == "" {
			return nil
		}
		cmds, err := parsePath(d)
		if err != nil {
			return nil
		}
		fill := c.resolveColor(attrOf(e, "fill"), c.fill)
		stroke := c.resolveColor(attrOf(e, "stroke"), c.stroke)
		sw := c.strokeWidth(e)
		return pathToOps(cmds, c.t, fill, stroke, sw)

	case "circle":
		cx, ok1 := e.numAttr("cx")
		cy, ok2 := e.numAttr("cy")
		r, ok3 := e.numAttr("r")
		if !ok1 || !ok2 || !ok3 {
			return nil
		}
		p := c.t.apply(pt{cx, cy})
		return []canvas.Op{{
			Kind: canvas.OpCircle, CX: p.x, CY: p.y, R: r * c.t.s,
			Fill: c.resolveColor(attrOf(e, "fill"), c.fill),
			Stroke: c.resolveColor(attrOf(e, "stroke"), c.stroke),
			StrokeWidth: c.strokeWidth(e),
		}}

	case "ellipse":
		cx, ok1 := e.numAttr("cx")
		cy, ok2 := e.numAttr("cy")
		rx, ok3 := e.numAttr("rx")
		ry, ok4 := e.numAttr("ry")
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return nil
		}
		// 用 A 弧线近似椭圆，转成 path（全程矢量）。
		left, right := cx-rx, cx+rx
		d := fmt.Sprintf("M%g %g A%g %g 0 1 0 %g %g A%g %g 0 1 0 %g %g Z",
			left, cy, rx, ry, right, cy, rx, ry, left, cy)
		cmds, err := parsePath(d)
		if err != nil {
			return nil
		}
		return pathToOps(cmds, c.t, c.resolveColor(attrOf(e, "fill"), c.fill),
			c.resolveColor(attrOf(e, "stroke"), c.stroke), c.strokeWidth(e))

	case "rect":
		x, ok1 := e.numAttr("x")
		y, ok2 := e.numAttr("y")
		w, ok3 := e.numAttr("width")
		h, ok4 := e.numAttr("height")
		if !ok1 {
			x = 0
			ok1 = true
		}
		if !ok2 {
			y = 0
			ok2 = true
		}
		if !ok3 || !ok4 {
			return nil
		}
		p := c.t.apply(pt{x, y})
		rad, _ := e.numAttr("rx")
		return []canvas.Op{{
			Kind: canvas.OpRect, X: p.x, Y: p.y, W: w * c.t.s, H: h * c.t.s,
			Radius: rad * c.t.s,
			Fill: c.resolveColor(attrOf(e, "fill"), c.fill),
			Stroke: c.resolveColor(attrOf(e, "stroke"), c.stroke),
			StrokeWidth: c.strokeWidth(e),
		}}

	case "line":
		x1, ok1 := e.numAttr("x1")
		y1, ok2 := e.numAttr("y1")
		x2, ok3 := e.numAttr("x2")
		y2, ok4 := e.numAttr("y2")
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return nil
		}
		a, b := c.t.apply(pt{x1, y1}), c.t.apply(pt{x2, y2})
		return []canvas.Op{{
			Kind: canvas.OpLine, X1: a.x, Y1: a.y, X2: b.x, Y2: b.y,
			Stroke: c.resolveColor(attrOf(e, "stroke"), c.stroke),
			StrokeWidth: c.strokeWidth(e),
		}}

	case "polygon", "polyline":
		pts := parsePoints(attrOf(e, "points"))
		if len(pts) == 0 {
			return nil
		}
		var b strings.Builder
		for i, p := range pts {
			q := c.t.apply(p)
			if i == 0 {
				b.WriteString(fmt.Sprintf("M%g %g ", q.x, q.y))
			} else {
				b.WriteString(fmt.Sprintf("L%g %g ", q.x, q.y))
			}
		}
		if e.name == "polygon" {
			b.WriteString("Z")
		}
		cmds, err := parsePath(b.String())
		if err != nil {
			return nil
		}
		return pathToOps(cmds, c.t, c.resolveColor(attrOf(e, "fill"), c.fill),
			c.resolveColor(attrOf(e, "stroke"), c.stroke), c.strokeWidth(e))
	}
	return nil
}

func (c *parseCtx) strokeWidth(e svgElem) float32 {
	if c.strokeW > 0 {
		return c.strokeW * c.t.s
	}
	if sw, ok := e.numAttr("stroke-width"); ok {
		return sw * c.t.s
	}
	return 0
}

// ---- SVG 元素辅助 ----

type svgElem struct {
	name  string
	attrs map[string]string
}

func toElem(se xml.StartElement) svgElem {
	e := svgElem{name: se.Name.Local, attrs: make(map[string]string, len(se.Attr))}
	for _, a := range se.Attr {
		e.attrs[a.Name.Local] = a.Value
	}
	return e
}

func (e svgElem) attr(k string) (string, bool) {
	v, ok := e.attrs[k]
	return v, ok
}
func attrOf(e svgElem, k string) string { return e.attrs[k] }

func (e svgElem) numAttr(k string) (float32, bool) {
	v, ok := e.attrs[k]
	if !ok {
		return 0, false
	}
	v = strings.TrimSpace(strings.TrimSuffix(v, "px"))
	f, err := strconv.ParseFloat(v, 32)
	return float32(f), err == nil
}

func (e svgElem) viewBox() (viewBox, error) {
	v, ok := e.attrs["viewBox"]
	if !ok {
		return viewBox{}, nil
	}
	var vb viewBox
	n, err := fmt.Sscanf(strings.TrimSpace(v), "%f %f %f %f", &vb.x, &vb.y, &vb.w, &vb.h)
	if err != nil || n != 4 {
		return viewBox{}, fmt.Errorf("icon: 非法 viewBox %q", v)
	}
	return vb, nil
}

func parsePoints(s string) []pt {
	var out []pt
	p := &dParser{s: s}
	for {
		x, y, ok := p.num2()
		if !ok {
			break
		}
		out = append(out, pt{x, y})
		p.skipWSComma()
	}
	return out
}

func max(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
