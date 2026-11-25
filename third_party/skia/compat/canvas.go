package compat

import (
	cc "github.com/richardwilkes/canvas/canvas"
	cfont "github.com/richardwilkes/canvas/font"
	cr "github.com/richardwilkes/canvas/raster"
	"github.com/richardwilkes/canvas/shaders"
)

// Canvas 包装 canvas.Canvas 指针并可选持有其 raster 像素缓冲（负责生命周期）。
// 值类型、可比较。由 NewCanvasWithDimensions 创建的实例拥有 pix，Delete 时释放；
// 从外部 Surface.GetCanvas 得到的实例 pix=nil，Delete 不释放像素。
type Canvas struct {
	c   *cc.Canvas
	pix *cr.Pixmap
}

// NewCanvasWithDimensions 创建一块离屏 raster canvas（持有像素缓冲）。
func NewCanvasWithDimensions(width, height int32, _ SurfaceProps) Canvas {
	pix := cr.NewPixmap(width, height)
	return Canvas{c: cc.NewForPixmap(pix), pix: pix}
}

func (c Canvas) IsNil() bool { return c.c == nil }
func (c Canvas) Delete()     { c.c, c.pix = nil, nil }

func (c Canvas) Clear(color Color) { c.c.Clear(color) }
func (c Canvas) Save() int32       { return int32(c.c.Save()) }
func (c Canvas) Restore()          { c.c.Restore() }
func (c Canvas) Scale(sx, sy float32) {
	c.c.Scale(sx, sy)
}
func toClipOp(op ClipOp) cr.ClipOp {
	if op == ClipOpDifference {
		return cr.ClipDifference
	}
	return cr.ClipIntersect
}
func (c Canvas) ClipRect(rect Rect, op ClipOp, aa bool) {
	c.c.ClipRect(toGeomRect(rect), toClipOp(op), aa)
}
func (c Canvas) ClipPath(path Path, op ClipOp, aa bool) {
	c.c.ClipPath(path.p, toClipOp(op), aa)
}
func (c Canvas) DrawRect(rect Rect, paint Paint) {
	c.c.DrawRect(toGeomRect(rect), paint.p)
}
func (c Canvas) DrawRoundRect(rect Rect, rx, ry float32, paint Paint) {
	c.c.DrawRoundRect(toGeomRect(rect), rx, ry, paint.p)
}
func (c Canvas) DrawCircleScalars(cx, cy, radius float32, paint Paint) {
	c.c.DrawCircle(cx, cy, radius, paint.p)
}
func (c Canvas) DrawLineScalars(x0, y0, x1, y1 float32, paint Paint) {
	c.c.DrawLine(x0, y0, x1, y1, paint.p)
}
func (c Canvas) DrawPath(path Path, paint Paint) {
	c.c.DrawPath(path.p, paint.p)
}
func (c Canvas) DrawImageRect(img Image, src, dst Rect, _ SamplingOptions, paint Paint, _ CanvasSrcRectConstraint) {
	if img.p == nil {
		return
	}
	c.c.DrawImageRect(img.p, toGeomRect(src), toGeomRect(dst), shaders.SamplingOptions{}, paint.p, cc.ConstraintStrict)
}
func (c Canvas) DrawString(str string, x, y float32, f Font, paint Paint) {
	if f.p == nil {
		return
	}
	c.c.DrawSimpleText([]byte(str), cfont.TextEncodingUTF8, x, y, f.p, paint.p)
}

// Pixmap 返回 CPU raster canvas 的像素缓冲（供 GL 窗口上传纹理使用）。
// 非 raster canvas（外部 surface）返回 nil。
func (c Canvas) Pixmap() *cr.Pixmap { return c.pix }
