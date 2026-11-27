//go:build js && wasm

package gxweb

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"syscall/js"

	"github.com/robertpanvip/gox/runtime/canvas"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// jsDriver 是 syscall/js 驱动：直接操作浏览器 DOM。它是 memDriver 的镜像实现，
// 共享同一套 Driver 契约；后端核心无需知道运行在哪个平台。
type jsDriver struct {
	mu       sync.Mutex
	next     ElemID
	elements map[ElemID]*jsElem
	root     js.Value // 挂载容器
	doc      js.Value
	win      js.Value
	funcs    []js.Func // 保活：js.Func 被 GC 后回调失效

	// selectionInjected 标记全局 ::selection 规则是否已注入（懒加载一次）。
	selectionInjected bool
	// animationInjected 标记全局 @keyframes 规则是否已注入（懒加载一次）。
	animationInjected bool

	// RAF 投递：同一帧内多次 Post 只登记一次 requestAnimationFrame。
	// 浏览器在下一帧回调里统一 drain，避免事件处理期间同步重入 DOM 提交。
	rafOnce      js.Func
	rafQueue     []func()
	rafScheduled bool
}

type jsElem struct {
	id        ElemID
	nodeID    gxscene.NodeID
	v         js.Value
	kind      string
	listeners map[string]js.Func
	canvasCtx js.Value
}

func newJSDriver(root js.Value) *jsDriver {
	d := &jsDriver{
		elements: map[ElemID]*jsElem{},
		doc:      js.Global().Get("document"),
		win:      js.Global(),
	}
	if !root.Truthy() {
		root = d.doc.Get("body")
	}
	d.root = root
	d.rafOnce = js.FuncOf(func(this js.Value, args []js.Value) any {
		d.runRAF()
		return nil
	})
	d.funcs = append(d.funcs, d.rafOnce)
	return d
}

// postRAF 把 fn 排进下一帧。
//
// 去重说明（rafScheduled）：早期实现用「同帧只注册一次 RAF」去重。但 Go 1.27
// 栈切换（pause/PAUSE）事件机存在「pause 后第一个事件仅复位 PAUSE、回调不执行」
// 的行为：首个 RAF 回调被吃掉后 runRAF 永不执行，rafScheduled 永久卡 true，
// 队列无限积压——表现为 web 端 UI 冻结在首帧（playground 全部交互失效的根因）。
// 改为每次 post 都注册 RAF：runRAF 幂等（空队列空跑），浏览器同帧合并回调，
// 即使个别回调被事件机吃掉，后续注册仍会驱动队列消费。
func (d *jsDriver) postRAF(fn func()) {
	if fn == nil {
		return
	}
	d.mu.Lock()
	d.rafQueue = append(d.rafQueue, fn)
	d.rafScheduled = true
	d.win.Call("requestAnimationFrame", d.rafOnce)
	// 诊断通道：把回调暴露到 window，供 E2E 手动驱动/验证注册链。
	js.Global().Set("__gxRAF", d.rafOnce)
	d.mu.Unlock()
}

// runRAF 执行并清空本帧队列。在锁外调用 fn，允许回调继续 Post 下一帧。
func (d *jsDriver) runRAF() {
	d.mu.Lock()
	q := d.rafQueue
	d.rafQueue = nil
	d.rafScheduled = false
	d.mu.Unlock()
	for _, fn := range q {
		fn()
	}
}

// Post 实现 gxapp.UIExecutor：把任务投递到浏览器下一帧执行。
// UIExecutorDispatcher 已对同一帧内的多次 RequestFrame 去抖，因此一次点击/按键
// 只会提交一帧 DOM 补丁，且事件回调本身不再被同步绘制阻塞。
func (w *Window) Post(fn func()) error {
	if fn == nil {
		return nil
	}
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return fmt.Errorf("gxweb: 窗口已关闭，无法投递 RAF 任务")
	}
	d, ok := w.driver.(*jsDriver)
	if !ok {
		return fmt.Errorf("gxweb: RAF 投递需要 js 驱动")
	}
	d.postRAF(fn)
	return nil
}

// Mount 把元素挂到宿主容器（document.querySelector 选中的节点，缺省 body）。
func (d *jsDriver) Mount(e ElemID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if el := d.elements[e]; el != nil {
		d.root.Call("appendChild", el.v)
	}
}

func (d *jsDriver) Create(kind string, id gxscene.NodeID) ElemID {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.next++
	var v js.Value
	if kind == "#text" {
		v = d.doc.Call("createTextNode", "")
	} else {
		tag := kind
		switch tag {
		case "#root", "#frag", "", "div-frag":
			tag = "div"
		}
		v = d.doc.Call("createElement", tag)
		if tag == "button" {
			// 原生 button 的默认光标是 default，可点击控件统一 pointer。
			// 之后 GX 的 cursor 样式属性（isStyleKey）仍可覆盖此默认值。
			v.Get("style").Call("setProperty", "cursor", "pointer")
		}
	}
	e := &jsElem{id: d.next, nodeID: id, v: v, kind: kind, listeners: map[string]js.Func{}}
	d.elements[e.id] = e
	return e.id
}

func (d *jsDriver) Destroy(e ElemID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	el := d.elements[e]
	if el == nil {
		return
	}
	for name, fn := range el.listeners {
		el.v.Call("removeEventListener", name, fn)
		fn.Release()
		delete(el.listeners, name)
	}
	el.v.Call("remove")
	delete(d.elements, e)
}

func (d *jsDriver) InsertBefore(parent, child, before ElemID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, c := d.elements[parent], d.elements[child]
	if p == nil || c == nil {
		return
	}
	var anchor js.Value
	if before != 0 {
		if b := d.elements[before]; b != nil {
			anchor = b.v
		}
	}
	if anchor.Truthy() {
		p.v.Call("insertBefore", c.v, anchor)
	} else {
		p.v.Call("appendChild", c.v)
	}
}

// injectSelectionCSSLocked 懒注入一次全局 ::selection 规则（调用方持 d.mu）。
// 用 id 幂等，避免宿主 HTML 已内置同名规则时重复注入。
func (d *jsDriver) injectSelectionCSSLocked() {
	if d.selectionInjected {
		return
	}
	d.selectionInjected = true
	if d.doc.Call("getElementById", "gx-selection-css").Truthy() {
		return
	}
	style := d.doc.Call("createElement", "style")
	style.Call("setAttribute", "id", "gx-selection-css")
	style.Set("textContent", selectionGlobalCSS)
	head := d.doc.Get("head")
	if head.Truthy() {
		head.Call("appendChild", style)
	} else if d.doc.Get("body").Truthy() {
		d.doc.Get("body").Call("appendChild", style)
	}
}

// injectAnimationCSSLocked 懒注入一次内置 @keyframes（用 id 幂等），与 ::selection
// 规则同构。所有内置动画的关键帧集中在此，宿主零 CSS 依赖。
func (d *jsDriver) injectAnimationCSSLocked() {
	if d.animationInjected {
		return
	}
	d.animationInjected = true
	if d.doc.Call("getElementById", "gx-animation-css").Truthy() {
		return
	}
	style := d.doc.Call("createElement", "style")
	style.Call("setAttribute", "id", "gx-animation-css")
	style.Set("textContent", animationGlobalCSS)
	head := d.doc.Get("head")
	if head.Truthy() {
		head.Call("appendChild", style)
	} else if d.doc.Get("body").Truthy() {
		d.doc.Get("body").Call("appendChild", style)
	}
}

func (d *jsDriver) SetAttr(e ElemID, key, value string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	el := d.elements[e]
	if el == nil {
		return
	}
	if key == "canvas-ops" {
		d.canvasOpsLocked(el, value)
		return
	}
	// ::selection 伪元素子表：CSS 伪元素无法用内联 style 直接设置，改为
	// 注入一条全局规则读取元素上的自定义属性，并把子表的 bg/color 写进
	// 这两个变量。变量缺省时全局规则的 var() 无值，浏览器保留默认选区。
	if isSelectionKey(key) {
		d.injectSelectionCSSLocked()
		bg, fg := selectionVars(value)
		style := el.v.Get("style")
		if bg == "" {
			style.Call("removeProperty", selectionVarBg)
		} else {
			style.Call("setProperty", selectionVarBg, cssValue("bg", bg))
		}
		if fg == "" {
			style.Call("removeProperty", selectionVarFg)
		} else {
			style.Call("setProperty", selectionVarFg, cssValue("color", fg))
		}
		return
	}
	// gradient-text 是三端统一的语义开关：Skia/Rust 把 bg 渐变裁到文字，
	// Web 端在此翻译成 background-clip:text + color:transparent（浏览器原生
	// 没有 shader，靠 CSS 裁切），使编辑器无需在宿主 HTML 里内联专用 CSS。
	if isGradientTextProp(key) {
		style := el.v.Get("style")
		if gradientTextEnabled(value) {
			style.Call("setProperty", "-webkit-background-clip", "text")
			style.Call("setProperty", "background-clip", "text")
			style.Call("setProperty", "color", "transparent")
		} else {
			style.Call("removeProperty", "-webkit-background-clip")
			style.Call("removeProperty", "background-clip")
			style.Call("removeProperty", "color")
		}
		return
	}
	// animation 是三端统一的声明式动画 prop：翻译成原生 CSS animation，并在
	// 首次使用时注入共享 @keyframes。空/非法值移除属性，回到静态样式。
	if strings.EqualFold(key, "animation") {
		css := animationCSS(value)
		if css == "" {
			el.v.Get("style").Call("removeProperty", "animation")
		} else {
			d.injectAnimationCSSLocked()
			el.v.Get("style").Call("setProperty", "animation", css)
		}
		return
	}
	// scroll-top / scroll-left 是元素滚动位置（el.scrollTop），不是 CSS 属性。
	// 此前被当 style.setProperty("scrollTop") 写入——setProperty 只认 CSS 属性名，
	// 静默无效，虚拟列表滚轮/受控滚动在 web 端从未生效过。
	if key == "scroll-top" || key == "scroll-left" {
		if value == "" {
			return
		}
		if f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(value, "px")), 64); err == nil {
			el.v.Set(cssName(key), f)
		}
		return
	}
	if isStyleKey(key) && !strings.HasPrefix(key, "&:") && !strings.HasPrefix(key, "$:") {
		style := el.v.Get("style")
		if value == "" {
			style.Call("removeProperty", cssName(key))
			style.Call("removeProperty", "background-image") // bg 渐变可能走此键
		} else {
			// GX 场景树用 flex-direction / gap / align-items 声明布局——Skia 端由
			// 布局引擎直接消费；浏览器端这些 CSS 属性只在 display:flex 容器上生效。
			// 因此任何 flex 意图属性出现时，同步把容器切成 flex，保持两端布局一致。
			// 注意只在有值时设置，清属性不反向删除 display（避免误伤宿主自己的布局）。
			if isFlexLayoutKey(key) {
				style.Call("setProperty", "display", "flex")
				// GX 三端约定：flex 容器默认主轴 column（Skia 零值 / Rust
				// unwrap_or("column")）。浏览器初始值 row，未显式声明时补齐。
				if flexDefaultColumn(key, style.Call("getPropertyValue", "flex-direction").String()) {
					style.Call("setProperty", "flex-direction", "column")
				}
			}
			style.Call("setProperty", styleTargetProp(key, value), cssValue(key, value))
		}
		return
	}
	if strings.HasPrefix(key, "&:") || strings.HasPrefix(key, "$:") {
		return // 伪状态子表由 CSS 类承载，见 applyPseudo
	}
	if value == "" {
		el.v.Call("removeAttribute", key)
	} else {
		el.v.Call("setAttribute", key, value)
	}
}

func (d *jsDriver) RemoveAttr(e ElemID, key string) { d.SetAttr(e, key, "") }

func (d *jsDriver) SetText(e ElemID, text string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if el := d.elements[e]; el != nil {
		el.v.Set("textContent", text)
	}
}

// SetRichText 一次 innerHTML 写入着色片段。 syscall/js 的每次跨边界调用都昂贵，
// 富文本把一行的 N 个 token 从 N 次 textContent/setAttribute 合并为 1 次 innerHTML。
func (d *jsDriver) SetRichText(e ElemID, richHTML string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if el := d.elements[e]; el != nil {
		el.v.Set("innerHTML", richHTML)
	}
}

func (d *jsDriver) Listen(e ElemID, event string, cb func(FiredEvent)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	el := d.elements[e]
	if el == nil {
		return
	}
	if old := el.listeners[event]; old.Truthy() {
		el.v.Call("removeEventListener", event, old)
		old.Release()
	}
	fn := js.FuncOf(func(this js.Value, args []js.Value) any {
		ev := mapFiredEvent(event, el.nodeID, args, d.doc)
		cb(ev)
		return nil
	})
	el.listeners[event] = fn
	d.funcs = append(d.funcs, fn)
	el.v.Call("addEventListener", event, fn)
}

func (d *jsDriver) Focus(e ElemID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if el := d.elements[e]; el != nil {
		el.v.Call("focus")
	}
}

func (d *jsDriver) Rect(e ElemID) (x, y, w, h float32, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if el := d.elements[e]; el == nil {
		return 0, 0, 0, 0, false
	} else {
		r := el.v.Call("getBoundingClientRect")
		return float32(r.Get("x").Float()), float32(r.Get("y").Float()),
			float32(r.Get("width").Float()), float32(r.Get("height").Float()), true
	}
}

func (d *jsDriver) ComputedStyle(e ElemID, state string) map[string]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	el := d.elements[e]
	if el == nil {
		return map[string]string{}
	}
	// 伪状态在 DOM 侧用内联样式临时叠加，取完即恢复（同步，不产生可见闪烁）。
	var restore []func()
	if state != "" {
		restore = applyPseudo(el.v, state, d.win)
	}
	cs := d.win.Call("getComputedStyle", el.v)
	out := map[string]string{}
	for _, k := range gxscene.StylePropNames {
		prop := cssName(k)
		v := cs.Call("getPropertyValue", prop).String()
		if v != "" {
			out[k] = v
		}
	}
	for _, f := range restore {
		f()
	}
	return out
}

func (d *jsDriver) CanvasOps(e ElemID, opsJSON string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	el := d.elements[e]
	if el == nil {
		return
	}
	d.canvasOpsLocked(el, opsJSON)
}

func (d *jsDriver) canvasOpsLocked(el *jsElem, opsJSON string) {
	if !el.canvasCtx.Truthy() {
		el.canvasCtx = el.v.Call("getContext", "2d")
	}
	ctx := el.canvasCtx
	if !ctx.Truthy() {
		return
	}
	ops := canvas.Decode(opsJSON)
	// 重置画布尺寸：优先取布局盒（元素已插入 DOM 时准确）；首次建树时
	// CanvasOps 早于 InsertBefore，clientWidth=0，此时回退到 style 里的
	// width/height（GX 组件写入的 CSS 值），否则 bitmap 停留在默认 300×150，
	// 而 CSS 只有 15px，图标被压到亚像素几乎不可见。
	w := el.v.Get("clientWidth").Float()
	h := el.v.Get("clientHeight").Float()
	if w <= 0 || h <= 0 {
		w = cssPixel(el.v.Get("style").Get("width").String())
		h = cssPixel(el.v.Get("style").Get("height").String())
	}
	if w > 0 && h > 0 {
		el.v.Set("width", int(w))
		el.v.Set("height", int(h))
	}
	ctx.Call("clearRect", 0, 0, w, h)
	for _, op := range ops {
		drawOp(ctx, op)
	}
}

// cssPixel 从 CSS 长度值解析出像素数（"15px" → 15）；非长度返回 0。
func cssPixel(s string) float64 {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(s, "px"), "PX"))
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

// Root 返回挂载容器（供外部直接操作，如设置 body 样式）。
func (d *jsDriver) Root() js.Value { return d.root }

// Close 释放所有事件回调，避免泄漏。
func (d *jsDriver) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, el := range d.elements {
		for name, fn := range el.listeners {
			el.v.Call("removeEventListener", name, fn)
			fn.Release()
			delete(el.listeners, name)
		}
	}
	d.elements = map[ElemID]*jsElem{}
}

// ---- DOM 事件 → FiredEvent 映射 ----

func mapFiredEvent(name string, id gxscene.NodeID, args []js.Value, doc js.Value) FiredEvent {
	ev := FiredEvent{Name: name, NodeID: id}
	if len(args) > 0 && args[0].Truthy() {
		e := args[0]
		if t := e.Get("clientX"); t.Truthy() {
			ev.X = t.Float()
		}
		if t := e.Get("clientY"); t.Truthy() {
			ev.Y = t.Float()
		}
		if t := e.Get("button"); t.Truthy() {
			ev.Button = t.Int()
		}
		if t := e.Get("key"); t.Truthy() {
			ev.Key = t.String()
		}
		ev.Mods = readMods(e)
		target := e.Get("target")
		if target.Truthy() {
			if t := target.Get("value"); t.Truthy() {
				ev.Value = t.String()
			}
		}
		ev.Raw = e
	}
	return ev
}

func readMods(e js.Value) uint8 {
	var m uint8
	if e.Get("shiftKey").Truthy() {
		m |= 1
	}
	if e.Get("ctrlKey").Truthy() {
		m |= 2
	}
	if e.Get("altKey").Truthy() {
		m |= 4
	}
	if e.Get("metaKey").Truthy() {
		m |= 8
	}
	return m
}

// ---- canvas 指令绘制（与 Skia/Rust 后端同协议） ----

func drawOp(ctx js.Value, op canvas.Op) {
	switch op.Kind {
	case canvas.OpRect:
		if op.Fill != "" {
			ctx.Set("fillStyle", op.Fill)
			if op.Radius > 0 {
				roundRect(ctx, op.X, op.Y, op.W, op.H, op.Radius)
				ctx.Call("fill")
			} else {
				ctx.Call("fillRect", op.X, op.Y, op.W, op.H)
			}
		}
		if op.Stroke != "" {
			ctx.Set("strokeStyle", op.Stroke)
			ctx.Set("lineWidth", op.StrokeWidth)
			ctx.Call("strokeRect", op.X, op.Y, op.W, op.H)
		}
	case canvas.OpCircle:
		ctx.Call("beginPath")
		ctx.Call("arc", op.CX, op.CY, op.R, 0, 6.283185307179586)
		if op.Fill != "" {
			ctx.Set("fillStyle", op.Fill)
			ctx.Call("fill")
		}
		if op.Stroke != "" {
			ctx.Set("strokeStyle", op.Stroke)
			ctx.Set("lineWidth", op.StrokeWidth)
			ctx.Call("stroke")
		}
	case canvas.OpLine:
		ctx.Call("beginPath")
		ctx.Call("moveTo", op.X1, op.Y1)
		ctx.Call("lineTo", op.X2, op.Y2)
		if op.Stroke != "" {
			ctx.Set("strokeStyle", op.Stroke)
			ctx.Set("lineWidth", op.StrokeWidth)
			ctx.Call("stroke")
		}
	case canvas.OpPath:
		ctx.Call("beginPath")
		applyPath(ctx, op.D)
		if op.Fill != "" {
			ctx.Set("fillStyle", op.Fill)
			ctx.Call("fill")
		}
		if op.Stroke != "" {
			ctx.Set("strokeStyle", op.Stroke)
			ctx.Set("lineWidth", op.StrokeWidth)
			ctx.Call("stroke")
		}
	}
}

func roundRect(ctx js.Value, x, y, w, h, r float32) {
	ctx.Call("beginPath")
	ctx.Call("moveTo", x+r, y)
	ctx.Call("lineTo", x+w-r, y)
	ctx.Call("quadraticCurveTo", x+w, y, x+w, y+r)
	ctx.Call("lineTo", x+w, y+h-r)
	ctx.Call("quadraticCurveTo", x+w, y+h, x+w-r, y+h)
	ctx.Call("lineTo", x+r, y+h)
	ctx.Call("quadraticCurveTo", x, y+h, x, y+h-r)
	ctx.Call("lineTo", x, y+r)
	ctx.Call("quadraticCurveTo", x, y, x+r, y)
	ctx.Call("closePath")
}

// applyPath 解析 SVG path 子集 M/L/H/V/C/Q/Z，与 Rust 后端 SVG 子集一致。
// H/V 是相对当前点的水平/垂直线，需跟踪 lastX/lastY。
func applyPath(ctx js.Value, d string) {
	// SVG path 允许命令字母与数字粘连（如 "M4.375 2.5 L12.5"），不能只按
	// 空白分词——否则 cmd 变成 "M4.375"，switch 匹配不到任何命令，整条路径
	// 被静默忽略（表现为图标 canvas 全空）。这里逐字符切分：命令字母单独成
	// token，数字（含小数/指数/正负号）单独成 token，逗号与空白视作分隔符。
	tokens := tokenizePath(d)
	i := 0
	num := func() float64 {
		if i >= len(tokens) {
			return 0
		}
		f, _ := strconv.ParseFloat(tokens[i], 64)
		i++
		return f
	}
	var lastX, lastY float64
	for i < len(tokens) {
		cmd := tokens[i]
		i++
		switch cmd {
		case "M", "m":
			lastX, lastY = num(), num()
			ctx.Call("moveTo", lastX, lastY)
		case "L", "l":
			lastX, lastY = num(), num()
			ctx.Call("lineTo", lastX, lastY)
		case "H", "h":
			lastX = num()
			ctx.Call("lineTo", lastX, lastY)
		case "V", "v":
			lastY = num()
			ctx.Call("lineTo", lastX, lastY)
		case "C", "c":
			x1, y1, x2, y2, x, y := num(), num(), num(), num(), num(), num()
			ctx.Call("bezierCurveTo", x1, y1, x2, y2, x, y)
			lastX, lastY = x, y
		case "S", "s":
			x2, y2, x, y := num(), num(), num(), num()
			ctx.Call("bezierCurveTo", lastX, lastY, x2, y2, x, y)
			lastX, lastY = x, y
		case "Q", "q":
			cx, cy, x, y := num(), num(), num(), num()
			ctx.Call("quadraticCurveTo", cx, cy, x, y)
			lastX, lastY = x, y
		case "T", "t":
			x, y := num(), num()
			ctx.Call("quadraticCurveTo", lastX, lastY, x, y)
			lastX, lastY = x, y
		case "A", "a":
			rx, ry, rot, large, sweep, x, y := num(), num(), num(), num(), num(), num(), num()
			endAngle := 0.0
			if sweep != 0 {
				endAngle = 6.283185307179586
			}
			ctx.Call("ellipse", lastX, lastY, rx, ry, rot*3.141592653589793/180, 0, endAngle, large != 0)
			lastX, lastY = x, y
		case "Z", "z":
			ctx.Call("closePath")
		}
	}
}

// applyPseudo 为 getComputedStyle(state) 的伪状态请求返回恢复闭包。
//
// 真实 CSS 伪类（:hover/:focus/...）由浏览器在 getComputedStyle 时自动计算；
// 但 JS 的 getComputedStyle 不接受「指定伪状态参数」——它读的是元素当前实际状态。
// 因此这里用 element.matches(sel) 检测伪状态是否命中：命中则无需改动（浏览器已
// 反映），未命中则读取浏览器为该伪类注册的 CSS 变量（若业务用 CSS 伪类声明）。
// 当前实现不修改 DOM，返回 nil 恢复列表——伪状态权威值来自 CSS，非内联。
func applyPseudo(_ js.Value, _ string, _ js.Value) []func() {
	return nil
}

// ProbeRAF 诊断 RAF 投递链状态：scheduled=是否已注册浏览器回调、
// queueLen=排队未执行的任务数。scheduled=true 且 queueLen>0 说明浏览器
// 没有回调 rafOnce（投递链断在浏览器侧）；scheduled=false 且 queueLen>0
// 说明注册被复位但队列未消费（异常路径）。
// 方法体引用 jsDriver（仅 js/wasm 构建存在），因此必须定义在本文件而非
// window.go——否则 native（memDriver）构建因找不到该类型而编译失败。
func (w *Window) ProbeRAF() map[string]any {
	d, ok := w.driver.(*jsDriver)
	if !ok {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return map[string]any{
		"scheduled": d.rafScheduled,
		"queueLen":  len(d.rafQueue),
	}
}
