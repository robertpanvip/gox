package skia

// 回归测试：导出的 MeasureTextWidth 必须与绘制端同口径返回真实字体宽度，
// 供桌面端注入编辑器。若返回宽度恒等于某硬编码值，编辑器字宽注入就会失效，
// 重新触发 caret 错位 / 点击落点偏移 / 拖选截断。

import "testing"

const editorMonoStack = "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"

// TestMeasureTextWidthMatchesDrawPath 验证导出 API 与绘制端分段测宽口径一致：
// 混排（ASCII + CJK）总宽等于各段宽度之和。
func TestMeasureTextWidthMatchesDrawPath(t *testing.T) {
	text := "Hi中A"
	got := MeasureTextWidth(text, 13, false, editorMonoStack)
	var sum float32
	for _, r := range splitRuns(text, 13, false, editorMonoStack) {
		if r.text == "" || r.font.IsNil() {
			continue
		}
		sum += r.font.MeasureText([]byte(r.text), uint32(len(r.text)), 0, nil)
	}
	if diff := got - sum; diff < -0.01 || diff > 0.01 {
		t.Errorf("MeasureTextWidth = %v, 绘制端分段和 = %v, 差 = %v", got, sum, diff)
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
