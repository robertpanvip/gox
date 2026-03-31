package gui

import (
	"fmt"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font/basicfont"
)

// ButtonProps Button 组件的属性
type ButtonProps struct {
	Text      string
	Width     string
	Height    string
	FontSize  int
	TextColor Color
	BackColor Color
	OnClick   func()
}

// Button Button 组件
type Button struct {
	BaseComponent
	Props       ButtonProps
	Text        string
	FontSize    int
	TextColor   Color
	BackColor   Color
	Hovered     bool
	HoveredLast bool
	ClickFunc   func()
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
	
	// 设置默认颜色（如果 R,G,B 都是 0，则使用默认色）
	if props.TextColor.R == 0 && props.TextColor.G == 0 && props.TextColor.B == 0 {
		props.TextColor = ColorWhite // 默认白色文字
	}
	if props.BackColor.R == 0 && props.BackColor.G == 0 && props.BackColor.B == 0 {
		props.BackColor = ColorBlue // 默认蓝色背景
	}
	
	button := &Button{
		Props:       props,
		Text:        props.Text,
		FontSize:    props.FontSize,
		TextColor:   props.TextColor,
		BackColor:   props.BackColor,
		Hovered:     false,
		HoveredLast: false,
		ClickFunc:   props.OnClick,
	}
	
	return button
}

// SetOnClick 设置点击事件处理器
func (b *Button) SetOnClick(handler func()) {
	b.ClickFunc = handler
}

// Update 更新按钮状态
func (b *Button) Update() {
	// 获取鼠标位置
	mx, my := b.GetApp().CursorPos()
	
	// 检查鼠标是否在按钮上
	b.Hovered = mx >= b.Rect.X && mx <= b.Rect.X+b.Rect.Width &&
		my >= b.Rect.Y && my <= b.Rect.Y+b.Rect.Height
	
	// 检测点击
	if b.Hovered && !b.HoveredLast && b.GetApp().IsMouseButtonPressed(MouseButtonLeft) {
		if b.ClickFunc != nil {
			b.ClickFunc()
		}
	}
	
	b.HoveredLast = b.Hovered
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
		bgColor = Color{R: 200, G: 200, B: 200, A: 255}
	}
	vector.DrawFilledRect(screen, float32(b.Rect.X), float32(b.Rect.Y), float32(b.Rect.Width), float32(b.Rect.Height), bgColor.ToGoColor(), true)

	// 绘制按钮文字
	textX := b.Rect.X + 5
	textY := b.Rect.Y + int(b.FontSize) + 5
	text.Draw(screen, b.Text, basicfont.Face7x13, textX, textY, b.TextColor.ToGoColor())
}

// ButtonHO 高阶 Button 组件（返回 func() TemplateResult）
func ButtonHO(props ButtonProps) func() TemplateResult {
	return func() TemplateResult {
		return TemplateResult{
			StaticCode: fmt.Sprintf(`<button text="%s">`, props.Text),
			Static: []func() Component{
				func() Component {
					return NewButton(props)
				},
			},
		}
	}
}
