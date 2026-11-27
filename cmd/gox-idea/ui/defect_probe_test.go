package ui

import (
	"strings"
	"testing"

	skia "github.com/robertpanvip/gox-backend-skia"
)

// 本文件是「GUI 缺陷自动化检测」的编辑器逻辑层探针。
// 桌面 IDE 的点击定位、拖选复制、滚动都依赖 EditorState 的列↔像素换算，
// 这些不变量无需起 GUI 即可被稳定断言。

const probeCodeFontStack = "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"

// TestEditorWidthsInjectedMatchesSkia 检测「字宽未注入」缺陷：
// 编辑器 caret 渲染与点击命中用的字符宽度，必须与 Skia 实际绘制宽度一致。
// 若装配层（main.go）漏掉字宽注入，编辑器沿用默认 7.83，而 Skia 实测等宽字体
// 单格并非此值，偏差累积后表现为「点击 div 落点偏移、拖选到行尾只复制到前半段」。
func TestEditorWidthsInjectedMatchesSkia(t *testing.T) {
	const fontSize = 13
	// 环境守卫：CI runner 若未装 CJK 字体，Skia 对「中」只能回退到 tofu
	// 字形宽（与编辑器内置的 CJK 双格宽必然不符），此时测的是环境而非
	// 装配层缺陷，跳过。
	if !skia.HasCJKFont() {
		t.Skip("系统未安装 CJK 字体，宽字符宽度对齐无从验证")
	}
	// Skia 实测的绘制端真实字宽（绘制端用 pickFontFamily 解析字体栈后的口径）。
	skiaNarrow := float64(skia.MeasureTextWidth("0000000000", fontSize, false, probeCodeFontStack) / 10)
	skiaWide := float64(skia.MeasureTextWidth("中中中", fontSize, false, probeCodeFontStack) / 3)

	// 当前编辑器实际使用的字宽（装配层是否注入，读这里可知）。
	editorNarrow := PrefixWidth("0", 1)
	editorWide := PrefixWidth("中", 1)

	// 容差 0.3px：允许实测舍入与跨平台 minor 差异，但 7.83 vs 7.2 这类
	// 足以造成明显错位的偏差会被抓出。
	if diff := editorNarrow - skiaNarrow; diff > 0.3 || diff < -0.3 {
		t.Fatalf("抓到缺陷：编辑器窄字符宽 %.4f 与 Skia 绘制宽 %.4f 不符（差 %.4f）——main.go 漏注入字宽？", editorNarrow, skiaNarrow, diff)
	}
	if diff := editorWide - skiaWide; diff > 0.3 || diff < -0.3 {
		t.Fatalf("抓到缺陷：编辑器宽字符宽 %.4f 与 Skia 绘制宽 %.4f 不符（差 %.4f）", editorWide, skiaWide, diff)
	}
}

// TestCaretPickRoundTripWithMeasuredWidths 注入 Skia 实测字宽后，验证
// PickColumn(PrefixWidth(col)) 能精确还原每一列——这是「点击落点正确」的核心不变量。
func TestCaretPickRoundTripWithMeasuredWidths(t *testing.T) {
	const fontSize = 13
	narrow := float64(skia.MeasureTextWidth("0000000000", fontSize, false, probeCodeFontStack) / 10)
	wide := float64(skia.MeasureTextWidth("中中中", fontSize, false, probeCodeFontStack) / 3)
	SetCharWidth(narrow)
	SetWideWidth(wide)

	lines := []string{
		"func view() *gxscene.Instance {",
		"    return <div>Hello, {name}!</div>",
		"    // 中文注释 变量",
	}
	EditorSetText(strings.Join(lines, "\n"))

	for li, line := range lines {
		rs := []rune(line)
		for col := 0; col <= len(rs); col++ {
			x := PrefixWidth(line, col)
			if got := PickColumn(li, x); got != col {
				t.Errorf("line %d col %d: PickColumn(PrefixWidth)=%d——点击会落错列", li, col, got)
			}
		}
	}
}

// TestSelectLineCopiesWholeLine 固化「选行复制完整」：三击选中 hello 行，
// 复制结果必须包含整行，而非截断在中间（字宽错误的典型症状）。
func TestSelectLineCopiesWholeLine(t *testing.T) {
	const helloLine = "    return <div>Hello, {name}!</div>"
	EditorSetText("func view() {\n" + helloLine + "\n}")
	EditorSelectLine(1)
	if got := EditorSelectedText(); got != helloLine+"\n" {
		t.Errorf("选行复制 = %q, want %q", got, helloLine+"\n")
	}
}

// TestCursorClampedOnShortDoc 固化「切短文档光标不越界」：从长文档切到短文档
// 后，SetText 必须把光标 clamp 到合法行（末行），而不是停留在已不存在的行。
// 这是「未占满就滚动」类 bug 的相关不变量；scrollTop 归零由 widgets 底层测试覆盖。
func TestCursorClampedOnShortDoc(t *testing.T) {
	var big []string
	for i := 0; i < 100; i++ {
		big = append(big, "line")
	}
	EditorSetText(strings.Join(big, "\n"))
	EditorSetCursor(90, 0) // 光标放到长文档靠后位置

	EditorSetText("one\ntwo\nthree") // 切成只有 3 行的短文档
	if ln := EditorLine(); ln < 0 || ln > 2 {
		t.Errorf("切短文档后光标行 = %d, want 落在 [0,2]", ln)
	}
	if col := EditorCol(); col < 0 {
		t.Errorf("切短文档后光标列 = %d, want >= 0", col)
	}
}
