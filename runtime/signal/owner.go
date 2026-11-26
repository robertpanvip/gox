// Owner、Cleanup 与批处理调度。
//
// SolidJS 的核心不是 Signal 本身，而是「响应式计算所处的 Owner 树」：每个
// Effect/Computed 都属于一个 Owner，Owner 销毁时级联销毁其下所有计算并执行
// cleanup。这让条件分支切换、列表元素删除等场景能够精确释放副作用。
//
// 单线程模型：Owner 栈、批处理深度、待调度 Effect 都是唯一的——只有 UI Runtime
// 会访问它们，无需按 goroutine 分桶，也无需加锁。后台任务必须经 Signal.Safe
// 投递进 UI Runtime 后才能触碰响应式系统。
package signal

import (
	"fmt"
	"strings"
	"sync"
)

// ownerStack 是当前 Owner 栈（栈顶为最内层作用域）。单线程唯一实例。
var ownerStack []*Owner

// Owner 是一个响应式作用域，持有其下的计算、子作用域与生命周期回调。
//
// 线程归属契约（评审 P1，方案 A）：Owner 的创建（CreateRoot/RunWithOwner）、
// Dispose、OnCleanup/OnMount 的注册与执行，以及 Computed/Effect 的创建，全部
// 只能在 UI Runtime 线程上进行。后台 goroutine 必须先经 Runtime.Dispatch 投递。
// 因此这些结构没有 goid 隔离、依赖单线程串行语义。
//
// 仍保留 mu：sync.Once（句柄 Dispose）、任务队列、App Close 等并发控制点确实需要
// 同步原语；「响应式单线程」不等于「整个程序无需锁」。mu 在这里另外
// 作为契约被意外破坏时的防御，不承诺跨线程并发 Dispose 的线性一致性。
type Owner struct {
	mu       sync.Mutex
	parent   *Owner
	children []*Owner
	comps    []*node
	cleanups []func()
	mounts   []func()
	mounted  bool
	disposed bool
	// values 是 Owner 上的 keyed 值挂载点（React Context 的落点）。
	values map[any]any
}

// ProvideValue 在当前 Owner 上挂一个 keyed 值，供后代沿 parent 链 LookupValue。
// 同一 key 重复 Provide 覆盖旧值（内层 Provider 覆盖外层）。o 为 nil 时 no-op。
func (o *Owner) ProvideValue(key, value any) {
	if o == nil {
		return
	}
	o.mu.Lock()
	if o.values == nil {
		o.values = make(map[any]any)
	}
	o.values[key] = value
	o.mu.Unlock()
}

// LookupValue 从 start 起沿 parent 链向上查找最近的 key，找到即返回 (v, true)。
// 这是 React Context「就近覆盖」的核心：内层 Owner 先命中，外层被遮蔽。
func LookupValue(start *Owner, key any) (any, bool) {
	for o := start; o != nil; {
		o.mu.Lock()
		v, ok := o.values[key]
		parent := o.parent
		o.mu.Unlock()
		if ok {
			return v, true
		}
		o = parent
	}
	return nil, false
}

// LookupValue 是包级 LookupValue 的方法形式，nil receiver 安全。
func (o *Owner) LookupValue(key any) (any, bool) { return LookupValue(o, key) }

// Computation 是可销毁的响应式计算句柄。Effect 返回它；Computed 也可通过
// ComputedWithHandle 取得。销毁是幂等的。
//
// 线程契约：Dispose 属于响应式生命周期操作，必须在 UI Runtime 线程调用；后台
// goroutine 请包装成 `app.Dispatch(func(){ h.Dispose() })`。句柄内的 sync.Once
// 仅保证幂等，不提供跨线程同步的额外承诺。
type Computation interface {
	Dispose()
}

// handle 把 node 包装成 Computation。
type handle struct {
	n    *node
	once sync.Once
}

func (h *handle) Dispose() {
	if h == nil {
		return
	}
	h.once.Do(func() {
		if caught := disposeNode(h.n); len(caught) > 0 {
			repanicDisposeErrors(caught) // 销毁语义：资源已全部释放后统一上报
		}
	})
}

// ---------- 销毁期异常隔离（评审 P1-01）----------
//
// 销毁语义与重跑语义不同：重跑时 cleanup panic 原样传播（withObserver 的明确
// 规则，剩余 fn 不执行、下次重跑重来）；销毁只有一次，某个用户 cleanup panic
// 绝不允许阻断其余资源的释放——被跳过的 effect 不会被再次销毁、被跳过的子
// Owner 清单已从父上摘除，中断即永久泄漏。因此销毁路径全面采用「单点捕获、
// 尽力释放、末尾统一上报」：单个 panic 被捕获收集，全部资源释放完毕后按
// 「单异常保真、多异常汇总」re-panic，异常不静默丢失。

// runFuncSafely 执行 fn 并捕获其 panic，返回收集到的 panic 值（通常 0 或 1 个）。
func runFuncSafely(fn func()) (caught []any) {
	defer func() {
		if r := recover(); r != nil {
			caught = append(caught, r)
		}
	}()
	fn()
	return nil
}

// repanicDisposeErrors 按销毁语义上报收集到的异常：单个异常原样 re-panic
// （保留类型与值，调用方的 errors.As/类型断言可用），多个异常包装成
// disposeErrors 汇总。caught 为空时 no-op。
func repanicDisposeErrors(caught []any) {
	switch len(caught) {
	case 0:
		return
	case 1:
		panic(caught[0])
	default:
		panic(disposeErrors(caught))
	}
}

// disposeErrors 是 Owner/handle 销毁期间多个 cleanup panic 的汇总载体。
type disposeErrors []any

func (e disposeErrors) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "dispose: %d 个 cleanup panic", len(e))
	for i, p := range e {
		fmt.Fprintf(&b, "\n  [%d] %v", i, p)
	}
	return b.String()
}

// disposeNode 把一个计算节点从依赖图中彻底摘除：断开所有上下游边，
// 标记 disposed，使后续调度不再执行它。返回销毁期间 cleanup 收集到的
// panic（资源释放不受单个 cleanup 异常影响）；调用方决定是否/如何上报。
func disposeNode(n *node) (caught []any) {
	if n == nil || n.disposed {
		return nil
	}
	checkUIThread("disposeNode") // 摘依赖边：必须 UI 线程
	n.disposed = true
	for dep := range n.deps {
		delete(dep.subs, n)
	}
	clear(n.deps)
	for sub := range n.subs {
		delete(sub.deps, n)
	}
	clear(n.subs)
	cleanups := n.runCleanups
	n.runCleanups = nil
	// 逆序执行，单个 panic 捕获收集、不阻断其余 cleanup。
	for _, c := range cleanups {
		caught = append(caught, runFuncSafely(c)...)
	}
	return caught
}

// CreateRoot 创建一个独立的响应式根作用域，f 内创建的 Effect/Computed 都归属它。
// f 返回后执行该 Owner 的 OnMount 回调（一次性）。返回的 Owner 可 Dispose 以级联销毁。
func CreateRoot(f func(o *Owner)) *Owner {
	o := &Owner{}
	var parent *Owner
	if len(ownerStack) > 0 {
		parent = ownerStack[len(ownerStack)-1]
	}
	ownerStack = append(ownerStack, o)

	if parent != nil {
		o.parent = parent
		parent.mu.Lock()
		if !parent.disposed {
			parent.children = append(parent.children, o)
		}
		parent.mu.Unlock()
	}

	defer func() {
		ownerStack = ownerStack[:len(ownerStack)-1]
	}()

	if f != nil {
		f(o)
	}
	runMounts(o)
	return o
}

// GetOwner 返回当前 Owner 栈顶，无作用域时返回 nil。
func GetOwner() *Owner {
	if len(ownerStack) == 0 {
		return nil
	}
	return ownerStack[len(ownerStack)-1]
}

// RunWithOwner 在指定 Owner 下执行 f，f 内创建的计算与注册的 cleanup 归属 o。
func RunWithOwner(o *Owner, f func()) { runWithOwner(o, f) }

// runWithOwner 带「栈已由 withObserver 预压」检测：若 o 正是栈顶则跳过重复压栈，
// 避免 defer 交错弹出打乱 Owner 栈的 LIFO 顺序。
func runWithOwner(o *Owner, f func()) {
	if o == nil {
		if f != nil {
			f()
		}
		return
	}
	alreadyTop := len(ownerStack) > 0 && ownerStack[len(ownerStack)-1] == o
	if !alreadyTop {
		ownerStack = append(ownerStack, o)
	}
	defer func() {
		if alreadyTop {
			return
		}
		ownerStack = ownerStack[:len(ownerStack)-1]
	}()
	if f != nil {
		f()
	}
}

// OnCleanup 注册一个回调，在「当前计算节点」重新执行前或销毁时执行。
// 优先归属正在执行的 effect/computed（每次重跑前先清理）；若不在计算内但在
// 某 Owner 中，则归属 Owner（仅销毁时执行）。
func OnCleanup(fn func()) {
	if fn == nil {
		return
	}
	if len(observers) > 0 {
		n := observers[len(observers)-1]
		if !n.disposed {
			n.runCleanups = append(n.runCleanups, fn)
		}
		return
	}
	o := GetOwner()
	if o == nil {
		return
	}
	o.mu.Lock()
	if o.disposed {
		o.mu.Unlock()
		fn() // 已销毁：立即执行，等价于「错过时机就补做」
		return
	}
	o.cleanups = append(o.cleanups, fn)
	o.mu.Unlock()
}

// OnMount 注册一个回调，在所属 Owner 首次挂载完成后执行一次。
func OnMount(fn func()) {
	if fn == nil {
		return
	}
	o := GetOwner()
	if o == nil {
		return
	}
	o.mu.Lock()
	if o.mounted {
		o.mu.Unlock()
		fn() // 已挂载：立即补做
		return
	}
	o.mounts = append(o.mounts, fn)
	o.mu.Unlock()
}

func runMounts(o *Owner) {
	o.mu.Lock()
	if o.mounted {
		o.mu.Unlock()
		return
	}
	o.mounted = true
	mounts := o.mounts
	o.mounts = nil
	o.mu.Unlock()
	for _, fn := range mounts {
		fn()
	}
}

// Dispose 销毁该 Owner：先销毁其下所有计算（含子 Owner），再逆序执行 cleanup。幂等。
//
// 销毁期异常隔离（评审 P1-01）：三个阶段（computation、子 Owner、cleanup）全部
// 单点捕获收集——任何用户 cleanup panic 都不会阻断其余资源的释放。全部释放完毕
// 后统一上报：单个 panic 原样 re-panic，多个包装成 disposeErrors。此前的实现是
// 首个 panic 直接中断，而 comps/children/cleanups 清单已从 Owner 上摘除，中断即
// 永久泄漏（再次 Dispose 也无法补做）。
func (o *Owner) Dispose() {
	if o == nil {
		return
	}
	o.mu.Lock()
	if o.disposed {
		o.mu.Unlock()
		return
	}
	o.disposed = true
	comps := o.comps
	children := o.children
	cleanups := o.cleanups
	o.comps = nil
	o.children = nil
	o.cleanups = nil
	o.mounts = nil
	o.mu.Unlock()

	var caught []any
	// 阶段 1：销毁所有 computation。disposeNode 已隔离单个 cleanup panic。
	for _, n := range comps {
		caught = append(caught, disposeNode(n)...)
	}
	// 阶段 2：销毁子 Owner。子 Dispose 自身会隔离并汇总上报——父在此捕获，
	// 作为一条记录并入本层汇总，保证兄弟子树不受影响。
	for _, ch := range children {
		caught = append(caught, runFuncSafely(ch.Dispose)...)
	}
	// 阶段 3：逆序执行 Owner 级 cleanup，单点捕获。
	for i := len(cleanups) - 1; i >= 0; i-- {
		caught = append(caught, runFuncSafely(cleanups[i])...)
	}
	// 阶段 4：全部资源已释放，统一上报。
	repanicDisposeErrors(caught)
}

// attachToOwner 把新建计算节点登记到当前 Owner，供其销毁时级联清理。
func attachToOwner(n *node) {
	if len(ownerStack) == 0 {
		return
	}
	o := ownerStack[len(ownerStack)-1]
	n.owner = o
	o.mu.Lock()
	if !o.disposed {
		o.comps = append(o.comps, n)
	}
	o.mu.Unlock()
}

// newChildOwner 在给定 Owner 下创建一个子 Owner 并登记。
func newChildOwner(parent *Owner) *Owner {
	ch := &Owner{parent: parent}
	if parent == nil {
		return ch
	}
	parent.mu.Lock()
	if !parent.disposed {
		parent.children = append(parent.children, ch)
	}
	parent.mu.Unlock()
	return ch
}

// ---------- 批处理 ----------

var (
	batchDepth    int     // Batch 嵌套深度（单实例）
	pendingEffect []*node // 待执行 Effect，去重（单实例）
	batchEpoch    uint64  // pending 去重 epoch
)

// Batch 把 f 内的多次 signal 写入合并成一批：期间被标脏的 Effect 只收集不执行，
// f 结束后统一调度一次。嵌套 Batch 只在最外层结束时冲刷。
// Batch 只负责合并更新，不做线程切换——线程切换由 Runtime.Dispatch 负责。
//
// 去重（评审 P1）：epoch 在**每个 Batch 开始时仅递增一次**，同一批内所有写入
// 共用它。Effect 用 batchMark 记录自己是否已入队，天然保证每批最多入队一次，
// 避免旧的「每次写入都重扫整个 pendingEffect 列表并重新打标」的 O(n²) 扫描。
//
// panic 语义（明确规则）：f 内 panic 时 defer 仍会冲刷本批已收集的 Effect（已写入
// 的 Signal 值保留、Effect 正常执行），随后 panic 原样向上传播。这与「写入已生效
// 就不该丢失」的直觉一致；如需原子回滚请用更高层事务，Batch 不提供。
func Batch(f func()) {
	if f == nil {
		return
	}
	batchDepth++
	if batchDepth == 1 {
		// 新的一批：递增 epoch。同一批（含嵌套）共用，Effect 首次入队即打标。
		batchEpoch++
	}
	defer func() {
		batchDepth--
		if batchDepth != 0 {
			return
		}
		pending := pendingEffect
		pendingEffect = nil
		// 在帧边界内冲刷：一批 effect 全部跑完后统一触发 AfterEffects hooks。
		// 无 pending（本批写入都被相等性短路）时不进边界，避免触发无效帧。
		if len(pending) > 0 {
			withFlush(func() {
				for _, e := range pending {
					scheduleEffect(e)
				}
			})
		}
	}()
	f()
}

// dispatchEffects 在写信号后调用：批处理中则收集去重，否则立即调度。
func dispatchEffects(effects []*node) {
	if len(effects) == 0 {
		return
	}
	if batchDepth > 0 {
		// epoch 由 Batch 入口统一推进；此处只按节点 batchMark 去重，不再重扫
		// 整个 pendingEffect 列表（评审 P1）。
		epoch := batchEpoch
		for _, e := range effects {
			if e.batchMark == epoch {
				continue
			}
			e.batchMark = epoch
			pendingEffect = append(pendingEffect, e)
		}
		return
	}
	// 帧边界内调度：本轮写触发的 effect 跑完后统一触发 AfterEffects hooks。
	withFlush(func() {
		for _, e := range effects {
			scheduleEffect(e)
		}
	})
}
