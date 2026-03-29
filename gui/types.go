package gui

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
)

// Color 颜色类型
type Color struct {
	R, G, B, A uint8
}

func NewColor(r, g, b, a uint8) Color {
	return Color{R: r, G: g, B: b, A: a}
}

func (c Color) ToGoColor() color.Color {
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}
}

// 预定义颜色
var (
	ColorWhite       = NewColor(255, 255, 255, 255)
	ColorBlack       = NewColor(0, 0, 0, 255)
	ColorRed         = NewColor(255, 0, 0, 255)
	ColorGreen       = NewColor(0, 255, 0, 255)
	ColorBlue        = NewColor(0, 0, 255, 255)
	ColorGray        = NewColor(128, 128, 128, 255)
	ColorLightGray   = NewColor(220, 220, 220, 255)
	ColorDarkGray    = NewColor(64, 64, 64, 255)
	ColorTransparent = NewColor(0, 0, 0, 0)
)

// Rect 矩形区域
type Rect struct {
	X, Y, Width, Height int
}

// Component 组件接口
type Component interface {
	Render(screen *ebiten.Image)
	GetRect() Rect
	SetRect(Rect)
	OnClick(x, y int)
	OnMouseMove(x, y int)
	IsVisible() bool
	SetVisible(bool)
}

// BaseComponent 基础组件
type BaseComponent struct {
	Rect     Rect
	Visible  bool
	Children []Component
	Parent   Component
}

func (b *BaseComponent) GetRect() Rect {
	return b.Rect
}

func (b *BaseComponent) SetRect(rect Rect) {
	b.Rect = rect
}

func (b *BaseComponent) OnClick(x, y int) {
	// 默认实现，子类可以重写
}

func (b *BaseComponent) OnMouseMove(x, y int) {
	// 默认实现，子类可以重写
}

func (b *BaseComponent) IsVisible() bool {
	return b.Visible
}

func (b *BaseComponent) SetVisible(visible bool) {
	b.Visible = visible
}

func (b *BaseComponent) AddChild(child Component) {
	b.Children = append(b.Children, child)
}

func (b *BaseComponent) RemoveChild(child Component) {
	for i, c := range b.Children {
		if c == child {
			b.Children = append(b.Children[:i], b.Children[i+1:]...)
			break
		}
	}
}
