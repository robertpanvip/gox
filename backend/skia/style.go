package skia

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/robertpanvip/gox/cssbox"
)

// 本后端对 Node.Props 的样式约定（additive，host 不解释这些键）。
// host 只负责把 JSX 属性原样放进 Props，具体语义由后端决定——这正体现了
// "host 描述结构、后端解释表现" 的解耦。未列出的键被忽略，不影响绘制。
//
// 基础：
//
//	color / bg(background)  颜色
//	width / height          尺寸（整数或 "120px"）
//	font-size / padding / weight
//	border-radius / border / border-width
//	opacity                 0..1，作用于背景与文本 alpha
//
// flexbox（Yoga 3.x，语义同 CSS）：
//
//	flex-direction / flex-wrap / justify-content / align-items / align-content
//	gap / row-gap / column-gap
//
// 定位（position:absolute + inset）：
//
//	position       "relative"(默认) / "absolute"
//	top/right/bottom/left   inset（整数或 px）；absolute 相对直接父节点的 content box
//
// 溢出与滚动（overflow + 虚拟滚动容器）：
//
//	overflow       "visible"(默认) / "hidden" / "scroll" / "auto"
//	scroll-top / scroll-left   滚动偏移（px）；容器按此裁剪并对子树施加偏移
//
// 变换与动画：
//
//	transform      "translateX(10px) translateY(-4px) scale(1.1)" / "translate(x,y)"
//	transition     "opacity 300ms ease, transform 200ms linear, width .4s ease-in-out"
//
// 阴影：
//
//	box-shadow     "offsetX offsetY blur spread color"，如 "0 4px 12px 0 rgba(0,0,0,0.35)"
const (
	propColor        = "color"
	propBg           = "bg"
	propBgAlt        = "background"
	propWidth        = "width"
	propHeight       = "height"
	propFontSize     = "font-size"
	propFontFamily   = "font-family"
	propPadding      = "padding"
	propWeight       = "weight"
	propRadius       = "border-radius"
	propBorder       = "border"
	propBorderWidth  = "border-width"
	propOpacity      = "opacity"
	propDirection    = "flex-direction"
	propWrap         = "flex-wrap"
	propJustify      = "justify-content"
	propAlign        = "align-items"
	propAlignContent = "align-content"
	propGap          = "gap"
	propRowGap       = "row-gap"
	propColumnGap    = "column-gap"

	// flex 弹性与尺寸约束
	propFlexGrow   = "flex-grow"
	propFlexShrink = "flex-shrink"
	propMinWidth   = "min-width"
	propMaxWidth   = "max-width"
	propMinHeight  = "min-height"
	propMaxHeight  = "max-height"

	// 外边距（四边单独 + 1~4 值简写）
	propMargin       = "margin"
	propMarginTop    = "margin-top"
	propMarginRight  = "margin-right"
	propMarginBottom = "margin-bottom"
	propMarginLeft   = "margin-left"

	// 定位
	propPosition = "position"
	propTop      = "top"
	propRight    = "right"
	propBottom   = "bottom"
	propLeft     = "left"

	// 溢出与滚动
	propOverflow   = "overflow"
	propScrollTop  = "scroll-top"
	propScrollLeft = "scroll-left"

	// 变换与动画
	propTransform  = "transform"
	propTransition = "transition"

	// 阴影
	propBoxShadow = "box-shadow"
)

// flexDirection 是 flex 主轴方向。零值 column 兼容原有纵向布局。
type flexDirection int

const (
	directionColumn flexDirection = iota
	directionRow
	directionRowReverse
	directionColumnReverse
)

type flexWrap int

const (
	wrapNoWrap flexWrap = iota
	wrapWrap
	wrapReverse
)

type justifyContent int

const (
	justifyStart justifyContent = iota
	justifyCenter
	justifyEnd
	justifySpaceBetween
	justifySpaceAround
	justifySpaceEvenly
)

type alignItems int

const (
	alignStretch alignItems = iota
	alignFlexStart
	alignFlexEnd
	alignCenter
	alignBaseline
)

type alignContent int

const (
	contentStretch alignContent = iota
	contentFlexStart
	contentFlexEnd
	contentCenter
	contentSpaceBetween
	contentSpaceAround
)

// positionMode 是定位方式。relative 参与 flex 流（默认），absolute 脱离流并按 inset 定位。
type positionMode int

const (
	positionRelative positionMode = iota
	positionAbsolute
)

// overflowMode 是内容溢出策略。
type overflowMode int

const (
	overflowVisible overflowMode = iota
	overflowHidden
	overflowScroll
	overflowAuto
)

// easing 是 transition 缓动函数类型。
type easing int

const (
	easeLinear  easing = iota
	easeDefault        // "ease" = cubic-bezier(.25,.1,.25,1)
	easeIn
	easeOut
	easeInOut
)

// edges 是四边 inset，索引 0..3 = top/right/bottom/left；math.NaN() 表示 auto。
type edges [4]float32

// boxShadow 是阴影参数。
type boxShadow struct {
	offsetX, offsetY float32
	blur, spread     float32
	color            uint32 // ARGB（含 alpha）
}

// transformSpec 是 2D 变换（translate + 等比 scale）。
type transformSpec struct {
	translateX, translateY float32
	scale                  float32 // 默认 1
}

// transitionItem 是单条 transition 声明。
type transitionItem struct {
	property string
	duration float32 // 毫秒
	delay    float32 // 毫秒
	easing   easing
}

// style 是一帧里从 Props 解析出的已解析样式值。
type style struct {
	color    uint32 // ARGB
	hasColor bool
	bg       uint32 // ARGB
	hasBg    bool
	width    float32
	height   float32
	fontSize int
	padding  edges // 四边内边距 [top,right,bottom,left]，支持 1~4 值简写
	bold     bool
	opacity  float32 // 0..1

	// 视觉修饰：圆角、描边、阴影
	radius      float32
	border      uint32 // ARGB
	hasBorder   bool
	borderWidth float32
	shadow      boxShadow
	hasShadow   bool

	// 定位
	position positionMode
	inset    edges

	// 溢出与滚动
	overflow   overflowMode
	scrollTop  float32
	scrollLeft float32

	// 变换与动画
	transform  transformSpec
	transition []transitionItem

	// flex 布局字段
	direction    flexDirection
	wrap         flexWrap
	justify      justifyContent
	align        alignItems
	alignContent alignContent
	gap          float32
	rowGap       float32
	columnGap    float32

	// flex 弹性与尺寸约束（NaN=未设置；未设置的属性沿用 Yoga 默认值，
	// 与"只透传显式样式、不改历史行为"的原则一致）。
	flexGrow   float32
	flexShrink float32
	minWidth   float32
	maxWidth   float32
	minHeight  float32
	maxHeight  float32
	// 四边外边距 [top,right,bottom,left]，0=未设置（负外边距不支持）。
	margin edges
}

func defaultStyle() style {
	return style{
		color: argb(255, 0, 0, 0), fontSize: 16, padding: edges{4, 4, 4, 4},
		opacity: 1, transform: transformSpec{scale: 1},
	}
}

// parseStyleWithState 先按强制伪状态覆盖 props，再解析。供 headless 验证
// hover/active 样式：给定一个带 "&:hover" 子表的 props 与 state="hover"，
// 返回的 style 反映子表覆盖后的值。state="" 时等价于 parseStyle。
func parseStyleWithState(props map[string]any, state string) style {
	// 无条件走 applyPseudoStyle：交互态由 state 指定，而 disabled 是节点自身属性、
	// 与 state 无关（对齐 Rust——disabled 总被检查，无 state 也必须生效）。
	return parseStyle(applyPseudoStyle(props, state))
}

// parseStyle 把 Node.Props 解析成 style。所有解析失败都静默回退默认值。
func parseStyle(props map[string]any) style {
	s := defaultStyle()
	if props == nil {
		return s
	}
	if v, ok := propString(props[propColor]); ok {
		if c, ok := parseColor(v); ok {
			s.color, s.hasColor = c, true
		}
	}
	for _, k := range []string{propBg, propBgAlt} {
		if v, ok := propString(props[k]); ok {
			if c, ok := parseColor(v); ok {
				s.bg, s.hasBg = c, true
				break
			}
		}
	}
	if v, ok := propNum(props[propWidth]); ok {
		s.width = float32(v)
	}
	if v, ok := propNum(props[propHeight]); ok {
		s.height = float32(v)
	}
	if v, ok := propNum(props[propFontSize]); ok && v > 0 {
		s.fontSize = int(v)
	}
	if v, ok := propString(props[propPadding]); ok {
		if sides, ok := cssbox.Float32s(v); ok {
			// 负值无意义，逐边裁剪到 0。
			for i := range sides {
				if sides[i] < 0 {
					sides[i] = 0
				}
			}
			s.padding = edges(sides)
		}
	}
	// flex 弹性与 min/max 约束：数值或 px 字符串；缺省 NaN 表示未设置。
	if v, ok := propNum(props[propFlexGrow]); ok {
		s.flexGrow = float32(v)
	}
	if v, ok := propNum(props[propFlexShrink]); ok {
		s.flexShrink = float32(v)
	}
	if v, ok := propNum(props[propMinWidth]); ok {
		s.minWidth = float32(v)
	}
	if v, ok := propNum(props[propMaxWidth]); ok {
		s.maxWidth = float32(v)
	}
	if v, ok := propNum(props[propMinHeight]); ok {
		s.minHeight = float32(v)
	}
	if v, ok := propNum(props[propMaxHeight]); ok {
		s.maxHeight = float32(v)
	}
	// 外边距：1~4 值简写 + 四边单独覆盖（与 CSS 级联顺序一致）。负值裁剪到 0。
	if v, ok := propString(props[propMargin]); ok {
		if sides, ok := cssbox.Float32s(v); ok {
			for i := range sides {
				if sides[i] < 0 {
					sides[i] = 0
				}
			}
			s.margin = edges(sides)
		}
	}
	if v, ok := propNum(props[propMarginTop]); ok && v > 0 {
		s.margin[0] = float32(v)
	}
	if v, ok := propNum(props[propMarginRight]); ok && v > 0 {
		s.margin[1] = float32(v)
	}
	if v, ok := propNum(props[propMarginBottom]); ok && v > 0 {
		s.margin[2] = float32(v)
	}
	if v, ok := propNum(props[propMarginLeft]); ok && v > 0 {
		s.margin[3] = float32(v)
	}
	if v, ok := propString(props[propWeight]); ok {
		s.bold = strings.EqualFold(v, "bold") || v == "700"
	}
	if v, ok := propNum(props[propOpacity]); ok {
		s.opacity = float32(v)
		if s.opacity < 0 {
			s.opacity = 0
		} else if s.opacity > 1 {
			s.opacity = 1
		}
	}
	if v, ok := propNum(props[propRadius]); ok && v >= 0 {
		s.radius = float32(v)
	}
	if v, ok := propNum(props[propBorderWidth]); ok && v > 0 {
		s.borderWidth = float32(v)
	}
	if v, ok := propString(props[propBorder]); ok {
		if c, ok := parseColor(v); ok {
			s.border, s.hasBorder = c, true
		}
	}
	// 定位
	if v, ok := propString(props[propPosition]); ok && strings.EqualFold(strings.TrimSpace(v), "absolute") {
		s.position = positionAbsolute
	}
	s.inset = edges{
		propNumAuto(props[propTop]), propNumAuto(props[propRight]),
		propNumAuto(props[propBottom]), propNumAuto(props[propLeft]),
	}
	// 溢出与滚动
	if v, ok := propString(props[propOverflow]); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "hidden":
			s.overflow = overflowHidden
		case "scroll":
			s.overflow = overflowScroll
		case "auto":
			s.overflow = overflowAuto
		default:
			s.overflow = overflowVisible
		}
	}
	if v, ok := propNum(props[propScrollTop]); ok {
		s.scrollTop = float32(v)
	}
	if v, ok := propNum(props[propScrollLeft]); ok {
		s.scrollLeft = float32(v)
	}
	// 变换与阴影
	if v, ok := propString(props[propTransform]); ok {
		s.transform = parseTransform(v)
	}
	if v, ok := propString(props[propBoxShadow]); ok && strings.TrimSpace(v) != "" && !strings.EqualFold(strings.TrimSpace(v), "none") {
		if sh, ok := parseBoxShadow(v); ok {
			s.shadow, s.hasShadow = sh, true
		}
	}
	if v, ok := propString(props[propTransition]); ok {
		s.transition = parseTransition(v)
	}

	// flex
	if v, ok := propString(props[propDirection]); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "row":
			s.direction = directionRow
		case "row-reverse":
			s.direction = directionRowReverse
		case "column-reverse":
			s.direction = directionColumnReverse
		default:
			s.direction = directionColumn
		}
	}
	if v, ok := propString(props[propWrap]); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "wrap":
			s.wrap = wrapWrap
		case "wrap-reverse":
			s.wrap = wrapReverse
		default:
			s.wrap = wrapNoWrap
		}
	}
	if v, ok := propString(props[propJustify]); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "center":
			s.justify = justifyCenter
		case "flex-end", "end", "right":
			s.justify = justifyEnd
		case "space-between":
			s.justify = justifySpaceBetween
		case "space-around":
			s.justify = justifySpaceAround
		case "space-evenly":
			s.justify = justifySpaceEvenly
		default:
			s.justify = justifyStart
		}
	}
	if v, ok := propString(props[propAlign]); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "flex-start", "start", "left", "top":
			s.align = alignFlexStart
		case "flex-end", "end", "right", "bottom":
			s.align = alignFlexEnd
		case "center":
			s.align = alignCenter
		case "baseline":
			s.align = alignBaseline
		default:
			s.align = alignStretch
		}
	}
	if v, ok := propString(props[propAlignContent]); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "flex-start", "start":
			s.alignContent = contentFlexStart
		case "flex-end", "end":
			s.alignContent = contentFlexEnd
		case "center":
			s.alignContent = contentCenter
		case "space-between":
			s.alignContent = contentSpaceBetween
		case "space-around":
			s.alignContent = contentSpaceAround
		default:
			s.alignContent = contentStretch
		}
	}
	if v, ok := propNum(props[propGap]); ok && v >= 0 {
		s.gap = float32(v)
	}
	if v, ok := propNum(props[propRowGap]); ok && v >= 0 {
		s.rowGap = float32(v)
	}
	if v, ok := propNum(props[propColumnGap]); ok && v >= 0 {
		s.columnGap = float32(v)
	}
	return s
}

// overlayPseudoInto 把单个伪状态子表覆盖进 dst（原地）。子表在 host 展平后是
// props[pseudoKey] 的 JSON 串；展开后覆盖同名普通属性。找不到/解析失败则无操作。
func overlayPseudoInto(dst map[string]any, pseudoKey string) {
	raw, ok := dst[pseudoKey]
	if !ok {
		return
	}
	var obj map[string]any
	switch x := raw.(type) {
	case string:
		if err := json.Unmarshal([]byte(x), &obj); err != nil {
			return
		}
	case map[string]any:
		obj = x
	default:
		return
	}
	for k, v := range obj {
		switch x := v.(type) {
		case string, float64, bool:
			dst[k] = x
		}
	}
}

// isDisabledProp 判断节点自身 disabled 属性。兼容 bool 与字符串 "true"/"1"
// （与 Rust style::is_disabled 语义一致）。
func isDisabledProp(props map[string]any) bool {
	v, ok := props["disabled"]
	if !ok {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s == "true" || s == "1"
	default:
		return false
	}
}

// applyPseudoStyle 按当前状态把伪状态子表覆盖进 props 的副本（headless 验证用）。
// host 展平后 props 含 "&:hover"/"&:focus"/"&:active"/"&:disabled" 的 JSON 串；
// 本函数展开子表并覆盖同名普通属性，语义与 Rust 的 style::apply_pseudo_state 对齐。
//
// state 是显式指定的交互伪状态："hover"/"focus"/"active"（"" 表示无交互态）；
// "disabled" 也可显式指定。此外节点自身 disabled 为真时，&:disabled 子表总会
// 在最后叠加（优先级最高，盖过交互态）——与 Rust「disabled 由节点属性驱动、
// 优先级最高」完全一致。Skia 无真实鼠标/焦点事件，此函数供测试/特殊宿主使用，
// 不改动入参。
func applyPseudoStyle(props map[string]any, state string) map[string]any {
	out := make(map[string]any, len(props))
	for k, v := range props {
		out[k] = v
	}
	// 显式交互态（host 已把 $: 规整为 &:）。
	switch state {
	case "hover":
		overlayPseudoInto(out, "&:hover")
	case "focus":
		overlayPseudoInto(out, "&:focus")
	case "active":
		overlayPseudoInto(out, "&:active")
	case "disabled":
		overlayPseudoInto(out, "&:disabled")
	}
	// 禁用由节点自身属性驱动，优先级最高，最后叠加。
	if isDisabledProp(props) {
		overlayPseudoInto(out, "&:disabled")
	}
	return out
}

// propNumAuto 取 inset：数值或 px；"auto"/缺省返回 NaN。
func propNumAuto(v any) float32 {
	if v == nil {
		return float32(math.NaN())
	}
	if s, ok := propString(v); ok && strings.EqualFold(strings.TrimSpace(s), "auto") {
		return float32(math.NaN())
	}
	if n, ok := propNum(v); ok {
		return float32(n)
	}
	return float32(math.NaN())
}

// parseTransform 解析 transform 字符串，支持 translate(x,y) / translateX/Y / scale。
func parseTransform(s string) transformSpec {
	t := transformSpec{scale: 1}
	low := strings.ToLower(s)
	// translate(10px, -4px)
	if i := strings.Index(low, "translate("); i >= 0 {
		if j := strings.Index(s[i:], ")"); j > 0 {
			inner := s[i+len("translate(") : i+j]
			parts := strings.Split(inner, ",")
			if len(parts) >= 1 {
				if v, ok := propNum(parts[0]); ok {
					t.translateX = float32(v)
				}
			}
			if len(parts) >= 2 {
				if v, ok := propNum(parts[1]); ok {
					t.translateY = float32(v)
				}
			}
		}
	}
	if i := strings.Index(low, "translatex("); i >= 0 {
		if j := strings.Index(s[i:], ")"); j > 0 {
			if v, ok := propNum(s[i+len("translatex(") : i+j]); ok {
				t.translateX = float32(v)
			}
		}
	}
	if i := strings.Index(low, "translatey("); i >= 0 {
		if j := strings.Index(s[i:], ")"); j > 0 {
			if v, ok := propNum(s[i+len("translatey(") : i+j]); ok {
				t.translateY = float32(v)
			}
		}
	}
	if i := strings.Index(low, "scale("); i >= 0 {
		if j := strings.Index(s[i:], ")"); j > 0 {
			if v, ok := propNum(s[i+len("scale(") : i+j]); ok && v > 0 {
				t.scale = float32(v)
			}
		}
	}
	return t
}

// parseBoxShadow 解析 "offsetX offsetY blur spread color"（spread 与 color 可省略）。
func parseBoxShadow(s string) (boxShadow, bool) {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) < 2 {
		return boxShadow{}, false
	}
	nums := make([]float32, 0, 4)
	colorStr := ""
	for _, f := range fields {
		if strings.Contains(f, "(") || strings.HasPrefix(f, "#") || isNamedColor(f) {
			colorStr = strings.TrimSuffix(strings.Join(append([]string{f}, fields[len(nums)+1:]...), " "), "")
			break
		}
		v, ok := propNum(f)
		if !ok {
			break
		}
		nums = append(nums, float32(v))
	}
	if len(nums) < 2 {
		return boxShadow{}, false
	}
	sh := boxShadow{offsetX: nums[0], offsetY: nums[1]}
	if len(nums) >= 3 {
		sh.blur = nums[2]
	}
	if len(nums) >= 4 {
		sh.spread = nums[3]
	}
	sh.color = argb(90, 0, 0, 0) // 默认半透明黑
	if colorStr != "" {
		if c, ok := parseColor(colorStr); ok {
			sh.color = c
		}
	}
	return sh, true
}

func isNamedColor(s string) bool {
	_, ok := namedColors[strings.ToLower(strings.TrimSpace(s))]
	return ok
}

// parseTransition 解析 CSS 风格 transition 声明（逗号分隔多段）。
func parseTransition(s string) []transitionItem {
	var out []transitionItem
	for _, seg := range strings.Split(s, ",") {
		seg = strings.TrimSpace(seg)
		if seg == "" || strings.EqualFold(seg, "none") {
			continue
		}
		it := transitionItem{}
		for _, tok := range strings.Fields(seg) {
			t := strings.ToLower(tok)
			switch {
			case strings.HasSuffix(t, "ms"):
				if v, err := strconv.ParseFloat(strings.TrimSuffix(t, "ms"), 32); err == nil {
					it.duration = float32(v)
				}
			case strings.HasSuffix(t, "s"):
				if v, err := strconv.ParseFloat(strings.TrimSuffix(t, "s"), 32); err == nil {
					it.duration = float32(v * 1000)
				}
			case t == "linear":
				it.easing = easeLinear
			case t == "ease-in":
				it.easing = easeIn
			case t == "ease-out":
				it.easing = easeOut
			case t == "ease-in-out":
				it.easing = easeInOut
			case t == "ease":
				it.easing = easeDefault
			default:
				// 首个非关键字 token 视为属性名（支持 all）。
				if it.property == "" {
					it.property = t
				}
			}
		}
		if it.property != "" && it.duration > 0 {
			out = append(out, it)
		}
	}
	return out
}

// propString 从 any 取字符串：接受 string 或常见标量，回退 false。
func propString(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	switch x := v.(type) {
	case string:
		return x, true
	case int, int64, float64, bool:
		return fmt.Sprint(x), true
	default:
		return "", false
	}
}

// propNum 从 any 取数值：整数/浮点，或带 px 后缀的字符串。
func propNum(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case string:
		t := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(x), "px"))
		f, err := strconv.ParseFloat(t, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// parseColor 解析 #rgb/#rrggbb/#aarrggbb/#rrggbbaa/rgb()/rgba()/命名色 为 ARGB uint32。
func parseColor(s string) (uint32, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if named, ok := namedColors[s]; ok {
		return named, true
	}
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		switch len(h) {
		case 3:
			r, _ := strconv.ParseUint(string(h[0])+string(h[0]), 16, 8)
			g, _ := strconv.ParseUint(string(h[1])+string(h[1]), 16, 8)
			b, _ := strconv.ParseUint(string(h[2])+string(h[2]), 16, 8)
			return argb(255, uint32(r), uint32(g), uint32(b)), true
		case 6:
			v, err := strconv.ParseUint(h, 16, 32)
			if err == nil {
				return argb(255, uint32(v>>16&0xff), uint32(v>>8&0xff), uint32(v&0xff)), true
			}
		case 8:
			v, err := strconv.ParseUint(h, 16, 32)
			if err == nil {
				// CSS #rrggbbaa：末两位是 alpha；兼容旧 AARRGGBB（首两位非 0 时按旧式）。
				r := uint32(v >> 24 & 0xff)
				g := uint32(v >> 16 & 0xff)
				b := uint32(v >> 8 & 0xff)
				a := uint32(v & 0xff)
				if a == 0 {
					a = 255
				} // 避免旧式 #rrggbbaa 里 alpha=00 被误判；调用方通常传非 0
				return argb(a, r, g, b), true
			}
		}
	}
	if strings.HasPrefix(s, "rgba(") && strings.HasSuffix(s, ")") {
		parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(s, "rgba("), ")"), ",")
		if len(parts) == 4 {
			r, e1 := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 8)
			g, e2 := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 8)
			b, e3 := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 8)
			a, e4 := strconv.ParseFloat(strings.TrimSpace(parts[3]), 32)
			if e1 == nil && e2 == nil && e3 == nil && e4 == nil {
				av := uint32(a * 255)
				if a > 1 {
					av = uint32(a)
				}
				return argb(av, uint32(r), uint32(g), uint32(b)), true
			}
		}
	}
	if strings.HasPrefix(s, "rgb(") && strings.HasSuffix(s, ")") {
		parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(s, "rgb("), ")"), ",")
		if len(parts) == 3 {
			r, e1 := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 8)
			g, e2 := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 8)
			b, e3 := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 8)
			if e1 == nil && e2 == nil && e3 == nil {
				return argb(255, uint32(r), uint32(g), uint32(b)), true
			}
		}
	}
	return 0, false
}

func argb(a, r, g, b uint32) uint32 {
	return a<<24 | r<<16 | g<<8 | b
}

var namedColors = map[string]uint32{
	"black": argb(255, 0, 0, 0), "white": argb(255, 255, 255, 255),
	"red": argb(255, 220, 40, 40), "green": argb(255, 40, 180, 60),
	"blue": argb(255, 40, 90, 220), "gray": argb(255, 128, 128, 128),
	"grey": argb(255, 128, 128, 128), "yellow": argb(255, 230, 200, 40),
}

// easingValue 把缓动进度 t（0..1）映射为插值权重。
func easingValue(e easing, t float32) float32 {
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	switch e {
	case easeLinear:
		return t
	case easeIn:
		return t * t * t
	case easeOut:
		return 1 - (1-t)*(1-t)*(1-t)
	case easeInOut:
		if t < 0.5 {
			return 4 * t * t * t
		}
		return 1 - float32(math.Pow(float64(-2*t+2), 3)/2)
	default: // ease
		// cubic-bezier(.25,.1,.25,1) 的近似
		return t * t * (3 - 2*t)
	}
}
