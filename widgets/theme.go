// gx:nocollections
// 本文件为原生 Go 组件源码，经 gox build 转译为 theme.go。
// gx:nocollections 声明跳过集合/裸对象字面量 desugar，保留手写 opcode 的原生 Go 语义。
// Theme 是基于 Context 的预制设计令牌系统。
//
// 与 apple_theme.go 的编译期常量不同，这里把整套设计令牌放进 Context，使
// 明暗主题可在运行时切换：SetTheme / ThemeProvider 写入新令牌后，所有用
// UseTheme() 读取的组件插槽自动重绘。既有组件的 Theme* 常量保持不变（向后
// 兼容），新代码或需要主题切换的场景改用 UseTheme()。
// 令牌覆盖面：颜色/圆角/控件高度/字号/阴影/间距（space1–space8）全部进 Theme，
// UseToken 均可读取；SetTheme 同步的是可变别名（全局通道），非颜色令牌多为编译期
// 常量、不随 SetTheme 变化——子树级主题隔离请用 ThemeProvider + UseToken，
// showcase 的「子树主题 · 明暗并置」分组是该能力的活示例。
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
	Bg        string // 页面底色（window / content）
	GroupedBg string // 分组底（比 Bg 更深一层）
	Panel     string // 卡片/面板底（elevated）
	PanelAlt  string // 次级面板/斑马纹
	Skeleton  string // 骨架屏占位块（比次级填充略深，保证在卡片上可见）
	InputBg   string // 输入框底
	Border    string // 常规描边（opaque separator）
	Border2   string // 更亮描边（separator）
	Hairline  string // 高光细描边（半透明）
	Material  string // 半透明材质（浮层）
	Scrim     string // 遮罩

	// 文字
	Text     string // label
	Muted    string // secondaryLabel
	Faint    string // tertiaryLabel
	OnAccent string // 强调色背景上的文字

	// 语义色
	Accent  string
	Success string
	Warning string
	Danger  string
	Info    string
	Purple  string
	Pink    string
	Teal    string
	Indigo  string
	Orange  string

	// 圆角标度
	RadiusXSmall  string
	RadiusSmall   string
	RadiusControl string
	RadiusCard    string
	RadiusPanel   string
	RadiusModal   string
	RadiusPill    string

	// 控件高度
	ControlHeight      string
	ControlHeightSmall string
	ControlHeightLarge string
	RowHeight          string

	// 字号阶梯
	FontLargeTitle  string
	FontTitle1      string
	FontTitle2      string
	FontTitle3      string
	FontHeadline    string
	FontBody        string
	FontCallout     string
	FontSubheadline string
	FontFootnote    string

	// 阴影标度
	ShadowCard  string
	ShadowFloat string
	ShadowModal string

	// 间距标度（8pt 阶梯全量：space1–space8）
	Space1 string
	Space2 string
	Space3 string
	Space4 string
	Space5 string
	Space6 string
	Space8 string

	// 代码编辑器（Syntax）——语法高亮与编辑器底色，随明暗主题切换。
	// 用 UseToken("syntax-keyword") 等读取；编辑器组件据此生成渐变色标。
	SyntaxText    string // 普通代码文字
	SyntaxKeyword string // 关键字
	SyntaxString  string // 字符串字面量
	SyntaxNumber  string // 数字
	SyntaxComment string // 注释
	SyntaxTag     string // JSX 标签名
	SyntaxAttr    string // JSX 属性名
	SyntaxPunct   string // 标点/括号
	EditorBg      string // 编辑器底色
	EditorGutter  string // 行号槽文字
	EditorActive  string // 光标所在行底色
	EditorCaret   string // 光标颜色
	EditorSelect  string // 选区色块（8 位 hex 含 alpha，Skia 半透明/Web CSS 可覆盖）
}

// DarkTheme 是 Apple macOS HIG 深色主题（系统色精确取值）。
var DarkTheme = Theme{
	Name: "dark",
	Bg:   AppleWindowBg, GroupedBg: AppleGroupedBg, Panel: AppleElevated, PanelAlt: AppleFill,
	Skeleton: AppleFill2,
	InputBg:  AppleInputBg, Border: AppleSeparator, Border2: AppleSeparator2, Hairline: AppleHairline,
	Material: AppleMaterialThick, Scrim: AppleScrim,
	Text: AppleLabel, Muted: AppleLabel2, Faint: AppleLabel3, OnAccent: "#ffffff",
	Accent: AppleBlue, Success: AppleGreen, Warning: AppleOrange, Danger: AppleRed, Info: DarkCyan,
	Purple: ApplePurple, Pink: ApplePink, Teal: DarkCyan, Indigo: AppleIndigo, Orange: AppleOrange,
	RadiusXSmall: RadiusXSmall, RadiusSmall: RadiusSmall, RadiusControl: RadiusControl,
	RadiusCard: RadiusCard, RadiusPanel: RadiusPanel, RadiusModal: RadiusModal, RadiusPill: RadiusPill,
	ControlHeight: ControlHeight, ControlHeightSmall: ControlHeightSmall,
	ControlHeightLarge: ControlHeightLarge, RowHeight: RowHeight,
	FontLargeTitle: FontLargeTitle, FontTitle1: FontTitle1, FontTitle2: FontTitle2,
	FontTitle3: FontTitle3, FontHeadline: FontHeadline, FontBody: FontBody,
	FontCallout: FontCallout, FontSubheadline: FontSubheadline, FontFootnote: FontFootnote,
	ShadowCard: ShadowCard, ShadowFloat: ShadowFloat, ShadowModal: ShadowModal,
	Space1: Space1, Space2: Space2, Space3: Space3, Space4: Space4,
	Space5: Space5, Space6: Space6, Space8: Space8,
	// 代码编辑器（深色）：沿用 VS Code Dark+，与既有 playground 视觉一致。
	SyntaxText: "#d4d4d4", SyntaxKeyword: "#c586c0", SyntaxString: "#ce9178",
	SyntaxNumber: "#b5cea8", SyntaxComment: "#6a9955", SyntaxTag: "#569cd6",
	SyntaxAttr: "#9cdcfe", SyntaxPunct: "#808080",
	EditorBg: "#1e1e1e", EditorGutter: "#858585", EditorActive: "#2a2d3a",
	EditorCaret: "#aeafad", EditorSelect: "#264f7899",
}

// LightTheme 是 Apple macOS HIG 浅色主题（系统色精确取值）。
var LightTheme = Theme{
	Name: "light",
	Bg:   "#ffffff", GroupedBg: "#f2f2f7", Panel: "#ffffff", PanelAlt: "#f2f2f7",
	Skeleton: "#d8d8dd",
	InputBg:  "#ffffff", Border: "#c6c6c8", Border2: "#d1d1d6", Hairline: LightHairline,
	Material: "#ffffffeb", Scrim: "#0000004d",
	Text: "#1d1d1f", Muted: "#86868b", Faint: "#aeaeb2", OnAccent: "#ffffff",
	Accent: LightBlue, Success: LightGreen, Warning: LightOrange, Danger: LightRed, Info: LightCyan,
	Purple: LightPurple, Pink: LightPink, Teal: LightCyan, Indigo: LightIndigo, Orange: LightOrange,
	RadiusXSmall: RadiusXSmall, RadiusSmall: RadiusSmall, RadiusControl: RadiusControl,
	RadiusCard: RadiusCard, RadiusPanel: RadiusPanel, RadiusModal: RadiusModal, RadiusPill: RadiusPill,
	ControlHeight: ControlHeight, ControlHeightSmall: ControlHeightSmall,
	ControlHeightLarge: ControlHeightLarge, RowHeight: RowHeight,
	FontLargeTitle: FontLargeTitle, FontTitle1: FontTitle1, FontTitle2: FontTitle2,
	FontTitle3: FontTitle3, FontHeadline: FontHeadline, FontBody: FontBody,
	FontCallout: FontCallout, FontSubheadline: FontSubheadline, FontFootnote: FontFootnote,
	// Apple HIG 阴影（本轮重定）：低 alpha、大 blur、近零 offset；配合 6 层算法三端一致。
	ShadowCard: "0 1px 4px 0 #0000001a", ShadowFloat: "0 6px 18px 0 #00000026",
	ShadowModal: "0 16px 44px 0 #0000003d",
	Space1:      Space1, Space2: Space2, Space3: Space3, Space4: Space4,
	Space5: Space5, Space6: Space6, Space8: Space8,
	// 代码编辑器（浅色）：Apple 浅色语法配色，底色配合系统页面底色。
	SyntaxText: "#1d1d1f", SyntaxKeyword: "#ad3fc1", SyntaxString: "#c41a16",
	SyntaxNumber: "#1c00cf", SyntaxComment: "#4b9955", SyntaxTag: "#2563c7",
	SyntaxAttr: "#6b3fa0", SyntaxPunct: "#6e6e73",
	EditorBg: "#ffffff", EditorGutter: "#aeaeb2", EditorActive: "#e8f0fb",
	EditorCaret: "#1d1d1f", EditorSelect: "#b4d5fe80",
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
// 同时同步旧版 Theme*/tone 映射，让尚未改造为 dynAttr 的兼容组件
// 在浅色主题下也不会残留深色控件；动态插槽仍由 themeCtx 驱动。
func SetTheme(t Theme) {
	syncLegacyThemeTokens(t)
	themeCtx.Set(t)
}

// syncLegacyThemeTokens 把当前主题写回旧版包级令牌。
// 这里不是丢弃旧 API：大量业务代码直接引用 ThemeBg/ThemePanel/toneColor，
// 让它们跟随 SetTheme 变化，才能在明暗双主题下保持一致观感。
func syncLegacyThemeTokens(t Theme) {
	ThemeBg = t.Bg
	ThemeGrouped = t.GroupedBg
	ThemePanel = t.Panel
	ThemeFill = t.PanelAlt
	ThemeSkeleton = t.Skeleton
	ThemeBorder = t.Border
	ThemeBorder2 = t.Border2
	ThemeHairline = t.Hairline
	ThemeMaterial = t.Material
	ThemeScrim = t.Scrim
	ThemeText = t.Text
	ThemeMuted = t.Muted
	ThemeFaint = t.Faint
	ThemeOnAccent = t.OnAccent
	ThemeAccent = t.Accent
	ThemeInputBg = t.InputBg
	CursorColor = t.Accent
	ShadowCard = t.ShadowCard
	ShadowFloat = t.ShadowFloat
	ShadowModal = t.ShadowModal

	yellow := DarkYellow
	mint := DarkMint
	gray := DarkGray
	if t.Name == "light" {
		yellow = LightYellow
		mint = LightMint
		gray = LightGray
	}
	legacyTones := map[string]string{
		"primary": t.Accent,
		"blue":    t.Accent,
		"info":    t.Accent,
		"cyan":    t.Info,
		"teal":    t.Teal,
		"purple":  t.Purple,
		"orange":  t.Warning,
		"yellow":  yellow,
		"green":   t.Success,
		"pink":    t.Pink,
		"indigo":  t.Indigo,
		"mint":    mint,
		"gray":    gray,
		"danger":  t.Danger,
		"red":     t.Danger,
	}
	for k, v := range legacyTones {
		tones[k] = v
		if _, ok := appleTones[k]; ok {
			appleTones[k] = v
		}
	}
}

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
	case "grouped", "groupedbg":
		return t.GroupedBg
	case "panel", "surface":
		return t.Panel
	case "panelalt", "zebra":
		return t.PanelAlt
	case "skeleton":
		return t.Skeleton
	case "inputbg", "input":
		return t.InputBg
	case "border":
		return t.Border
	case "border2":
		return t.Border2
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
	case "accent", "primary", "blue":
		return t.Accent
	case "success", "green":
		return t.Success
	case "warning", "orange":
		return t.Warning
	case "danger", "red", "error":
		return t.Danger
	case "info", "cyan":
		return t.Info
	case "purple":
		return t.Purple
	case "pink":
		return t.Pink
	case "teal":
		return t.Teal
	case "indigo":
		return t.Indigo
	case "radiolsmall":
		return t.RadiusXSmall
	case "radiusxsmall":
		return t.RadiusXSmall
	case "radiussmall":
		return t.RadiusSmall
	case "radiuscontrol":
		return t.RadiusControl
	case "radiuscard":
		return t.RadiusCard
	case "radiuspanel":
		return t.RadiusPanel
	case "radiusmodal":
		return t.RadiusModal
	case "radiuspill":
		return t.RadiusPill
	case "controlheight":
		return t.ControlHeight
	case "controlheightsmall":
		return t.ControlHeightSmall
	case "controlheightlarge":
		return t.ControlHeightLarge
	case "rowheight":
		return t.RowHeight
	case "fontlargetitle":
		return t.FontLargeTitle
	case "fonttitle1":
		return t.FontTitle1
	case "fonttitle2":
		return t.FontTitle2
	case "fonttitle3":
		return t.FontTitle3
	case "fontheadline":
		return t.FontHeadline
	case "fontbody":
		return t.FontBody
	case "fontcallout":
		return t.FontCallout
	case "fontsubheadline":
		return t.FontSubheadline
	case "fontfootnote":
		return t.FontFootnote
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
	case "space5":
		return t.Space5
	case "space6":
		return t.Space6
	case "space8":
		return t.Space8
	// 代码编辑器语法色 / 底色。
	case "syntaxtext":
		return t.SyntaxText
	case "syntaxkeyword":
		return t.SyntaxKeyword
	case "syntaxstring":
		return t.SyntaxString
	case "syntaxnumber":
		return t.SyntaxNumber
	case "syntaxcomment":
		return t.SyntaxComment
	case "syntaxtag":
		return t.SyntaxTag
	case "syntaxattr":
		return t.SyntaxAttr
	case "syntaxpunct":
		return t.SyntaxPunct
	case "editorbg":
		return t.EditorBg
	case "editorgutter":
		return t.EditorGutter
	case "editoractive":
		return t.EditorActive
	case "editorcaret":
		return t.EditorCaret
	case "editorselect":
		return t.EditorSelect
	}
	return ""
}

// toneToken 把语义 tone 名映射到 Theme 字段值（响应当前主题）。
// 供组件的 toneColor 动态版本使用——主题切换时强调色随之变化。
func toneToken(t Theme, name string) string {
	switch normalizeToken(name) {
	case "", "primary", "accent", "blue", "info":
		return t.Accent
	case "success", "green":
		return t.Success
	case "warning", "orange":
		return t.Warning
	case "danger", "red", "error":
		return t.Danger
	case "purple":
		return t.Purple
	case "pink":
		return t.Pink
	case "teal", "cyan":
		return t.Teal
	case "indigo":
		return t.Indigo
	case "mint":
		return t.Info
	case "muted", "secondary":
		return t.Muted
	case "text", "label":
		return t.Text
	case "panel", "surface":
		return t.Panel
	case "bg", "window":
		return t.Bg
	case "border":
		return t.Border
	}
	if len(name) > 0 && name[0] == '#' {
		return name // 已是具体颜色
	}
	return t.Accent
}

// UseTone 返回一个 getter，读取当前主题下 tone 对应的颜色。
// 组件用 dynAttr 插槽接它，主题切换时强调色自动重绘。
func UseTone(name string) func() string {
	use := themeCtx.Use()
	return func() string { return toneToken(use(), name) }
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
