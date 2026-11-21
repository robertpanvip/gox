package gxscene

import (
	"sync"

	"github.com/robertpanvip/gox/runtime/signal"
)

// Branch 是一个条件分支控制器，对应 JSX 里的 if/else 区域。
//
// 语义对齐 SolidJS 的条件渲染：
//   - selector 在「当前响应式 Effect 上下文」中求值，读到的 signal 被登记为依赖；
//   - 命中索引变化时，销毁旧分支的 Owner（级联停止其所有 Effect、执行 Cleanup），
//     在新 Owner 下运行对应 builder 构建子树，并对子树接线；
//   - 命中索引不变时不重建——分支内部的 signal 变化由子树自身的细粒度 Effect 处理，
//     绝不重跑根组件。
//
// Branch 由一个稳定的 getter 返回（编译期生成），因此同一 Branch 对象跨 effect
// 重跑保持不变；变化检测发生在 Resolve 内部的 selector 索引比较。
type Branch struct {
	mu       sync.Mutex
	selector func() int
	builders []func() *Instance
	idx      int
	cur      *Instance
	owner    *signal.Owner
	disposed bool
	init     bool        // 是否已完成首次求值（idx 初始为 0，需要区分「从未求值」）
	sink     *changeSink // 所属实例树的变更缓冲，传给各分支子实例
	bctx     *bindCtx    // 统一接线上下文，传给新建的分支子树
}

// NewBranch 创建一个条件分支控制器。
//   - selector: 返回当前命中的分支索引；应读取 signal 以便被自动追踪。
//   - builders: 每个分支的子树构造函数（编译期由对应 if/else 分支的 JSX 生成）。
//
// selector/builders 为 nil 或空时，Resolve 返回 nil（渲染空）。
func NewBranch(selector func() int, builders ...func() *Instance) *Branch {
	return &Branch{selector: selector, builders: builders, idx: -1}
}

// BranchGetter 是编译器生成条件分支时使用的工厂：它捕获一个稳定的 *Branch，
// 返回一个每次调用都产出同一对象的 getter。这样 effect 重跑不会重建 Branch 状态。
//
// 编译器为 if/else 生成的 getter 形如：
//
//	gxscene.BranchGetter(
//	    func() int { if visible() { return 0 }; return 1 },
//	    func() *gxscene.Instance { return <then 子树> },
//	    func() *gxscene.Instance { return <else 子树> },
//	)
func BranchGetter(selector func() int, builders ...func() *Instance) func() any {
	b := NewBranch(selector, builders...)
	return func() any { return b }
}

// Resolve 在响应式 Effect 上下文中被调用：
//  1. 求值 selector（登记依赖）；
//  2. 索引变化时切换 Owner 与子树；
//  3. 返回当前命中的子树 *Instance（供 graft）。
//
// 注意：selector 必须在调用方的 effect 栈内执行——Resolve 由 SetPart 在
// BindReactive 建立的 effect 回调中调用，因此依赖被正确登记到该 effect。
func (b *Branch) Resolve() *Instance {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	if b.disposed {
		b.mu.Unlock()
		return nil
	}
	sel := -1
	if b.selector != nil {
		sel = b.selector() // 锁内求值：selector 只读 signal，不重入 b.mu
	}
	if b.init && sel == b.idx {
		cur := b.cur
		b.mu.Unlock()
		return cur // 分支未变：返回缓存子树，不重建
	}
	b.init = true
	b.idx = sel

	// 切换前销毁旧分支作用域：级联停止其所有 Effect / Cleanup。
	oldOwner := b.owner
	b.owner = nil
	b.cur = nil
	b.mu.Unlock()

	if oldOwner != nil {
		oldOwner.Dispose()
	}

	var cur *Instance
	if sel >= 0 && sel < len(b.builders) && b.builders[sel] != nil {
		// 在新 Owner 下构建并接线子树：子树内的插值 Effect 归属该 Owner，
		// 分支再次切换/销毁时随之级联清理。
		b.owner = signal.CreateRoot(func(o *signal.Owner) {
			cur = b.builders[sel]()
			if cur != nil {
				if b.sink != nil {
					cur.changes = b.sink // 共享所属实例树的变更缓冲
				}
				// 用所属实例树的统一接线上下文递归接线（含 onDirty 重绘回调）。
				// 这替代了原先的裸 BindReactive——后者让嵌套分支切换后树变了却不重绘。
				// 分支子树挂在 slotBranch.child 上、其叶子由根聚合 effect 的 Update
				// 递归覆盖，因此这里只接结构插槽，不另建聚合 effect（避免重复重绘）。
				if b.bctx != nil {
					cur.bindStructure(b.bctx)
				} else {
					cur.BindReactive(signal.Effect)
				}
			}
		})
	}

	b.mu.Lock()
	if b.disposed {
		// 并发销毁：刚建好的 owner 需要立即释放。
		b.mu.Unlock()
		if b.owner != nil {
			b.owner.Dispose()
		}
		return nil
	}
	b.cur = cur
	b.mu.Unlock()
	return cur
}

// adoptSink 实现 subtreeController：记录所属实例树的变更缓冲，传给各分支子实例。
func (b *Branch) adoptSink(s *changeSink) { b.sink = s }

// adoptBind 实现 subtreeController：记录统一接线上下文，供 Resolve 构建新分支时递归接线。
func (b *Branch) adoptBind(ctx *bindCtx) { b.bctx = ctx }

// Dispose 销毁当前分支作用域（幂等）。级联停止子树内所有 Effect / Cleanup。
func (b *Branch) Dispose() {
	if b == nil {
		return
	}
	b.mu.Lock()
	if b.disposed {
		b.mu.Unlock()
		return
	}
	b.disposed = true
	owner := b.owner
	b.owner = nil
	b.cur = nil
	b.mu.Unlock()
	if owner != nil {
		owner.Dispose()
	}
}

// Current 返回当前命中的子树（测试/调试用）。
func (b *Branch) Current() *Instance {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cur
}
