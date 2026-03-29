package gui

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text"
	"golang.org/x/image/font/basicfont"
)

// LabelProps Label 组件的属性
type LabelProps struct {
	Text      string
	FontSize  float64
	TextColor Color
}

// Label 标签组件
type Label struct {
	BaseComponent
	Props LabelProps
}

// NewLabel 创建 Label 组件
func NewLabel(props LabelProps) *Label {
	return &Label{
		Props: props,
		BaseComponent: BaseComponent{
			Visible: true,
		},
	}
}

// SetText 设置标签文字
func (l *Label) SetText(text string) {
	l.Props.Text = text
}

// Render 渲染标签
func (l *Label) Render(screen *ebiten.Image) {
	if !l.IsVisible() {
		return
	}

	// 使用 ebitengine 的 text 包绘制文字
	text.Draw(screen, l.Props.Text, basicfont.Face7x13, l.Rect.X, l.Rect.Y+int(l.Props.FontSize), l.Props.TextColor.ToGoColor())
}
