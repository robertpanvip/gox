package gui

import (
	"github.com/hajimehoshi/ebiten/v2"
)

// FxComponentBase 新的 FX 组件基类（函数式）
type FxComponentBase struct {
	BaseComponent
	template    *TemplateResult
	renderFunc  func() TemplateResult
	signals     []*Signal[int] // 简化：只支持 int 类型的 signal
	isFirstRender bool
}

// NewFxComponentBase 创建 FX 组件基类
func NewFxComponentBase(renderFunc func() TemplateResult) *FxComponentBase {
	return &FxComponentBase{
		renderFunc:    renderFunc,
		signals:       make([]*Signal[int], 0),
		isFirstRender: true,
	}
}

// Render 渲染
func (f *FxComponentBase) Render(screen *ebiten.Image) {
	if !f.IsVisible() {
		return
	}
	
	// 第一次渲染或需要重新渲染
	if f.template == nil || f.isFirstRender {
		f.template = f.renderFunc()
		f.isFirstRender = false
		
		// 为所有信号订阅更新回调
		for _, signal := range f.signals {
			signal.Subscribe(func() {
				f.RequestUpdate()
			})
		}
	}
	
	// 渲染模板
	if f.template != nil {
		f.template.Render(screen)
	}
}

// RequestUpdate 请求更新（当 signal 变化时调用）
func (f *FxComponentBase) RequestUpdate() {
	// 重新计算模板
	newTemplate := f.renderFunc()
	
	// 增量更新：比较新旧模板，只更新变化的部分
	f.updateTemplate(newTemplate)
}

// updateTemplate 增量更新模板
func (f *FxComponentBase) updateTemplate(newTemplate *TemplateResult) {
	if f.template == nil {
		f.template = newTemplate
		return
	}
	
	// 计算差异
	patches := diffTemplateResults(f.template, newTemplate)
	
	// 应用补丁
	if len(patches) > 0 {
		applyPatches(f.template, patches)
		
		// 重新渲染
		// TODO: 触发重绘
	}
}

// CreateSignal 创建信号（组件内部使用）
func (f *FxComponentBase) CreateSignal(initialValue int) *Signal[int] {
	signal := NewSignal(initialValue)
	f.signals = append(f.signals, signal)
	return signal
}
