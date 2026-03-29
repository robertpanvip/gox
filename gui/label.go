package gui

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text"
	"golang.org/x/image/font/basicfont"
)

// LabelProps Label 组件的属性（继承 DivProps 的所有属性）
type LabelProps struct {
	// 继承 Div 的所有属性
	DivProps
	
	// Label 特有属性
	Text      string
	FontSize  float64
	TextColor Color
}

// Label 标签组件（继承自 Div）
type Label struct {
	*Div
	Text      string
	FontSize  float64
	TextColor Color
}

// NewLabel 创建 Label 组件
func NewLabel(props LabelProps) *Label {
	// 创建 Div 作为基类
	div := NewDiv(props.DivProps)
	
	label := &Label{
		Div:       div,
		Text:      props.Text,
		FontSize:  props.FontSize,
		TextColor: props.TextColor,
	}
	
	return label
}

// SetText 设置标签文字
func (l *Label) SetText(text string) {
	l.Text = text
}

// Render 渲染标签
func (l *Label) Render(screen *ebiten.Image) {
	if !l.IsVisible() {
		return
	}

	// 使用 ebitengine 的 text 包绘制文字
	text.Draw(screen, l.Text, basicfont.Face7x13, l.Rect.X, l.Rect.Y+int(l.FontSize), l.TextColor.ToGoColor())
	
	// 调用基类的 Render 来渲染子组件
	l.Div.Render(screen)
}
