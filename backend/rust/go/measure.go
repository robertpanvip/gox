package rustbackend

// MeasureEditorAdvances 实测编辑器代码字体的字符 advance（供桌面装配层启动时注入）。
//
// 背景：widgets.EditorState 的点击换算（PickColumn）与光标渲染（PrefixWidth）
// 依赖 narrow/wide 两个字符宽度信号，默认值 7.83/13.02 只与 Web 端 canvas 实测
// 接近；桌面端实际渲染用的等宽族（Windows 上 Consolas 13px ≈ 7.15）与默认值
// 不符，会导致点击定位随列号累计偏移、插入位置错位。装配层应在启动时调用本
// 函数并把结果注入 ui.SetCharWidth/SetWideWidth（Web 端已做同样注入）。
//
// 与 skia 后端 MeasureEditorAdvances 同名同语义，迁移消费方（playground /
// gox-idea）时 import 替换即可零改动。测量走 Rust 侧 glyphon 真实 shaping，
// 与 V2 绘制端字形来源完全同源——测出的 advance 就是渲染 advance。
//
// familyStack 是编辑器代码行的 CSS font-family 栈（与 Web 端一致）；
// size 是代码字号（px）。返回：
//   - narrow：窄字符（ASCII）平均 advance；
//   - wide：宽字符（CJK）平均 advance（cosmic-text 自动 CJK 回退）。
//
// 字体系统在 Rust 侧进程级缓存，首次调用后的重复调用开销可忽略。
func MeasureEditorAdvances(familyStack string, size int) (narrow, wide float64) {
	return cMeasureTextAdvances(familyStack, size)
}
