// gx:nocollections
// 本文件为原生 Go 组件源码，经 gox build 转译为 apple_theme.go。
// gx:nocollections 声明跳过集合/裸对象字面量 desugar，保留手写 opcode 的原生 Go 语义。
package widgets

// Apple 风格设计系统（Apple HIG 深色模式）。
//
// 目标：让预制控件在观感上贴近 macOS/iOS 的深色appearance——分层中性灰背景、
// 系统语义色、连续圆角、柔和多层阴影、半透明"材质"（vibrancy 的近似）、
// 8pt 间距标度与清晰的字号层级。
//
// 说明：
//   - 后端没有真正的 backdrop-filter（读回背景像素做模糊），因此"毛玻璃"
//     用带 alpha 的半透明底色 + 细描边 + 柔和阴影近似；在有真实背景内容的
//     窗口里能呈现接近 macOS material 的层次感。Rust/TinySkia 端因不支持
//     alpha 混合，会退化为不透明的实色（见各组件对 material 的使用）。
//   - 所有常量都是纯字符串/数值，host 只负责透传，两个后端语义一致。

// ---- 系统灰阶（深色模式 system colors）----
const (
	AppleWindowBg   = "#1c1c1e" // window / systemBackground
	AppleGroupedBg  = "#000000" // grouped background（更深的分组底）
	AppleElevated   = "#2c2c2e" // secondarySystemBackground / 卡片
	AppleFill       = "#3a3a3c" // tertiarySystemBackground / 控件填充
	AppleFill2      = "#48484a" // 更亮的填充（hover/高亮）
	AppleSeparator  = "#38383a" // separator / 描边
	AppleSeparator2 = "#545458" // opaque separator
	AppleLabel      = "#ffffff" // label
	AppleLabel2     = "#98989f" // secondaryLabel
	AppleLabel3     = "#6e6e73" // tertiaryLabel
	AppleInputBg    = "#1c1c1e" // 输入框底（与 elevated 一致）
)

// ---- 系统语义色（深色模式 system colors）----
const (
	AppleBlue   = "#0a84ff" // 主强调 / 链接 / 选中
	AppleGreen  = "#30d158"
	AppleRed    = "#ff453a"
	AppleOrange = "#ff9f0a"
	AppleYellow = "#ffd60a"
	ApplePurple = "#bf5af2"
	ApplePink   = "#ff375f"
	AppleTeal   = "#40c8e0"
	AppleIndigo = "#5e5ce6"
)

// ---- 半透明材质（#rrggbbaa，alpha 近似 vibrancy）----
const (
	AppleMaterialThin = "#2c2c2ed9" // 85% 不透明：工具栏/浮层
	AppleMaterialReg  = "#3a3a3ce6" // 90%：卡片/弹窗
	AppleMaterialThick = "#1c1c1ef2" // 95%：模态面板
	AppleScrim        = "#00000099" // 60% 黑遮罩
	AppleHairline     = "#ffffff1f" // 12% 白高光描边
)

// ---- 圆角标度（continuous 观感用偏大值近似 squircle）----
const (
	RadiusSmall  = "8"
	RadiusControl = "10"
	RadiusCard   = "14"
	RadiusPanel  = "18"
	RadiusModal  = "22"
	RadiusPill   = "999"
)

// ---- 柔和多层阴影（Apple 阴影：大扩散、低 alpha、向下偏移）----
const (
	ShadowCard  = "0 6px 24px 0 #00000059"
	ShadowFloat = "0 12px 40px 0 #00000080"
	ShadowModal = "0 24px 70px 0 #000000b3"
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

// appleTones 把语义 tone 名映射到 Apple 系统色。toneColor 会优先查这张表。
// 保留原 tones 里的名字（primary/cyan/...）以兼容现有业务调用。
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
