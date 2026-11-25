package compat

import usk "github.com/robertpanvip/gox-skia"

// Image 是值类型、可比较（imageCache 用 sync.Map 存值）。
type Image struct{ p usk.Image }

func (i Image) IsNil() bool    { return i.p == nil }
func (i Image) Width() int     { return usk.ImageGetWidth(i.p) }
func (i Image) Height() int    { return usk.ImageGetHeight(i.p) }
func (i Image) Delete() {
	if i.p != nil {
		usk.ImageUnref(i.p)
	}
}

// Data 包装上游 Data。
type Data = usk.Data

func DataMakeWithCopy(data []byte, _ uint32) Data { return usk.DataNewWithCopy(data) }

// ImageInfo 是值类型，提供与 pekim go-skia 一致的链式构造。
type ImageInfo struct{ v usk.ImageInfo }

func NewImageInfo() ImageInfo { return ImageInfo{usk.ImageInfo{}} }
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
	v := info.v
	return Image{usk.ImageNewRasterData(&v, pixels, int(rowBytes))}
}

// SamplingOptions 值类型；上游无构造，用补充的 SamplingOptionsNew。
type SamplingOptions = usk.SamplingOptions

func NewSamplingOptions() SamplingOptions { return usk.SamplingOptionsNew() }

// SurfaceProps 值类型。
type SurfaceProps struct{ p usk.SurfaceProps }

func NewSurfaceProps() SurfaceProps {
	return SurfaceProps{usk.SurfacePropsNew(usk.PixelGeometryUnknown)}
}
func NewSurfacePropsPixelGeometry(flags uint32, pg PixelGeometry) SurfaceProps {
	return SurfaceProps{usk.SurfacePropsNew(pg)}
}
