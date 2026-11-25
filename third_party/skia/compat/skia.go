// Package compat 提供与 github.com/pekim/go-skia v0.1.0 同形的 API 外观，
// 内部委托给 unison 的完整 Skia 绑定（github.com/robertpanvip/gox-skia）。
//
// 目的：让 GoX 的 backend/skia 绘制层与真实 GL 窗口只需改 import 路径，
// 即可从能力受限的 pekim 绑定迁移到支持原生 Shader/MaskFilter/ImageFilter
// 的完整绑定，同时保留旧 API 的值类型语义（Rect/Font/Image 等为 struct 值）。
//
// 值类型（Rect/Font/Image/Paint/Path/Canvas/...）用 struct 包装上游指针；
// 因为这些类型在业务代码里按值传递、可比较（Font 用作 map key、与零值比较），
// 所以必须是可比较的值类型——struct{ ptr } 满足此约束。
package compat

import (
	usk "github.com/robertpanvip/gox-skia"
	"github.com/richardwilkes/toolbox/v2/geom"
)

// Color 是 ARGB 的 uint32，与 Skia C API 一致。
type Color = usk.Color

// ColorSetARGB 以 A/R/G/B 分量构造颜色。
func ColorSetARGB(a, r, g, b uint32) Color { return Color(a<<24 | r<<16 | g<<8 | b) }

// ---------- Rect ----------

// Rect 是值类型矩形：左上角 (X,Y)，尺寸 (W,H)。与 pekim go-skia 的
// RectMakeXYWH/MakeWH 语义一致（存 x/y/w/h，非 left/top/right/bottom）。
type Rect struct{ X, Y, W, H float32 }

func RectMakeXYWH(x, y, w, h float32) Rect    { return Rect{x, y, w, h} }
func RectMakeWH(w, h float32) Rect            { return Rect{0, 0, w, h} }
func (r Rect) MakeWH(w, h float32) Rect       { return Rect{r.X, r.Y, w, h} }
func (r Rect) Width() float32                 { return r.W }
func (r Rect) Height() float32                { return r.H }
func (r Rect) IsEmpty() bool                  { return r.W <= 0 || r.H <= 0 }

func toGeomRect(r Rect) geom.Rect { return geom.Rect{X: r.X, Y: r.Y, Width: r.W, Height: r.H} }

// ---------- Point / Size ----------

type Point struct{ X, Y float32 }

// ---------- 枚举常量（数值对齐 pekim go-skia / Skia C API）----------

type PaintStyle = usk.PaintStyle

const (
	PaintStyleFill          PaintStyle = 0
	PaintStyleStroke        PaintStyle = 1
	PaintStyleStrokeAndFill PaintStyle = 2
)

type ClipOp = usk.ClipOp

const (
	ClipOpDifference ClipOp = 0
	ClipOpIntersect  ClipOp = 1
)

type PathDirection = usk.Direction

const (
	PathDirectionCW  PathDirection = 0
	PathDirectionCCW PathDirection = 1
)

type TextEncoding = usk.TextEncoding

const (
	TextEncodingUTF8  TextEncoding = 0
	TextEncodingUTF16 TextEncoding = 1
	TextEncodingUTF32 TextEncoding = 2
)

type CanvasSrcRectConstraint int32

const CanvasSrcRectConstraintStrict CanvasSrcRectConstraint = 0

type ColorType = usk.ColorType

const (
	ColorTypeRGBA_8888 ColorType = usk.ColorTypeRGBA8888
	ColorTypeBGRA_8888 ColorType = usk.ColorTypeBGRA8888
)

type AlphaType = usk.AlphaType

const AlphaTypePremul AlphaType = usk.AlphaTypePreMul

type PixelGeometry = usk.PixelGeometry

const PixelGeometryRGB_H PixelGeometry = usk.PixelGeometryRGBH

type FontStyleWeight = usk.FontWeight
type FontStyleSpacing = usk.FontSpacing
type FontStyleSlant = usk.FontSlant

const (
	FontWeightNormal FontStyleWeight = 400
	FontWeightBold   FontStyleWeight = 700
	FontSpacingNormal FontStyleSpacing = 0
	FontSlantUpright  FontStyleSlant = 0
)

// ---------- Paint ----------

// Paint 包装上游 Paint 指针。值类型、可比较（仅含一个指针字段）。
type Paint struct{ p usk.Paint }

func NewPaint() Paint { return Paint{usk.PaintNew()} }
func (p Paint) Delete() {
	if p.p != nil {
		usk.PaintDelete(p.p)
	}
}
func (p Paint) IsNil() bool { return p.p == nil }
func (p Paint) SetColor(c Color) { usk.PaintSetColor(p.p, c) }
func (p Paint) SetStyle(s PaintStyle) { usk.PaintSetStyle(p.p, s) }
func (p Paint) SetStrokeWidth(w float32) { usk.PaintSetStrokeWidth(p.p, w) }
func (p Paint) SetStrokeMiter(m float32) { usk.PaintSetStrokeMiter(p.p, m) }
func (p Paint) SetStrokeCap(c usk.StrokeCap) { usk.PaintSetStrokeCap(p.p, c) }
func (p Paint) SetStrokeJoin(j usk.StrokeJoin) { usk.PaintSetStrokeJoin(p.p, j) }
func (p Paint) SetAntiAlias(aa bool) { usk.PaintSetAntialias(p.p, aa) }
func (p Paint) SetDither(d bool) { usk.PaintSetDither(p.p, d) }
func (p Paint) SetBlendMode(m usk.BlendMode) { usk.PaintSetBlendMode(p.p, m) }

// SetAlpha 通过乘改颜色 alpha 实现（上游绑定无 PaintSetAlpha）。
func (p Paint) SetAlpha(a uint32) {
	c := usk.PaintGetColor(p.p)
	r, g, b := uint32(c>>16&0xff), uint32(c>>8&0xff), uint32(c&0xff)
	usk.PaintSetColor(p.p, ColorSetARGB(a, r, g, b))
}

// SetShader 设置渐变/图案 shader（上游原生能力）。
func (p Paint) SetShader(s Shader) { usk.PaintSetShader(p.p, s.p) }
func (p Paint) GetShader() Shader  { return Shader{usk.PaintGetShader(p.p)} }
func (p Paint) SetMaskFilter(f MaskFilter) { usk.PaintSetMaskFilter(p.p, f.p) }
func (p Paint) SetImageFilter(f ImageFilter) { usk.PaintSetImageFilter(p.p, f.p) }

// ---------- Shader / Filter ----------

type TileMode = usk.TileMode

const (
	TileModeClamp  TileMode = usk.TileMode(0)
	TileModeRepeat TileMode = usk.TileMode(1)
	TileModeMirror TileMode = usk.TileMode(2)
	TileModeDecal  TileMode = usk.TileMode(3)
)

type Shader struct{ p usk.Shader }
func (s Shader) IsNil() bool { return s.p == nil }
func (s Shader) Delete() { if s.p != nil { usk.ShaderUnref(s.p) } }

// NewLinearGradientShader 用原生 Skia shader 构造线性渐变。
// start/end 为 canvas 坐标系下的起止点，colors 与 positions 等长（positions∈[0,1] 单调递增）。
// 内部强制使用单位矩阵——上游绑定对零值 geom.Matrix 会返回 nil shader。
func NewLinearGradientShader(start, end Point, colors []Color, positions []float32) Shader {
	return Shader{usk.ShaderNewLinearGradient(
		geom.Point{X: start.X, Y: start.Y},
		geom.Point{X: end.X, Y: end.Y},
		colors, positions, TileModeClamp, geom.NewIdentityMatrix(),
	)}
}

// Radial-gradient shader（供后续扩展）。
func NewRadialGradientShader(center Point, radius float32, colors []Color, positions []float32) Shader {
	return Shader{usk.ShaderNewRadialGradient(
		geom.Point{X: center.X, Y: center.Y}, radius,
		colors, positions, TileModeClamp, geom.NewIdentityMatrix(),
	)}
}

type MaskFilter struct{ p usk.MaskFilter }
func (f MaskFilter) IsNil() bool { return f.p == nil }

type ImageFilter struct{ p usk.ImageFilter }
func (f ImageFilter) IsNil() bool { return f.p == nil }

// ---------- Path ----------

type Path struct{ p usk.Path }

func NewPath() Path { return Path{usk.PathNew()} }
func (p Path) Delete() { if p.p != nil { usk.PathDelete(p.p) } }
func (p Path) IsEmpty() bool { return usk.PathIsEmpty(p.p) }
func (p Path) MoveToPoint(x, y float32) { usk.PathMoveTo(p.p, geom.Point{X: x, Y: y}) }
func (p Path) LineTo(x, y float32) { usk.PathLineTo(p.p, geom.Point{X: x, Y: y}) }
func (p Path) QuadTo(x1, y1, x2, y2 float32) {
	usk.PathQuadTo(p.p, geom.Point{X: x1, Y: y1}, geom.Point{X: x2, Y: y2})
}
func (p Path) CubicTo(x1, y1, x2, y2, x3, y3 float32) {
	usk.PathCubicTo(p.p, geom.Point{X: x1, Y: y1}, geom.Point{X: x2, Y: y2}, geom.Point{X: x3, Y: y3})
}
func (p Path) Close() { usk.PathClose(p.p) }
func (p Path) AddRoundRect1(rect Rect, rx, ry float32, dir PathDirection) {
	usk.PathAddRoundedRect(p.p, toGeomRect(rect), geom.Size{Width: rx, Height: ry}, usk.Direction(dir))
}
