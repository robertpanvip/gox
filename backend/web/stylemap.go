package gxweb

import (
	"strconv"
	"strings"
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
		"text-align", "line-height", "letter-spacing", "z-index", "cursor":
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

// cssValue 把样式值规整为 CSS 值：纯数字的长度属性补 px；颜色/关键字原样。
func cssValue(key, v string) string {
	if v == "" {
		return v
	}
	if _, err := strconv.ParseFloat(v, 64); err != nil {
		return v
	}
	switch key {
	case "opacity", "weight", "z-index", "flex-grow", "flex-shrink", "line-height", "order":
		return v // 无量纲
	}
	return v + "px"
}
