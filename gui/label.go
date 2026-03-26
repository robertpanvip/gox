package gui

import (
    "github.com/fogleman/gg"
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

// NewLabel 创建 Label 组件 (TSX 风格)
func NewLabel(props LabelProps) *Label {
    return &Label{
        Props: props,
        Visible: true,
    }
}

func (l *Label) Render(dc *gg.Context) {
    if !l.Visible {
        return
    }
    
    // 绘制文字
    dc.SetColor(l.Props.TextColor.ToGoColor())
    dc.LoadFontFace("Arial", l.Props.FontSize)
    dc.DrawString(l.Props.Text, float64(l.Rect.X), float64(l.Rect.Y)+l.Props.FontSize)
}
