package gxweb

import (
	"strconv"
	"strings"

	"github.com/robertpanvip/gox/cssbox"
)

// isStyleKey 判断 key 是否为受限 CSS sheet 属性（与 gxscene.StylePropNames 一致，
// 额外兼容少量常用键；伪状态子表 "&:x" 也归此列）。纯字符串判断，两驱动共享。
func isStyleKey(key string) bool {
	switch strings.ToLower(key) {
	case "color", "bg", "background", "width", "height", "font-size", "weight",
		"padding", "border-radius", "border", "border-width", "opacity",
		"flex-direction", "flex-wrap", "justify-content", "align-items", "align-content",
		"gap", "row-gap", "column-gap", "position", "top", "right", "bottom", "left",
		"overflow", "scroll-top", "scroll-left", "transform", "transition", "box-shadow",
		"flex-grow", "flex-shrink", "min-width", "max-width", "min-height", "max-height",
		"margin", "margin-top", "margin-bottom", "margin-left", "margin-right",
		"text-align", "line-height", "letter-spacing", "z-index", "cursor",
		"white-space":
		return true
	}
	return strings.HasPrefix(key, "&:") || strings.HasPrefix(key, "$:")
}

// cssName 把 GX 样式名映射到 CSS 属性名。数值型长度由 cssValue 加 px。
func cssName(key string) string {
	switch key {
	case "bg":
		return "background"
	case "weight":
		return "font-weight"
	case "scroll-top":
		return "scrollTop"
	case "scroll-left":
		return "scrollLeft"
	}
	return key
}

// boxShorthandProps 是支持 1~4 值简写的盒模型属性。这些属性的值先经 cssbox
// 解析成四边，再逐边补 px 拼回 CSS 简写串；避免 "7 14" 被整串 ParseFloat 失败后
// 输出无单位 CSS（浏览器忽略）导致 padding / margin 失效。
var boxShorthandProps = map[string]bool{
	"padding": true, "margin": true, "border-radius": true, "gap": true,
}

// dimensionlessProps 是无量纲属性，数值不加 px。
var dimensionlessProps = map[string]bool{
	"opacity": true, "weight": true, "z-index": true,
	"flex-grow": true, "flex-shrink": true, "line-height": true, "order": true,
}

// isFlexLayoutKey 判定一个 GX 样式属性是否表达了 flex 布局意图。
// 浏览器只在 display:flex 容器上解释这些属性，故 js 驱动设置它们时需同步补
// display:flex（见 dom_js.go SetAttr），否则 GX 场景树在 Web 上退化为块级堆叠。
func isFlexLayoutKey(key string) bool {
	switch strings.ToLower(key) {
	case "flex-direction", "flex-wrap", "justify-content", "align-items",
		"align-content", "gap", "row-gap", "column-gap", "flex-grow", "flex-shrink":
		return true
	}
	return false
}

// cssValue 把样式值规整为 CSS 值：纯数字的长度属性补 px；盒模型简写属性支持
// 1~4 值逐边补单位；颜色/关键字原样。
func cssValue(key, v string) string {
	if v == "" {
		return v
	}
	if boxShorthandProps[key] {
		if edges, ok := cssbox.Values(v); ok {
			return cssbox.Shorthand(cssbox.WithUnit(edges))
		}
		return v // 非数值简写（如 auto）原样透传
	}
	if _, err := strconv.ParseFloat(v, 64); err != nil {
		return v
	}
	if dimensionlessProps[key] {
		return v // 无量纲
	}
	return v + "px"
}
