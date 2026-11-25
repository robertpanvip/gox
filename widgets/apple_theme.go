package widgets

// Apple macOS 设计系统（对齐 Apple Human Interface Guidelines）。
//
// 所有取值来自 Apple HIG 的 system colors / typography / layout 规范：
//   - 系统色：Blue #007aff(亮)/#0a84ff(暗) 等 12 色明暗双套；
//     label / secondaryLabel / tertiaryLabel；systemGroupedBackground 分层。
//   - 排版：macOS 字号阶梯 body=13 / callout=12 / subheadline=11 / headline=13bold /
//     title3=15 / title2=17 / title1=22 / largeTitle=26。
//   - 布局：8pt 间距标度；连续圆角 sm=6/md=10/lg=12/panel=14；控件高 28pt。
//   - 材质：半透明 background + hairline 描边 + 分层阴影（vibrancy 的近似）。
//
// 说明：后端没有 backdrop-filter（读回背景像素做模糊），"毛玻璃"用带 alpha 的
// 半透明底色 + 细描边 + 柔和阴影近似。Rust/TinySkia 端若不混合 alpha 会退化为
// 不透明实色。所有常量是纯字符串/数值，host 只透传，两个后端语义一致。

// ---- 系统色：亮色模式（Apple HIG system colors, Light Appearance）----
const (
	LightBlue   = "#007aff"
	LightGreen  = "#34c759"
	LightRed    = "#ff3b30"
	LightOrange = "#ff9500"
	LightYellow = "#ffcc00"
	LightPurple = "#af52de"
	LightPink   = "#ff2d55"
	LightTeal   = "#30b0c7"
	LightCyan   = "#32ade6"
	LightIndigo = "#5856d6"
	LightMint   = "#00c7be"
	LightGray   = "#8e8e93"
)

// ---- 系统色：暗色模式（Apple HIG system colors, Dark Appearance）----
const (
	DarkBlue   = "#0a84ff"
	DarkGreen  = "#30D158"
	DarkRed    = "#FF453A"
	DarkOrange = "#FF9F0A"
	DarkYellow = "#FFD60A"
	DarkPurple = "#BF5AF2"
	DarkPink   = "#FF375F"
	DarkTeal   = "#40CBE0"
	DarkCyan   = "#64D2FF"
	DarkIndigo = "#5E5CE6"
	DarkMint   = "#63E6E2"
	DarkGray   = "#8E8E93"
)

// ---- 背景分层（macOS grouped / window / control backgrounds）----
// 亮：window=#fff, grouped=#f2f2f7, elevated=#fff；
// 暗：window=#1e1e1e(窗口内容区), grouped=#000(分组底), elevated=#2c2c2e(卡片)。
const (
	AppleWindowBg   = "#1e1e1e" // window / content background
	AppleGroupedBg  = "#000000" // grouped background（更深的分组底）
	AppleElevated   = "#2c2c2e" // secondarySystemBackground / 卡片
	AppleFill       = "#3a3a3c" // tertiarySystemBackground / 控件填充
	AppleFill2      = "#48484a" // 更亮填充（systemGray3 暗 / hover）
	AppleSeparator  = "#38383a" // opaque separator / 常规描边
	AppleSeparator2 = "#545458" // separator（更亮的描边）
	AppleLabel      = "#f5f5f7" // label（主文字）
	AppleLabel2     = "#a1a1a6" // secondaryLabel
	AppleLabel3     = "#6e6e73" // tertiaryLabel
	AppleInputBg    = "#1c1c1e" // 输入框底
)

// ---- 系统强调色（暗色模式，Theme* 兼容值的默认来源）----
const (
	AppleBlue   = DarkBlue
	AppleGreen  = DarkGreen
	AppleRed    = DarkRed
	AppleOrange = DarkOrange
	AppleYellow = DarkYellow
	ApplePurple = DarkPurple
	ApplePink   = DarkPink
	AppleTeal   = DarkCyan
	AppleIndigo = DarkIndigo
)

// ---- 半透明材质（#rrggbbaa，alpha 近似 vibrancy）----
const (
	AppleMaterialThin  = "#2c2c2ecc" // thin：工具栏/浮层（80%）
	AppleMaterialReg   = "#3a3a3ce6" // regular：卡片/弹窗（90%）
	AppleMaterialThick = "#1e1e1ef2" // thick：模态面板（95%）
	AppleScrim         = "#00000099" // 60% 黑遮罩
	AppleHairline      = "#ffffff1f" // 12% 白高光描边
	LightHairline      = "#00000014" // 浅色模式高光描边
)

// ---- 连续圆角标度（Apple HIG radius scale）----
const (
	RadiusXSmall  = "4"  // 小徽章/勾框
	RadiusSmall   = "6"  // 控件/按钮/输入框（macOS control）
	RadiusControl = "6"  // 与 RadiusSmall 同义，保留兼容
	RadiusCard    = "12" // 卡片
	RadiusPanel   = "14" // 面板/分组
	RadiusModal   = "16" // 模态/浮层
	RadiusPill    = "999"
)

// ---- hairline 描边宽度（Apple HIG）----
// Apple 的 separator / hairline 本质是 0.5pt：@2x Retina 屏恰好落到 1 物理像素，
// 是 macOS 分隔线、卡片与控件描边的标志性细节。三端均按逻辑点解析浮点宽度，
// 因此统一用该 token，避免 1pt 描边比 Apple 原生粗一倍。
const HairlineWidth = "0.5"

// ---- 控件高度（macOS regular=28pt, small=24, large=32）----
const (
	ControlHeightSmall  = "24"
	ControlHeight       = "28"
	ControlHeightLarge  = "32"
	RowHeight           = "28" // 表格/列表行高（default）
)

// ---- 字号阶梯（macOS HIG typography scale, pt）----
const (
	FontLargeTitle  = "26"
	FontTitle1      = "22"
	FontTitle2      = "17"
	FontTitle3      = "15"
	FontHeadline    = "13" // + bold
	FontBody        = "13"
	FontCallout     = "12"
	FontSubheadline = "11"
	FontFootnote    = "10"
)

// ---- 柔和多层阴影（Apple HIG：低 alpha、大 blur、近零 offset）----
// Apple 阴影是「极低不透明度的多层柔和叠加」，深色背景上需约 2× 才可见。
// 后端用 6 层矩形近似模拟高斯，叠加后中心 alpha ≈ token alpha × 0.71，
// 故 token 取值略高于目标视觉值。三端共用同一套 token + 同一套 6 层算法。
// 用 var 而不是 const：SetTheme 会按明暗主题同步成对应强度（浅色更轻）。
var (
	ShadowCard  = "0 2px 8px 0 #00000033"
	ShadowFloat = "0 8px 24px 0 #0000004d"
	ShadowModal = "0 20px 56px 0 #00000066"
)

// ---- 8pt 间距标度 ----
const (
	Space1 = "4"
	Space2 = "8"
	Space3 = "12"
	Space4 = "16"
	Space5 = "20"
	Space6 = "24"
	Space8 = "32"
)

// appleTones 把语义 tone 名映射到暗色系统色。toneColor 会优先查这张表。
// 保留旧名（primary/cyan/...）以兼容现有业务调用。
var appleTones = map[string]string{
	"primary": AppleBlue,
	"blue":    AppleBlue,
	"cyan":    AppleTeal,
	"teal":    AppleTeal,
	"purple":  ApplePurple,
	"orange":  AppleOrange,
	"green":   AppleGreen,
	"yellow":  AppleYellow,
	"pink":    ApplePink,
	"mint":    DarkMint,
	"indigo":  AppleIndigo,
	"danger":  AppleRed,
	"red":     AppleRed,
	"info":    AppleBlue,
}

// appleToneColor 解析 tone：先 Apple 系统色，再旧 tones，缺省主强调色。
func appleToneColor(tone string) string {
	if c, ok := appleTones[tone]; ok {
		return c
	}
	return toneColor(tone)
}
