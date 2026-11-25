package compat

import usk "github.com/robertpanvip/gox-skia"

// Typeface 包装字体面指针，值类型可比较（getFont 用 IsNil 判空）。
type Typeface struct{ p usk.TypeFace }

func (t Typeface) IsNil() bool { return t.p == nil }

// FontStyle 包装上游 FontStyle 指针。
type FontStyle struct{ p usk.FontStyle }

func FontStyleNormal() FontStyle {
	return FontStyle{usk.FontStyleNew(usk.FontWeight(FontWeightNormal), usk.FontSpacing(FontSpacingNormal), usk.FontSlant(FontSlantUpright))}
}
func FontStyleBold() FontStyle {
	return FontStyle{usk.FontStyleNew(usk.FontWeight(FontWeightBold), usk.FontSpacing(FontSpacingNormal), usk.FontSlant(FontSlantUpright))}
}

// FontMgr 包装字体管理器。
type FontMgr struct{ p usk.FontMgr }

func FontMgrDefault() FontMgr { return FontMgr{usk.FontMgrRefDefault()} }
func (m FontMgr) MatchFamilyStyle(family string, style FontStyle) Typeface {
	return Typeface{usk.FontMgrMatchFamilyStyle(m.p, family, style.p)}
}
func (m FontMgr) IsNil() bool { return m.p == nil }

// Font 是值类型、可比较（业务代码把它作为 sync.Map 值并与零值比较）。
type Font struct{ p usk.Font }

func NewFontTypefaceSize(tf Typeface, size float32) Font {
	return Font{usk.FontNewWithValues(tf.p, size, 1, 0)}
}
func (f Font) IsNil() bool { return f.p == nil }
func (f Font) Delete() {
	if f.p != nil {
		usk.FontDelete(f.p)
	}
}
func (f Font) UnicharToGlyph(uni int32) uint16 { return usk.FontRuneToGlyph(f.p, rune(uni)) }

// MeasureText 返回文本宽度。上游只有 string 版本，忽略 bounds（业务传 nil）。
func (f Font) MeasureText(text []byte, byteLength uint32, _ TextEncoding, _ *Rect) float32 {
	return usk.FontMeasureText(f.p, string(text[:byteLength]))
}
