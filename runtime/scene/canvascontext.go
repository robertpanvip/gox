package gxscene

// 本文件实现 ref 句柄的命令式 canvas 上下文（A 方案）。
//
// 设计要点：
//   - el.getContext() 返回一个有状态的 *CanvasContext，业务用 Web 风格命令式调用
//     fillRect / moveTo / lineTo / stroke / fill … 绘制，不必手写 canvas.Encode；
//   - ctx 内部维护一份 []canvas.Op（命令式调用的累积状态），每次绘制后写回该节点
//     Props["canvas-ops"]，并记一条 ChangeSetAttr + 请求重绘——于是两端后端原封不动
//     照既有 canvas-ops 协议绘制，wire 协议与后端零改动；
//   - 这是「立即模式」：ctx 状态持久保留（画了就在），与 Web canvas 位图语义一致；
//     差异是协议无局部擦除原语，clearRect 语义为清空整份指令列表（见方法注释）。
//   - GX 侧按规范小写书写（el.getContext().fillRect(...)），由 transformExternal
//     机械还原为首字母大写的 Go 方法；ctx 的方法名全部大写开头。

import (
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/robertpanvip/gox/runtime/canvas"
)

// CanvasContext 是一个 canvas 节点的 2D 绘制上下文（命令式、有状态）。
// 同一节点多次 el.getContext() 返回同一实例，绘制状态跨调用保留。
type CanvasContext struct {
	mu sync.Mutex

	el   *Element // 宿主元素（含 NodeID / node / sink）
	ops  []canvas.Op
	path []string // 当前路径的 SVG 子命令（beginPath 清空）

	fillStyle   string
	strokeStyle string
	lineWidth   float32
}

func newCanvasContext(el *Element) *CanvasContext {
	return &CanvasContext{el: el}
}

// GetContext 返回该元素持有的 canvas 绘制上下文。非 canvas 节点 / 已销毁句柄返回 nil。
// 同一节点的上下文是缓存复用的，绘制状态连续。
//
// GX 小写写法：el.getContext()
func (e *Element) GetContext() *CanvasContext {
	if e == nil || e.dead || e.node == nil || !strings.EqualFold(e.node.Kind, "canvas") {
		return nil
	}
	if e.ctx == nil {
		e.ctx = newCanvasContext(e)
	}
	return e.ctx
}

// ---- 样式状态（Web 里是属性赋值，GX 里用 setXxx 方法）----

func (c *CanvasContext) SetFillStyle(color string) { c.mu.Lock(); c.fillStyle = color; c.mu.Unlock() }
func (c *CanvasContext) SetStrokeStyle(color string) {
	c.mu.Lock()
	c.strokeStyle = color
	c.mu.Unlock()
}
func (c *CanvasContext) SetLineWidth(w float32) { c.mu.Lock(); c.lineWidth = w; c.mu.Unlock() }

// ---- 矩形原语 ----

// FillRect 画一个填充矩形。
// GX: ctx.fillRect(x, y, w, h)
func (c *CanvasContext) FillRect(x, y, w, h float32) {
	c.mu.Lock()
	c.ops = append(c.ops, canvas.Op{Kind: canvas.OpRect, X: x, Y: y, W: w, H: h, Fill: c.fillStyle})
	c.mu.Unlock()
	c.flush()
}

// StrokeRect 画一个描边矩形。
func (c *CanvasContext) StrokeRect(x, y, w, h float32) {
	c.mu.Lock()
	c.ops = append(c.ops, canvas.Op{
		Kind: canvas.OpRect, X: x, Y: y, W: w, H: h,
		Stroke: c.strokeStyle, StrokeWidth: c.lineWidth,
	})
	c.mu.Unlock()
	c.flush()
}

// ClearRect 清空整份绘制指令（画布重置为空白）。
// 注意：Web 的 clearRect 只擦局部；本协议没有合成/擦除原语，故语义为「整幅清空」。
func (c *CanvasContext) ClearRect(_, _, _, _ float32) {
	c.mu.Lock()
	c.ops = nil
	c.mu.Unlock()
	c.flush()
}

// ---- 路径原语（累积成一条 SVG path，stroke/fill 时提交为 OpPath）----

func f(v float32) string { return strconv.FormatFloat(float64(v), 'f', -1, 32) }

// BeginPath 开始一条新路径，丢弃之前累积的路径命令（不影响已提交的图形）。
func (c *CanvasContext) BeginPath() { c.mu.Lock(); c.path = nil; c.mu.Unlock() }

// MoveTo 把当前点移到 (x,y)，对应 SVG M。
func (c *CanvasContext) MoveTo(x, y float32) {
	c.mu.Lock()
	c.path = append(c.path, "M"+f(x)+" "+f(y))
	c.mu.Unlock()
}

// LineTo 从当前点画直线到 (x,y)，对应 SVG L。
func (c *CanvasContext) LineTo(x, y float32) {
	c.mu.Lock()
	c.path = append(c.path, "L"+f(x)+" "+f(y))
	c.mu.Unlock()
}

// ClosePath 闭合当前路径，对应 SVG Z。
func (c *CanvasContext) ClosePath() {
	c.mu.Lock()
	c.path = append(c.path, "Z")
	c.mu.Unlock()
}

// Arc 追加一段圆弧到当前路径。后端 SVG 子集无 A 命令，用三次贝塞尔分段近似。
// 若路径为空，先移到弧起点，行为与 Web canvas 一致。
func (c *CanvasContext) Arc(cx, cy, r, start, end float32) {
	cmds := arcBezier(cx, cy, r, start, end)
	c.mu.Lock()
	if len(c.path) == 0 && len(cmds) > 0 {
		c.path = append(c.path, cmds[0]) // 首段已含 M 起点
		c.path = append(c.path, cmds[1:]...)
	} else {
		c.path = append(c.path, cmds...)
	}
	c.mu.Unlock()
}

// Stroke 用当前描边样式提交路径为一条 OpPath（不清除路径，可再 fill）。
func (c *CanvasContext) Stroke() {
	c.mu.Lock()
	d := strings.Join(c.path, " ")
	if d == "" {
		c.mu.Unlock()
		return
	}
	c.ops = append(c.ops, canvas.Op{
		Kind: canvas.OpPath, D: d, Stroke: c.strokeStyle, StrokeWidth: c.lineWidth,
	})
	c.mu.Unlock()
	c.flush()
}

// Fill 用当前填充样式提交路径为一条 OpPath。
func (c *CanvasContext) Fill() {
	c.mu.Lock()
	d := strings.Join(c.path, " ")
	if d == "" {
		c.mu.Unlock()
		return
	}
	c.ops = append(c.ops, canvas.Op{Kind: canvas.OpPath, D: d, Fill: c.fillStyle})
	c.mu.Unlock()
	c.flush()
}

// ---- 写回：指令 → Props + Change + 重绘 ----

// flush 把当前指令列表编码写回 canvas 节点的 Props["canvas-ops"]，
// 记一条 ChangeSetAttr 供增量后端，并请求宿主重绘。锁外执行，避免重入自锁。
func (c *CanvasContext) flush() {
	c.mu.Lock()
	encoded := canvas.Encode(c.ops)
	el := c.el
	c.mu.Unlock()
	if el == nil || el.dead || el.node == nil {
		return
	}
	if el.node.Props == nil {
		el.node.Props = map[string]any{}
	}
	el.node.Props[canvas.PropKey] = encoded
	// 增量后端（Rust）走 ChangeSetAttr；Skia 直接读 host 节点 Props。
	if el.owner != nil {
		el.owner.record(ChangeSetAttr, el.node, canvas.PropKey, encoded)
	}
	// 请求重绘；未注入宿主（纯 scene/headless）时 no-op，测试可自行 Draw。
	if el.sink != nil {
		el.sink.dirty()
	}
}

// arcBezier 用三次贝塞尔分段近似圆弧，返回 SVG 子命令。
// 每段 ≤ π/2；整圆分 4 段。kappa 是四分之一圆贝塞尔控制点系数。
func arcBezier(cx, cy, r, start, end float32) []string {
	if r <= 0 {
		return nil
	}
	twoPi := float32(2 * math.Pi)
	sweep := end - start
	if sweep <= 0 {
		sweep += twoPi // 逆时针/反向按正向整圈处理
	}
	const kappa = 0.5522847498
	segs := int(math.Ceil(float64(sweep) / (math.Pi / 2)))
	if segs < 1 {
		segs = 1
	}
	step := sweep / float32(segs)
	out := make([]string, 0, segs+1)

	pt := func(a float32) (float32, float32) {
		return cx + r*float32(math.Cos(float64(a))), cy + r*float32(math.Sin(float64(a)))
	}
	// 起点：M（由调用方决定是否保留为路径首命令）
	sx, sy := pt(start)
	out = append(out, "M"+f(sx)+" "+f(sy))

	for i := 0; i < segs; i++ {
		a0 := start + step*float32(i)
		a1 := a0 + step
		x0, y0 := pt(a0)
		x1, y1 := pt(a1)
		// 圆弧在 a0/a1 的切线方向，控制点沿切线外推 kappa*r
		t0 := a0 + float32(math.Pi/2)
		t1 := a1 - float32(math.Pi/2)
		c1x, c1y := x0+kappa*r*float32(math.Cos(float64(t0))), y0+kappa*r*float32(math.Sin(float64(t0)))
		c2x, c2y := x1+kappa*r*float32(math.Cos(float64(t1))), y1+kappa*r*float32(math.Sin(float64(t1)))
		out = append(out, "C"+f(c1x)+" "+f(c1y)+" "+f(c2x)+" "+f(c2y)+" "+f(x1)+" "+f(y1))
	}
	return out
}
