// gx:nocollections
// 本文件为原生 Go 组件源码，经 gox build 转译为 context.go。
// gx:nocollections 声明跳过集合/裸对象字面量 desugar，保留手写 opcode 的原生 Go 语义。
// Context 是 GX 的响应式上下文，语义对齐 React Context：
//
//   - Provider 在自己的响应式作用域（Owner）里挂一个 value signal，只有它的
//     子树能读到；兄弟子树互相隔离，嵌套时内层就近覆盖外层。
//   - Use() 在组件函数里调用，沿当前 Owner 的 parent 链找最近的 Provider 绑定，
//     把它捕获成 getter；value 变化只让读到它的消费者重算，其它子树不受影响。
//   - 分支 / keyed 列表重建子树时会恢复建树期 Owner（见 scene/reactive.go），
//     因此 if / For 里的组件照样能读到所属 Provider 的值。
//   - 没有任何 Provider 时回落到 Context 自带的默认 signal，Set / Value 写读它——
//     这保留了 SetTheme 这类全局单例用法（应用只有一个上下文时更简单）。
//
// 实现落点：signal.Owner 提供 ProvideValue / LookupValue（沿 parent 链就近查找），
// Context 本身是纯 Go 运行期通道，不进场景树 wire 协议，零后端改动。
package widgets

import (
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// Context 是一个带类型的响应式上下文句柄。零值不可用，请用 NewContext 创建。
type Context[T any] struct {
	def signal.Signal[T] // 无 Provider 时的默认值通道（Set 写它）
}

// NewContext 创建一个默认值为 def 的 Context。
func NewContext[T any](def T) *Context[T] {
	return &Context[T]{def: signal.New(def)}
}

// Use 返回读取「当前作用域 context 值」的 getter，可直接作为 GX 动态插槽使用：
//   - 若在组件函数里调用且处于某个 Provider 子树内，沿 Owner 链就近捕获该
//     Provider 的 value getter；读取时登记依赖，值变化只重算这些消费者。
//   - 若不在任何 Provider 内（如组件外、顶层 handler），回落到默认 signal。
//
// 注意：Use 必须在「建树期 / 响应式重算期」调用才有作用域可查；拿到 getter 后
// 可长期保存，它捕获的绑定不随后续求值改变（与 React useContext 的快照一致）。
func (c *Context[T]) Use() func() T {
	if v, ok := signal.LookupValue(signal.GetOwner(), c); ok {
		switch sig := v.(type) {
		case *valueCell[T]:
			return sig.get
		case func() T:
			return sig
		case func() any:
			return func() T {
				if t, isT := sig().(T); isT {
					return t
				}
				var zero T
				return zero
			}
		}
	}
	return func() T { return c.def() }
}

// Set 写入默认值通道，触发所有「不在 Provider 内」的 Use 消费者重绘。
// 子树内有 Provider 时，请用 Provider 的 value getter 驱动更新——Provider 的
// 值只由它自己决定（React 语义），Set 不会穿透覆盖子树。
func (c *Context[T]) Set(v T) { c.def(v) }

// Value 立即读取当前作用域的值（非响应式，供事件 handler 等一次性读取）。
// 处于 Provider 子树内时读该 Provider 的值，否则读默认值。
func (c *Context[T]) Value() T { return c.Use()() }

// valueCell 承载 Provider 的值。静态值用恒 getter；动态值用 signal.Computed，
// 使上游 signal 变化时订阅该 cell 的消费者 effect 被精确唤醒。
type valueCell[T any] struct {
	get func() T
}

// Provider 声明式地为子树提供 context 值，是透明容器（gx-role=context-provider）。
//
// Props:
//   - value    T 或 func() T / func() any：值；getter 形式随上游 signal 响应式变化。
//   - children func() any → *Instance：子树。
//
// 作用域语义：Provider 用 signal.CreateRoot 建独立 Owner 挂值，children 在该
// Owner 下构建——所以只有这棵子树能 Lookup 到本值；嵌套 Provider 就近覆盖，
// 兄弟 Provider 互不影响。value 变化通过 getter 内的 signal 订阅驱动消费者重绘。
func (c *Context[T]) Provider(props gxscene.Props) *gxscene.Instance {
	cell := makeCell[T](props["value"])

	childrenGet := gxscene.Get[any](props, "children")
	var inst *gxscene.Instance
	// 在独立 Owner 下建子树并挂值：子树组件 Use() 沿 Owner 链命中本 cell。
	_ = signal.CreateRoot(func(o *signal.Owner) {
		o.ProvideValue(c, cell)
		var v any
		if childrenGet != nil {
			v = childrenGet()
		}
		if i, ok := v.(*gxscene.Instance); ok {
			inst = i
		}
	})

	if inst == nil {
		// 无有效子树时仍返回一个空容器，保持组件契约（总是返回 *Instance）。
		inst = gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
			el("div"), attr("gx-role", "context-provider"), closeEl(),
		}})
		return inst
	}

	// 用一层透明容器承载子树，便于测试/调试识别 Provider 边界；
	// 子树实例原样嫁接，不复制、不重建，保住其局部状态与响应式接线。
	return gxscene.Render(gxscene.Template{
		Ops: []gxscene.Op{el("div"), attr("gx-role", "context-provider"), dynChild(0), closeEl()},
		N:   1,
	}, func() any { return inst })
}

// makeCell 把 Provider 的 value prop 规整成响应式 valueCell。
// 静态值退化成恒 getter；getter 形式用 Computed 订阅上游，值变化可被消费者感知。
func makeCell[T any](v any) *valueCell[T] {
	switch x := v.(type) {
	case func() T:
		// 保持 getter 的原生依赖追踪：直接复用，不额外 Computed（少一层）。
		return &valueCell[T]{get: x}
	case func() any:
		return &valueCell[T]{get: func() T {
			if t, ok := x().(T); ok {
				return t
			}
			var zero T
			return zero
		}}
	case T:
		return &valueCell[T]{get: func() T { return x }}
	default:
		var zero T
		return &valueCell[T]{get: func() T { return zero }}
	}
}
