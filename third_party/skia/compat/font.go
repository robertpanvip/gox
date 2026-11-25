package compat

import (
	cfont "github.com/richardwilkes/canvas/font"
	"github.com/richardwilkes/canvas/fontmgr"
)

// Typeface 包装字体面指针，值类型可比较（getFont 用 IsNil 判空）。
type Typeface struct{ p *cfont.Typeface }

func (t Typeface) IsNil() bool { return t.p == nil }

// FontStyle 值类型：保存 normal/bold 标记，构造字体时转成 canvas 的 Style。
type FontStyle struct{ bold bool }

func FontStyleNormal() FontStyle { return FontStyle{bold: false} }
func FontStyleBold() FontStyle   { return FontStyle{bold: true} }

// FontMgr 包装字体管理器。
type FontMgr struct{ p *fontmgr.Manager }

func FontMgrDefault() FontMgr { return FontMgr{fontmgr.Default()} }
func (m FontMgr) MatchFamilyStyle(family string, style FontStyle) Typeface {
	fs := cfont.NormalStyle()
	if style.bold {
		fs = cfont.BoldStyle()
	}
	// canvas 的字体管理器不认识 CSS/Skia 通用族名，但空串会返回系统默认字体；
	// 把常见通用名映射为默认，保持与旧 go-skia 一致的语义。
	switch family {
	case "sans-serif", "serif", "monospace", "system-ui":
		family = ""
	}
	return Typeface{p: m.p.MatchFamilyStyle(family, fs)}
}
func (m FontMgr) IsNil() bool { return m.p == nil }

// Font 是值类型、可比较（业务代码把它作为 sync.Map 值并与零值比较）。
type Font struct{ p *cfont.Font }

func NewFontTypefaceSize(tf Typeface, size float32) Font {
	if tf.p == nil {
		return Font{}
	}
	return Font{p: cfont.NewFont(tf.p, size, 1, 0)}
}
func (f Font) IsNil() bool { return f.p == nil }
func (f Font) Delete()     { f.p = nil }
func (f Font) UnicharToGlyph(uni int32) uint16 {
	if f.p == nil {
		return 0
	}
	return f.p.UnicharToGlyph(uni)
}

// MeasureText 返回文本宽度。canvas 的 MeasureText 需要 paint spec，传 nil 用默认。
func (f Font) MeasureText(text []byte, byteLength uint32, _ TextEncoding, _ *Rect) float32 {
	if f.p == nil {
		return 0
	}
	return f.p.MeasureText(text[:byteLength], cfont.TextEncodingUTF8, nil, nil)
}
