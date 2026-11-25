package compat

import "testing"

// TestLinearGradientShaderPixels 端到端验证：用 compat 暴露的 API 构造线性渐变
// shader → 设到 Paint → 画矩形 → 读回像素，确认左侧偏红、右侧偏蓝。
// 证明 compat 包装的原生 canvas shader 能力真实可用（非仅构造成功）。
func TestLinearGradientShaderPixels(t *testing.T) {
	const W, H = 64, 16
	c := NewCanvasWithDimensions(W, H, NewSurfaceProps())
	if c.IsNil() {
		t.Fatal("canvas 创建失败")
	}
	sh := NewLinearGradientShader(Point{X: 0, Y: 0}, Point{X: W, Y: 0},
		[]Color{ColorSetARGB(255, 255, 0, 0), ColorSetARGB(255, 0, 0, 255)},
		[]float32{0, 1})
	if sh.IsNil() {
		t.Fatal("shader 构造失败")
	}
	defer sh.Delete()

	paint := NewPaint()
	defer paint.Delete()
	paint.SetShader(sh)
	c.DrawRect(RectMakeWH(W, H), paint)

	pix := c.Pixmap()
	if pix == nil {
		t.Fatal("pixmap 不可用")
	}
	// canvas 像素 word 布局：低字节为 R。渐变红→蓝，左端 R 应远大于右端。
	row := (H / 2) * W
	leftR := int(byte(pix.Pix[row]))
	rightR := int(byte(pix.Pix[row+(W-1)]))
	if leftR <= rightR+40 {
		t.Fatalf("渐变方向/着色错误：左端R=%d 右端R=%d（期望红→蓝，左R明显大于右R）", leftR, rightR)
	}
}
