package gui

import (
    "github.com/fogleman/gg"
)

// ButtonProps Button 组件的属性
type ButtonProps struct {
    Text       string
    FontSize   float64
    BgColor    Color
    TextColor  Color
    HoverColor Color
    OnClick    func()
}

// Button 按钮组件
type Button struct {
    BaseComponent
    Props     ButtonProps
    IsHovered bool
}

// NewButton 创建 Button 组件 (TSX 风格)
func NewButton(props ButtonProps) *Button {
    button := &Button{
        Props: props,
        Visible: true,
        IsHovered: false,
    }
    // 设置默认颜色
    if props.BgColor.A == 0 {
        button.Props.BgColor = ColorLightGray
    }
    if props.TextColor.A == 0 {
        button.Props.TextColor = ColorBlack
    }
    if props.HoverColor.A == 0 {
        button.Props.HoverColor = NewColor(200, 200, 200, 255)
    }
    return button
}

func (b *Button) Render(dc *gg.Context) {
    if !b.Visible {
        return
    }
    
    // 绘制背景
    if b.IsHovered {
        dc.SetColor(b.Props.HoverColor.ToGoColor())
    } else {
        dc.SetColor(b.Props.BgColor.ToGoColor())
    }
    dc.DrawRectangle(float64(b.Rect.X), float64(b.Rect.Y), float64(b.Rect.Width), float64(b.Rect.Height))
    dc.Fill()
    
    // 绘制边框
    dc.SetColor(ColorBlack.ToGoColor())
    dc.SetLineWidth(1)
    dc.DrawRectangle(float64(b.Rect.X), float64(b.Rect.Y), float64(b.Rect.Width), float64(b.Rect.Height))
    dc.Stroke()
    
    // 绘制文字
    dc.SetColor(b.Props.TextColor.ToGoColor())
    dc.LoadFontFace("Arial", b.Props.FontSize)
    
    // 文字居中
    textWidth, _ := dc.MeasureString(b.Props.Text)
    textX := float64(b.Rect.X) + float64(b.Rect.Width)/2 - textWidth/2
    textY := float64(b.Rect.Y) + float64(b.Rect.Height)/2 + b.Props.FontSize/3
    dc.DrawString(b.Props.Text, textX, textY)
}

func (b *Button) OnClick(x, y int) {
    if b.Props.OnClick != nil {
        b.Props.OnClick()
    }
}

func (b *Button) OnMouseMove(x, y int) {
    // 检查鼠标是否在按钮上
    b.IsHovered = (x >= b.Rect.X && x <= b.Rect.X+b.Rect.Width &&
                   y >= b.Rect.Y && y <= b.Rect.Y+b.Rect.Height)
}
