package gui

import (
    "github.com/fogleman/gg"
)

// Container 容器组件（垂直布局）
type Container struct {
    BaseComponent
    BgColor   Color
    Padding   int
    Spacing   int
}

func NewContainer() *Container {
    return &Container{
        BgColor:  ColorTransparent,
        Padding:  10,
        Spacing:  5,
    }
}

func (c *Container) Render(dc *gg.Context) {
    if !c.Visible {
        return
    }
    
    // 绘制背景
    if c.BgColor.A > 0 {
        dc.SetColor(c.BgColor.ToGoColor())
        dc.DrawRectangle(float64(c.Rect.X), float64(c.Rect.Y), float64(c.Rect.Width), float64(c.Rect.Height))
        dc.Fill()
    }
    
    // 渲染子组件
    for _, child := range c.Children {
        child.Render(dc)
    }
}

// AddChild 添加子组件（垂直布局）
func (c *Container) AddChild(comp Component) {
    y := c.Rect.Y + c.Padding
    if len(c.Children) > 0 {
        // 获取最后一个组件的位置
        lastChild := c.Children[len(c.Children)-1]
        lastRect := lastChild.GetRect()
        y = lastRect.Y + lastRect.Height + c.Spacing
    }
    
    // 设置组件的位置和大小
    comp.SetRect(Rect{
        X:      c.Rect.X + c.Padding,
        Y:      y,
        Width:  c.Rect.Width - 2*c.Padding,
        Height: 40, // 默认高度
    })
    
    // 设置组件可见
    if base, ok := comp.(*Label); ok {
        base.Visible = true
    }
    if base, ok := comp.(*Button); ok {
        base.Visible = true
    }
    
    c.Children = append(c.Children, comp)
}

func (c *Container) OnClick(x, y int) {
    for _, child := range c.Children {
        child.OnClick(x, y)
    }
}

func (c *Container) OnMouseMove(x, y int) {
    for _, child := range c.Children {
        child.OnMouseMove(x, y)
    }
}
