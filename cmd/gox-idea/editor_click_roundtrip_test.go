package main

import (
	"strings"
	"testing"

	skia "github.com/robertpanvip/gox-backend-rust"
	"github.com/robertpanvip/gox-idea/ui"
	"github.com/robertpanvip/gox/widgets"
)

// TestEditorClickRoundTrip 回归测试：光标列 ↔ 点击像素必须双向闭环。
//
// 背景 bug：桌面装配层从未注入实测字符宽度，编辑器沿用默认 7.83px，而
// Windows 上 Skia 实际渲染 Consolas 13px ≈ 7.15px——假定宽 > 实际宽，点击
// 换算出的列偏小且随列号累计放大，表现为「点击定位偏移、空格插错位置」。
// 修复：main.go 启动时用 skia.MeasureEditorAdvances 实测注入。本测试模拟
// 同一注入流程，并断言 PrefixWidth 与 PickColumn 互逆（用真实 Consolas 度量）。
func TestEditorClickRoundTrip(t *testing.T) {
	narrow, wide := skia.MeasureEditorAdvances(widgets.EditorCodeFont(), widgets.EditorCodeFontSize)
	if narrow <= 0 || wide <= 0 {
		t.Fatalf("实测字符宽度非法: narrow=%v wide=%v", narrow, wide)
	}
	t.Logf("实测: narrow=%.4fpx wide=%.4fpx（原默认 7.83/13.02）", narrow, wide)
	ui.SetCharWidth(narrow)
	ui.SetWideWidth(wide)

	// 把编辑器设为一段典型 GX 源码（含用户报障的 return <div> 行与中文注释行）。
	src := "pub func render(name string) any {\n    return <div class=\"card\">{name}</div>\n}\n// 中文注释：宽字符也要换算正确\n"
	ui.EditorSetSource(src)

	lines := strings.Split(src, "\n")
	for ln, line := range lines {
		for col := 0; col <= len([]rune(line)); col++ {
			x := ui.PrefixWidth(line, col)
			got := ui.PickColumn(ln, x)
			if got != col {
				t.Fatalf("行 %d 列 %d 闭环失败: x=%.2fpx 反算回列 %d", ln, col, x, got)
			}
		}
		// 真实点击往往落在字符中间：任意 x 都必须落在合法列边界上。
		maxX := ui.PrefixWidth(line, len([]rune(line)))
		for x := 0.0; x < maxX; x += 1.7 {
			if c := ui.PickColumn(ln, x); c < 0 || c > len([]rune(line)) {
				t.Fatalf("行 %d x=%.1fpx 反算列 %d 越界", ln, x, c)
			}
		}
	}
}
