package gui

import (
	"github.com/hajimehoshi/ebiten/v2"
)

// Container 容器组件
type Container struct {
	BaseComponent
}

// NewContainer 创建容器
func NewContainer() *Container {
	container := &Container{
		BaseComponent: BaseComponent{
			Visible:  true,
			Children: make([]Component, 0),
			UseYoga:  true,
			Layout:   NewLayoutEngine(),
		},
	}
	// 默认设置为 FlexRow 布局
	container.Layout.SetFlexDirection(FlexRow)
	return container
}

// SetFlexDirection 设置弹性盒子方向
func (c *Container) SetFlexDirection(dir FlexDirection) {
	if c.Layout != nil {
		c.Layout.SetFlexDirection(dir)
	}
}

// SetJustifyContent 设置主轴对齐方式
func (c *Container) SetJustifyContent(justify JustifyContent) {
	if c.Layout != nil {
		c.Layout.SetJustifyContent(justify)
	}
}

// SetAlignItems 设置交叉轴对齐方式
func (c *Container) SetAlignItems(align AlignItems) {
	if c.Layout != nil {
		c.Layout.SetAlignItems(align)
	}
}

// SetPadding 设置内边距
func (c *Container) SetPadding(top, right, bottom, left int) {
	if c.Layout != nil {
		c.Layout.SetPadding(top, right, bottom, left)
	}
}

// AddChild 添加子组件（使用 Yoga 布局）
func (c *Container) AddChild(child Component) {
	c.Children = append(c.Children, child)
	
	// 如果使用 Yoga 布局，添加子节点
	if c.UseYoga && c.Layout != nil {
		// 为子组件创建布局引擎（如果还没有）
		if baseChild, ok := child.(*Label); ok {
			if baseChild.Layout == nil {
				baseChild.Layout = NewLayoutEngine()
			}
			c.Layout.AddChild(baseChild.Layout)
		} else if baseChild, ok := child.(*Button); ok {
			if baseChild.Layout == nil {
				baseChild.Layout = NewLayoutEngine()
			}
			c.Layout.AddChild(baseChild.Layout)
		} else if baseChild, ok := child.(*Container); ok {
			if baseChild.Layout == nil {
				baseChild.Layout = NewLayoutEngine()
			}
			c.Layout.AddChild(baseChild.Layout)
		}
	}
}

// DoLayout 执行布局计算
func (c *Container) DoLayout() {
	if c.UseYoga && c.Layout != nil {
		// 计算布局
		c.Layout.CalculateLayout(c.Rect.X, c.Rect.Y, c.Rect.Width, c.Rect.Height)
		
		// 应用布局结果到子组件
		for i, child := range c.Children {
			if i < len(c.Layout.Children) {
				childLayout := c.Layout.Children[i]
				childRect := childLayout.GetComputedRect()
				child.SetRect(childRect)
			}
		}
	}
}

// Render 渲染容器及其所有子组件
func (c *Container) Render(screen *ebiten.Image) {
	if !c.IsVisible() {
		return
	}

	// 渲染所有子组件
	for _, child := range c.Children {
		if child.IsVisible() {
			child.Render(screen)
		}
	}
}

// OnClick 处理点击事件 - 转发给子组件
func (c *Container) OnClick(x, y int) {
	for _, child := range c.Children {
		if child.IsVisible() {
			child.OnClick(x, y)
		}
	}
}

// OnMouseMove 处理鼠标移动事件 - 转发给子组件
func (c *Container) OnMouseMove(x, y int) {
	for _, child := range c.Children {
		if child.IsVisible() {
			child.OnMouseMove(x, y)
		}
	}
}
