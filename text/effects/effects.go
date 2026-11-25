// Package effects 定义与渲染后端无关的文本效果语义模型，以及声明式 CSS 属性到该模型的映射。
//
// 分层原则：
//   - Scene 只保存声明式样式（CSS 属性字符串），后端把样式解析成这里的 TextEffects。
//   - Text Effects 与 Text Layout 分离：改颜色 / 阴影 / 渐变不应触发重新布局。
//   - 颜色用 CSS 字符串表示，不暴露 Skia Paint / tiny-skia Paint / glyphon 对象。
//   - 模型不含 UseMask 之类渲染策略；后端按 Capabilities 自行选择 NativePaint、Shader 或 AlphaMask 路径。
//
// 本包只描述「要什么效果」，不描述「怎么画」。它是 P0 的语义契约，供 P1 各后端接入。
package effects

import (
	"strconv"
	"strings"
)

// Color 是声明式 CSS 颜色字符串，如 "#fff"、"rgba(0,0,0,.3)"、"red"。
// 不映射到任何后端的具体颜色类型，解析由后端在绘制阶段完成。
type Color = string

// TextFillKind 是文字填充来源。
type TextFillKind uint8

const (
	// TextFillNone 表示没有显式填充效果，文字走继承色（后端默认路径）。
	TextFillNone           TextFillKind = iota
	TextFillSolid                       // -webkit-text-fill-color: <color>
	TextFillLinearGradient              // background-image: linear-gradient(...)
	TextFillRadialGradient              // background-image: radial-gradient(...)
	TextFillImage                       // background-image: url(...)
)

// StrokeJoin 是描边拐角连接方式，语义对应 CSS/Skia 的 stroke-linejoin。
type StrokeJoin uint8

const (
	StrokeJoinMiter StrokeJoin = iota
	StrokeJoinRound
	StrokeJoinBevel
)

// TextShadow 是单个文字阴影。多重阴影按 CSS 列表顺序保存，不合并。
type TextShadow struct {
	Color   Color
	OffsetX float32
	OffsetY float32
	Blur    float32
}

// TextStroke 是文字描边。描边在填充之前绘制，不改变文字排版宽度，但应扩大绘制边界。
type TextStroke struct {
	Color      Color
	Width      float32
	Join       StrokeJoin
	MiterLimit float32
}

// GradientStop 是渐变色标，Offset 归一化到 0..1。
type GradientStop struct {
	Color  Color
	Offset float32
}

// Gradient 是解析后的渐变描述。Angle 为 CSS 角度（0=向上，90=向右），radial 忽略 Angle。
type Gradient struct {
	Kind  TextFillKind
	Angle float32
	Stops []GradientStop
}

// ImagePattern 是图片填充描述，对应 background-image: url(...) 及其定位参数。
type ImagePattern struct {
	URL      string
	Size     string // cover / contain / auto / "100px 200px"
	Position string // center / top / "10% 20%"
	Repeat   string // repeat / no-repeat / repeat-x / repeat-y
}

// TextFill 是文字填充：一种 Kind 决定消费哪个字段。
type TextFill struct {
	Kind     TextFillKind
	Color    Color         // TextFillSolid
	Gradient *Gradient     // TextFillLinearGradient / TextFillRadialGradient
	Image    *ImagePattern // TextFillImage
}

// TextEffects 是后端解析声明式样式后得到的统一文本效果描述。
// 零值表示无效果，后端应走普通文字绘制路径、不分配离屏层。
type TextEffects struct {
	Shadows []TextShadow
	Stroke  *TextStroke
	Fill    *TextFill
}

// Active 报告是否存在任何需要额外绘制的效果。
func (e TextEffects) Active() bool {
	return len(e.Shadows) > 0 || e.Stroke != nil || e.Fill != nil
}

// HasFill 报告是否有显式填充（渐变 / 图片 / 覆盖色）。
func (e TextEffects) HasFill() bool { return e.Fill != nil }

// ---- 三层缓存键 ----
//
// 键均为可比较值类型，可直接作 map key。分层让「改颜色」只失效最外层，
// 不重新布局、不重新生成字形几何。

// LayoutKey 决定文字布局结果（GlyphRun / 位置 / 行信息 / TextBounds）。
// 文字内容、字体、字号、字距、行高或换行宽度变化时才需要重新布局。
type LayoutKey struct {
	Text          string
	FontFamily    string
	FontSize      float32
	Weight        int
	LetterSpacing float32
	LineHeight    float32
	MaxWidth      float32
}

// GeometryKey 决定字形路径或覆盖率。字体、Glyph ID、字号、栅格化比例变化时失效。
type GeometryKey struct {
	FontFamily string
	GlyphID    uint32
	FontSize   float32
	Scale      float32 // DPR：避免缩放后复用低分辨率字形
}

// EffectKey 决定模糊阴影 / 描边 Mask 等效果产物。
// Effect 只编码几何相关参数（偏移、模糊半径、描边宽度），不含颜色——
// Alpha 模糊结果可被不同颜色复用，改色只需重新着色。
type EffectKey struct {
	Geometry GeometryKey
	Effect   string
	DPR      float32
}

// ---- 后端能力矩阵 ----
//
// 后端据此选择实现路径，而不是把策略塞进 TextEffects。
// Skia 可用原生 Paint 描边，但当前 go-skia 绑定未暴露 Shader / MaskFilter，
// 渐变与阴影需走 CPU 条带 / 离屏 alpha mask；Glyphon 走 GPU；Web 走原生 CSS。

// Capabilities 描述一个后端能原生完成哪些效果。
type Capabilities struct {
	NativeShadow bool // 原生阴影（MaskFilter / CSS text-shadow）
	NativeStroke bool // 原生文字描边（Paint Stroke / CSS stroke）
	ShaderFill   bool // 原生渐变着色（Paint Shader / GPU shader）
	ImageFill    bool // 原生图片填充（Image Shader / GPU texture）
	AlphaMask    bool // 可生成字形 Alpha 覆盖率
	GlyphPath    bool // 可输出字形矢量路径
}

// SkiaCapabilities 返回当前 go-skia 绑定的保守能力集。
func SkiaCapabilities() Capabilities {
	return Capabilities{NativeStroke: true, AlphaMask: true, GlyphPath: true}
}

// GlyphonCapabilities 返回 GPU 文本后端的参考能力集。
func GlyphonCapabilities() Capabilities {
	return Capabilities{ShaderFill: true, ImageFill: true, AlphaMask: true}
}

// WebCapabilities 返回浏览器原生 CSS 的能力集。
func WebCapabilities() Capabilities {
	return Capabilities{NativeShadow: true, NativeStroke: true, ShaderFill: true, ImageFill: true}
}

// ---- CSS 属性映射 ----

// Parse 把声明式样式 props 解析成统一的 TextEffects。
//
// 关键语义：只有 background-clip（或 -webkit-background-clip）含 "text" 时，
// background-image 的渐变 / 图片才作为文字填充；否则渐变 / 图片仍是背景，
// 不作用于文字。不能仅凭存在 linear-gradient 就判定为渐变文字。
func Parse(props map[string]string) TextEffects {
	var e TextEffects
	e.Shadows = parseTextShadow(firstNonEmpty(props, "text-shadow"))
	e.Stroke = parseTextStroke(props)
	e.Fill = parseTextFill(props)
	return e
}

// firstNonEmpty 返回第一个非空的 props 值。
func firstNonEmpty(props map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := props[k]; strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// splitTopLevel 按 sep 切分，忽略括号与引号内的分隔符
// （rgba(0,0,0,.3) 与 url("data:...") 都含逗号）。
func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth := 0
	quote := byte(0)
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote && (i == 0 || s[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case sep:
			if depth == 0 {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, s[start:])
	return out
}

// parseLength 解析带 CSS 长度单位的值，无单位数值按像素处理。
func parseLength(s string) (float32, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	low := strings.ToLower(s)
	for _, u := range []string{"rem", "px", "em", "pt", "ch"} {
		if strings.HasSuffix(low, u) {
			v, err := strconv.ParseFloat(strings.TrimSpace(s[:len(s)-len(u)]), 64)
			return float32(v), err == nil
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	return float32(v), err == nil
}

// parseTextShadow 解析 text-shadow 列表，保留每条阴影为一个独立条目。
// 支持颜色在数值前或后，如 "0 0 8px red" 与 "red 0 0 8px"。
func parseTextShadow(s string) []TextShadow {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "none") {
		return nil
	}
	parts := splitTopLevel(s, ',')
	out := make([]TextShadow, 0, len(parts))
	for _, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		var sh TextShadow
		nums := 0
		var color []string
		for _, f := range fields {
			if v, ok := parseLength(f); ok {
				switch nums {
				case 0:
					sh.OffsetX = v
				case 1:
					sh.OffsetY = v
				case 2:
					sh.Blur = v
				}
				nums++
				continue
			}
			color = append(color, f)
		}
		if nums < 2 { // CSS 要求至少两个偏移量
			continue
		}
		sh.Color = Color(strings.Join(color, ""))
		out = append(out, sh)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseTextStroke 解析 -webkit-text-stroke / text-stroke 及独立的 color、width 属性。
func parseTextStroke(props map[string]string) *TextStroke {
	spec := firstNonEmpty(props, "-webkit-text-stroke", "text-stroke")
	width := firstNonEmpty(props, "-webkit-text-stroke-width", "text-stroke-width")
	color := firstNonEmpty(props, "-webkit-text-stroke-color", "text-stroke-color")
	for _, f := range strings.Fields(spec) {
		if v, ok := parseLength(f); ok {
			if width == "" {
				width = f
			}
			_ = v
			continue
		}
		if color == "" {
			color = f
		}
	}
	if width == "" && color == "" {
		return nil
	}
	w := float32(0)
	if width != "" {
		w, _ = parseLength(width)
	}
	return &TextStroke{Color: Color(color), Width: w, Join: StrokeJoinMiter, MiterLimit: 4}
}

// parseTextFill 解析文字填充来源。
func parseTextFill(props map[string]string) *TextFill {
	clip := strings.ToLower(firstNonEmpty(props, "background-clip", "-webkit-background-clip"))
	if strings.Contains(clip, "text") {
		image := firstNonEmpty(props, "background-image", "background", "bg")
		if g := parseGradient(image); g != nil {
			return &TextFill{Kind: g.Kind, Gradient: g}
		}
		if u, ok := parseImageURL(image); ok {
			return &TextFill{
				Kind: TextFillImage,
				Image: &ImagePattern{
					URL:      u,
					Size:     firstNonEmpty(props, "background-size", "bg-size"),
					Position: firstNonEmpty(props, "background-position", "bg-position"),
					Repeat:   firstNonEmpty(props, "background-repeat", "bg-repeat"),
				},
			}
		}
	}
	if c := firstNonEmpty(props, "-webkit-text-fill-color", "text-fill-color"); c != "" {
		if strings.EqualFold(strings.TrimSpace(c), "transparent") {
			return &TextFill{Kind: TextFillSolid, Color: Color(c)}
		}
		return &TextFill{Kind: TextFillSolid, Color: Color(c)}
	}
	return nil
}

// parseImageURL 从 background-image 值中提取 url(...)，兼容单双引号。
func parseImageURL(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(strings.ToLower(s), "url(") || !strings.HasSuffix(s, ")") {
		return "", false
	}
	inner := strings.TrimSpace(s[4 : len(s)-1])
	inner = strings.Trim(inner, "'\"")
	return inner, inner != ""
}

// parseGradient 解析 linear-gradient(...) / radial-gradient(...)。
func parseGradient(s string) *Gradient {
	s = strings.TrimSpace(s)
	low := strings.ToLower(s)
	switch {
	case strings.HasPrefix(low, "linear-gradient("):
		return parseGradientDecl(s, len("linear-gradient("), TextFillLinearGradient)
	case strings.HasPrefix(low, "radial-gradient("):
		return parseGradientDecl(s, len("radial-gradient("), TextFillRadialGradient)
	}
	return nil
}

// parseGradientDecl 解析渐变声明，prefixLen 为 "xxx-gradient(" 的长度。
func parseGradientDecl(s string, prefixLen int, kind TextFillKind) *Gradient {
	if !strings.HasSuffix(s, ")") {
		return nil
	}
	inner := s[prefixLen : len(s)-1]
	parts := splitTopLevel(inner, ',')
	g := &Gradient{Kind: kind}
	if len(parts) == 0 {
		return nil
	}
	first := strings.TrimSpace(parts[0])
	low := strings.ToLower(first)
	if a, ok := parseAngle(first); ok {
		g.Angle = a
		parts = parts[1:]
	} else if low == "circle" || low == "ellipse" || strings.Contains(low, " at ") {
		// radial-gradient 的形状/中心声明，不是色标。
		parts = parts[1:]
	}
	for _, p := range parts {
		f := strings.Fields(strings.TrimSpace(p))
		if len(f) == 0 || !looksLikeColor(f[0]) {
			continue
		}
		st := GradientStop{Color: Color(f[0])}
		if len(f) >= 2 {
			st.Offset = parseOffset(f[1])
		}
		g.Stops = append(g.Stops, st)
	}
	if len(g.Stops) == 0 {
		return nil
	}
	return g
}

// parseAngle 解析 CSS 渐变方向，返回角度（0=向上，90=向右，180=向下）。
func parseAngle(s string) (float32, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasSuffix(s, "deg") {
		v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s, "deg")), 64)
		return float32(v), err == nil
	}
	if strings.HasSuffix(s, "turn") {
		v, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s, "turn")), 64)
		return float32(v * 360), err == nil
	}
	if strings.HasPrefix(s, "to ") {
		angle := 0.0
		for _, d := range strings.Fields(s[3:]) {
			switch d {
			case "top":
				angle = 0
			case "right":
				angle = 90
			case "bottom":
				angle = 180
			case "left":
				angle = 270
			}
		}
		return float32(angle), true
	}
	return 0, false
}

// parseOffset 把色标位置转成 0..1 比例。支持 "50%"、"0.25"、"10"。
func parseOffset(s string) float32 {
	s = strings.ToLower(strings.TrimSpace(s))
	if strings.HasSuffix(s, "%") {
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		if err == nil {
			return float32(v / 100)
		}
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	if v > 1 {
		return float32(v / 100) // 容错：把 "10" 当作 10%
	}
	return float32(v)
}

// looksLikeColor 粗判一个 token 是否为颜色，用于跳过渐变方向与形状声明。
func looksLikeColor(s string) bool {
	if s == "" {
		return false
	}
	if s[0] == '#' {
		return true
	}
	low := strings.ToLower(s)
	if strings.HasPrefix(low, "rgb") || strings.HasPrefix(low, "hsl") || strings.HasPrefix(low, "var(") {
		return true
	}
	if _, err := strconv.ParseFloat(s, 64); err == nil {
		return false
	}
	if _, ok := parseLength(s); ok {
		return false
	}
	return true // 关键字颜色 red / blue / transparent
}
