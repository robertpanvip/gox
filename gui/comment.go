package gui

import (
	"github.com/hajimehoshi/ebiten/v2"
)

// Comment DOM 注释节点
type Comment struct {
	BaseComponent
	Data     string      // 注释数据
	RealNode Component   // 实际替换的组件（动态内容）
}

// NewComment 创建注释节点
func NewComment(data string) *Comment {
	return &Comment{
		Data: data,
	}
}

// AppendChild 添加子节点（实际是替换 RealNode）
func (c *Comment) AppendChild(child Component) {
	c.RealNode = child
}

// RemoveChild 移除子节点
func (c *Comment) RemoveChild(child Component) {
	if c.RealNode == child {
		c.RealNode = nil
	}
}

// ReplaceChild 替换子节点
func (c *Comment) ReplaceChild(newChild, oldChild Component) {
	if c.RealNode == oldChild {
		c.RealNode = newChild
	}
}

// Render 渲染注释节点（渲染实际的 RealNode）
func (c *Comment) Render(screen *ebiten.Image) {
	if c.RealNode != nil {
		c.RealNode.Render(screen)
	}
}

// SetData 设置注释数据
func (c *Comment) SetData(data string) {
	c.Data = data
}

// GetData 获取注释数据
func (c *Comment) GetData() string {
	return c.Data
}
