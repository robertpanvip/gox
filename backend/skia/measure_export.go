package skia

// 本文件把后端"文本测宽"能力按与绘制端完全相同的口径导出，供装配层
// （如桌面端 Playground）实测编辑器等宽字体宽度并注入编辑器。
//
// 背景：编辑器的 caret 渲染（PrefixWidth）与点击命中（PickColumn）用字符
// 宽度把「列」换算成「像素」。这些宽度必须与文本绘制端一致，否则光标会
// 视觉错位、点击/拖选会落到错误的列。Web 端用 canvas 实测后注入；
// 桌面端走本文件的 Skia 实测同名 API。

// MeasureTextWidth 返回 text 在指定字号/字重/字体栈下的像素宽度，
// 口径与绘制端 paintText 完全一致：按字体分段（含 CJK fallback）逐段
// 用 Font.MeasureText 累加。family 是 CSS font-family 栈，由 pickFontFamily
// 解析出本机可用族名；空串表示系统默认字体。
//
// fontSize 单位为逻辑像素（CSS px），与编辑器/场景树里的 font-size 一致。
func MeasureTextWidth(text string, fontSize int, bold bool, family string) float32 {
	return skiaFontMeasurer()(text, fontSize, bold, family)
}
