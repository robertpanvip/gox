package ui

// 回归测试：桌面端/网页端在启动时把实测字体宽度注入编辑器后，
// caret 渲染（PrefixWidth）与点击命中（PickColumn）必须在每个字符边界
// 精确互逆。原默认字宽 7.83/13.02 与 Skia 实测不符，导致点击 <div>
// 落点偏移、拖选整行只复制到前面一部分——注入实测宽度后这些都不成立。
//
// 字宽从外部传入（由装配层用各自后端的测宽 API 实测），本包保持不依赖
// 具体渲染后端。测宽 API 本身的正确性由 backend/skia 的测试覆盖。

import "testing"

// TestMeasuredWidthsKeepCaretPickInverse 用一组"非默认"的实测字宽注入后，
// 验证 hello 示例每行每列的 PrefixWidth/PickColumn 回环一致。
func TestMeasuredWidthsKeepCaretPickInverse(t *testing.T) {
	// 模拟 Skia 对 DejaVu Sans Mono 13px 的实测值（与默认值 7.83/13.02 不同）。
	const measuredNarrow = 7.215 // MeasureTextWidth("0", 13)
	const measuredWide = 8.9     // CJK 实测（以非 2×narrow 的值验证独立性）
	SetCharWidth(measuredNarrow)
	SetWideWidth(measuredWide)

	lines := []string{
		"// sig 声明 + 裸读",
		"func view() *gxscene.Instance {",
		"    return <div>Hello, {name}!</div>",
		"}",
	}
	EditorSetText(JoinLines(lines))

	for li, line := range lines {
		rs := []rune(line)
		for col := 0; col <= len(rs); col++ {
			x := PrefixWidth(line, col)
			if got := PickColumn(li, x); got != col {
				t.Errorf("line %d col %d: PickColumn(PrefixWidth)=%d", li, col, got)
			}
		}
	}
}
