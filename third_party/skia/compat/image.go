package compat

import (
	"github.com/richardwilkes/canvas/imagecore"
	"github.com/richardwilkes/canvas/shaders"
)

// Image 是值类型、可比较（imageCache 用 sync.Map 存值）。
type Image struct{ p *imagecore.Image }

func (i Image) IsNil() bool  { return i.p == nil }
func (i Image) Width() int   { if i.p == nil { return 0 }; return int(i.p.Width()) }
func (i Image) Height() int  { if i.p == nil { return 0 }; return int(i.p.Height()) }
func (i Image) Delete()      { i.p = nil }

// Data 在纯 Go canvas 中不需要（像素直接在 []byte），保留不透明占位。
type Data struct{ p []byte }

func DataMakeWithCopy(data []byte, _ uint32) Data { return Data{p: append([]byte(nil), data...)} }

// ImageInfo 是值类型，提供与 pekim go-skia 一致的链式构造。
type ImageInfo struct{ v imagecore.ImageInfo }

func NewImageInfo() ImageInfo { return ImageInfo{} }
func (i ImageInfo) MakeColorType(ct ColorType) ImageInfo {
	i.v.ColorType = ct
	return i
}
func (i ImageInfo) MakeAlphaType(at AlphaType) ImageInfo {
	i.v.AlphaType = at
	return i
}
func (i ImageInfo) MakeWidth(w int32) ImageInfo  { i.v.Width = w; return i }
func (i ImageInfo) MakeHeight(h int32) ImageInfo { i.v.Height = h; return i }
func (i ImageInfo) MakeWH(w, h int32) ImageInfo  { i.v.Width, i.v.Height = w, h; return i }
func (i ImageInfo) Width() int32                { return i.v.Width }
func (i ImageInfo) Height() int32               { return i.v.Height }
func (i ImageInfo) ColorType() ColorType        { return i.v.ColorType }
func (i ImageInfo) AlphaType() AlphaType        { return i.v.AlphaType }

func ImagesRasterFromData(info ImageInfo, pixels Data, rowBytes uint32) Image {
	img := imagecore.NewRasterData(info.v, pixels.p, int(rowBytes))
	return Image{p: img}
}

// SamplingOptions 值类型；canvas 用零值采样即可。
type SamplingOptions struct{}

func NewSamplingOptions() SamplingOptions { return SamplingOptions{} }

// SurfaceProps 值类型占位。
type SurfaceProps struct{}

func NewSurfaceProps() SurfaceProps { return SurfaceProps{} }
func NewSurfacePropsPixelGeometry(flags uint32, pg PixelGeometry) SurfaceProps {
	return SurfaceProps{}
}

// 供 canvas.go 使用：shaders.SamplingOptions 零值转换。
func toSamplingOptions(_ SamplingOptions) shaders.SamplingOptions { return shaders.SamplingOptions{} }
