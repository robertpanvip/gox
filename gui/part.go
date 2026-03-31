package gui

// Part 动态部分接口
type Part interface {
	Update(value interface{})
}

// TextPart 文本动态部分
type TextPart struct {
	Placeholder *Comment  // 占位符节点
}

// NewTextPart 创建文本部分
func NewTextPart(placeholder *Comment) *TextPart {
	return &TextPart{
		Placeholder: placeholder,
	}
}

// Update 更新文本内容
func (p *TextPart) Update(value interface{}) {
	if text, ok := value.(string); ok {
		textNode := NewText(text)
		p.Placeholder.AppendChild(textNode)
	}
}

// AttributePart 属性动态部分
type AttributePart struct {
	Element Component  // 目标组件
	Name    string     // 属性名
}

// NewAttributePart 创建属性部分
func NewAttributePart(element Component, name string) *AttributePart {
	return &AttributePart{
		Element: element,
		Name:    name,
	}
}

// Update 更新属性值
func (p *AttributePart) Update(value interface{}) {
	if str, ok := value.(string); ok {
		// 尝试调用 SetAttribute
		if element, ok := p.Element.(interface{ SetAttribute(string, string) }); ok {
			element.SetAttribute(p.Name, str)
		}
	}
}

// ChildPart 子组件动态部分
type ChildPart struct {
	Placeholder *Comment  // 占位符节点
	Current     Component // 当前子组件
}

// NewChildPart 创建子组件部分
func NewChildPart(placeholder *Comment) *ChildPart {
	return &ChildPart{
		Placeholder: placeholder,
	}
}

// Update 更新子组件
func (p *ChildPart) Update(value interface{}) {
	// 根据值类型创建不同的组件
	if component, ok := value.(Component); ok {
		if p.Current != nil {
			p.Placeholder.ReplaceChild(component, p.Current)
		} else {
			p.Placeholder.AppendChild(component)
		}
		p.Current = component
	}
}
