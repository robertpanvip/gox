package ui

import (
	"math"
	"testing"
)

// TestCaretMetricsConsistency 固化 caret 的核心不变量：PickColumn(x) 与
// PrefixWidth(s, col) 共用同一套字符度量，互为逆运算；宽字符用 wideWidth
// 而非简单 2×charWidth（很多 monospace 字体里 CJK 并非恰好两倍宽）。
func TestCaretMetricsConsistency(t *testing.T) {
	// 用实测字体参数（ui-monospace 13px：ASCII 7.83、CJK 13.02）。
	SetCharWidth(7.83)
	SetWideWidth(13.02)

	lines := []string{
		"package main",
		"func f() { return }",
		"// 中文注释 + 全角（括号）",
		"\t缩进\t混合",
	}
	// 先建立 tokenCache / textCache，否则 lineText 返回空串。
	EditorSetText(JoinLines(lines))

	for li, line := range lines {
		// 每个 rune 边界：pickColumn 落在前缀像素宽度上应还原该列。
		rs := []rune(line)
		for col := 0; col <= len(rs); col++ {
			x := PrefixWidth(line, col)
			got := PickColumn(li, x)
			if got != col {
				t.Errorf("line %d col %d: PickColumn(prefixWidth) = %d", li, col, got)
			}
		}
		// 首列边界左侧仍为 0，末尾右侧仍为末列。
		if got := PickColumn(li, -5); got != 0 {
			t.Errorf("line %d: PickColumn(-5) = %d, want 0", li, got)
		}
		if got := PickColumn(li, PrefixWidth(line, len(rs))+100); got != len(rs) {
			t.Errorf("line %d: PickColumn(end+100) = %d, want %d", li, got, len(rs))
		}
	}
}

// TestWideCharNotDoubleNarrow 验证 CJK 宽度用独立的 wideWidth，不是 2×charWidth。
func TestWideCharNotDoubleNarrow(t *testing.T) {
	SetCharWidth(7.83)
	SetWideWidth(13.02)
	narrow := PrefixWidth("a", 1)
	wide := PrefixWidth("中", 1)
	if math.Abs(narrow-7.83) > 1e-9 {
		t.Errorf("narrow width = %v, want 7.83", narrow)
	}
	if math.Abs(wide-13.02) > 1e-9 {
		t.Errorf("wide width = %v, want 13.02", wide)
	}
	if math.Abs(wide-2*narrow) < 1 {
		t.Errorf("wide should not equal 2*narrow: wide=%v 2*narrow=%v", wide, 2*narrow)
	}
}
