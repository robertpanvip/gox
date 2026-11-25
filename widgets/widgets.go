// Package widgets 是 skia 后端预制的 GX 控件库。
//
// 它是一组「组件函数」——签名为 func(gxscene.Props) *gxscene.Instance，与 GX
// 编译器对大写组件名的调用约定完全一致。因此 GX 侧只需：
//
//	import widgets "github.com/robertpanvip/gox/widgets"
//	<widgets.button label="Go" variant="primary" onClick={...}/>
//
// 全部外观（配色、圆角、描边、间距、字号）都预制在这里，业务 JSX 只描述语义。
// 包名小写在 GX 侧书写，编译器按 Go 来源约定还原为大写导出名（button → Button）。
//
// 本包只依赖 host 的 gxscene / signal，不依赖 skia 绘制代码——它产出的是「带样式
// 属性的场景树」，任何理解这些 additive 属性的后端都能绘制。因此核心 runtime 零改动。
//
// Input 的焦点与键盘输入也封装在本包：
//   - 每个 Input 用业务传入的 id（Props["gx-input-id"]）作稳定焦点标识——组件嵌在
//     动态 children 插槽里会被反复重建，随机 token 会失效，id 跨重建不变；未提供 id
//     时按组件序号兜底生成。
//   - Focus(node) 写入全局焦点 id（signal），边框/光标 getter 读它 → 自动重绘；
//   - 全局 blink signal 由后台 goroutine 定期翻转，光标 getter 读它 → 光标闪烁；
//   - InputChar/InputBackspace 读焦点节点当前 value、算新文本，派发其 onInput handler。
//
// GUI 层（如 GLFW 适配）只需在命中/键盘回调里调这几个函数，无需理解控件内部结构。
package widgets

import Signal "github.com/robertpanvip/gox/runtime/signal"

import (
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// ---- 预制主题（Apple HIG，默认深色，SetTheme 可整体切换）----
// 集中在这里，业务无需（也不应）在 JSX 里散写颜色。tone 名 → 强调色。
// Theme* 保留原名称以兼容现有业务与测试；它们现在由 SetTheme 同步为当前
// 明暗主题的令牌值，因此旧组件在浅色主题下也不会残留深色控件。
// 更完整的 Apple 配色/圆角/阴影/材质标度见 apple_theme.go。
var (
	ThemeBg       = AppleWindowBg   // 页面底色
	ThemeGrouped  = AppleGroupedBg  // 分组底
	ThemePanel    = AppleElevated   // 卡片底
	ThemeFill     = AppleFill       // 控件填充 / 次级底
	ThemeSkeleton = AppleFill2      // 骨架屏占位块
	ThemeBorder   = AppleSeparator  // 常规描边
	ThemeBorder2  = AppleSeparator2 // 更亮描边 / 分隔线
	ThemeHairline = AppleHairline   // 高光细描边（浮层）
	ThemeMaterial = AppleMaterialThick // 半透明材质（浮层）
	ThemeScrim    = AppleScrim      // 遮罩
	ThemeText     = AppleLabel      // 主文字
	ThemeMuted    = AppleLabel2     // 次要文字/占位符
	ThemeFaint    = AppleLabel3     // 三级文字
	ThemeOnAccent = "#ffffff"       // 强调色背景上的文字
	ThemeAccent   = AppleBlue       // 主强调（系统蓝）
	ThemeInputBg  = AppleInputBg    // 输入框底（光标靠同色隐藏）
	CursorColor   = AppleBlue
)

var tones = map[string]string{
	"primary": AppleBlue,
	"cyan":    AppleTeal,
	"purple":  ApplePurple,
	"orange":  AppleOrange,
	"green":   AppleGreen,
	"yellow":  AppleYellow,
	"pink":    ApplePink,
	"indigo":  AppleIndigo,
	"danger":  AppleRed,
	"info":    AppleBlue,
}

func toneColor(tone string) string {
	if c, ok := appleTones[tone]; ok {
		return c
	}
	if c, ok := tones[tone]; ok {
		return c
	}
	return ThemeAccent
}

// ---------- 焦点与光标状态（全局，signal 驱动重绘）----------

var (
	inputSeq atomic.Uint64 // token 生成器
)

var focusToken = Signal.New(""   )// 当前聚焦输入框的 token；空串表示无焦点
var blinkOn = Signal.New(true )// 光标是否可见

// toggleBlink 经 UI Runtime 投递翻转光标状态。
//
// 它是 blinkOn 唯一的写入点。后台 ticker 只负责定时调用它，绝不直接读写
// 响应式 signal——读写会重建依赖图（clearDeps/track 改 map），与 UI 线程
// 并发写会触发 fatal error: concurrent map writes。UI 未就绪/已关闭时
// Signal.safe 安全跳过，不静默在后台线程执行。
func toggleBlink() {
	Signal.Safe(func() { blinkOn(!blinkOn() )})
}

func init() {
	// 光标闪烁：每 530ms 翻转一次。写 signal 会触发订阅了光标 getter 的 app
	// 聚合 effect 重跑 → Update → Draw。窗口关闭后 effect 被 Dispose，写 signal
	// 不再触发任何重绘。
	go func() {
		t := time.NewTicker(530 * time.Millisecond)
		defer t.Stop()
		for range t.C {
			toggleBlink()
		}
	}()
}

// defaultOnInput 让 Input 在没有 onInput 回调时也能安全派发（丢弃输入）。
func defaultOnInput(_ gxscene.EventContext) {}

// ---------- Button ----------

// Button 预制按钮。props:
//
//	label   按钮文字
//	variant primary / secondary / ghost / danger（默认 secondary）
//	width   像素宽度（默认按内容）
//	height  像素高度（默认按内容，padding 撑开）
//	onClick Handler 事件
func Button(props gxscene.Props) *gxscene.Instance {
	label := gxscene.Get[string](props, "label")
	variant := gxscene.Get[string](props, "variant")
	width := gxscene.Get[string](props, "width")
	height := gxscene.Get[string](props, "height")
	icon := gxscene.Get[string](props, "icon") // 可选前置矢量图标名

	bgSlot, colorSlot, borderSlot := 0, 1, 2
	clickSlot, labelSlot, iconSlot := 3, 4, 5

	// 全部走主题 getter：主题切换时按钮自动重绘（浅色模式不再残留深色底）。
	accentOf := tkTone("primary")
	dangerOf := tkTone("danger")
	panelOf := tkPanel()
	bgOf := tkBg()
	textOf := tkText()
	onAccentOf := tkOnAccent()
	borderOf := tkBorder()

	bgGetter := func() any {
		switch variant() {
		case "primary":
			return accentOf()
		case "danger":
			return dangerOf()
		case "ghost":
			return panelOf()
		default:
			return panelOf()
		}
	}
	colorGetter := func() any {
		switch variant() {
		case "primary", "danger":
			return onAccentOf() // 实色底配白字
		default:
			return textOf()
		}
	}
	borderGetter := func() any {
		switch variant() {
		case "primary":
			return accentOf()
		case "danger":
			return dangerOf()
		case "ghost":
			return bgOf() // 与底同色 → 视觉无描边
		default:
			return borderOf()
		}
	}

	ops := []gxscene.Op{
		el("button"),
		// macOS 控件高 28pt：固定 height + 对称 padding，紧凑对齐 HIG。
		attr("padding", "7"), attr("border-radius", RadiusSmall),
		attr("border-width", HairlineWidth), attr("font-size", FontBody), attr("weight", "bold"),
		// icon + label 横向并排：显式 row，否则默认 column 会把图标与文字竖排。
		attr("flex-direction", "row"), attr("gap", "5"),
		attr("align-items", "center"), attr("justify-content", "center"),
		attr("height", ControlHeight),
		dynAttr("bg", bgSlot), dynAttr("color", colorSlot), dynAttr("border", borderSlot),
	}
	n := 5
	hasIcon := icon() != ""
	if hasIcon {
		n = 6
	}
	if w := width(); w != "" {
		ops = append(ops, attr("width", w))
	}
	if h := height(); h != "" {
		ops = append(ops, attr("height", h))
	}
	ops = append(ops, event("onClick", clickSlot))
	if hasIcon {
		ops = append(ops, gxscene.Op{Code: gxscene.OpComp, Arg: "ButtonIcon", Slot: iconSlot})
	}
	ops = append(ops, dynChild(labelSlot), closeEl())

	getters := []func() any{
		func() any { return bgGetter() },
		func() any { return colorGetter() },
		func() any { return borderGetter() },
		func() any { return gxscene.Get[gxscene.Handler](props, "onClick")() },
		func() any { return label() },
	}
	if hasIcon {
		getters = append(getters, func() any {
			// 实色按钮（primary/danger）图标用白，其它用正文色。
			tone := "text"
			if v := variant(); v == "primary" || v == "danger" {
				tone = "onaccent"
			}
			return iconNode(icon(), 15, tone)
		})
	}
	return gxscene.Render(gxscene.Template{Ops: ops, N: n}, getters...)
}

// ---------- Input ----------

// Input 预制单行输入框。props:
//
//	value       当前文本（string getter，通常传 signal）
//	placeholder 占位符（默认空）
//	onInput     输入回调 Handler；用 BindInput 包装 func(string)
//	width       像素宽度（默认 240）
//	tone        聚焦强调色（默认 primary）
//
// 根节点标记 Props["gx-input-id"]，供 GUI 层做焦点管理与键盘派发。
//
// id 是业务传入的稳定标识（HTML input 也有 id 概念）：Input 嵌在动态 children 插槽
// 里会被反复重建，随机 token 每重建一次就变、焦点会丢；id 跨重建不变。未传 id 时
// 才按组件序号兜底生成（这种情形下的焦点稳定性由调用方负责）。
func Input(props gxscene.Props) *gxscene.Instance {
	id := gxscene.Get[string](props, "id")
	focusID := id()
	if focusID == "" {
		focusID = "input-" + strconv.FormatUint(inputSeq.Add(1), 10)
	}
	value := gxscene.Get[string](props, "value")
	placeholder := gxscene.Get[string](props, "placeholder")
	width := gxscene.Get[string](props, "width")
	tone := gxscene.Get[string](props, "tone")
	icon := gxscene.Get[string](props, "icon") // 可选前置矢量图标名

	w := width()
	if w == "" {
		w = "240"
	}
	// 全部主题 getter：跟随明暗切换。
	accentOf := tkTone(tone())
	inputBgOf := tkInput()
	borderOf := tkBorder()
	textOf := tkText()
	mutedOf := tkMuted()

	const (
		borderSlot = iota
		bgSlot
		textColorSlot
		cursorSlot
		textSlot
		valueSlot // 把当前值写回根 Props["value"]，供 GUI 层计算插入/退格后的新文本
		inputSlot
		iconSlot
		nSlots
	)

	hasIcon := icon() != ""
	ops := []gxscene.Op{
		el("input"),
		attr("gx-input-id", focusID),
		attr("border-radius", RadiusSmall),
		// macOS 输入框高 28pt：固定 height + padding，13px 文字自然居中。
		attr("border-width", HairlineWidth), attr("padding", "6"), attr("height", ControlHeight),
		attr("flex-direction", "row"), attr("align-items", "center"), attr("width", w),
		dynAttr("bg", bgSlot), dynAttr("border", borderSlot),
		dynAttr("value", valueSlot),
		event("onInput", inputSlot),
	}
	if hasIcon {
		// 可选前置矢量图标（如 search），随主题用 muted 色。
		ops = append(ops, gxscene.Op{Code: gxscene.OpComp, Arg: "InputIcon", Slot: iconSlot})
	}
	ops = append(ops,
		// 文本：值非空显示值（亮色），否则显示占位符（灰）。
		el("span"), attr("font-size", FontBody), dynAttr("color", textColorSlot), dynChild(textSlot), closeEl(),
		// 光标：2px 宽的色块；未聚焦或非闪烁相位时用输入底同色隐藏。
		el("span"), attr("width", "2"), attr("height", "13"), dynAttr("bg", cursorSlot), closeEl(),
		closeEl(),
	)

	focused := func() bool { return focusToken() == focusID }
	text := func() string {
		v := value()
		if v != "" {
			return v
		}
		return placeholder()
	}

	// 无前置图标时不声明 iconSlot，N 收敛到 inputSlot+1，避免空 getter 返回 nil
	// 被 embedValue 当子树解引用而崩溃。
	n := nSlots
	if !hasIcon {
		n = inputSlot + 1
	}
	return gxscene.Render(gxscene.Template{Ops: ops, N: n},
		func() any { // border
			if focused() {
				return accentOf()
			}
			return borderOf()
		},
		func() any { // bg
			return inputBgOf()
		},
		func() any { // text color
			if value() != "" {
				return textOf()
			}
			return mutedOf()
		},
		func() any { // cursor bg
			if focused() && blinkOn() {
				return accentOf()
			}
			return inputBgOf()
		},
		func() any { return text() },
		func() any { return value() },
		func() any {
			h := gxscene.Get[gxscene.Handler](props, "onInput")()
			if h == nil {
				return gxscene.Handler(defaultOnInput)
			}
			return h
		},
		func() any { return iconNode(icon(), 15, "muted") },
	)
}

// BindInput 把 func(string) 包装成 Input 的 onInput Handler。
// 新文本从 EventContext.Synthetic.Data["text"] 取出后回调 fn——业务侧写：
//
//	<widgets.input value={text} onInput={widgets.bindInput(func(s string) { text = s })}/>
func BindInput(fn func(string)) gxscene.Handler {
	return gxscene.Handler(func(ctx gxscene.EventContext) {
		s := ""
		if ctx.Synthetic != nil {
			if v, ok := ctx.Synthetic.Data["text"]; ok {
				s, _ = v.(string)
			}
		}
		fn(s)
	})
}

// IsInput 报告节点是否是预制输入框（带焦点 id 标记）。
func IsInput(n *gxscene.Node) bool {
	if n == nil || n.Props == nil {
		return false
	}
	_, ok := n.Props["gx-input-id"]
	return ok
}

// Focus 把节点设为当前焦点输入框；非输入框则清空焦点。
// 写焦点 id 会触发订阅了边框/光标 getter 的 effect 重绘。
func Focus(n *gxscene.Node) {
	if !IsInput(n) {
		focusToken("")
		return
	}
	focusToken(fmt.Sprint(n.Props["gx-input-id"]))
}

// Blur 清空焦点。
func Blur() { focusToken("" )}

// Focused 返回当前是否有聚焦输入框。
func Focused() bool { return focusToken() != "" }

// FocusedInputIn 从当前帧场景树里找回焦点输入框节点。
// 用稳定的 gx-input-id 而非裸指针定位，嵌在动态插槽里的组件被重建后仍能命中；
// GUI 层每次按键时调用它。
func FocusedInputIn(root *gxscene.Node) *gxscene.Node {
	id := focusToken()
	if id == "" {
		return nil
	}
	var found *gxscene.Node
	var walk func(*gxscene.Node)
	walk = func(n *gxscene.Node) {
		if found != nil || n == nil {
			return
		}
		if n.Props != nil {
			if v, ok := n.Props["gx-input-id"]; ok && fmt.Sprint(v) == id {
				found = n
				return
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return found
}

// inputValue 读输入框节点当前显示的值（由动态属性插槽写入 Props）。
func inputValue(n *gxscene.Node) string {
	if n == nil || n.Props == nil {
		return ""
	}
	v, _ := n.Props["value"].(string)
	return v
}

// dispatchText 计算新文本并派发节点上的 onInput handler。
func dispatchText(n *gxscene.Node, next string) bool {
	if n == nil {
		return false
	}
	h := n.Handler("onInput")
	if h == nil {
		return false
	}
	fn, ok := h.(gxscene.Handler)
	if !ok {
		return false
	}
	fn(gxscene.EventContext{
		Synthetic: &gxscene.SyntheticEvent{
			Type:       gxscene.EvInput,
			NativeName: "onInput",
			Target:     n,
			Data:       map[string]any{"text": next},
		},
	})
	return true
}

// InputChar 把一个可打印字符追加到焦点输入框，返回是否处理。
func InputChar(n *gxscene.Node, ch rune) bool {
	if !IsInput(n) {
		return false
	}
	return dispatchText(n, inputValue(n)+string(ch))
}

// InputBackspace 删除焦点输入框末尾一个字符（按 rune），返回是否处理。
func InputBackspace(n *gxscene.Node) bool {
	if !IsInput(n) {
		return false
	}
	rs := []rune(inputValue(n))
	if len(rs) == 0 {
		return false
	}
	return dispatchText(n, string(rs[:len(rs)-1]))
}

// ---------- Card / Badge / Progress / Divider ----------

// Card 预制圆角卡片容器。props: title, width, padding, tone（左侧强调条颜色），children。
// Card 已迁移到 card.gx（GX 语言编写），编译产物为 card.go。

// Badge 预制小徽章。props: label, tone, dot（是否前置圆点）。
func Badge(props gxscene.Props) *gxscene.Instance {
	label := gxscene.Get[string](props, "label")
	tone := gxscene.Get[string](props, "tone")
	dot := gxscene.Get[bool](props, "dot")

	toneOf := tkTone(tone())
	panelOf := tkPanel()

	const (
		bgSlot = iota
		colorSlot
		labelSlot
		nSlots
	)
	ops := []gxscene.Op{
		el("span"), attr("border-radius", RadiusPill),
		attr("padding", "5"), attr("font-size", FontFootnote), attr("weight", "bold"),
		attr("flex-direction", "row"), attr("align-items", "center"), attr("gap", "5"),
		dynAttr("bg", bgSlot), dynAttr("color", colorSlot),
	}
	if dot() {
		ops = append(ops,
			el("span"), attr("width", "7"), attr("height", "7"),
			attr("border-radius", "4"), dynAttr("bg", colorSlot), closeEl(),
		)
	}
	ops = append(ops, dynChild(labelSlot), closeEl())
	return gxscene.Render(gxscene.Template{Ops: ops, N: nSlots},
		func() any { return panelOf() },
		func() any { return toneOf() },
		func() any { return label() },
	)
}

// Progress 预制进度条。props: value(0-100), width, tone, label。
func Progress(props gxscene.Props) *gxscene.Instance {
	value := gxscene.Get[int](props, "value")
	width := gxscene.Get[string](props, "width")
	tone := gxscene.Get[string](props, "tone")
	label := gxscene.Get[string](props, "label")

	w := width()
	if w == "" {
		w = "300"
	}
	trackW, _ := strconv.Atoi(w)
	accentOf := tkTone(tone())
	mutedOf := tkMuted()
	trackOf := tkInput()

	const (
		fillSlot = iota
		trackBgSlot
		fillBgSlot
		labelColorSlot
		labelSlot
		nSlots
	)
	ops := []gxscene.Op{
		el("div"), attr("flex-direction", "column"), attr("gap", "6"), attr("width", w),
		el("span"), attr("font-size", FontCallout), attr("weight", "bold"),
		dynAttr("color", labelColorSlot), dynChild(labelSlot), closeEl(),
		// macOS 进度条：轨道 6px、圆角胶囊、强调色填充。
		el("div"), attr("border-radius", RadiusPill), attr("height", "6"),
		dynAttr("bg", trackBgSlot),
		el("div"), attr("height", "6"), attr("border-radius", RadiusPill),
		dynAttr("bg", fillBgSlot), dynAttr("width", fillSlot), closeEl(),
		closeEl(), closeEl(),
	}
	return gxscene.Render(gxscene.Template{Ops: ops, N: nSlots},
		func() any {
			v := value()
			if v < 0 {
				v = 0
			}
			if v > 100 {
				v = 100
			}
			return strconv.Itoa(trackW * v / 100)
		},
		func() any { return trackOf() },
		func() any { return accentOf() },
		func() any { return mutedOf() },
		func() any { return label() },
	)
}

// Divider 预制分隔线。props: width（默认 100% 由父撑开）。
func Divider(props gxscene.Props) *gxscene.Instance {
	width := gxscene.Get[string](props, "width")
	borderOf := tkBorder()
	ops := []gxscene.Op{el("div"), attr("height", "1"), dynAttr("bg", 0)}
	if w := width(); w != "" {
		ops = append(ops, attr("width", w))
	}
	ops = append(ops, closeEl())
	return gxscene.Render(gxscene.Template{Ops: ops, N: 1},
		func() any { return borderOf() },
	)
}

// Row 预制横向 flex 容器。props: gap, align, justify, children。
func Row(props gxscene.Props) *gxscene.Instance {
	return flexBox(props, "row")
}

// Col 预制纵向 flex 容器。props: gap, align, justify, children。
func Col(props gxscene.Props) *gxscene.Instance {
	return flexBox(props, "column")
}

// flexStyleKeys 是 Col/Row 额外透传给后端的通用样式属性（静态值）。
// 这样预制容器也能用 bg / shadow / absolute / overflow 等新样式，
// 而不必为每个属性改组件 API——属性名与后端 style.go 约定一致。
var flexStyleKeys = []string{
	"bg", "background", "height", "padding", "border-radius", "border", "border-width",
	"color", "font-size", "weight", "opacity",
	"box-shadow", "transition", "transform",
	"position", "top", "right", "bottom", "left",
	"overflow", "scroll-top", "scroll-left", "flex-wrap", "align-content", "row-gap", "column-gap",
}

func flexBox(props gxscene.Props, dir string) *gxscene.Instance {
	gap := gxscene.Get[string](props, "gap")
	align := gxscene.Get[string](props, "align")
	justify := gxscene.Get[string](props, "justify")
	width := gxscene.Get[string](props, "width")
	children := gxscene.Child(props)

	ops := []gxscene.Op{el("div"), attr("flex-direction", dir)}
	if g := gap(); g != "" {
		ops = append(ops, attr("gap", g))
	}
	if a := align(); a != "" {
		ops = append(ops, attr("align-items", a))
	}
	if j := justify(); j != "" {
		ops = append(ops, attr("justify-content", j))
	}
	if w := width(); w != "" {
		ops = append(ops, attr("width", w))
	}
	// 透传其它静态样式属性（值转字符串；getter 在组件构建期求值一次）。
	for _, k := range flexStyleKeys {
		if v, ok := props[k]; ok {
			if s, ok := propToStyleString(v); ok && s != "" {
				ops = append(ops, attr(k, s))
			}
		}
	}
	ops = append(ops, dynChild(0), closeEl())
	return gxscene.Render(gxscene.Template{Ops: ops, N: 1},
		func() any { return children() },
	)
}

// propToStyleString 把静态标量转成样式字符串；getter/复合类型跳过。
func propToStyleString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case int:
		return fmt.Sprintf("%d", x), true
	case int64:
		return fmt.Sprintf("%d", x), true
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(x), 'g', -1, 32), true
	default:
		return "", false
	}
}
