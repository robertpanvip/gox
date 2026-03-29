package gui

import (
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// DivProps Div 组件的属性（CSS 风格）
type DivProps struct {
	// 布局属性
	Display        string
	FlexDirection  string
	JustifyContent string
	AlignItems     string
	AlignSelf      string
	FlexWrap       string
	FlexGrow       float32
	FlexShrink     float32
	FlexBasis      string
	
	// 尺寸
	Width      string
	Height     string
	MinWidth   string
	MinHeight  string
	MaxWidth   string
	MaxHeight  string
	
	// 间距
	Padding      string
	PaddingTop   string
	PaddingRight string
	PaddingBottom string
	PaddingLeft  string
	
	Margin      string
	MarginTop   string
	MarginRight string
	MarginBottom string
	MarginLeft  string
	
	// 定位
	Position string
	Left     string
	Right    string
	Top      string
	Bottom   string
	
	// 边框
	BorderWidth  string
	BorderColor  string
	BorderStyle  string
	BorderRadius string
	
	// 背景
	BackgroundColor string
	
	// 子组件
	Children []Component
}

// Div 通用容器组件（类似 HTML 的 div）
type Div struct {
	BaseComponent
	Style      *Style
	Children   []Component
	Layout     *LayoutEngine
	Background *ebiten.Image
}

// NewDiv 创建 Div 组件（支持两种方式：DivProps 或 *Style，第三个参数是 children）
func NewDiv(props interface{}, children ...Component) *Div {
	var style *Style
	
	switch p := props.(type) {
	case DivProps:
		style = &Style{
			Display:         "flex", // 默认启用 flex 布局
			FlexDirection:   p.FlexDirection,
			JustifyContent:  p.JustifyContent,
			AlignItems:      p.AlignItems,
			AlignSelf:       p.AlignSelf,
			FlexWrap:        p.FlexWrap,
			FlexGrow:        p.FlexGrow,
			FlexShrink:      p.FlexShrink,
			FlexBasis:       p.FlexBasis,
			Width:           p.Width,
			Height:          p.Height,
			MinWidth:        p.MinWidth,
			MinHeight:       p.MinHeight,
			MaxWidth:        p.MaxWidth,
			MaxHeight:       p.MaxHeight,
			Padding:         p.Padding,
			PaddingTop:      p.PaddingTop,
			PaddingRight:    p.PaddingRight,
			PaddingBottom:   p.PaddingBottom,
			PaddingLeft:     p.PaddingLeft,
			Margin:          p.Margin,
			MarginTop:       p.MarginTop,
			MarginRight:     p.MarginRight,
			MarginBottom:    p.MarginBottom,
			MarginLeft:      p.MarginLeft,
			Position:        p.Position,
			Left:            p.Left,
			Right:           p.Right,
			Top:             p.Top,
			Bottom:          p.Bottom,
			BorderWidth:     p.BorderWidth,
			BorderColor:     p.BorderColor,
			BorderStyle:     p.BorderStyle,
			BorderRadius:    p.BorderRadius,
			BackgroundColor: p.BackgroundColor,
		}
	case *Style:
		style = p
		// 如果没有设置 display，默认为 "flex"
		if style.Display == "" {
			style.Display = "flex"
		}
	default:
		style = &Style{
			Display: "flex", // 默认启用 flex 布局
		}
	}
	
	div := &Div{
		Style:    style,
		Children: make([]Component, 0),
		Layout:   NewLayoutEngine(),
	}
	
	// 设置默认可见性
	div.SetVisible(true)
	
	// 添加 children
	for _, child := range children {
		div.AddChild(child)
	}
	
	// 应用样式到布局引擎
	div.applyStyleToLayout()
	
	return div
}

// applyStyleToLayout 将样式应用到布局引擎
func (d *Div) applyStyleToLayout() {
	if d.Layout == nil {
		return
	}
	
	// 设置弹性方向
	if d.Style.FlexDirection == "column" || d.Style.FlexDirection == "column-reverse" {
		d.Layout.SetFlexDirection(FlexColumn)
	} else {
		d.Layout.SetFlexDirection(FlexRow)
	}
	
	// 设置主轴对齐
	switch d.Style.JustifyContent {
	case "center":
		d.Layout.SetJustifyContent(JustifyCenter)
	case "flex-end":
		d.Layout.SetJustifyContent(JustifyFlexEnd)
	case "space-between":
		d.Layout.SetJustifyContent(JustifySpaceBetween)
	case "space-around":
		d.Layout.SetJustifyContent(JustifySpaceAround)
	default:
		d.Layout.SetJustifyContent(JustifyFlexStart)
	}
	
	// 设置交叉轴对齐
	switch d.Style.AlignItems {
	case "center":
		d.Layout.SetAlignItems(AlignCenter)
	case "flex-end":
		d.Layout.SetAlignItems(AlignFlexEnd)
	case "flex-start":
		d.Layout.SetAlignItems(AlignFlexStart)
	default:
		d.Layout.SetAlignItems(AlignStretch)
	}
	
	// 解析并设置 padding
	if d.Style.Padding != "" {
		top, right, bottom, left := parsePadding(d.Style.Padding)
		d.Layout.SetPadding(top, right, bottom, left)
	}
}

// SetStyle 设置样式
func (d *Div) SetStyle(style *Style) *Div {
	d.Style = style
	d.applyStyleToLayout()
	return d
}

// AddChild 添加子组件
func (d *Div) AddChild(child Component) *Div {
	d.Children = append(d.Children, child)
	
	// 为子组件创建布局节点
	if baseChild, ok := child.(*Div); ok {
		if baseChild.Layout == nil {
			baseChild.Layout = NewLayoutEngine()
		}
		d.Layout.AddChild(baseChild.Layout)
	} else if baseChild, ok := child.(*Label); ok {
		if baseChild.Layout == nil {
			baseChild.Layout = NewLayoutEngine()
		}
		d.Layout.AddChild(baseChild.Layout)
	} else if baseChild, ok := child.(*Button); ok {
		if baseChild.Layout == nil {
			baseChild.Layout = NewLayoutEngine()
		}
		d.Layout.AddChild(baseChild.Layout)
	}
	
	return d
}

// RemoveChild 移除子组件
func (d *Div) RemoveChild(child Component) *Div {
	for i, c := range d.Children {
		if c == child {
			d.Children = append(d.Children[:i], d.Children[i+1:]...)
			break
		}
	}
	return d
}

// DoLayout 执行布局计算
func (d *Div) DoLayout() {
	if d.Layout == nil {
		return
	}
	
	// 计算布局
	d.Layout.CalculateLayout(d.Rect.X, d.Rect.Y, d.Rect.Width, d.Rect.Height)
	
	// 应用布局结果到子组件
	for i, child := range d.Children {
		if i < len(d.Layout.Children) {
			childLayout := d.Layout.Children[i]
			childRect := childLayout.GetComputedRect()
			child.SetRect(childRect)
		}
	}
}

// Render 渲染 Div
func (d *Div) Render(screen *ebiten.Image) {
	if !d.IsVisible() {
		return
	}
	
	// 绘制背景
	if d.Style.BackgroundColor != "" {
		bgColor := parseColor(d.Style.BackgroundColor)
		if bgColor != ColorTransparent {
			vector.DrawFilledRect(
				screen,
				float32(d.Rect.X),
				float32(d.Rect.Y),
				float32(d.Rect.Width),
				float32(d.Rect.Height),
				bgColor.ToGoColor(),
				true,
			)
		}
	}
	
	// 绘制边框
	borderWidth := parseSize(d.Style.BorderWidth)
	if borderWidth > 0 {
		borderColor := parseColor(d.Style.BorderColor)
		if borderColor == ColorTransparent {
			borderColor = ColorBlack
		}
		vector.StrokeRect(
			screen,
			float32(d.Rect.X),
			float32(d.Rect.Y),
			float32(d.Rect.Width),
			float32(d.Rect.Height),
			float32(borderWidth),
			borderColor.ToGoColor(),
			true,
		)
	}
	
	// 执行布局计算
	d.DoLayout()
	
	// 渲染子组件
	for _, child := range d.Children {
		if child.IsVisible() {
			child.Render(screen)
		}
	}
}

// OnClick 处理点击事件
func (d *Div) OnClick(x, y int) {
	for _, child := range d.Children {
		if child.IsVisible() {
			child.OnClick(x, y)
		}
	}
}

// OnMouseMove 处理鼠标移动事件
func (d *Div) OnMouseMove(x, y int) {
	for _, child := range d.Children {
		if child.IsVisible() {
			child.OnMouseMove(x, y)
		}
	}
}

// GetStyle 获取样式
func (d *Div) GetStyle() *Style {
	return d.Style
}
