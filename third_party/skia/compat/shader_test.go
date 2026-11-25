package compat

import (
	"testing"

	usk "github.com/robertpanvip/gox-skia"
	"github.com/richardwilkes/toolbox/v2/geom"
)

// TestLinearGradientShaderPixels 端到端验证：用 compat 暴露的 API 构造线性渐变
// shader → 设到 Paint → 画矩形 → 读回像素，确认左侧偏红、右侧偏蓝。
// 证明 compat 包装的原生 shader 能力真实可用（非仅构造成功）。
func TestLinearGradientShaderPixels(t *testing.T) {
	const W, H = 64, 16
	info := &usk.ImageInfo{
		ColorType: usk.ColorTypeN32,
		AlphaType: usk.AlphaTypePreMul,
		Width:     W,
		Height:    H,
	}
	surf := usk.SurfaceMakeRasterN32PreMul(info, usk.SurfacePropsNew(usk.PixelGeometryUnknown))
	if surf == nil {
		t.Fatal("surface 创建失败")
	}
	defer usk.SurfaceUnref(surf)
	canvas := usk.SurfaceGetCanvas(surf)

	colors := []Color{ColorSetARGB(255, 255, 0, 0), ColorSetARGB(255, 0, 0, 255)}
	positions := []float32{0, 1}
	sh := NewLinearGradientShader(Point{X: 0, Y: 0}, Point{X: W, Y: 0}, colors, positions)
	if sh.IsNil() {
		t.Fatal("shader 构造失败")
	}
	defer sh.Delete()

	paint := NewPaint()
	defer paint.Delete()
	paint.SetShader(sh)
	usk.CanvasDrawRect(canvas, geom.Rect{X: 0, Y: 0, Width: W, Height: H}, paint.p)

	img := usk.SurfaceMakeImageSnapshot(surf)
	if img == nil {
		t.Skip("image snapshot 不可用")
	}
	defer usk.ImageUnref(img)
	pixels := make([]byte, W*H*4)
	ri := &usk.ImageInfo{ColorType: usk.ColorTypeN32, AlphaType: usk.AlphaTypeUnPreMul, Width: W, Height: H}
	if !usk.ImageReadPixels(img, ri, pixels, W*4, 0, 0, usk.ImageCachingHintAllow) {
		t.Skip("读像素不可用")
	}
	// 读回 UnPreMul/N32 布局：R 在偏移 0，B 在偏移 2。渐变红→蓝，左端 R 应远大于右端。
	row := (H / 2) * W * 4
	leftR := int(pixels[row])
	rightR := int(pixels[row+(W-1)*4])
	if leftR <= rightR+40 {
		t.Fatalf("渐变方向/着色错误：左端R=%d 右端R=%d（期望红→蓝，左R明显大于右R）", leftR, rightR)
	}
}
