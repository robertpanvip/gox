package gui

import (
	"github.com/hajimehoshi/ebiten/v2"
)

// FxComponent fx 组件接口（类似 lit-html 的 LitElement）
type FxComponent interface {
	Component
	RequestUpdate()
	GetTemplateResult() *TemplateResult
}

// BaseFxComponent fx 组件基类
type BaseFxComponent struct {
	BaseComponent
	templateResult *TemplateResult
	updateCallbacks []func()
}

// RequestUpdate 请求更新（类似 lit-html 的 requestUpdate）
// 当状态变化时调用此方法，会触发所有动态部分的更新
func (b *BaseFxComponent) RequestUpdate() {
	// 更新模板中的所有动态部分
	if b.templateResult != nil {
		b.templateResult.Update()
	}
	
	// 调用所有更新回调
	for _, cb := range b.updateCallbacks {
		cb()
	}
}

// SetTemplateResult 设置模板结果
func (b *BaseFxComponent) SetTemplateResult(result *TemplateResult) {
	b.templateResult = result
}

// GetTemplateResult 获取模板结果
func (b *BaseFxComponent) GetTemplateResult() *TemplateResult {
	return b.templateResult
}

// AddUpdateCallback 添加更新回调
func (b *BaseFxComponent) AddUpdateCallback(cb func()) {
	b.updateCallbacks = append(b.updateCallbacks, cb)
}

// Render 渲染 fx 组件
func (b *BaseFxComponent) Render(screen *ebiten.Image) {
	if !b.IsVisible() {
		return
	}
	
	// 渲染模板结果
	if b.templateResult != nil {
		b.templateResult.Render(screen)
	}
}

// OnClick 默认点击处理
func (b *BaseFxComponent) OnClick(x, y int) {
	if b.templateResult != nil {
		for _, part := range b.templateResult.StaticParts {
			if part != nil && part.IsVisible() {
				part.OnClick(x, y)
			}
		}
	}
}

// OnMouseMove 默认鼠标移动处理
func (b *BaseFxComponent) OnMouseMove(x, y int) {
	if b.templateResult != nil {
		for _, part := range b.templateResult.StaticParts {
			if part != nil && part.IsVisible() {
				part.OnMouseMove(x, y)
			}
		}
	}
}
