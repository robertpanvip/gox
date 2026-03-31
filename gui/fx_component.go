package gui

import (
	"github.com/hajimehoshi/ebiten/v2"
)

// 当前 FxWrapper（用于编译器注入）
var currentWrapper *FxWrapper

// GetCurrentWrapper 获取当前 FxWrapper（由编译器调用）
func GetCurrentWrapper() *FxWrapper {
	return currentWrapper
}

// TemplateResult 模板结果（lit-html 风格）
type TemplateResult struct {
	StaticCode string        // 静态代码标识（用于比较模板）
	Dynamic    []interface{} // 动态值数组（用于比较变化）
	Factory    func() (Component, []Part) // 工厂函数：返回 Root 组件和 Parts 数组
}

// Render 渲染模板结果（第一次渲染）
func (t *TemplateResult) Render(screen *ebiten.Image) {
	if t.Factory != nil {
		root, _ := t.Factory()
		if root != nil && root.IsVisible() {
			root.Render(screen)
		}
	}
}

// Update 从新的 TemplateResult 更新（比较并更新变化的部分）
func (t *TemplateResult) Update(new TemplateResult, parts []Part) {
	// 比较 Dynamic 数组
	for i, newValue := range new.Dynamic {
		if i < len(t.Dynamic) {
			oldValue := t.Dynamic[i]
			
			// 值变化了，更新对应的 Part
			if newValue != oldValue && i < len(parts) {
				parts[i].Update(newValue)
			}
		}
	}
	
	// 更新 Dynamic 数组
	t.Dynamic = new.Dynamic
}

// FxWrapper 函数式组件包装器（lit-html 风格）
type FxWrapper struct {
	BaseComponent
	componentFunc func() TemplateResult
	lastTemplate  *TemplateResult  // 上次的 TemplateResult
	parts         []Part           // Parts 数组引用
	root          Component        // 根组件
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
	
	// 设置当前 wrapper（用于编译器注入的触发更新）
	currentWrapper = f
	defer func() { currentWrapper = nil }()
	
	// 执行组件函数，获取 TemplateResult
	newTemplate := f.componentFunc()
	
	// 检查是否有上次的 TemplateResult
	if f.lastTemplate != nil {
		// 比较 StaticCode
		if newTemplate.StaticCode == f.lastTemplate.StaticCode {
			// 模板相同，比较并更新 Dynamic
			f.lastTemplate.Update(newTemplate, f.parts)
		} else {
			// 模板不同，重新创建组件树
			root, parts := newTemplate.Factory()
			f.root = root
			f.parts = parts
			
			// 初始化 Dynamic 值
			for i, value := range newTemplate.Dynamic {
				if i < len(parts) {
					parts[i].Update(value)
				}
			}
		}
	} else {
		// 第一次渲染，创建组件树
		root, parts := newTemplate.Factory()
		f.root = root
		f.parts = parts
		
		// 初始化 Dynamic 值
		for i, value := range newTemplate.Dynamic {
			if i < len(parts) {
				parts[i].Update(value)
			}
		}
	}
	
	// 保存当前的 TemplateResult
	f.lastTemplate = &newTemplate
	
	// 渲染根组件
	if f.root != nil {
		f.root.Render(screen)
	}
}

// RequestUpdate 请求重新渲染（由编译器自动调用）
func (f *FxWrapper) RequestUpdate() {
	// TODO: 通知 App 触发重绘
	// 暂时标记需要更新
}

// WrapTemplateResult 将 TemplateResult 包装为 Component
func WrapTemplateResult(componentFunc func() TemplateResult) Component {
	return NewFxWrapper(componentFunc)
}
