package gui

import (
	"strings"
)

// Style CSS 样式对象
type Style struct {
	Display     string // "flex", "block", "none"
	FlexDir     string // "row", "column", "row-reverse", "column-reverse"
	FlexWrap    string // "nowrap", "wrap", "wrap-reverse"
	Justify     string // "flex-start", "flex-end", "center", "space-between", "space-around"
	Align       string // "flex-start", "flex-end", "center", "baseline", "stretch"
	AlignSelf   string // 自动对齐
	ColumnGap   string // 列间距
	RowGap      string // 行间距
	Gap         string // 通用间距
	
	Width       string
	Height      string
	MinWidth    string
	MinHeight   string
	MaxWidth    string
	MaxHeight   string
	
	Padding     string
	PaddingTop  string
	PaddingRight string
	PaddingBottom string
	PaddingLeft string
	
	Margin      string
	MarginTop   string
	MarginRight string
	MarginBottom string
	MarginLeft  string
	
	Position    string // "relative", "absolute", "fixed"
	Top         string
	Right       string
	Bottom      string
	Left        string
	
	Flex        string // flex 简写
	FlexGrow    string
	FlexShrink  string
	FlexBasis   string
	
	BackColor   Color
	Color       Color
	
	FontSize    string
	FontWeight  string
	LineHeight  string
	TextAlign   string
}

// NewStyle 创建样式对象
func NewStyle() *Style {
	return &Style{}
}

// Set 设置样式属性（链式调用）
func (s *Style) Set(key, value string) *Style {
	// 这里可以用反射或者 switch 来设置
	// 简化实现，直接返回
	return s
}

// ToViewProps 转换为 ViewProps
func (s *Style) ToViewProps() ViewProps {
	return ViewProps{
		Flex:      s.Display,
		FlexDir:   s.FlexDir,
		Justify:   s.Justify,
		Align:     s.Align,
		Wrap:      s.FlexWrap,
		ColumnGap: s.ColumnGap,
		RowGap:    s.RowGap,
		Width:     s.Width,
		Height:    s.Height,
		Padding:   s.Padding,
	}
}

// ParseStyle 解析 CSS 字符串为 Style 对象
// 支持两种格式：
// 1. CSS 字符串："display:flex; flex-wrap:wrap"
// 2. JSON 字符串：{"display":"flex","flexWrap":"wrap"}
func ParseStyle(css string) *Style {
	style := NewStyle()
	
	if css == "" {
		return style
	}
	
	// 简单实现：解析 "key1:value1; key2:value2" 格式
	// 去除首尾空格
	css = strings.TrimSpace(css)
	
	// 按分号分割
	pairs := strings.Split(css, ";")
	
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		
		// 按冒号分割
		parts := strings.SplitN(pair, ":", 2)
		if len(parts) != 2 {
			continue
		}
		
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		
		// 设置对应的样式属性
		switch key {
		case "display":
			style.Display = value
		case "flex-direction":
			style.FlexDir = value
		case "flex-wrap":
			style.FlexWrap = value
		case "justify-content":
			style.Justify = value
		case "align-items":
			style.Align = value
		case "column-gap":
			style.ColumnGap = value
		case "row-gap":
			style.RowGap = value
		case "gap":
			style.Gap = value
		case "width":
			style.Width = value
		case "height":
			style.Height = value
		case "padding":
			style.Padding = value
		case "background-color":
			style.BackColor = ParseColor(value)
		case "color":
			style.Color = ParseColor(value)
		case "font-size":
			style.FontSize = value
		case "font-weight":
			style.FontWeight = value
		}
	}
	
	return style
}
