package compat

import (
	usk "github.com/robertpanvip/gox-skia"
	"github.com/richardwilkes/toolbox/v2/geom"
)

// Canvas 包装上游 Canvas 指针并可选持有其 raster Surface（负责生命周期）。
// 值类型、可比较。由 NewCanvasWithDimensions 创建的实例拥有 surface，Delete
// 时释放；从外部 Surface.GetCanvas 得到的实例 owned=false，Delete 不释放。
type Canvas struct {
	c       usk.Canvas
	surface usk.Surface
}

// NewCanvasWithDimensions 创建一块离屏 raster canvas（持有 surface）。
func NewCanvasWithDimensions(width, height int32, props SurfaceProps) Canvas {
	info := &usk.ImageInfo{
		ColorType: usk.ColorTypeN32,
		AlphaType: usk.AlphaTypePreMul,
		Width:     width,
		Height:    height,
	}
	s := usk.SurfaceMakeRasterN32PreMul(info, props.p)
	return Canvas{c: usk.SurfaceGetCanvas(s), surface: s}
}

func (c Canvas) IsNil() bool { return c.c == nil }
func (c Canvas) Delete() {
	if c.surface != nil {
		usk.SurfaceUnref(c.surface)
	}
}
func (c Canvas) Clear(color Color) { usk.CanvasClear(c.c, color) }
func (c Canvas) Save() int32        { return int32(usk.CanvasSave(c.c)) }
func (c Canvas) Restore()           { usk.CanvasRestore(c.c) }
func (c Canvas) Scale(sx, sy float32) {
	usk.CanvasScale(c.c, geom.Point{X: sx, Y: sy})
}
func (c Canvas) ClipRect(rect Rect, op ClipOp, aa bool) {
	usk.CanavasClipRectWithOperation(c.c, toGeomRect(rect), usk.ClipOp(op), aa)
}
func (c Canvas) ClipPath(path Path, op ClipOp, aa bool) {
	usk.CanavasClipPathWithOperation(c.c, path.p, usk.ClipOp(op), aa)
}
func (c Canvas) DrawRect(rect Rect, paint Paint) {
	usk.CanvasDrawRect(c.c, toGeomRect(rect), paint.p)
}
func (c Canvas) DrawRoundRect(rect Rect, rx, ry float32, paint Paint) {
	usk.CanvasDrawRoundRect(c.c, toGeomRect(rect), geom.Size{Width: rx, Height: ry}, paint.p)
}
func (c Canvas) DrawCircleScalars(cx, cy, radius float32, paint Paint) {
	usk.CanvasDrawCircle(c.c, geom.Point{X: cx, Y: cy}, radius, paint.p)
}
func (c Canvas) DrawLineScalars(x0, y0, x1, y1 float32, paint Paint) {
	usk.CanvasDrawLine(c.c, geom.Point{X: x0, Y: y0}, geom.Point{X: x1, Y: y1}, paint.p)
}
func (c Canvas) DrawPath(path Path, paint Paint) {
	usk.CanvasDrawPath(c.c, path.p, paint.p)
}
func (c Canvas) DrawImageRect(img Image, src, dst Rect, sampling SamplingOptions, paint Paint, _ CanvasSrcRectConstraint) {
	usk.CanvasDrawImageRect(c.c, img.p, toGeomRect(src), toGeomRect(dst), sampling, paint.p)
}
func (c Canvas) DrawString(str string, x, y float32, font Font, paint Paint) {
	usk.CanvasDrawSimpleText(c.c, str, geom.Point{X: x, Y: y}, font.p, paint.p)
}
