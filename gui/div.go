package gui

import (
	"github.com/hajimehoshi/ebiten/v2"
)

// DivProps Div 组件的属性
type DivProps struct {
	Style    *Style      // CSS 样式对象
	Children []Component // 子元素
	Text     string      // 支持直接传入文字
}

// Div Div 组件（基于 View 实现）
type Div struct {
	*View // 组合 View
}

// NewDiv 创建 Div 组件
func NewDiv(props DivProps) *Div {
	// 转换为 ViewProps
	viewProps := ViewProps{
		Children: props.Children,
		Text:     props.Text,
	}
	
	// 如果提供了 Style，应用样式
	if props.Style != nil {
		viewProps.Style = props.Style
		viewProps.Flex = props.Style.Display
		viewProps.FlexDir = props.Style.FlexDir
		viewProps.Justify = props.Style.Justify
		viewProps.Align = props.Style.Align
		viewProps.Wrap = props.Style.FlexWrap
		viewProps.ColumnGap = props.Style.ColumnGap
		viewProps.RowGap = props.Style.RowGap
		viewProps.Width = props.Style.Width
		viewProps.Height = props.Style.Height
		viewProps.Padding = props.Style.Padding
	}
	
	// 创建 View
	view := NewView(viewProps)
	
	// 设置背景色
	if props.Style != nil && props.Style.BackColor != nil {
		view.BackColor = props.Style.BackColor
	}
	
	return &Div{
		View: view,
	}
}

// Render 渲染 Div（委托给 View）
func (d *Div) Render(screen *ebiten.Image) {
	if d.View != nil {
		d.View.Render(screen)
	}
}

// AppendChild 添加子节点
func (d *Div) AppendChild(child Component) {
	if d.View != nil && d.View.Children != nil {
		d.View.Children = append(d.View.Children, child)
	}
}

// InsertBefore 在指定子节点之前插入
func (d *Div) InsertBefore(newChild, refChild Component) {
	if d.View != nil && d.View.Children != nil {
		// 找到 refChild 的位置
		for i, child := range d.View.Children {
			if child == refChild {
				// 在位置 i 之前插入
				d.View.Children = append(d.View.Children[:i], append([]Component{newChild}, d.View.Children[i:]...)...)
				return
			}
		}
		// 如果没找到，追加到末尾
		d.AppendChild(newChild)
	}
}

// RemoveChild 移除子节点
func (d *Div) RemoveChild(child Component) {
	if d.View != nil && d.View.Children != nil {
		for i, c := range d.View.Children {
			if c == child {
				d.View.Children = append(d.View.Children[:i], d.View.Children[i+1:]...)
				return
			}
		}
	}
}

// ReplaceChild 替换子节点
func (d *Div) ReplaceChild(newChild, oldChild Component) {
	if d.View != nil && d.View.Children != nil {
		for i, c := range d.View.Children {
			if c == oldChild {
				d.View.Children[i] = newChild
				return
			}
		}
	}
}

// SetAttribute 设置属性（支持 style 对象）
func (d *Div) SetAttribute(name, value string) {
	if d.View == nil {
		return
	}
	
	// 特殊处理 style 属性
	if name == "style" {
		// value 可能是 JSON 字符串或者 CSS 字符串
		// 这里简化处理，实际应用需要解析
		if style := ParseStyle(value); style != nil {
			d.View.Style = style
			// 应用样式到 View
			d.applyStyle(style)
		}
		return
	}
	
	// 直接设置 View 的属性（兼容旧代码）
	switch name {
	case "text":
		d.View.Text = value
	case "width":
		d.View.Width = value
	case "height":
		d.View.Height = value
	}
}

// GetAttribute 获取属性
func (d *Div) GetAttribute(name string) string {
	if d.View == nil {
		return ""
	}
	
	switch name {
	case "text":
		return d.View.Text
	case "width":
		return d.View.Width
	case "height":
		return d.View.Height
	case "style":
		// 返回样式对象的字符串表示
		if d.View.Style != nil {
			// TODO: 实现 Style 的字符串化
			return "style object"
		}
		return ""
	default:
		return ""
	}
}

// applyStyle 应用样式到 View
func (d *Div) applyStyle(style *Style) {
	if d.View == nil {
		return
	}
	
	d.View.Flex = style.Display
	d.View.FlexDir = style.FlexDir
	d.View.Justify = style.Justify
	d.View.Align = style.Align
	d.View.Wrap = style.FlexWrap
	d.View.ColumnGap = style.ColumnGap
	d.View.RowGap = style.RowGap
	d.View.Width = style.Width
	d.View.Height = style.Height
	d.View.Padding = style.Padding
	
	if style.BackColor != nil {
		d.View.BackColor = style.BackColor
	}
}
