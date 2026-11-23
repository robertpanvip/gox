// gx:nocollections
// 本文件为原生 Go 组件源码，经 gox build 转译为 theme.go。
// gx:nocollections 声明跳过集合/裸对象字面量 desugar，保留手写 opcode 的原生 Go 语义。
// Theme 是基于 Context 的预制设计令牌系统。
//
// 与 apple_theme.go 的编译期常量不同，这里把整套设计令牌放进 Context，使
// 明暗主题可在运行时切换：SetTheme / ThemeProvider 写入新令牌后，所有用
// UseTheme() 读取的组件插槽自动重绘。既有组件的 Theme* 常量保持不变（向后
// 兼容），新代码或需要主题切换的场景改用 UseTheme()。
//
// 用法：
//
//	// 应用根节点包一层，确立初始主题
//	widgets.SetTheme(widgets.LightTheme)          // 或直接 Set
//	app.Render(func() any { return rootView() })
//
//	// 组件内读当前令牌（getter，随主题切换重绘）
//	accent := widgets.UseTheme().Accent           // func() string
//	// 作为动态属性插槽：dynAttr("bg", slot) + getter accent
//
//	// 运行时切换（所有消费者重绘）
//	widgets.SetTheme(widgets.DarkTheme)
package widgets

import (
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// Theme 是一套设计令牌。字段与 apple_theme.go / widgets.go 的常量语义一致，
// 值是具体颜色/尺寸字符串，直接作为 Props 传给后端。
type Theme struct {
	Name string // 主题名（"dark" / "light" / 自定义）

	// 背景与表面
	Bg        string // 页面底色
	Panel     string // 卡片/面板底
	PanelAlt  string // 次级面板/斑马纹
	InputBg   string // 输入框底
	Border    string // 常规描边
	Hairline  string // 高光细描边（半透明）
	Material  string // 半透明材质（浮层）
	Scrim     string // 遮罩

	// 文字
	Text    string
	Muted   string
	Faint   string
	OnAccent string // 强调色背景上的文字

	// 语义色
	Accent  string
	Success string
	Warning string
	Danger  string
	Info    string

	// 圆角标度
	RadiusSmall  string
	RadiusControl string
	RadiusCard   string
	RadiusPanel  string
	RadiusPill   string

	// 阴影标度
	ShadowCard  string
	ShadowFloat string
	ShadowModal string

	// 间距标度
	Space1 string
	Space2 string
	Space3 string
	Space4 string
}

// DarkTheme 是 Apple HIG 深色主题（与现有 Theme* 常量一致）。
var DarkTheme = Theme{
	Name: "dark",
	Bg: AppleWindowBg, Panel: AppleElevated, PanelAlt: AppleFill,
	InputBg: AppleInputBg, Border: AppleSeparator, Hairline: AppleHairline,
	Material: AppleMaterialThick, Scrim: AppleScrim,
	Text: AppleLabel, Muted: AppleLabel2, Faint: AppleLabel3, OnAccent: "#ffffff",
	Accent: AppleBlue, Success: AppleGreen, Warning: AppleOrange,
	Danger: AppleRed, Info: AppleBlue,
	RadiusSmall: RadiusSmall, RadiusControl: RadiusControl, RadiusCard: RadiusCard,
	RadiusPanel: RadiusPanel, RadiusPill: RadiusPill,
	ShadowCard: ShadowCard, ShadowFloat: ShadowFloat, ShadowModal: ShadowModal,
	Space1: Space1, Space2: Space2, Space3: Space3, Space4: Space4,
}

// LightTheme 是 Apple HIG 浅色主题。
var LightTheme = Theme{
	Name: "light",
	Bg: "#f2f2f7", Panel: "#ffffff", PanelAlt: "#f9f9fb",
	InputBg: "#ffffff", Border: "#c6c6c8", Hairline: "#00000014",
	Material: "#ffffffeb", Scrim: "#0000004d",
	Text: "#1c1c1e", Muted: "#6e6e73", Faint: "#8e8e93", OnAccent: "#ffffff",
	Accent: "#007aff", Success: "#34c759", Warning: "#ff9f0a",
	Danger: "#ff3b30", Info: "#007aff",
	RadiusSmall: RadiusSmall, RadiusControl: RadiusControl, RadiusCard: RadiusCard,
	RadiusPanel: RadiusPanel, RadiusPill: RadiusPill,
	ShadowCard: "0 6px 24px 0 #0000001f", ShadowFloat: "0 12px 40px 0 #0000002e",
	ShadowModal: "0 24px 70px 0 #00000040",
	Space1: Space1, Space2: Space2, Space3: Space3, Space4: Space4,
}

// themeCtx 是全应用共享的主题通道，默认深色。
var themeCtx = NewContext(DarkTheme)

// ThemeContext 暴露主题句柄，供需要自定义主题通道时读取。
// 常规场景直接用 UseTheme / SetTheme / ThemeProvider 即可。
func ThemeContext() *Context[Theme] { return themeCtx }

// UseTheme 返回读取当前主题的 getter。组件把它的字段作为动态属性插槽的
// getter 使用，主题切换时自动重绘。
func UseTheme() func() Theme { return themeCtx.Use() }

// CurrentTheme 立即读取当前主题（非响应式，供 handler 使用）。
func CurrentTheme() Theme { return themeCtx.Value() }

// SetTheme 切换主题，所有 UseTheme 消费者重绘。
func SetTheme(t Theme) { themeCtx.Set(t) }

// ThemeProvider 以声明式组件形式确立子树主题。value 传 Theme 或 func() Theme /
// func() any getter（getter 形式随上游 signal 响应式切换）。
// 语义与 Context.Provider 一致：只作用于自己的子树，嵌套时内层就近覆盖，兄弟
// 区域互不影响；没有 Provider 的地方回落到 SetTheme 设置的全局默认主题。
func ThemeProvider(props gxscene.Props) *gxscene.Instance {
	return themeCtx.Provider(props)
}

// UseToken 返回一个 getter，读取当前主题里指定语义令牌的颜色/尺寸值。
// name 支持字段名（"accent"/"bg"/"radius-card"/"space-2" 等，大小写不敏感）。
// 便于组件写 dynAttr("bg", slot) + UseToken("panel") 而不必解构整个 Theme。
func UseToken(name string) func() string {
	use := themeCtx.Use()
	return func() string { return themeToken(use(), name) }
}

// themeToken 从主题中按名取令牌；未知名回退空串（调用方可自行兜底）。
func themeToken(t Theme, name string) string {
	switch normalizeToken(name) {
	case "name":
		return t.Name
	case "bg", "window":
		return t.Bg
	case "panel", "surface":
		return t.Panel
	case "panelalt", "zebra":
		return t.PanelAlt
	case "inputbg", "input":
		return t.InputBg
	case "border":
		return t.Border
	case "hairline":
		return t.Hairline
	case "material":
		return t.Material
	case "scrim":
		return t.Scrim
	case "text", "label":
		return t.Text
	case "muted", "secondary":
		return t.Muted
	case "faint", "tertiary":
		return t.Faint
	case "onaccent":
		return t.OnAccent
	case "accent", "primary":
		return t.Accent
	case "success", "green":
		return t.Success
	case "warning", "orange":
		return t.Warning
	case "danger", "red", "error":
		return t.Danger
	case "info", "blue":
		return t.Info
	case "radiussmall":
		return t.RadiusSmall
	case "radiuscontrol":
		return t.RadiusControl
	case "radiuscard":
		return t.RadiusCard
	case "radiuspanel":
		return t.RadiusPanel
	case "radiuspill":
		return t.RadiusPill
	case "shadowcard":
		return t.ShadowCard
	case "shadowfloat":
		return t.ShadowFloat
	case "shadowmodal":
		return t.ShadowModal
	case "space1":
		return t.Space1
	case "space2":
		return t.Space2
	case "space3":
		return t.Space3
	case "space4":
		return t.Space4
	}
	return ""
}

// normalizeToken 归一化令牌名：去掉连字符/下划线/空格并转小写。
// 这样 "radius-card" / "RadiusCard" / "radius_card" 都映射到 radiuscard。
func normalizeToken(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '-' || c == '_' || c == ' ' {
			continue
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out = append(out, c)
	}
	return string(out)
}
