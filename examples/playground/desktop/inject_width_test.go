package main

// 回归测试：桌面端必须把 Skia 实测字宽注入编辑器，否则 caret 渲染与点击
// 命中的列↔像素换算会用默认 7.83px/字符，与 Skia 实际绘制宽度不符——
// 表现为点击 <div> 落点偏移、拖选到视觉行尾却只复制到前面一部分文本。

import (
	"testing"

	"github.com/robertpanvip/gox-playground/ui"
)

// TestEditorWidthsMeasuredNotDefault 验证实测宽度确实来自 Skia 的等宽字体，
// 处于合理范围。回归背景：MeasureTextWidth 曾漏掉 pickFontFamily，用默认字体
// 测出 7.41（且 i/M/空格宽度各异），注入后 caret 与点击/拖选全面错位。
func TestEditorWidthsMeasuredNotDefault(t *testing.T) {
	w := measureEditorFontWidths()
	if w.Narrow <= 0 || w.Wide <= 0 {
		t.Fatalf("实测宽度应 > 0, got narrow=%v wide=%v", w.Narrow, w.Wide)
	}
	// 修复后等宽字体单格约 7.83；若落回 7.41 附近说明漏了 pickFontFamily。
	if w.Narrow < 7.6 || w.Narrow > 9 {
		t.Errorf("实测窄字符宽度 %v 异常（等宽字体应约 7.83，7.41 是漏 pickFontFamily 的错误值）", w.Narrow)
	}
}

// TestInjectedWidthsKeepCaretPickConsistent 验证注入实测字宽后，
// hello 示例每一行的每个列边界，PickColumn(PrefixWidth(col)) 都能精确还原列。
// 这锁定「点击 div 落点正确、拖选整行能复制到完整文本」的核心不变量。
func TestInjectedWidthsKeepCaretPickConsistent(t *testing.T) {
	injectEditorCharWidth()

	lines := []string{
		"// sig 声明 + 裸读",
		"func view() *gxscene.Instance {",
		"    return <div>Hello, {name}!</div>",
		"}",
	}
	ui.EditorSetText(ui.JoinLines(lines))

	for li, line := range lines {
		rs := []rune(line)
		for col := 0; col <= len(rs); col++ {
			x := ui.PrefixWidth(line, col)
			if got := ui.PickColumn(li, x); got != col {
				t.Errorf("line %d col %d: PickColumn(PrefixWidth)=%d (字宽注入后仍错位)", li, col, got)
			}
		}
	}
}

// TestSelectLineCopiesWholeHelloLine 直接复现用户场景：三击选中 hello 那行，
// 复制结果必须包含完整行文本（而非截断在 {nam）。
func TestSelectLineCopiesWholeHelloLine(t *testing.T) {
	injectEditorCharWidth()
	const helloLine = "    return <div>Hello, {name}!</div>"
	ui.EditorSetText("func view() {\n" + helloLine + "\n}")
	ui.EditorSelectLine(1)
	if got := ui.EditorSelectedText(); got != helloLine+"\n" {
		t.Errorf("选行复制 = %q, want %q", got, helloLine+"\n")
	}
}
