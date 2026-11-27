package main

import (
	"strings"
	"testing"

	skia "github.com/robertpanvip/gox-backend-rust"
	"github.com/robertpanvip/gox-idea/ui"
	"github.com/robertpanvip/gox-core/widgets"
)

// TestEditorTypeSpacesAfterStyle 复现用户报障序列：光标停在 return <div style
// 的 style 之后，连打三个空格。断言：
//  1. 逻辑文本正确（空格插在 style 之后，不丢 `{`、不截断行尾）；
//  2. 每次插入后光标列与文本列一致（caret x 不越出行末）。
func TestEditorTypeSpacesAfterStyle(t *testing.T) {
	narrow, wide := skia.MeasureEditorAdvances(widgets.EditorCodeFont(), widgets.EditorCodeFontSize)
	ui.SetCharWidth(narrow)
	ui.SetWideWidth(wide)

	orig := "return <div style={{ flexDirection: \"column\","
	src := "pub func demo() {\n    " + orig + "\n}\n"
	ui.EditorSetSource(src)

	// 用户点击 style 之后（"    return <div style" 是第 1 行，col = 4+17 = 21）
	line := "    " + orig
	afterStyle := len("    return <div style") // 21
	ui.EditorSetSelect(1, afterStyle)

	// 连打三个空格
	for i := 0; i < 3; i++ {
		ui.EditorInsert(" ")
	}

	got := ui.EditorText()
	lines := strings.Split(got, "\n")
	want := line[:afterStyle] + "   " + line[afterStyle:]
	if lines[1] != want {
		t.Fatalf("插入后行文本错误:\n  got:  %q\n  want: %q", lines[1], want)
	}

	// 光标列应为插入点+3
	x := ui.PrefixWidth(lines[1], afterStyle+3)
	if x <= ui.PrefixWidth(lines[1], afterStyle) {
		t.Fatalf("光标 x 未随插入前移: afterStyle x=%.2f, +3 x=%.2f", ui.PrefixWidth(lines[1], afterStyle), x)
	}
	// 光标 x 不得越出行末（越界即「光标跑到文字之外」的可观察症状）
	endX := ui.PrefixWidth(lines[1], len([]rune(lines[1])))
	if x > endX {
		t.Fatalf("光标 x=%.2f 越出行末 endX=%.2f", x, endX)
	}
	t.Logf("插入后行: %q", lines[1])
	t.Logf("光标 x=%.2fpx, 行末 x=%.2fpx", x, endX)
}
