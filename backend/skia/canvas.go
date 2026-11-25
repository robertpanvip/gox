package skia

// canvas 原语：解析 Props["canvas-ops"]（runtime/canvas 协议）并用 Skia 绘制。
// canvas 节点本身是一个普通 NodeDiv（参与布局/定位/裁剪），指令在其绘制阶段解释，
// 坐标相对 canvas 布局盒左上角。与 Rust 端共用 runtime/canvas 的指令协议。

import (
	"strconv"
	"strings"

	sk "github.com/robertpanvip/gox-skia/compat"
	"github.com/robertpanvip/gox/runtime/canvas"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// paintCanvas 绘制 canvas 节点的所有指令。
func paintCanvas(p *painter, n *gxscene.Node, box layoutBox) {
	raw, _ := n.Props[canvas.PropKey].(string)
	ops := canvas.Decode(raw)
	if len(ops) == 0 {
		return
	}
	ox, oy := box.X, box.Y
	for _, op := range ops {
		switch op.Kind {
		case canvas.OpRect:
			drawCanvasRect(p, op, ox, oy)
		case canvas.OpCircle:
			drawCanvasCircle(p, op, ox, oy)
		case canvas.OpLine:
			drawCanvasLine(p, op, ox, oy)
		case canvas.OpPath:
			drawCanvasPath(p, op, ox, oy)
		}
	}
	*p.log = append(*p.log, DrawOp{Kind: "canvas", Name: n.Kind, X: box.X, Y: box.Y, W: box.W, H: box.H})
}

func canvasPaint(fill string, stroke string, strokeW float32) (fillP, strokeP *sk.Paint) {
	if c, ok := parseColor(fill); ok {
		p := sk.NewPaint()
		p.SetColor(argbColor(c>>24&0xff, c>>16&0xff, c>>8&0xff, c&0xff))
		p.SetAntiAlias(true)
		fillP = &p
	}
	if c, ok := parseColor(stroke); ok && strokeW > 0 {
		p := sk.NewPaint()
		p.SetColor(argbColor(c>>24&0xff, c>>16&0xff, c>>8&0xff, c&0xff))
		p.SetStyle(sk.PaintStyleStroke)
		p.SetStrokeWidth(strokeW)
		p.SetAntiAlias(true)
		strokeP = &p
	}
	return
}

func drawCanvasRect(p *painter, op canvas.Op, ox, oy float32) {
	x, y, w, h := ox+op.X, oy+op.Y, op.W, op.H
	if w <= 0 || h <= 0 {
		return
	}
	fp, sp := canvasPaint(op.Fill, op.Stroke, op.StrokeWidth)
	defer func() {
		if fp != nil {
			(*fp).Delete()
		}
		if sp != nil {
			(*sp).Delete()
		}
	}()
	r := radiusClamp(op.Radius, w, h)
	rect := sk.RectMakeXYWH(x, y, w, h)
	if fp != nil {
		if r > 0 {
			p.canvas.DrawRoundRect(rect, r, r, *fp)
		} else {
			p.canvas.DrawRect(rect, *fp)
		}
	}
	if sp != nil {
		if r > 0 {
			p.canvas.DrawRoundRect(rect, r, r, *sp)
		} else {
			p.canvas.DrawRect(rect, *sp)
		}
	}
}

func drawCanvasCircle(p *painter, op canvas.Op, ox, oy float32) {
	if op.R <= 0 {
		return
	}
	fp, sp := canvasPaint(op.Fill, op.Stroke, op.StrokeWidth)
	defer func() {
		if fp != nil {
			(*fp).Delete()
		}
		if sp != nil {
			(*sp).Delete()
		}
	}()
	cx, cy := ox+op.CX, oy+op.CY
	if fp != nil {
		p.canvas.DrawCircleScalars(cx, cy, op.R, *fp)
	}
	if sp != nil {
		p.canvas.DrawCircleScalars(cx, cy, op.R, *sp)
	}
}

func drawCanvasLine(p *painter, op canvas.Op, ox, oy float32) {
	if c, ok := parseColor(op.Stroke); ok {
		paint := sk.NewPaint()
		defer paint.Delete()
		paint.SetColor(argbColor(c>>24&0xff, c>>16&0xff, c>>8&0xff, c&0xff))
		paint.SetStyle(sk.PaintStyleStroke)
		w := op.StrokeWidth
		if w <= 0 {
			w = 1
		}
		paint.SetStrokeWidth(w)
		paint.SetAntiAlias(true)
		p.canvas.DrawLineScalars(ox+op.X1, oy+op.Y1, ox+op.X2, oy+op.Y2, paint)
	}
}

// drawCanvasPath 解析 SVG path 子集（M/L/H/V/C/Q/Z），绝对坐标。
func drawCanvasPath(p *painter, op canvas.Op, ox, oy float32) {
	path := sk.NewPath()
	var cx, cy float32
	// 命令字母前后插空格，使 "M0 0 L10 10" 分离为 "M 0 0 L 10 10"。
	sep := strings.NewReplacer(
		"M", " M ", "L", " L ", "H", " H ", "V", " V ", "C", " C ", "Q", " Q ", "Z", " Z ",
		"z", " z ",
	).Replace(op.D)
	tokens := strings.FieldsFunc(sep, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' || r == '\t'
	})
	num := func(s string) float32 {
		v, _ := strconv.ParseFloat(s, 32)
		return float32(v)
	}
	for i := 0; i < len(tokens); {
		cmd := tokens[i][0]
		i++
		switch cmd {
		case 'M', 'L':
			if i+1 >= len(tokens) {
				return
			}
			x, y := ox+num(tokens[i]), oy+num(tokens[i+1])
			i += 2
			if cmd == 'M' {
				path.MoveToPoint(x, y)
				cx, cy = x, y
			} else {
				path.LineTo(x, y)
				cx, cy = x, y
			}
		case 'H':
			if i >= len(tokens) {
				return
			}
			cx = ox + num(tokens[i])
			i++
			path.LineTo(cx, cy)
		case 'V':
			if i >= len(tokens) {
				return
			}
			cy = oy + num(tokens[i])
			i++
			path.LineTo(cx, cy)
		case 'Q':
			if i+3 >= len(tokens) {
				return
			}
			x1, y1, x2, y2 := ox+num(tokens[i]), oy+num(tokens[i+1]), ox+num(tokens[i+2]), oy+num(tokens[i+3])
			i += 4
			path.QuadTo(x1, y1, x2, y2)
			cx, cy = x2, y2
		case 'C':
			if i+5 >= len(tokens) {
				return
			}
			x1, y1, x2, y2, x3, y3 := ox+num(tokens[i]), oy+num(tokens[i+1]), ox+num(tokens[i+2]), oy+num(tokens[i+3]), ox+num(tokens[i+4]), oy+num(tokens[i+5])
			i += 6
			path.CubicTo(x1, y1, x2, y2, x3, y3)
			cx, cy = x3, y3
		case 'Z', 'z':
			path.Close()
		default:
			return // 未知指令：安全停止
		}
	}
	fp, sp := canvasPaint(op.Fill, op.Stroke, op.StrokeWidth)
	defer func() {
		if fp != nil {
			(*fp).Delete()
		}
		if sp != nil {
			(*sp).Delete()
		}
	}()
	if fp != nil {
		p.canvas.DrawPath(path, *fp)
	}
	if sp != nil {
		p.canvas.DrawPath(path, *sp)
	}
}
