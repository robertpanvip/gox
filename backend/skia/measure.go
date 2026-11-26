package skia

import (
	sk "github.com/robertpanvip/gox-skia/compat"
)

// MeasureEditorAdvances 实测编辑器代码字体的字符 advance（供桌面装配层启动时注入）。
//
// 背景：widgets.EditorState 的点击换算（PickColumn）与光标渲染（PrefixWidth）
// 依赖 narrow/wide 两个字符宽度信号，默认值 7.83/13.02 只与 Web 端 canvas 实测
// 接近；桌面端 Skia 实际用的等宽族（Windows 上为 Consolas 13px，advance≈7.15）
// 与默认值不符，导致点击定位随列号累计偏移、插入位置错位。装配层应在启动时
// 调用本函数并把结果注入 ui.SetCharWidth/SetWideWidth（Web 端已做同样注入，
// 见 playground/web/main.go）。
//
// familyStack 是编辑器代码行的 CSS font-family 栈（与 Web 端一致）；
// size 是代码字号（px）。返回：
//   - narrow：窄字符（ASCII）平均 advance；
//   - wide：宽字符（CJK）平均 advance（经 resolveFont 的 CJK fallback 解析）。
//
// 字体度量在首次调用后由 skia 缓存，重复调用开销可忽略。
func MeasureEditorAdvances(familyStack string, size int) (narrow, wide float64) {
	family := pickFontFamily(familyStack)

	// 窄字符：取一串有代表性的 ASCII（覆盖字母/数字/常见标点/空格）。
	const sample = "abcdefghij0123456789{}()<>=+-*/. "
	total, count := 0.0, 0
	for _, r := range splitRuns(sample, size, false, family) {
		w := r.font.MeasureText([]byte(r.text), uint32(len(r.text)), sk.TextEncodingUTF8, nil)
		total += float64(w)
		count += len([]rune(r.text))
	}
	if count > 0 {
		narrow = total / float64(count)
	}

	// 宽字符：CJK 代表字符，走 resolveFont 的 cjkFamilies fallback。
	const cjkSample = "中中中中"
	if f := resolveFont('中', size, false, family); !f.IsNil() {
		w := f.MeasureText([]byte(cjkSample), uint32(len(cjkSample)), sk.TextEncodingUTF8, nil)
		wide = float64(w) / float64(len([]rune(cjkSample)))
	}
	return narrow, wide
}
