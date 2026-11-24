// Package signal 是 GX 响应式 signal 语法糖的运行时。
//
// 线程模型（GX 响应式系统的正式设计原则）：
//
//	响应式系统是单线程的，业务任务可以是多线程的。
//
// Signal 读写、Computed 重算、Effect 执行、Owner 生命周期全部发生在唯一的
// UI Runtime 执行上下文中。因此依赖追踪不需要 goroutine id、不需要全局锁、
// 不需要 runtime.Stack 抓栈——Signal 读热路径只有一次切片判空 + 一次 map 写入。
// 后台 goroutine（网络 / IO / 定时器）不得直接读写响应式 Signal，而应通过
// Signal.Safe(fn) 把更新闭包投递给 UI Runtime；Signal.Batch(fn) 只负责在同一
// 上下文内合并一批写入，不做线程切换。
//
// GX 写法（signal/let/effect）：
//
//	signal count = 0
//	let doubled = Signal.computed(() => count * 2)
//	Signal.effect(() => println(count))
//	count = 5                       // 写 → effect 自动重跑
//
// 转译后用「函数式 signal」表达读写，无需 .get()/.set()：
//
//	count := Signal.New(0)                                   // Signal[int]
//	doubled := Signal.Computed(func() int { return count()*2 })
//	Signal.Effect(func() { println(count()) })
//	count(5)                          // 无参调用=读，带参调用=写
//
// Signal[T] 是一个 func(...T) T：无参调用读当前值，带参调用写入新值并
// 触发依赖它的 computed/effect 失效与重跑。依赖追踪采用「推拉结合」：
//   - 读 signal/computed 时，若存在当前观察者，登记依赖边；
//   - 写 signal 时，向下游传播脏标记（computed）并收集 effect 重跑；
//   - computed 惰性求值：脏标记只在真正被读时触发重算，避免中间抖动。
package signal

import (
	"reflect"
	"sync"
)

// Signal[T] 是读写一体的 signal。无参调用读，返回当前值；带参调用写第一个参数。
type Signal[T any] func(...T) T

// ---------- 依赖追踪核心（单线程，无锁） ----------

// observers 是当前观察者栈，栈顶即正在重算的 computed/effect。
// 严格单线程模型下它只被 UI Runtime 访问，无需按 goroutine 分桶，也无需加锁。
var observers []*node

// node 是一个依赖图节点：源 signal、computed 或 effect 共用。
type node struct {
	subs        map[*node]struct{} // 下游：谁依赖我
	deps        map[*node]struct{} // 上游：我依赖谁
	dirty       bool               // computed 的缓存是否失效
	version     uint64             // 失效版本号：检测「计算期间又变脏」
	computing   bool               // computed 是否正在重算（单线程重入即环）
	isEffect    bool               // 是否为 effect（写时直接重跑，而非仅标脏）
	run         func()             // computed 的重算 / effect 的函数体
	running     bool               // effect 是否正在执行（合并执行期间的重跑请求）
	queued      bool               // 执行期间又收到重跑请求
	owner       *Owner             // 所属响应式作用域，销毁时级联 Dispose
	disposed    bool               // 已销毁：不再重跑、不再登记依赖
	equals      func(a, b any) bool
	runCleanups []func()
	mark        uint64 // collectDirty 去重标记（epoch）
	batchMark   uint64 // Batch 收集 pending 时去重标记（epoch）
}

func newNode(isEffect bool) *node {
	return &node{
		subs:     make(map[*node]struct{}),
		deps:     make(map[*node]struct{}),
		dirty:    true,
		isEffect: isEffect,
	}
}

// track 把当前观察者栈顶登记为 n 的下游。调用方已确认 observers 非空。
func track(n *node) {
	ob := observers[len(observers)-1]
	ob.deps[n] = struct{}{}
	n.subs[ob] = struct{}{}
}

// markDirty 把一个 computed 节点标脏并递增失效版本号。
func markDirty(n *node) {
	n.dirty = true
	n.version++
}

// clearDeps 清空 n 的上游依赖。复用原 map（clear 保留 bucket），避免每次
// 重跑都 make 新 map 制造持续的 GC 压力。
func clearDeps(n *node) {
	for dep := range n.deps {
		delete(dep.subs, n)
	}
	clear(n.deps)
}

var dirtyEpoch uint64

// collectDirty 从 n 向下游传播脏标记，返回需要重跑的 effect。
// 用节点上的 mark + 全局 epoch 去重，零 map 分配（相比旧的每次 new seen map）。
func collectDirty(n *node) []*node {
	if len(n.subs) == 0 {
		return nil
	}
	dirtyEpoch++
	epoch := dirtyEpoch
	var effects []*node
	var walk func(*node)
	walk = func(cur *node) {
		for sub := range cur.subs {
			if sub.mark == epoch {
				continue
			}
			sub.mark = epoch
			if sub.isEffect {
				effects = append(effects, sub)
			} else {
				markDirty(sub)
				walk(sub)
			}
		}
	}
	walk(n)
	return effects
}

// withObserver 执行 fn，期间把 n 压入观察者栈并重建依赖集合。
// 恢复用 defer 保证：即便 fn panic（如循环依赖检测触发），栈也不会被污染。
func withObserver(n *node, fn func()) {
	observers = append(observers, n)
	clearDeps(n)
	cleanups := n.runCleanups
	n.runCleanups = nil
	for _, c := range cleanups {
		c() // 每次重跑前执行上一轮的 onCleanup（SolidJS 语义）
	}
	defer func() {
		observers = observers[:len(observers)-1]
	}()
	// 恢复创建时的 Owner 作用域：重跑/条件分支/列表重建子树时 fn 内会
	// 新建计算、注册 cleanup，并沿 Owner 链 Lookup context。
	runWithOwner(n.owner, fn)
}

// ---------- 公开 API ----------

// New 创建一个值为 v 的源 signal。相等性判断对可比较类型走 `==` 快路径，
// 不可比较类型（slice/map/func）回退 reflect.DeepEqual——避免无谓的深比较。
// 写入值未变化时短路，不触发下游。需要「每次写都通知」请用
// NewWithEquals(v, func(a,b T) bool { return false })。
func New[T any](v T) Signal[T] {
	return NewWithEquals(v, defaultEquals[T])
}

// defaultEquals 动态类型相同且可比较时用 ==，否则用 reflect.DeepEqual。
// 用动态类型判断而非类型参数约束，使 interface{} 类型 signal 也安全
// （写入不同动态类型→不相等；写入不可比较动态值→深比较，不会 panic）。
func defaultEquals[T any](a, b T) bool {
	av, bv := any(a), any(b)
	ta, tb := reflect.TypeOf(av), reflect.TypeOf(bv)
	if ta != tb {
		return false
	}
	if ta == nil || ta.Comparable() {
		return av == bv
	}
	return reflect.DeepEqual(a, b)
}

// NewWithEquals 用自定义相等策略创建 signal。equals(a,b) 返回 true 表示两值
// 相等，写入被忽略、不触发下游。equals 为 nil 时退化为「永不相等」（每次写都通知）。
func NewWithEquals[T any](v T, equals func(a, b T) bool) Signal[T] {
	n := newNode(false)
	if equals != nil {
		eq := equals
		n.equals = func(a, b any) bool {
			av, aok := a.(T)
			bv, bok := b.(T)
			if !aok || !bok {
				return false
			}
			return eq(av, bv)
		}
	}
	st := &state[T]{n: n, value: v}
	return func(args ...T) T {
		if len(args) > 0 {
			next := args[0]
			if n.equals != nil && n.equals(st.value, next) {
				return st.value // 相等：跳过标脏与调度
			}
			st.value = next
			dispatchEffects(collectDirty(st.n))
		}
		// 读热路径：无观察者时只做一次切片判空，直接返回——无锁、无 goid、
		// 无原子操作。 observers 非空说明当前在 computed/effect 内，登记依赖。
		if len(observers) > 0 {
			track(st.n)
		}
		return st.value
	}
}

// LockedGetter 把一个「读跟随变量、写落回变量」的 getter 包装成并发安全的 Signal[T]。
//
// 编译器为 struct 的 signal 字段生成的 getter 读写的是捕获的普通变量，
// 并非核心响应式节点。保留实例级锁仅为兼容这类「跟随普通变量」的写法，
// 使经由该 getter 的访问串行化（用户代码在别处直接读写同一变量需自行同步）。
// 需要完整响应式请改用 New/Computed 创建的真 signal 源。
func LockedGetter[T any](get func() T, set func(T)) Signal[T] {
	var mu sync.Mutex
	return func(args ...T) T {
		mu.Lock()
		defer mu.Unlock()
		if len(args) > 0 {
			set(args[0])
		}
		return get()
	}
}

// lockedMuRegistry 让「跟随同一变量」的多个 getter 复用同一把互斥锁。
// key 由编译器按被跟随变量的声明位置生成，保证同实例多字段/跨实例共享同一
// 变量时访问互斥。
var lockedMuRegistry sync.Map // map[string]*sync.Mutex

// LockedGetterShared 与 LockedGetter 语义相同，但按 key 共享互斥锁。
func LockedGetterShared[T any](key string, get func() T, set func(T)) Signal[T] {
	muAny, _ := lockedMuRegistry.LoadOrStore(key, new(sync.Mutex))
	mu := muAny.(*sync.Mutex)
	return func(args ...T) T {
		mu.Lock()
		defer mu.Unlock()
		if len(args) > 0 {
			set(args[0])
		}
		return get()
	}
}

// Computed 创建一个派生 signal：f 的结果被缓存，仅在依赖变化时惰性重算。
// 派生 signal 是只读的（带参调用写入会 panic）。
func Computed[T any](f func() T) Signal[T] {
	s, _ := ComputedWithHandle(f)
	return s
}

// ComputedWithHandle 创建派生 signal 并返回其 Computation 句柄。句柄可 Dispose
// 以断开依赖；若在某个 Owner 内创建，Owner 销毁时级联销毁。
func ComputedWithHandle[T any](f func() T) (Signal[T], Computation) {
	st := &state[T]{n: newNode(false)}
	attachToOwner(st.n)
	recompute := func() {
		if !st.n.dirty {
			return
		}
		// 单线程下 computing 即可区分「首次重算」与「重入成环」：同一个调用栈
		// 再次进入说明 f() 直接或间接读取了自身。立即 panic 阻止无限递归。
		if st.n.computing {
			panic("gx/signal: 检测到循环依赖：computed 直接或间接读取了自身")
		}
		st.n.computing = true
		defer func() { st.n.computing = false }()

		for {
			startVersion := st.n.version
			var result T
			panicked := true
			func() {
				withObserver(st.n, func() { result = f() })
				panicked = false
			}()
			if panicked {
				return // f() panic：result 无效，不写回
			}
			if st.n.version != startVersion {
				continue // 计算期间又脏：丢弃旧结果，保持 dirty 并重试
			}
			st.value = result
			st.n.dirty = false
			return
		}
	}
	st.n.run = recompute
	sig := func(args ...T) T {
		if len(args) > 0 {
			panic("signal.Computed: computed 值是只读的，不能写入")
		}
		recompute()
		if len(observers) > 0 {
			track(st.n)
		}
		return st.value
	}
	return sig, &handle{n: st.n}
}

// scheduleEffect 串行调度一个 effect 的执行。单线程下同一 effect 不会并发，
// 但执行期间写信号可能递归触发本 effect 的重跑请求：running 期间只记 queued，
// 本次结束后合并重跑一次，避免重复抖动。panic 时调度状态仍由 defer 复位。
func scheduleEffect(e *node) {
	if e.disposed {
		return
	}
	if e.running {
		e.queued = true
		return
	}
	e.running = true
	defer func() {
		e.running = false
		e.queued = false
	}()
	for {
		e.run()
		if e.queued {
			e.queued = false
			continue
		}
		return
	}
}

// Effect 立即执行一次 f，并在其依赖的任意 signal 变化时自动重跑。
// f 每次执行前重建依赖集合，因此条件分支里的依赖会被正确增删。
// 返回 Computation 句柄：Dispose() 可停止该 effect 并清理订阅；若在某个
// Owner 内创建，Owner 销毁时也会级联销毁它。
func Effect(f func()) Computation {
	n := newNode(true)
	n.run = func() { withObserver(n, f) }
	attachToOwner(n)
	scheduleEffect(n) // 首次立即执行
	return &handle{n: n}
}

// EffectWithOwner 在指定 Owner 下创建 effect，返回其句柄。
func EffectWithOwner(o *Owner, f func()) Computation {
	var h Computation
	RunWithOwner(o, func() { h = Effect(f) })
	return h
}

// state 保存 signal 的节点与当前值。单线程模型下 value 是普通字段——
// 只有 UI Runtime 会读写它，无需原子指针（省去每次写的装箱分配）。
type state[T any] struct {
	n     *node
	value T
}
