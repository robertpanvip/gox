package gui

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// ViewProps View 组件的属性
type ViewProps struct {
	Width     string
	Height    string
	X         string
	Y         string
	Flex      string
	FlexDir   string
	Justify   string
	Align     string
	Wrap      string
	ColumnGap string
	RowGap    string
	Padding   string
	Children  []Component // 支持子元素
	Text      string      // 支持直接传入文字作为子元素
	Style     *Style      // CSS 样式对象
}

// View View 组件（函数式组件）
type View struct {
	BaseComponent
	Props    ViewProps
	Children []Component
	Text     string
}

// NewView 创建 View 组件
func NewView(props ViewProps) *View {
	view := &View{
		Props:    props,
		Children: props.Children,
		Text:     props.Text,
	}
	
	// 如果有 Text，创建一个 Text 组件作为子元素
	if props.Text != "" {
		textComp := NewText(props.Text)
		view.Children = append(view.Children, textComp)
	}
	
	return view
}

// AddChild 添加子元素
func (v *View) AddChild(child Component) {
	v.Children = append(v.Children, child)
}

// Render 渲染 View
func (v *View) Render(screen *ebiten.Image) {
	if !v.IsVisible() {
		return
	}
	
	// 渲染背景（如果有）
	if v.BackColor != nil {
		vector.DrawFilledRect(
			screen,
			float32(v.Rect.X),
			float32(v.Rect.Y),
			float32(v.Rect.Width),
			float32(v.Rect.Height),
			v.BackColor.ToGoColor(),
			true,
		)
	}
	
	// 渲染所有子元素
	for _, child := range v.Children {
		if child != nil && child.IsVisible() {
			child.Render(screen)
		}
	}
}

// Text 简单的文本组件
type Text struct {
	BaseComponent
	Text     string
	FontSize int
	Color    Color
}

// NewText 创建 Text 组件
func NewText(text string) *Text {
	return &Text{
		Text:     text,
		FontSize: 16,
		Color:    ColorBlack,
	}
}

// Render 渲染 Text
func (t *Text) Render(screen *ebiten.Image) {
	if !t.IsVisible() || t.Text == "" {
		return
	}
	
	// 使用 vector 绘制简单的矩形代表文本（实际应该用 text 包）
	// 这里简化实现
	vector.DrawFilledRect(
		screen,
		float32(t.Rect.X),
		float32(t.Rect.Y),
		float32(t.Rect.Width),
		float32(t.Rect.Height),
		t.Color.ToGoColor(),
		true,
	)
}
