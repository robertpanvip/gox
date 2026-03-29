package gui

import (
	"github.com/hajimehoshi/ebiten/v2"
)

// State 响应式状态包装器（类似 Solid.js 的 signal）
type State[T any] struct {
	value    T
	updaters []func()
}

// NewState 创建响应式状态
func NewState[T any](initialValue T) *State[T] {
	return &State[T]{
		value:    initialValue,
		updaters: make([]func(), 0),
	}
}

// Get 获取状态值（并注册依赖）
func (s *State[T]) Get() T {
	// TODO: 这里需要记录当前正在执行的更新函数
	// 以便在 Set 时触发它
	return s.value
}

// Set 设置状态值并触发更新
func (s *State[T]) Set(newValue T) {
	s.value = newValue
	// 触发所有依赖这个状态的更新函数
	for _, updater := range s.updaters {
		updater()
	}
}

// Subscribe 订阅状态变化
func (s *State[T]) Subscribe(updater func()) {
	s.updaters = append(s.updaters, updater)
}

// FxComponent fx 组件接口
type FxComponent interface {
	Component
	RequestUpdate()
	GetTemplateResult() *TemplateResult
}

// BaseFxComponent fx 组件基类
type BaseFxComponent struct {
	BaseComponent
	templateResult  *TemplateResult
	updateCallbacks []func()
}

// RequestUpdate 请求更新
func (b *BaseFxComponent) RequestUpdate() {
	if b.templateResult != nil {
		b.templateResult.Update()
	}
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
