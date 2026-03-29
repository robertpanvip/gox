package gui

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font/basicfont"
)

// ButtonProps Button 组件的属性
type ButtonProps struct {
	Text      string
	FontSize  float64
	TextColor Color
	BackColor Color
}

// Button 按钮组件
type Button struct {
	BaseComponent
	Props       ButtonProps
	Hovered     bool
	OnClickFunc func()
}

// NewButton 创建 Button 组件
func NewButton(props ButtonProps) *Button {
	return &Button{
		Props: props,
		BaseComponent: BaseComponent{
			Visible: true,
		},
		Hovered: false,
	}
}

// SetOnClick 设置点击事件处理函数
func (b *Button) SetOnClick(handler func()) {
	b.OnClickFunc = handler
}

// Render 渲染按钮
func (b *Button) Render(screen *ebiten.Image) {
	if !b.IsVisible() {
		return
	}

	// 绘制按钮背景
	bgColor := b.Props.BackColor
	if b.Hovered {
		// 悬停时变亮
		bgColor = ColorLightGray
	}
	vector.DrawFilledRect(screen, float32(b.Rect.X), float32(b.Rect.Y), float32(b.Rect.Width), float32(b.Rect.Height), bgColor.ToGoColor(), true)

	// 绘制按钮文字
	textX := b.Rect.X + 5
	textY := b.Rect.Y + int(b.Props.FontSize) + 5
	text.Draw(screen, b.Props.Text, basicfont.Face7x13, textX, textY, b.Props.TextColor.ToGoColor())
}

// OnClick 处理点击事件
func (b *Button) OnClick(x, y int) {
	if b.OnClickFunc != nil {
		b.OnClickFunc()
	}
}

// OnMouseMove 处理鼠标移动事件
func (b *Button) OnMouseMove(x, y int) {
	// 检查鼠标是否在按钮上
	if x >= b.Rect.X && x <= b.Rect.X+b.Rect.Width &&
		y >= b.Rect.Y && y <= b.Rect.Y+b.Rect.Height {
		b.Hovered = true
	} else {
		b.Hovered = false
	}
}
