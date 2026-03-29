package gui

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font/basicfont"
)

// ButtonProps Button 组件的属性（继承 DivProps 的所有属性）
type ButtonProps struct {
	// 继承 Div 的所有属性
	DivProps
	
	// Button 特有属性
	Text      string
	FontSize  float64
	TextColor Color
	BackColor Color
	
	// 事件处理器（统一使用 on 前缀）
	OnClick    func()
	OnMouseEnter func()
	OnMouseLeave func()
}

// Button 按钮组件（继承自 Div）
type Button struct {
	*Div
	Text        string
	FontSize    float64
	TextColor   Color
	BackColor   Color
	Hovered     bool
	HoveredLast bool
	ClickFunc   func()
	MouseEnterFunc func()
	MouseLeaveFunc func()
}

// NewButton 创建 Button 组件
func NewButton(props ButtonProps) *Button {
	// 设置默认样式
	if props.Width == "" {
		props.Width = "100px" // 默认宽度
	}
	if props.Height == "" {
		props.Height = "40px" // 默认高度
	}
	if props.FontSize == 0 {
		props.FontSize = 16 // 默认字体大小
	}
	
	// 创建 Div 作为基类
	div := NewDiv(props.DivProps)
	
	button := &Button{
		Div:         div,
		Text:        props.Text,
		FontSize:    props.FontSize,
		TextColor:   props.TextColor,
		BackColor:   props.BackColor,
		Hovered:     false,
		HoveredLast: false,
	}
	
	// 设置事件处理器
	if props.OnClick != nil {
		button.ClickFunc = props.OnClick
	}
	if props.OnMouseEnter != nil {
		button.MouseEnterFunc = props.OnMouseEnter
	}
	if props.OnMouseLeave != nil {
		button.MouseLeaveFunc = props.OnMouseLeave
	}
	
	return button
}

// SetOnClick 设置点击事件处理函数
func (b *Button) SetOnClick(handler func()) {
	b.ClickFunc = handler
}

// SetOnMouseEnter 设置鼠标进入事件处理函数
func (b *Button) SetOnMouseEnter(handler func()) {
	b.MouseEnterFunc = handler
}

// SetOnMouseLeave 设置鼠标离开事件处理函数
func (b *Button) SetOnMouseLeave(handler func()) {
	b.MouseLeaveFunc = handler
}

// Render 渲染按钮
func (b *Button) Render(screen *ebiten.Image) {
	if !b.IsVisible() {
		return
	}

	// 绘制按钮背景
	bgColor := b.BackColor
	if b.Hovered {
		// 悬停时变亮
		bgColor = ColorLightGray
	}
	vector.DrawFilledRect(screen, float32(b.Rect.X), float32(b.Rect.Y), float32(b.Rect.Width), float32(b.Rect.Height), bgColor.ToGoColor(), true)

	// 绘制按钮文字
	textX := b.Rect.X + 5
	textY := b.Rect.Y + int(b.FontSize) + 5
	text.Draw(screen, b.Text, basicfont.Face7x13, textX, textY, b.TextColor.ToGoColor())
	
	// 调用基类的 Render 来渲染子组件
	b.Div.Render(screen)
}

// OnClick 处理点击事件
func (b *Button) OnClick(x, y int) {
	if b.ClickFunc != nil {
		b.ClickFunc()
	}
}

// OnMouseMove 处理鼠标移动事件
func (b *Button) OnMouseMove(x, y int) {
	// 检查鼠标是否在按钮上
	if x >= b.Rect.X && x <= b.Rect.X+b.Rect.Width &&
		y >= b.Rect.Y && y <= b.Rect.Y+b.Rect.Height {
		if !b.HoveredLast {
			// 刚刚进入
			b.Hovered = true
			if b.MouseEnterFunc != nil {
				b.MouseEnterFunc()
			}
		}
	} else {
		if b.HoveredLast {
			// 刚刚离开
			b.Hovered = false
			if b.MouseLeaveFunc != nil {
				b.MouseLeaveFunc()
			}
		}
	}
	b.HoveredLast = b.Hovered
	
	// 调用基类的 OnMouseMove
	b.Div.OnMouseMove(x, y)
}
