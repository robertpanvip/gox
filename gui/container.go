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
	return &Container{
		BaseComponent: BaseComponent{
			Visible:  true,
			Children: make([]Component, 0),
		},
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
