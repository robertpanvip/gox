package skia

// 回归测试：导出的 MeasureTextWidth 必须与绘制端同口径返回真实字体宽度，
// 供桌面端注入编辑器。若返回宽度恒等于某硬编码值，编辑器字宽注入就会失效，
// 重新触发 caret 错位 / 点击落点偏移 / 拖选截断。

import "testing"

const editorMonoStack = "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"

// TestMeasureTextWidthMatchesDrawPath 验证导出 API 与绘制端分段测宽口径一致：
// 混排（ASCII + CJK）总宽等于各段宽度之和。
// 关键：绘制端 paint.go 先把 CSS 字体栈经 pickFontFamily 解析成实际族名再测宽；
// 本测试复刻这一口径，防止 MeasureTextWidth 漏掉 pickFontFamily（该疏漏曾导致
// 注入字宽 7.41 与绘制端真实等宽字宽 7.83 不符，点击/拖选全套错位）。
func TestMeasureTextWidthMatchesDrawPath(t *testing.T) {
	text := "Hi中A"
	got := MeasureTextWidth(text, 13, false, editorMonoStack)
	picked := pickFontFamily(editorMonoStack)
	var sum float32
	for _, r := range splitRuns(text, 13, false, picked) {
		if r.text == "" || r.font.IsNil() {
			continue
		}
		sum += r.font.MeasureText([]byte(r.text), uint32(len(r.text)), 0, nil)
	}
	if diff := got - sum; diff < -0.01 || diff > 0.01 {
		t.Errorf("MeasureTextWidth = %v, 绘制端分段和 = %v, 差 = %v", got, sum, diff)
	}
}

// TestCodeFontIsMonospaced 锁定根因：编辑器代码字体必须是真正等宽的——
// '0'/'i'/'M'/空格 的 advance 完全一致。MeasureTextWidth 若漏掉 pickFontFamily，
// 会用默认字体测出 i=3.5/M=10.5 的变宽值，编辑器按恒定字宽反推列就会错位。
func TestCodeFontIsMonospaced(t *testing.T) {
	for _, s := range []string{"0000000000", "iiiiiiiiii", "MMMMMMMMMM", "        "} {
		w := MeasureTextWidth(s, 13, false, editorMonoStack) / float32(len([]rune(s)))
		if w < 7 || w > 9 {
			t.Errorf("等宽字体 %q 单格宽 %.4f 超出合理范围 [7,9]", s, w)
		}
	}
	// 三种差异极大的字符单格宽必须一致（等宽本质）。
	a := MeasureTextWidth("0000000000", 13, false, editorMonoStack) / 10
	b := MeasureTextWidth("iiiiiiiiii", 13, false, editorMonoStack) / 10
	if diff := a - b; diff < -0.01 || diff > 0.01 {
		t.Errorf("等宽字体但 '0'(%.4f) 与 'i'(%.4f) 宽度不同，差=%v", a, b, diff)
	}
}

// TestMeasureTextWidthNarrowAndWide 验证单格宽度 > 0 且 CJK 走独立 fallback。
func TestMeasureTextWidthNarrowAndWide(t *testing.T) {
	narrow := MeasureTextWidth("0000000000", 13, false, editorMonoStack) / 10
	wide := MeasureTextWidth("中中中", 13, false, editorMonoStack) / 3
	if narrow <= 0 || wide <= 0 {
		t.Fatalf("单格宽度应 > 0, narrow=%v wide=%v", narrow, wide)
	}
	if narrow > 20 || wide > 40 {
		t.Errorf("单格宽度数量级异常: narrow=%v wide=%v", narrow, wide)
	}
}
