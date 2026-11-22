package skia

import (
	"fmt"
	"strconv"
	"strings"
)

// 本后端对 Node.Props 的样式约定（additive，host 不解释这些键）。
// host 只负责把 JSX 属性原样放进 Props，具体语义由后端决定——这正体现了
// "host 描述结构、后端解释表现" 的解耦。未列出的键被忽略，不影响绘制。
//
//	color         文字颜色，形如 "#rrggbb" / "#rgb" / "red" / "rgb(r,g,b)"
//	bg / background 元素背景色
//	width / height 元素尺寸（像素整数或 "120px"）；缺省时按内容/父容器弹性分配
//	font-size     字号（像素整数或 "20px"），缺省 16
//	padding       内边距（像素整数或 "8px"），缺省 4
//	weight         字重："bold" 用粗体，其余常规
const (
	propColor    = "color"
	propBg       = "bg"
	propBgAlt    = "background"
	propWidth    = "width"
	propHeight   = "height"
	propFontSize = "font-size"
	propPadding  = "padding"
	propWeight   = "weight"
)

// style 是一帧里从 Props 解析出的已解析样式值。
type style struct {
	color    uint32 // ARGB
	hasColor bool
	bg       uint32 // ARGB
	hasBg    bool
	width    float32
	height   float32
	fontSize int
	padding  float32
	bold     bool
}

// 默认样式：黑字、无背景、字号 16、padding 4、尺寸 0（表示弹性）。
func defaultStyle() style {
	return style{color: argb(255, 0, 0, 0), fontSize: 16, padding: 4}
}

// parseStyle 把 Node.Props 解析成 style。所有解析失败都静默回退默认值，
// 保证后端永不因脏 props 崩溃。
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
	if v, ok := propNum(props[propPadding]); ok && v >= 0 {
		s.padding = float32(v)
	}
	if v, ok := propString(props[propWeight]); ok {
		s.bold = strings.EqualFold(v, "bold") || v == "700"
	}
	return s
}

// propString 从 any 取字符串：接受 string 或 fmt.Stringer 之外的标量，回退 fmt.Sprint。
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

// propNum 从 any 取数值：接受整数/浮点，或带 px 后缀的字符串（如 "120px"）。
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

// parseColor 解析常见颜色写法为 ARGB uint32（与 Skia ColorSetARGB 同序）。
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
				return uint32(v), true // AARRGGBB
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
