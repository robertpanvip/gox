package gui

// TemplatePart 模板片段（类似 lit-html 的 Part）
type TemplatePart interface {
	Update()
}

// TextPart 文本片段（用于动态文本内容）
type TextPart struct {
	Target *Label
	Render func() string
}

// Update 更新文本内容
func (p *TextPart) Update() {
	if p.Target != nil && p.Render != nil {
		p.Target.SetText(p.Render())
	}
}

// NewTextPart 创建文本片段
func NewTextPart(target *Label, render func() string) *TextPart {
	return &TextPart{
		Target: target,
		Render: render,
	}
}

// AttributePart 属性片段（用于动态属性）
type AttributePart struct {
	Target Component
	Render func() interface{}
	Apply  func(Component, interface{})
}

// Update 更新属性
func (p *AttributePart) Update() {
	if p.Target != nil && p.Render != nil && p.Apply != nil {
		p.Apply(p.Target, p.Render())
	}
}

// NewAttributePart 创建属性片段
func NewAttributePart(target Component, render func() interface{}, apply func(Component, interface{})) *AttributePart {
	return &AttributePart{
		Target: target,
		Render: render,
		Apply:  apply,
	}
}

// TemplateResult 模板渲染结果（类似 lit-html 的 TemplateResult）
type TemplateResult struct {
	StaticParts []Component    // 静态组件（创建一次）
	DynamicParts []TemplatePart // 动态片段（可更新）
}

// Render 渲染模板
func (t *TemplateResult) Render(screen *ebiten.Image) {
	// 渲染所有静态组件
	for _, part := range t.StaticParts {
		if part != nil && part.IsVisible() {
			part.Render(screen)
		}
	}
}

// Update 更新所有动态片段
func (t *TemplateResult) Update() {
	for _, part := range t.DynamicParts {
		if part != nil {
			part.Update()
		}
	}
}
