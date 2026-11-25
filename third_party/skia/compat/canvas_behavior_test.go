package compat

import (
	"testing"
)

// 本文件固化导出 Canvas 的行为契约。compat 是纯 Go canvas 对 go-skia API 的兼容层，
// backend/skia 的绘制与桌面脏矩形重绘都依赖下面这些语义。用像素级断言验证，而不是
// 只测"调用不崩"——历史上曾因仅靠源码推断 Clear/ClipRect 在 Scale 下的语义而误判。
//
// 像素布局：Pixmap 是 RGBA8888（内存字节序 R,G,B,A），RGBA8888Bytes() 按此顺序返回；
// 初始像素透明 (0,0,0,0)，非预乘语义下的纯色为 (R,G,B,255)。

// rgba 读回 (x,y) 的 R,G,B,A（左上原点）。
func rgba(t *testing.T, b []byte, w, x, y int) (r, g, bl, a int) {
	t.Helper()
	i := (y*w + x) * 4
	if i < 0 || i+3 >= len(b) {
		t.Fatalf("像素坐标 (%d,%d) 越界: buf=%d bytes", x, y, len(b))
	}
	return int(b[i]), int(b[i+1]), int(b[i+2]), int(b[i+3])
}

func assertPixel(t *testing.T, b []byte, w, x, y int, want [4]int, label string) {
	t.Helper()
	r, g, bl, a := rgba(t, b, w, x, y)
	got := [4]int{r, g, bl, a}
	if got != want {
		t.Errorf("%s @(%d,%d): got %v, want %v", label, x, y, got, want)
	}
}

var (
	red   = [4]int{255, 0, 0, 255}
	green = [4]int{0, 255, 0, 255}
	white = [4]int{255, 255, 255, 255}
)

// TestCanvasClearAndInitial 验证初始透明、Clear 填满整块画布且颜色精确。
func TestCanvasClearAndInitial(t *testing.T) {
	const W, H = 16, 16
	c := NewCanvasWithDimensions(W, H, NewSurfaceProps())
	defer c.Delete()
	if c.IsNil() {
		t.Fatal("canvas 不应为 nil")
	}
	pix := c.Pixmap()
	if pix == nil || pix.Width != W || pix.Height != H {
		t.Fatalf("pixmap 尺寸异常: %+v", pix)
	}
	b0 := pix.RGBA8888Bytes()
	assertPixel(t, b0, W, 0, 0, [4]int{0, 0, 0, 0}, "初始像素应透明")

	c.Clear(ColorSetARGB(255, 255, 0, 0))
	b := pix.RGBA8888Bytes()
	assertPixel(t, b, W, 0, 0, red, "Clear 左上")
	assertPixel(t, b, W, W-1, H-1, red, "Clear 右下")
}

// TestCanvasClearIgnoresCTM 验证 Clear 填充"当前 clip"而不受缩放矩阵影响——
// 这是 renderCached 全屏清屏在 Scale(dpr) 下仍覆盖整块 framebuffer 的依据。
func TestCanvasClearIgnoresCTM(t *testing.T) {
	const W, H = 40, 40
	c := NewCanvasWithDimensions(W, H, NewSurfaceProps())
	defer c.Delete()
	c.Clear(ColorSetARGB(255, 255, 0, 0))
	c.Save()
	c.Scale(2, 2)
	c.Clear(ColorSetARGB(255, 0, 255, 0))
	c.Restore()
	b := c.Pixmap().RGBA8888Bytes()
	// 若 Clear 受 CTM 影响，只会染绿左上 20×20；正确行为是整张画布都绿。
	for _, p := range [][2]int{{0, 0}, {19, 19}, {20, 20}, {W - 1, H - 1}} {
		assertPixel(t, b, W, p[0], p[1], green, "Scale 后 Clear 应仍覆盖全画布")
	}
}

// TestCanvasClearRespectsClip 验证 Clear 受 clip 限制（左闭右开边界）——
// 这是脏矩形局部重绘"只覆盖脏区、不误清邻区"的依据。
func TestCanvasClearRespectsClip(t *testing.T) {
	const W, H = 40, 40
	c := NewCanvasWithDimensions(W, H, NewSurfaceProps())
	defer c.Delete()
	c.Clear(ColorSetARGB(255, 255, 0, 0))
	c.Save()
	c.ClipRect(RectMakeXYWH(10, 10, 20, 20), ClipOpIntersect, false)
	c.Clear(ColorSetARGB(255, 0, 255, 0))
	c.Restore()
	b := c.Pixmap().RGBA8888Bytes()
	assertPixel(t, b, W, 9, 15, red, "clip 左外侧不变")
	assertPixel(t, b, W, 10, 15, green, "clip 左边界含")
	assertPixel(t, b, W, 29, 15, green, "clip 右内侧染绿")
	assertPixel(t, b, W, 30, 15, red, "clip 右边界不含（左闭右开）")
}

// TestCanvasScaleMapsDraw 验证 Scale 把逻辑坐标映射到物理像素。
func TestCanvasScaleMapsDraw(t *testing.T) {
	const W, H = 40, 40
	c := NewCanvasWithDimensions(W, H, NewSurfaceProps())
	defer c.Delete()
	c.Clear(ColorSetARGB(255, 255, 255, 255))
	p := NewPaint()
	p.SetColor(ColorSetARGB(255, 255, 0, 0))
	p.SetAntiAlias(false)
	c.Save()
	c.Scale(2, 2)
	c.DrawRect(RectMakeXYWH(5, 5, 10, 10), p) // 逻辑 (5,5)-(15,15) → 物理 (10,10)-(30,30)
	c.Restore()
	b := c.Pixmap().RGBA8888Bytes()
	assertPixel(t, b, W, 9, 9, white, "缩放矩形外左上")
	assertPixel(t, b, W, 10, 10, red, "缩放矩形左上")
	assertPixel(t, b, W, 25, 25, red, "缩放矩形内")
	assertPixel(t, b, W, 30, 30, white, "缩放矩形右下外（左闭右开）")
}

// TestCanvasClipRectMappedByCTM 验证 ClipRect 的坐标被当前矩阵映射——
// 这是脏矩形逻辑坐标经 Scale(dpr) 后落到正确物理像素的依据。
func TestCanvasClipRectMappedByCTM(t *testing.T) {
	const W, H = 40, 40
	c := NewCanvasWithDimensions(W, H, NewSurfaceProps())
	defer c.Delete()
	c.Clear(ColorSetARGB(255, 255, 0, 0))
	c.Save()
	c.Scale(2, 2)
	c.ClipRect(RectMakeXYWH(5, 5, 10, 10), ClipOpIntersect, false) // → 物理 (10,10)-(30,30)
	c.Clear(ColorSetARGB(255, 0, 255, 0))
	c.Restore()
	b := c.Pixmap().RGBA8888Bytes()
	assertPixel(t, b, W, 0, 0, red, "映射裁剪区外")
	assertPixel(t, b, W, 10, 10, green, "映射裁剪区左上")
	assertPixel(t, b, W, 29, 29, green, "映射裁剪区内")
	assertPixel(t, b, W, 39, 39, red, "映射裁剪区外右下")
}

// TestCanvasSaveRestore 验证 Save/Restore 成对恢复矩阵（绘制落点正确复位）。
func TestCanvasSaveRestore(t *testing.T) {
	const W, H = 40, 40
	c := NewCanvasWithDimensions(W, H, NewSurfaceProps())
	defer c.Delete()
	c.Clear(ColorSetARGB(255, 255, 255, 255))
	p := NewPaint()
	p.SetColor(ColorSetARGB(255, 255, 0, 0))
	p.SetAntiAlias(false)
	c.Save()
	c.Scale(2, 2)
	c.DrawRect(RectMakeXYWH(2, 2, 4, 4), p) // 物理 (4,4)-(12,12)
	c.Restore()
	c.DrawRect(RectMakeXYWH(20, 20, 4, 4), p) // 复位后逻辑=物理 (20,20)-(24,24)
	b := c.Pixmap().RGBA8888Bytes()
	assertPixel(t, b, W, 4, 4, red, "缩放后绘制")
	assertPixel(t, b, W, 20, 20, red, "Restore 后绘制应回到未缩放坐标")
	assertPixel(t, b, W, 30, 30, white, "复位后坐标未再被放大")
}

// TestCanvasDrawPrimitives 验证基本绘制原语：矩形左上原点、圆角、圆、线宽。
func TestCanvasDrawPrimitives(t *testing.T) {
	const W, H = 40, 40
	p := NewPaint()
	p.SetColor(ColorSetARGB(255, 255, 0, 0))
	p.SetAntiAlias(false)

	// 矩形：左上原点、左闭右开。
	c := NewCanvasWithDimensions(W, H, NewSurfaceProps())
	defer c.Delete()
	c.Clear(ColorSetARGB(255, 255, 255, 255))
	c.DrawRect(RectMakeXYWH(2, 2, 6, 6), p)
	b := c.Pixmap().RGBA8888Bytes()
	assertPixel(t, b, W, 2, 2, red, "矩形左上")
	assertPixel(t, b, W, 7, 7, red, "矩形右下内")
	assertPixel(t, b, W, 8, 8, white, "矩形右下外")

	// 圆角矩形：角点留白、边中心填充。
	c2 := NewCanvasWithDimensions(W, H, NewSurfaceProps())
	defer c2.Delete()
	c2.Clear(ColorSetARGB(255, 255, 255, 255))
	c2.DrawRoundRect(RectMakeXYWH(5, 5, 20, 20), 8, 8, p)
	b2 := c2.Pixmap().RGBA8888Bytes()
	assertPixel(t, b2, W, 5, 5, white, "圆角角点应留白")
	assertPixel(t, b2, W, 15, 5, red, "圆角上边中点")
	assertPixel(t, b2, W, 15, 15, red, "圆角中心")

	// 圆：中心与内部填充、外部留白。
	c3 := NewCanvasWithDimensions(W, H, NewSurfaceProps())
	defer c3.Delete()
	c3.Clear(ColorSetARGB(255, 255, 255, 255))
	c3.DrawCircleScalars(20, 20, 10, p)
	b3 := c3.Pixmap().RGBA8888Bytes()
	assertPixel(t, b3, W, 20, 20, red, "圆心")
	assertPixel(t, b3, W, 25, 20, red, "圆内")
	assertPixel(t, b3, W, 32, 20, white, "圆外")
}

// TestCanvasGradient 验证线性渐变方向正确（左红右蓝、单调过渡）。
func TestCanvasGradient(t *testing.T) {
	const W, H = 64, 16
	c := NewCanvasWithDimensions(W, H, NewSurfaceProps())
	defer c.Delete()
	sh := NewLinearGradientShader(Point{X: 0, Y: 0}, Point{X: W, Y: 0},
		[]Color{ColorSetARGB(255, 255, 0, 0), ColorSetARGB(255, 0, 0, 255)},
		[]float32{0, 1})
	defer sh.Delete()
	p := NewPaint()
	p.SetShader(sh)
	p.SetAntiAlias(false)
	c.DrawRect(RectMakeWH(W, H), p)
	b := c.Pixmap().RGBA8888Bytes()
	r0, _, _, _ := rgba(t, b, W, 0, H/2)
	r16, _, _, _ := rgba(t, b, W, 16, H/2)
	r32, _, _, _ := rgba(t, b, W, 32, H/2)
	r48, _, _, _ := rgba(t, b, W, 48, H/2)
	r63, _, _, _ := rgba(t, b, W, 63, H/2)
	if !(r0 > r16 && r16 > r32 && r32 > r48 && r48 > r63) {
		t.Errorf("渐变红通道应从左到右单调递减: %d %d %d %d %d", r0, r16, r32, r48, r63)
	}
	if r0 < 200 || r63 > 40 {
		t.Errorf("渐变端点应接近纯红/纯蓝: 左R=%d 右R=%d", r0, r63)
	}
}

// TestCanvasClipBoundaries 验证裁剪边界的精确性：左闭右开、非整数缩放取整、
// ClipOpDifference 挖空、Paint 值拷贝共享底层指针（API 文档声明的值类型语义）。
func TestCanvasClipBoundaries(t *testing.T) {
	const W, H = 40, 40

	// 非整数缩放 1.5 下裁剪边界：逻辑 (4,4)-(14,14) → 物理 (6,6)-(21,21)。
	c := NewCanvasWithDimensions(W, H, NewSurfaceProps())
	defer c.Delete()
	c.Clear(ColorSetARGB(255, 255, 0, 0))
	c.Save()
	c.Scale(1.5, 1.5)
	c.ClipRect(RectMakeXYWH(4, 4, 10, 10), ClipOpIntersect, false)
	c.Clear(ColorSetARGB(255, 0, 255, 0))
	c.Restore()
	b := c.Pixmap().RGBA8888Bytes()
	assertPixel(t, b, W, 5, 10, red, "1.5x 裁剪左外")
	assertPixel(t, b, W, 6, 10, green, "1.5x 裁剪左边界含")
	assertPixel(t, b, W, 20, 10, green, "1.5x 裁剪内")
	assertPixel(t, b, W, 21, 10, red, "1.5x 裁剪右边界不含")

	// Difference：先允许整屏，再挖掉中间块，Clear 只染块外。
	c2 := NewCanvasWithDimensions(W, H, NewSurfaceProps())
	defer c2.Delete()
	c2.Clear(ColorSetARGB(255, 0, 255, 0))
	c2.Save()
	c2.ClipRect(RectMakeWH(W, H), ClipOpIntersect, false)
	c2.ClipRect(RectMakeXYWH(10, 10, 20, 20), ClipOpDifference, false)
	c2.Clear(ColorSetARGB(255, 255, 0, 0))
	c2.Restore()
	b2 := c2.Pixmap().RGBA8888Bytes()
	assertPixel(t, b2, W, 20, 20, green, "Difference 挖空区保留底色")
	assertPixel(t, b2, W, 5, 5, red, "Difference 区外被 Clear")

	// Paint 值拷贝共享底层指针：p2 改色后 p 绘制也变绿（compat 文档声明的值类型语义）。
	p := NewPaint()
	p.SetColor(ColorSetARGB(255, 255, 0, 0))
	p2 := p
	p2.SetColor(ColorSetARGB(255, 0, 255, 0))
	c3 := NewCanvasWithDimensions(W, H, NewSurfaceProps())
	defer c3.Delete()
	c3.Clear(ColorSetARGB(255, 255, 255, 255))
	c3.DrawRect(RectMakeWH(10, 10), p)
	b3 := c3.Pixmap().RGBA8888Bytes()
	assertPixel(t, b3, W, 5, 5, green, "Paint 值拷贝应共享底层指针")
}

// TestCanvasDrawString 验证文字绘制产生可见像素、MeasureText 返回正宽度。
func TestCanvasDrawString(t *testing.T) {
	c := NewCanvasWithDimensions(80, 40, NewSurfaceProps())
	defer c.Delete()
	c.Clear(ColorSetARGB(255, 255, 255, 255))
	tf := FontMgrDefault().MatchFamilyStyle("", FontStyleNormal())
	if tf.IsNil() {
		t.Skip("系统无可用默认字体")
	}
	f := NewFontTypefaceSize(tf, 24)
	defer f.Delete()
	if w := f.MeasureText([]byte("Hi"), 2, TextEncodingUTF8, nil); w <= 0 {
		t.Errorf("MeasureText('Hi') 应为正宽度, got %v", w)
	}
	p := NewPaint()
	p.SetColor(ColorSetARGB(255, 255, 0, 0))
	c.DrawString("Hi", 5, 28, f, p)
	b := c.Pixmap().RGBA8888Bytes()
	nonWhite := 0
	for i := 0; i < len(b); i += 4 {
		if b[i] != 255 || b[i+1] != 255 || b[i+2] != 255 {
			nonWhite++
		}
	}
	if nonWhite == 0 {
		t.Error("DrawString 应产生非白像素（文字未绘制）")
	}
}
