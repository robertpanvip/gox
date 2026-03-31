package gui

import (
	"github.com/hajimehoshi/ebiten/v2"
)

// FxWrapper 函数式组件包装器
// 将 TemplateResult 包装为可渲染的 Component
type FxWrapper struct {
	BaseComponent
	componentFunc func() TemplateResult
	lastTemplate  *TemplateResult  // 上次的模板（用于比较）
	components    []Component      // 缓存的组件（避免重复创建）
}

// NewFxWrapper 创建函数式组件包装器
func NewFxWrapper(componentFunc func() TemplateResult) *FxWrapper {
	return &FxWrapper{
		componentFunc: componentFunc,
	}
}

// Render 渲染函数式组件
func (f *FxWrapper) Render(screen *ebiten.Image) {
	if !f.IsVisible() {
		return
	}
	
	// 每次都重新执行组件函数，获取最新的 TemplateResult
	newTemplate := f.componentFunc()
	
	// 判断是否需要重建模板
	needRebuild := f.shouldRebuild(&newTemplate)
	
	if needRebuild {
		// 模板变化了，需要重建
		f.lastTemplate = &newTemplate
		f.components = make([]Component, len(newTemplate.Static))
		
		// 调用工厂函数创建组件
		for i, factory := range newTemplate.Static {
			if factory != nil {
				f.components[i] = factory()
			}
		}
		
		// 渲染所有组件
		for _, comp := range f.components {
			if comp != nil && comp.IsVisible() {
				comp.Render(screen)
			}
		}
	} else {
		// 模板相同，但仍然需要重新调用工厂函数
		// 因为闭包捕获的值可能已经变化了
		for i, factory := range newTemplate.Static {
			if factory != nil && i < len(f.components) {
				f.components[i] = factory()
			}
		}
		
		// 渲染所有组件
		for _, comp := range f.components {
			if comp != nil && comp.IsVisible() {
				comp.Render(screen)
			}
		}
	}
}

// shouldRebuild 判断是否需要重建模板
func (f *FxWrapper) shouldRebuild(newTemplate *TemplateResult) bool {
	// 第一次渲染
	if f.lastTemplate == nil {
		return true
	}
	
	// 比较 Hash 值（类似 lit-html 比较 strings 引用）
	if f.lastTemplate.hash != newTemplate.hash {
		return true
	}
	
	// Hash 相同就认为是同一个模板，复用组件
	return false
}

// RequestUpdate 请求重新渲染
func (f *FxWrapper) RequestUpdate() {
	// 重新计算模板
	newTemplate := f.componentFunc()
	f.lastTemplate = &newTemplate
	
	// TODO: 触发重绘（通过 App）
}

// WrapTemplateResult 将 TemplateResult 包装为 Component
// 用于在 Static 中嵌套其他组件返回的 TemplateResult
// 注意：接收的是函数，不是 TemplateResult，这样每次都会重新执行组件函数
func WrapTemplateResult(componentFunc func() TemplateResult) Component {
	return &FxWrapper{
		componentFunc: componentFunc,
	}
}
