// Context 是 GX 的跨组件响应式数据通道。
//
// GX 组件是纯函数 func(Props) *Instance，彼此独立返回场景树，没有 React hook
// 那种"当前渲染上下文"。Context 用 signal 承载共享值，绕开逐层 props 透传：
//   - ctx.Provider(props) 把 value 写入 context（声明式，value 可为 getter，
//     随上游 signal 变化）；
//   - ctx.Use() 返回 func() T，直接作为 GX 动态插槽 getter 使用；读取时自动
//     订阅，Set / Provider 写入新值后所有消费者的插槽重算、组件重绘；
//   - 未挂载 Provider 时读默认值，因此组件可安全地在任意位置 Use。
//
// 语义边界（与 React Context 的差异，务必知悉）：
//   - Context 是"应用级单例通道"：一个 *Context[T] 全树共享一个当前值，最后
//     写入者生效。不支持 React 那种"按子树作用域隔离的多个 Provider"。需要多套
//     配置并存时用多个不同 Context 实例（如两套主题各一个 Context）。
//   - 取舍原因：GX 组件是纯函数、children 是延迟 getter，没有稳定的"组件所在
//     作用域"可查；signal 单例模型最契合 GX 细粒度响应式，且 context 不进场景
//     树协议（纯 Go 运行期通道），零后端改动。
package widgets

import (
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// Context 是一个带类型的响应式上下文句柄。零值不可用，请用 NewContext 创建。
type Context[T any] struct {
	sig signal.Signal[T]
}

// NewContext 创建一个默认值为 def 的 Context。
func NewContext[T any](def T) *Context[T] {
	return &Context[T]{sig: signal.New(def)}
}

// Use 返回读取当前值的 getter。该 getter 可直接作为 gxscene 动态插槽使用：
// 读取会建立 signal 依赖，Set / Provider 写入新值后订阅者自动重算。
func (c *Context[T]) Use() func() T {
	return func() T { return c.sig() }
}

// Set 写入新值，触发所有 Use 消费者重绘。值未变化（DeepEqual）时短路。
func (c *Context[T]) Set(v T) {
	c.sig(v)
}

// Value 立即读取当前值（非响应式，供事件 handler 等一次性读取场景使用）。
func (c *Context[T]) Value() T {
	return c.sig()
}

// Provider 声明式地把 value 写入 ctx 并渲染子树。它是透明容器
// （gx-role=context-provider），只承担"设初值 + 承载子树"，不产生可见样式。
//
// Props:
//   - value    T 或 func() T：新值，构造时读取一次写入（支持 getter 取当前值）。
//   - children func() any → *Instance：子树。
//
// 动态更新请直接调用 ctx.Set(v)：Context 是应用级单例通道，Set 会触发所有
// Use 消费者重绘。不要在子树重建时反复写 Provider —— 它的职责是确立初始值。
func (c *Context[T]) Provider(props gxscene.Props) *gxscene.Instance {
	switch v := props["value"].(type) {
	case func() T:
		c.Set(v())
	case T:
		c.Set(v)
	case func() any:
		if t, ok := v().(T); ok {
			c.Set(t)
		}
	}

	childrenGet := gxscene.Get[any](props, "children")
	return gxscene.Render(gxscene.Template{
		Ops: []gxscene.Op{
			el("div"), attr("gx-role", "context-provider"),
			dynChild(0), closeEl(),
		}, N: 1,
	}, func() any {
		return childrenGet()
	})
}
