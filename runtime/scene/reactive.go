package gxscene

import "github.com/robertpanvip/gox/runtime/signal"

// 本文件是 scene 与具体响应式库之间的适配层（T1.4）。
//
// scene 核心只需要三种响应式原语：建立 effect、创建 Owner 作用域、创建源 signal。
// 它们通过 ReactiveAdapter 注入，scene 的 branch.go / for.go 不再直接依赖 signal 包。
// 默认适配器 DefaultReactiveAdapter 用 signal 包实现；测试与替代运行时可注入自定义
// 适配器（如 mock 响应式系统）跑通 scene 测试——这是「替换注入实现可跑通」的验收点。
//
// 注意：Computation 仍是 signal.Computation 的类型别名（见 scene.go），因为大量现有
// API / 测试直接把 signal.Effect 传给 BindReactive，Go 函数类型不变性要求返回类型
// identical。别名保留了兼容性，同时 effect 的「行为」已完全由适配器决定。

// Owner 是响应式作用域的最小抽象：可级联销毁其下创建的计算。
type Owner interface {
	Dispose()
}

// Signal 是读写一体的信号最小抽象：无参读，带参写第一个值。
type Signal func(...any) any

// ReactiveAdapter 提供 scene 需要的响应式原语。
type ReactiveAdapter struct {
	// Effect 立即执行一次 f，并在其依赖变化时重跑；返回可销毁句柄。
	Effect func(f func()) Computation
	// Root 在独立作用域内执行 f，返回该作用域句柄供级联销毁。
	Root func(f func(o Owner)) Owner
	// NewSignal 创建一个值为 v 的源信号。
	NewSignal func(v any) Signal
	// CurrentOwner 返回当前 goroutine 所在的响应式 Owner（无作用域为 nil）。
	// 动态子树（Branch/For）在建树期捕获它，重建时用它恢复 context 作用域——
	// 这是 Context 在 if / For 子树里不丢值的必要条件。
	CurrentOwner func() Owner
	// RunWithOwner 在给定 Owner 下执行 f；o 为 nil 时直接执行。
	RunWithOwner func(o Owner, f func())
}

// DefaultReactiveAdapter 用 signal 包实现的默认适配器。
var DefaultReactiveAdapter = ReactiveAdapter{
	Effect: signal.Effect,
	Root: func(f func(o Owner)) Owner {
		var root Owner
		signal.CreateRoot(func(o *signal.Owner) {
			root = o
			f(o)
		})
		return root
	},
	NewSignal: func(v any) Signal {
		s := signal.New(v)
		return func(args ...any) any {
			if len(args) > 0 {
				// signal.New[any] 的首个写入值即新值。
				s(castAny(args[0]))
			}
			return s()
		}
	},
	CurrentOwner: func() Owner { return signal.GetOwner() },
	RunWithOwner: func(o Owner, f func()) {
		if so, ok := o.(*signal.Owner); ok {
			signal.RunWithOwner(so, f)
		} else {
			// 自定义适配器传入的非 signal Owner：无法恢复，直接执行。
			// 语义退化为「动态子树不继承 context」，不影响无 Context 的应用。
			if f != nil {
				f()
			}
		}
	},
}

// castAny 是恒等转换，仅用于让 any 值满足 Signal[any] 的泛型参数（其类型即 any）。
func castAny(v any) any { return v }

// activeAdapter 是当前 scene 使用的适配器；默认指向 signal 实现。
var activeAdapter = DefaultReactiveAdapter

// SetReactiveAdapter 替换 scene 的响应式适配器（传 nil 恢复默认）。
// 非线程安全，应在建树前调用。返回旧适配器便于恢复。
func SetReactiveAdapter(a ReactiveAdapter) (old ReactiveAdapter) {
	old = activeAdapter
	if a.Effect == nil || a.Root == nil || a.NewSignal == nil {
		activeAdapter = DefaultReactiveAdapter
		return old
	}
	// 新增的可选原语缺省回落到默认实现，兼容旧的自定义适配器。
	if a.CurrentOwner == nil {
		a.CurrentOwner = DefaultReactiveAdapter.CurrentOwner
	}
	if a.RunWithOwner == nil {
		a.RunWithOwner = DefaultReactiveAdapter.RunWithOwner
	}
	activeAdapter = a
	return old
}

// Reactive 返回当前适配器（供 scene 内部统一取用）。
func Reactive() ReactiveAdapter { return activeAdapter }
