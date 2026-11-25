// Package compat 提供与 github.com/pekim/go-skia v0.1.0 同形的 API 外观，
// 内部委托给纯 Go 的 Skia 渲染库 github.com/richardwilkes/canvas。
//
// 目的：让 GoX 的 backend/skia 绘制层与真实 GL 窗口只需改 import 路径，
// 即可用纯 Go canvas（零 cgo 静态库、9MB module）替换掉带 55MB libskia.a
// 的 fork 绑定，同时保留旧 API 的值类型语义（Rect/Font/Image 等为 struct 值）。
//
// 值类型（Rect/Font/Image/Paint/Path/Canvas/...）用 struct 包装上游指针；
// 因为这些类型在业务代码里按值传递、可比较（Font 用作 map key、与零值比较），
// 所以必须是可比较的值类型——struct{ ptr } 满足此约束。
package compat

import (
	cc "github.com/richardwilkes/canvas/canvas"
	"github.com/richardwilkes/canvas/colorcore"
	"github.com/richardwilkes/canvas/filtercore"
	"github.com/richardwilkes/canvas/geom"
	"github.com/richardwilkes/canvas/imagecore"
	"github.com/richardwilkes/canvas/maskfilter"
	cp "github.com/richardwilkes/canvas/path"
	"github.com/richardwilkes/canvas/shaders"
)

// Color 是 ARGB 的 uint32，与 Skia C API 一致。
type Color = colorcore.Color

// ColorSetARGB 以 A/R/G/B 分量构造颜色。
func ColorSetARGB(a, r, g, b uint32) Color {
	return colorcore.ARGB(uint8(a), uint8(r), uint8(g), uint8(b))
}

// ---------- Rect ----------

// Rect 是值类型矩形：左上角 (X,Y)，尺寸 (W,H)。
type Rect struct{ X, Y, W, H float32 }

func RectMakeXYWH(x, y, w, h float32) Rect    { return Rect{x, y, w, h} }
func RectMakeWH(w, h float32) Rect            { return Rect{0, 0, w, h} }
func (r Rect) MakeWH(w, h float32) Rect       { return Rect{r.X, r.Y, w, h} }
func (r Rect) Width() float32                 { return r.W }
func (r Rect) Height() float32                { return r.H }
func (r Rect) IsEmpty() bool                  { return r.W <= 0 || r.H <= 0 }

func toGeomRect(r Rect) geom.Rect { return geom.RectXYWH(r.X, r.Y, r.W, r.H) }

// ---------- Point / Size ----------

type Point struct{ X, Y float32 }

func toGeomPoint(p Point) geom.Point { return geom.Point{X: p.X, Y: p.Y} }

// ---------- 枚举常量（数值对齐 pekim go-skia / Skia C API）----------

type PaintStyle uint32

const (
	PaintStyleFill          PaintStyle = 0
	PaintStyleStroke        PaintStyle = 1
	PaintStyleStrokeAndFill PaintStyle = 2
)

type ClipOp uint32

const (
	ClipOpDifference ClipOp = 0
	ClipOpIntersect  ClipOp = 1
)

type PathDirection uint32

const (
	PathDirectionCW  PathDirection = 0
	PathDirectionCCW PathDirection = 1
)

type TextEncoding uint32

const (
	TextEncodingUTF8  TextEncoding = 0
	TextEncodingUTF16 TextEncoding = 1
	TextEncodingUTF32 TextEncoding = 2
)

type CanvasSrcRectConstraint int32

const CanvasSrcRectConstraintStrict CanvasSrcRectConstraint = 0

type ColorType = imagecore.ColorType

const (
	ColorTypeRGBA_8888 ColorType = imagecore.ColorTypeRGBA8888
	ColorTypeBGRA_8888 ColorType = imagecore.ColorTypeBGRA8888
)

type AlphaType = imagecore.AlphaType

const AlphaTypePremul AlphaType = imagecore.AlphaTypePremul

type PixelGeometry uint32

const PixelGeometryRGB_H PixelGeometry = 0

type FontStyleWeight uint32
type FontStyleSpacing uint32
type FontStyleSlant uint32

const (
	FontWeightNormal  FontStyleWeight = 400
	FontWeightBold    FontStyleWeight = 700
	FontSpacingNormal FontStyleSpacing = 0
	FontSlantUpright  FontStyleSlant = 0
)

// ---------- Paint ----------

// Paint 包装上游 Paint 指针。值类型、可比较（仅含一个指针字段）。
type Paint struct{ p *cc.Paint }

func NewPaint() Paint { return Paint{cc.NewPaint()} }
func (p Paint) Delete() {
	p.p = nil // Go GC 回收；保留方法签名以兼容旧调用
}
func (p Paint) IsNil() bool              { return p.p == nil }
func (p Paint) SetColor(c Color)         { p.p.Color = c }
func (p Paint) SetStyle(s PaintStyle)    { p.p.Style = cc.Style(s) }
func (p Paint) SetStrokeWidth(w float32) { p.p.StrokeWidth = w }
func (p Paint) SetStrokeMiter(m float32) { p.p.MiterLimit = m }
func (p Paint) SetAntiAlias(aa bool)     { p.p.AntiAlias = aa }
func (p Paint) SetDither(d bool)         { p.p.Dither = d }

// SetAlpha 通过改颜色 alpha 实现（与旧 shim 一致）。
func (p Paint) SetAlpha(a uint32) {
	p.p.Color = colorcore.ARGB(uint8(a), p.p.Color.R(), p.p.Color.G(), p.p.Color.B())
}

// SetShader 设置渐变/图案 shader。
func (p Paint) SetShader(s Shader) { p.p.Shader = s.p }
func (p Paint) GetShader() Shader  { return Shader{p.p.Shader} }
func (p Paint) SetMaskFilter(f MaskFilter) { p.p.MaskFilter = f.p }
func (p Paint) SetImageFilter(f ImageFilter) { p.p.ImageFilter = f.p }

// ---------- Shader / Filter ----------

type TileMode uint32

const (
	TileModeClamp  TileMode = 0
	TileModeRepeat TileMode = 1
	TileModeMirror TileMode = 2
	TileModeDecal  TileMode = 3
)

func toShadersTileMode(m TileMode) shaders.TileMode { return shaders.TileMode(m) }

type Shader struct{ p shaders.Shader }

func (s Shader) IsNil() bool { return s.p == nil }
func (s Shader) Delete()     { s.p = nil }

// NewLinearGradientShader 构造线性渐变 shader。
// start/end 为 canvas 坐标系下的起止点，colors 与 positions 等长。
func NewLinearGradientShader(start, end Point, colors []Color, positions []float32) Shader {
	return Shader{shaders.NewLinearGradient(
		toGeomPoint(start), toGeomPoint(end), colors, positions,
		shaders.TileClamp, nil,
	)}
}

// NewRadialGradientShader 供后续扩展。
func NewRadialGradientShader(center Point, radius float32, colors []Color, positions []float32) Shader {
	return Shader{shaders.NewRadialGradient(
		toGeomPoint(center), radius, colors, positions,
		shaders.TileClamp, nil,
	)}
}

// MaskFilter / ImageFilter：backend/skia 未使用，保留真实接口类型以兼容旧 API。
type MaskFilter struct{ p maskfilter.MaskFilter }
func (f MaskFilter) IsNil() bool { return f.p == nil }

type ImageFilter struct{ p filtercore.Filter }
func (f ImageFilter) IsNil() bool { return f.p == nil }

// ---------- Path ----------

type Path struct{ p *cp.Path }

func NewPath() Path { return Path{p: cp.New()} }
func (p Path) Delete() { p.p = nil }
func (p Path) IsEmpty() bool { return p.p == nil || p.p.IsEmpty() }
func (p Path) MoveToPoint(x, y float32) { p.p.MoveTo(x, y) }
func (p Path) LineTo(x, y float32)      { p.p.LineTo(x, y) }
func (p Path) QuadTo(x1, y1, x2, y2 float32) { p.p.QuadTo(x1, y1, x2, y2) }
func (p Path) CubicTo(x1, y1, x2, y2, x3, y3 float32) {
	p.p.CubicTo(x1, y1, x2, y2, x3, y3)
}
func (p Path) Close() { p.p.Close() }
func (p Path) AddRoundRect1(rect Rect, rx, ry float32, dir PathDirection) {
	d := geom.DirectionCW
	if dir == PathDirectionCCW {
		d = geom.DirectionCCW
	}
	p.p.AddRoundRect(toGeomRect(rect), rx, ry, d)
}
