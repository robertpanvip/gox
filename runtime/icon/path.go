// Package icon 提供后端无关的矢量图标解析：把 SVG 转成 canvas.Op 矢量指令。
//
// 设计要点（与 canvas / imgutil 同构，保持主模块零依赖）：
//   - 纯 Go 标准库实现，不引入任何第三方 SVG 库；
//   - 解析 SVG path 的 d 属性（完整命令集），规范化成后端支持的绝对
//     M/L/C/Z（H/V→L，Q→C，A→C，S/T 展开为 C），再烘焙 viewBox 变换，
//     使输出的每个坐标都是相对图标布局盒的绝对像素——后端零改动、直接绘制；
//   - 全程矢量，绝不栅格化为位图。
//
// 输出坐标系统一为「图标内容盒」：Parse 接受目标尺寸 size，把 viewBox 等比
// 缩放并居中到 [0,size]×[0,size]，preserveAspectRatio 默认 xMidYMid meet。
package icon

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/robertpanvip/gox/runtime/canvas"
)

// pt 是一个可变换的二维点。
type pt struct{ x, y float32 }

// transform 是烘焙进坐标的仿射变换：p' = p*s + t（等比缩放 + 平移）。
type transform struct {
	s    float32
	tx, ty float32
}

func (t transform) apply(p pt) pt {
	return pt{x: p.x*t.s + t.tx, y: p.y*t.s + t.ty}
}

// pathCmd 是规范化过程中累积的一条绝对命令。
// 最终 emit 时每段输出成一条 canvas.Op{Kind: OpPath}，fill/stroke 由调用方填。
type pathCmd struct {
	op  byte // 'M' 'L' 'C' 'Z'
	pts []pt // M/L:1 点; C:3 点; Z:0
}

// parsePath 解析 SVG path 的 d 属性为绝对命令序列（尚未应用变换）。
// 支持 M/m L/l H/h V/v C/c S/s Q/q T/t A/a Z/z，含隐式重复参数组。
func parsePath(d string) ([]pathCmd, error) {
	p := &dParser{s: d}
	var cmds []pathCmd
	var cur pt    // 当前点
	var start pt  // 子路径起点（Z 用）
	var prev byte // 上一条命令（用于 S/T 的反射控制点）
	var cx1, cy1 float32 // 上一条 C/Q 的第二控制点（用于 S/T 反射）

	for {
		p.skipWS()
		if p.eof() {
			break
		}
		cmd := p.peek()

		// 隐式重复：数字开头表示沿用上一条命令（M→L，m→l）。
		if (cmd >= '0' && cmd <= '9') || cmd == '.' || cmd == '-' || cmd == '+' {
			if prev == 0 {
				return nil, fmt.Errorf("icon: path 首命令缺失: %q", p.rest())
			}
			cmd = prev
			if cmd == 'M' {
				cmd = 'L'
			} else if cmd == 'm' {
				cmd = 'l'
			}
		} else {
			p.next()
			p.skipWS()
		}

		rel := cmd >= 'a' && cmd <= 'z'
		upper := strings.ToUpper(string(cmd))[0]

		switch upper {
		case 'M', 'L', 'T':
			for {
				x, y, ok := p.num2()
				if !ok {
					return nil, p.errf(upper)
				}
				target := pt{x, y}
				if rel {
					target = pt{x: cur.x + x, y: cur.y + y}
				}
				switch upper {
				case 'M':
					cmds = append(cmds, pathCmd{op: 'M', pts: []pt{target}})
					cur, start = target, target
					// M 之后的隐式参数组按 L 处理。
					upper = 'L'
					if rel {
						cmd = 'l'
					} else {
						cmd = 'L'
					}
				case 'L':
					cmds = append(cmds, pathCmd{op: 'L', pts: []pt{target}})
					cur = target
				case 'T':
					// 平滑二次曲线：控制点是上一个 Q 控制点关于 cur 的反射。
					qx, qy := cur.x, cur.y
					if prev == 'Q' || prev == 'q' || prev == 'T' || prev == 't' {
						qx = 2*cur.x - cx1
						qy = 2*cur.y - cy1
					}
					c := cubicFromQuad(cur, pt{qx, qy}, target)
					cmds = append(cmds, pathCmd{op: 'C', pts: c})
					cx1, cy1 = qx, qy
					cur = target
				}
				prev = cmd
				if !p.moreNums() {
					break
				}
				p.skipWSComma()
			}

		case 'H', 'V':
			for {
				v, ok := p.num()
				if !ok {
					return nil, p.errf(upper)
				}
				target := cur
				if upper == 'H' {
					target.x = v
					if rel {
						target.x = cur.x + v
					}
				} else {
					target.y = v
					if rel {
						target.y = cur.y + v
					}
				}
				cmds = append(cmds, pathCmd{op: 'L', pts: []pt{target}})
				cur = target
				prev = cmd
				if !p.moreNums() {
					break
				}
				p.skipWSComma()
			}

		case 'C', 'S':
			for {
				var c1, c2, end pt
				if upper == 'C' {
					x1, y1, ok1 := p.num2()
					x2, y2, ok2 := p.num2()
					x3, y3, ok3 := p.num2()
					if !ok1 || !ok2 || !ok3 {
						return nil, p.errf('C')
					}
					c1, c2, end = pt{x1, y1}, pt{x2, y2}, pt{x3, y3}
					if rel {
						c1 = pt{cur.x + x1, cur.y + y1}
						c2 = pt{cur.x + x2, cur.y + y2}
						end = pt{cur.x + x3, cur.y + y3}
					}
				} else { // S: 第一控制点为反射
					x2, y2, ok1 := p.num2()
					x3, y3, ok2 := p.num2()
					if !ok1 || !ok2 {
						return nil, p.errf('S')
					}
					c2, end = pt{x2, y2}, pt{x3, y3}
					if rel {
						c2 = pt{cur.x + x2, cur.y + y2}
						end = pt{cur.x + x3, cur.y + y3}
					}
					c1 = cur
					if prev == 'C' || prev == 'c' || prev == 'S' || prev == 's' {
						c1 = pt{2*cur.x - cx1, 2*cur.y - cy1}
					}
				}
				cmds = append(cmds, pathCmd{op: 'C', pts: []pt{c1, c2, end}})
				cx1, cy1 = c2.x, c2.y
				cur = end
				prev = cmd
				if !p.moreNums() {
					break
				}
				p.skipWSComma()
			}

		case 'Q':
			for {
				x1, y1, ok1 := p.num2()
				x2, y2, ok2 := p.num2()
				if !ok1 || !ok2 {
					return nil, p.errf('Q')
				}
				q1, end := pt{x1, y1}, pt{x2, y2}
				if rel {
					q1 = pt{cur.x + x1, cur.y + y1}
					end = pt{cur.x + x2, cur.y + y2}
				}
				c := cubicFromQuad(cur, q1, end)
				cmds = append(cmds, pathCmd{op: 'C', pts: c})
				cx1, cy1 = q1.x, q1.y
				cur = end
				prev = cmd
				if !p.moreNums() {
					break
				}
				p.skipWSComma()
			}

		case 'A':
			for {
				rx, ry, ok1 := p.num2()
				rot, ok2 := p.num()
				laf, ok3 := p.flag()
				sf, ok4 := p.flag()
				x, y, ok5 := p.num2()
				if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 {
					return nil, p.errf('A')
				}
				end := pt{x, y}
				if rel {
					end = pt{cur.x + x, cur.y + y}
				}
				cubics := arcToCubics(cur, rx, ry, rot, laf, sf, end)
				for _, c := range cubics {
					cmds = append(cmds, pathCmd{op: 'C', pts: c})
				}
				cur = end
				prev = cmd
				if !p.moreNums() {
					break
				}
				p.skipWSComma()
			}

		case 'Z':
			cmds = append(cmds, pathCmd{op: 'Z'})
			cur = start
			prev = cmd
			p.skipWSComma()

		default:
			return nil, fmt.Errorf("icon: 未知 path 命令 %q", cmd)
		}
	}
	return cmds, nil
}

// cubicFromQuad 把二次贝塞尔 (p0,p1,p2) 精确提升为三次。
func cubicFromQuad(p0, p1, p2 pt) []pt {
	return []pt{
		pt{x: p0.x + 2.0/3.0*(p1.x-p0.x), y: p0.y + 2.0/3.0*(p1.y-p0.y)},
		pt{x: p2.x + 2.0/3.0*(p1.x-p2.x), y: p2.y + 2.0/3.0*(p1.y-p2.y)},
		p2,
	}
}

// arcToCubics 把 SVG 椭圆弧转成 1~N 段三次贝塞尔（标准端点参数化算法）。
func arcToCubics(p0 pt, rx, ry, rotDeg float32, laf, sf bool, p1 pt) [][]pt {
	rot := float64(rotDeg) * math.Pi / 180
	cosr, sinr := math.Cos(rot), math.Sin(rot)
	x1, y1, x2, y2 := float64(p0.x), float64(p0.y), float64(p1.x), float64(p1.y)

	dx, dy := x1-x2, y1-y2
	x1p := cosr*dx/2 + sinr*dy/2
	y1p := -sinr*dx/2 + cosr*dy/2

	rxA, ryA := math.Abs(float64(rx)), math.Abs(float64(ry))
	if rxA == 0 || ryA == 0 {
		return [][]pt{{p0, p1, p1}}
	}

	// 半径校正：椭圆太小则等比放大以容纳两端点。
	lambda := x1p*x1p/(rxA*rxA) + y1p*y1p/(ryA*ryA)
	if lambda > 1 {
		s := math.Sqrt(lambda)
		rxA, ryA = s*rxA, s*ryA
	}

	sign := 1.0
	if laf != sf {
		sign = -1
	}
	num := rxA*rxA*ryA*ryA - rxA*rxA*y1p*y1p - ryA*ryA*x1p*x1p
	den := rxA*rxA*y1p*y1p + ryA*ryA*x1p*x1p
	var co float64
	if den > 0 {
		co = sign * math.Sqrt(math.Max(0, num/den))
	}
	cxp := co * rxA * y1p / ryA
	cyp := -co * ryA * x1p / rxA
	cx := cosr*cxp - sinr*cyp + (x1+x2)/2
	cy := sinr*cxp + cosr*cyp + (y1+y2)/2

	angle := func(ux, uy, vx, vy float64) float64 {
		d := ux*vx + uy*vy
		l := math.Sqrt((ux*ux+uy*uy)*(vx*vx+vy*vy))
		if l == 0 {
			return 0
		}
		a := math.Acos(math.Max(-1, math.Min(1, d/l)))
		if ux*vy-uy*vx < 0 {
			a = -a
		}
		return a
	}
	theta1 := angle(1, 0, (x1p-cxp)/rxA, (y1p-cyp)/ryA)
	dtheta := angle((x1p-cxp)/rxA, (y1p-cyp)/ryA, (-x1p-cxp)/rxA, (-y1p-cyp)/ryA)
	if !sf && dtheta > 0 {
		dtheta -= 2 * math.Pi
	} else if sf && dtheta < 0 {
		dtheta += 2 * math.Pi
	}

	segs := int(math.Ceil(math.Abs(dtheta) / (math.Pi / 2)))
	if segs < 1 {
		segs = 1
	}
	delta := dtheta / float64(segs)
	t := 4.0 / 3.0 * math.Tan(delta/4)
	out := make([][]pt, 0, segs)

	prev := p0
	for i := 1; i <= segs; i++ {
		th := theta1 + float64(i-1)*delta
		nx := th + delta
		// 端点
		px := cx + rxA*math.Cos(nx)*cosr - ryA*math.Sin(nx)*sinr
		py := cy + rxA*math.Cos(nx)*sinr + ryA*math.Sin(nx)*cosr
		// 起点切线
		t1x := -rxA*math.Sin(th)*cosr - ryA*math.Cos(th)*sinr
		t1y := -rxA*math.Sin(th)*sinr + ryA*math.Cos(th)*cosr
		// 终点切线
		t2x := -rxA*math.Sin(nx)*cosr - ryA*math.Cos(nx)*sinr
		t2y := -rxA*math.Sin(nx)*sinr + ryA*math.Cos(nx)*cosr
		c1 := pt{float32(float64(prev.x) + t*t1x), float32(float64(prev.y) + t*t1y)}
		c2 := pt{float32(px - t*t2x), float32(py - t*t2y)}
		end := pt{float32(px), float32(py)}
		out = append(out, []pt{c1, c2, end})
		prev = end
	}
	return out
}

// emitPath 把绝对命令序列应用变换，拼成后端可解析的 "M x y L x y C ... Z" 字符串。
func emitPath(cmds []pathCmd, t transform) string {
	var b strings.Builder
	num := func(v float32) string {
		if math.Abs(float64(v)) < 1e-4 {
			return "0"
		}
		// -1 精度：最短往返表示，去掉尾部多余的零，减小 props 体积。
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	}
	for _, c := range cmds {
		switch c.op {
		case 'M':
			p := t.apply(c.pts[0])
			b.WriteString("M" + num(p.x) + " " + num(p.y) + " ")
		case 'L':
			p := t.apply(c.pts[0])
			b.WriteString("L" + num(p.x) + " " + num(p.y) + " ")
		case 'C':
			a, bb, cc := t.apply(c.pts[0]), t.apply(c.pts[1]), t.apply(c.pts[2])
			b.WriteString("C" + num(a.x) + " " + num(a.y) + " " + num(bb.x) + " " + num(bb.y) + " " + num(cc.x) + " " + num(cc.y) + " ")
		case 'Z':
			b.WriteString("Z ")
		}
	}
	return strings.TrimSpace(b.String())
}

// ---- dParser：SVG path 数值流解析器 ----

type dParser struct {
	s string
	i int
}

func (p *dParser) eof() bool       { return p.i >= len(p.s) }
func (p *dParser) peek() byte      { return p.s[p.i] }
func (p *dParser) next()           { p.i++ }
func (p *dParser) rest() string    { return p.s[p.i:] }
func (p *dParser) errf(c byte) error {
	return fmt.Errorf("icon: 命令 %c 参数不足: %q", c, p.rest())
}
func isWS(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
func isNumStart(c byte) bool {
	return (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '+'
}

func (p *dParser) skipWS() {
	for !p.eof() && isWS(p.peek()) {
		p.i++
	}
}

func (p *dParser) skipWSComma() {
	for !p.eof() && (isWS(p.peek()) || p.peek() == ',') {
		p.i++
	}
}

func (p *dParser) moreNums() bool {
	j := p.i
	for j < len(p.s) && (isWS(p.s[j]) || p.s[j] == ',') {
		j++
	}
	return j < len(p.s) && isNumStart(p.s[j])
}

// num 解析一个数（含科学计数法）。
func (p *dParser) num() (float32, bool) {
	p.skipWSComma()
	if p.eof() || !isNumStart(p.peek()) {
		return 0, false
	}
	start := p.i
	if p.peek() == '+' || p.peek() == '-' {
		p.i++
	}
	seenDigit, seenDot, seenExp := false, false, false
	for !p.eof() {
		c := p.peek()
		switch {
		case c >= '0' && c <= '9':
			seenDigit = true
			p.i++
		case c == '.' && !seenDot && !seenExp:
			seenDot = true
			p.i++
		case (c == 'e' || c == 'E') && !seenExp && seenDigit:
			seenExp = true
			p.i++
			if !p.eof() && (p.peek() == '+' || p.peek() == '-') {
				p.i++
			}
		default:
			goto done
		}
	}
done:
	if !seenDigit {
		p.i = start
		return 0, false
	}
	v, err := strconv.ParseFloat(p.s[start:p.i], 32)
	if err != nil {
		return 0, false
	}
	return float32(v), true
}

func (p *dParser) num2() (float32, float32, bool) {
	x, ok1 := p.num()
	y, ok2 := p.num()
	return x, y, ok1 && ok2
}

// flag 解析 arc 的 0/1 标志（SVG 允许紧挨着写，如 "011"）。
func (p *dParser) flag() (bool, bool) {
	p.skipWSComma()
	if p.eof() {
		return false, false
	}
	c := p.peek()
	if c != '0' && c != '1' {
		return false, false
	}
	p.i++
	return c == '1', true
}

// ---- 公开：把已解析的路径命令转成 canvas ops ----

// pathToOps 把一段 path 命令 + 变换 + 样式转成一条 canvas.Op（Kind=path）。
func pathToOps(cmds []pathCmd, t transform, fill, stroke string, strokeW float32) []canvas.Op {
	d := emitPath(cmds, t)
	if d == "" {
		return nil
	}
	return []canvas.Op{{
		Kind:        canvas.OpPath,
		D:           d,
		Fill:        fill,
		Stroke:      stroke,
		StrokeWidth: strokeW,
	}}
}
