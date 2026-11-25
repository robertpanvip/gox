package gxweb

import (
	"fmt"
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

// expandBoxShadow 把单层 GX box-shadow token 展开成复刻 Skia/Rust 6 层矩形
// 近似的 CSS 多层阴影，使 Web 端与桌面/Rust 端的阴影视觉一致。
//
// Skia paintShadow（Rust paint_shadow_tiny 同算法）：
//
//	baseA = 颜色 alpha（<=0 时取 90）
//	for i = 6..1:
//	    t      = i / 6
//	    expand = spread + blur * t
//	    a      = baseA * (1 - t) / 6 * 1.8   （clamp 0..255）
//	    画圆角矩形：四周扩 expand，alpha=a，radius=原 radius+expand
//
// CSS box-shadow 的 spread 会同步把圆角半径增大 spread，故每层用
// "ox oy 0px expand rgba(r,g,b,a/255)" 精确对应 Skia 的一层实心圆角矩形。
// 输出顺序 i=1..6：CSS 第一个阴影绘制在最上层，对应 Skia 最后画的、最浓的内层。
//
// 已是多层（含逗号）或解析失败时原样透传，避免二次展开。
func expandBoxShadow(v string) string {
	ox, oy, blur, spread, r, g, b, baseA, ok := parseShadowToken(v)
	if !ok {
		return v
	}
	if baseA <= 0 {
		baseA = 90
	}
	const layers = 6
	parts := make([]string, 0, layers)
	for i := 1; i <= layers; i++ {
		t := float64(i) / layers
		expand := spread + blur*t
		a := float64(baseA) * (1 - t) / layers * 1.8
		if a < 0 {
			a = 0
		} else if a > 255 {
			a = 255
		}
		parts = append(parts, fmt.Sprintf("%gpx %gpx 0px %gpx rgba(%d,%d,%d,%.4f)",
			ox, oy, expand, r, g, b, a/255))
	}
	return strings.Join(parts, ", ")
}

// parseShadowToken 解析 "ox oy [blur] [spread] [color]"。color 缺省为半透明黑，
// 与 Skia parseBoxShadow 的默认（alpha=90）一致。
func parseShadowToken(v string) (ox, oy, blur, spread float64, r, g, b, a int, ok bool) {
	v = strings.TrimSpace(v)
	if v == "" || strings.Contains(v, ",") {
		return // 已多层或空：不展开
	}
	fields := strings.Fields(v)
	if len(fields) < 2 {
		return
	}
	nums := make([]float64, 0, 4)
	colorStr := ""
	for _, f := range fields {
		n, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSuffix(f, "px"), "PX"), 64)
		if err == nil && len(nums) < 4 {
			nums = append(nums, n)
			continue
		}
		colorStr = f
	}
	if len(nums) < 2 {
		return
	}
	ox, oy = nums[0], nums[1]
	if len(nums) >= 3 {
		blur = nums[2]
	}
	if len(nums) >= 4 {
		spread = nums[3]
	}
	a = 90 // Skia 默认半透明黑
	if colorStr != "" {
		r, g, b, a = parseShadowColor(colorStr)
	}
	ok = true
	return
}

// parseShadowColor 解析阴影颜色：#rgb / #rrggbb / #rrggbbaa / rgb() / rgba()。
// 无法识别时返回黑色 + alpha=90（与 Skia 兜底一致）。
func parseShadowColor(s string) (r, g, b, a int) {
	s = strings.ToLower(strings.TrimSpace(s))
	a = 90
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		switch len(h) {
		case 3:
			r, g, b = hexByte(h[0:1])*17, hexByte(h[1:2])*17, hexByte(h[2:3])*17
			a = 255
		case 6:
			r, g, b = hexByte(h[0:2]), hexByte(h[2:4]), hexByte(h[4:6])
			a = 255
		case 8:
			r, g, b, a = hexByte(h[0:2]), hexByte(h[2:4]), hexByte(h[4:6]), hexByte(h[6:8])
		}
		return
	}
	if strings.HasPrefix(s, "rgba(") || strings.HasPrefix(s, "rgb(") {
		inner := s[strings.Index(s, "(")+1 : strings.LastIndex(s, ")")]
		parts := strings.Split(inner, ",")
		if len(parts) >= 3 {
			r, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
			g, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
			b, _ = strconv.Atoi(strings.TrimSpace(parts[2]))
			a = 255
			if len(parts) >= 4 {
				af, err := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
				if err == nil {
					a = int(af * 255)
				}
			}
		}
	}
	return
}

func hexByte(s string) int {
	v, _ := strconv.ParseUint(s, 16, 8)
	return int(v)
}

// cssValue 把样式值规整为 CSS 值：纯数字的长度属性补 px；盒模型简写属性支持
// 1~4 值逐边补单位；box-shadow 展开为 6 层以对齐 Skia/Rust；颜色/关键字原样。
func cssValue(key, v string) string {
	if v == "" {
		return v
	}
	if key == "box-shadow" {
		return expandBoxShadow(v)
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
