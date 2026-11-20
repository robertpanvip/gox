// Owner、Cleanup 与批处理调度。
//
// SolidJS 的核心不是 Signal 本身，而是「响应式计算所处的Owner树」：每个
// Effect/Computed 都属于一个 Owner，Owner 销毁时级联销毁其下所有计算并执行
// cleanup。这让条件分支切换、列表元素删除等场景能够精确释放副作用，而不会
// 残留永不停止的 Effect。
//
// 设计要点：
//   - Owner 形成树，父销毁递归销毁子；
//   - Computation 是 Effect/Computed 的可销毁句柄（Dispose 幂等）；
//   - OnCleanup / OnMount 注册到当前 Owner（按 goroutine 隔离的栈）；
//   - Batch 合并同一轮内的多次写，仅在批次结束时统一调度 Effect。
package signal

import "sync"

// 当前 goroutine 的 Owner 栈——与 observers 栈同样按 goroutine id 隔离，
// 避免并发建立响应式作用域时互相污染。
var ownerStacks = map[uint64][]*Owner{}

// Owner 是一个响应式作用域，持有其下的计算、子作用域与生命周期回调。
type Owner struct {
	mu       sync.Mutex
	parent   *Owner
	children []*Owner
	comps    []*node
	cleanups []func()
	mounts   []func()
	mounted  bool
	disposed bool
}

// Computation 是可销毁的响应式计算句柄。Effect 返回它；Computed 也可通过
// ComputedWithHandle 取得。销毁是幂等的。
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
	h.once.Do(func() { disposeNode(h.n) })
}

// disposeNode 把一个计算节点从依赖图中彻底摘除：断开所有上下游边，
// 标记 disposed，使后续调度不再执行它。
func disposeNode(n *node) {
	if n == nil {
		return
	}
	mu.Lock()
	if n.disposed {
		mu.Unlock()
		return
	}
	n.disposed = true
	for dep := range n.deps {
		delete(dep.subs, n)
	}
	n.deps = make(map[*node]struct{})
	for sub := range n.subs {
		delete(sub.deps, n)
	}
	n.subs = make(map[*node]struct{})
	cleanups := n.runCleanups
	n.runCleanups = nil
	mu.Unlock()
	for _, c := range cleanups {
		c() // 锁外执行用户 cleanup
	}
}

// CreateRoot 创建一个独立的响应式根作用域，在 f 内创建的 Effect/Computed
// 都归属它。f 返回后执行该 Owner 的 OnMount 回调（一次性）。返回的 Owner
// 可随时 Dispose 以级联销毁整棵子树。
func CreateRoot(f func(o *Owner)) *Owner {
	o := &Owner{}
	id := goid()

	mu.Lock()
	st := ownerStacks[id]
	// 若在某个 Owner 内嵌套创建，则挂为其子作用域，随父一起销毁。
	var parent *Owner
	if len(st) > 0 {
		parent = st[len(st)-1]
	}
	ownerStacks[id] = append(ownerStacks[id], o)
	mu.Unlock()

	if parent != nil {
		parent.mu.Lock()
		if !parent.disposed {
			parent.children = append(parent.children, o)
		}
		parent.mu.Unlock()
	}

	defer func() {
		mu.Lock()
		st := ownerStacks[id]
		if len(st) <= 1 {
			delete(ownerStacks, id)
		} else {
			ownerStacks[id] = st[:len(st)-1]
		}
		mu.Unlock()
	}()

	if f != nil {
		f(o)
	}
	runMounts(o)
	return o
}

// GetOwner 返回当前 goroutine 所在的 Owner（栈顶），无作用域时返回 nil。
func GetOwner() *Owner {
	mu.Lock()
	defer mu.Unlock()
	st := ownerStacks[goid()]
	if len(st) == 0 {
		return nil
	}
	return st[len(st)-1]
}

// RunWithOwner 在指定 Owner 下执行 f，f 内创建的计算与注册的 cleanup 归属 o。
func RunWithOwner(o *Owner, f func()) {
	if o == nil {
		if f != nil {
			f()
		}
		return
	}
	id := goid()
	mu.Lock()
	ownerStacks[id] = append(ownerStacks[id], o)
	mu.Unlock()

	defer func() {
		mu.Lock()
		st := ownerStacks[id]
		if len(st) <= 1 {
			delete(ownerStacks, id)
		} else {
			ownerStacks[id] = st[:len(st)-1]
		}
		mu.Unlock()
	}()

	if f != nil {
		f()
	}
}

// OnCleanup 注册一个回调，在「当前计算节点」重新执行前或销毁时执行。
// SolidJS 语义：优先归属正在执行的 effect/computed（每次重跑前先清理）；
// 若不在任何计算内但在某 Owner 中，则归属 Owner（仅销毁时执行）。
func OnCleanup(fn func()) {
	if fn == nil {
		return
	}
	id := goid()
	mu.Lock()
	// 优先归属当前计算节点（观察者栈顶），这样重跑前会执行并清空。
	if st := observers[id]; len(st) > 0 {
		n := st[len(st)-1]
		if !n.disposed {
			n.runCleanups = append(n.runCleanups, fn)
		}
		mu.Unlock()
		return
	}
	mu.Unlock()

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

// Dispose 销毁该 Owner：先销毁其下所有计算（含子 Owner），再逆序执行 cleanup。
// 幂等。
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

	for _, n := range comps {
		disposeNode(n)
	}
	for _, ch := range children {
		ch.Dispose()
	}
	for i := len(cleanups) - 1; i >= 0; i-- {
		cleanups[i]()
	}
}

// attachToOwner 把新建的计算节点登记到当前 Owner，供其销毁时级联清理。
func attachToOwner(n *node) {
	id := goid()
	mu.Lock()
	st := ownerStacks[id]
	var o *Owner
	if len(st) > 0 {
		o = st[len(st)-1]
	}
	mu.Unlock()
	if o == nil {
		return
	}
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
	batchDepth    = map[uint64]int{}     // 每 goroutine 的 Batch 嵌套深度
	pendingEffect = map[uint64][]*node{} // 每 goroutine 待执行的 Effect（去重）
)

// Batch 把 f 内的多次 signal 写入合并成一批：期间被标脏的 Effect 只收集不执行，
// f 结束后统一调度一次。嵌套 Batch 只在最外层结束时冲刷。
func Batch(f func()) {
	if f == nil {
		return
	}
	id := goid()
	mu.Lock()
	batchDepth[id]++
	mu.Unlock()

	defer func() {
		mu.Lock()
		batchDepth[id]--
		if batchDepth[id] != 0 {
			mu.Unlock()
			return
		}
		delete(batchDepth, id)
		pending := pendingEffect[id]
		delete(pendingEffect, id)
		mu.Unlock()
		for _, e := range pending {
			scheduleEffect(e)
		}
	}()

	f()
}

// dispatchEffects 在写信号后（锁外）调用：批处理中则收集去重，否则立即调度。
func dispatchEffects(effects []*node) {
	if len(effects) == 0 {
		return
	}
	id := goid()
	mu.Lock()
	if batchDepth[id] > 0 {
		seen := make(map[*node]bool)
		for _, e := range pendingEffect[id] {
			seen[e] = true
		}
		for _, e := range effects {
			if !seen[e] {
				seen[e] = true
				pendingEffect[id] = append(pendingEffect[id], e)
			}
		}
		mu.Unlock()
		return
	}
	mu.Unlock()
	for _, e := range effects {
		scheduleEffect(e)
	}
}
