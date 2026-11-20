// Package signal 是 GX 响应式 signal 语法糖的运行时。
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
//
// 并发安全：全局互斥锁保护各 goroutine 的观察者栈与节点的订阅/依赖集合；
// 用户回调（compute/effect 函数体）在锁外执行，回调内仍可安全地读写 signal。
// 观察者栈按 goroutine id 隔离（observers map），避免并发下两个 withObserver
// 互相覆盖导致依赖登记到错误节点（H-8）。
package signal

import (
	"runtime"
	"sync"
)

// Signal[T] 是读写一体的 signal。无参调用读，返回当前值；带参调用写第一个参数。
type Signal[T any] func(...T) T

// ---------- 依赖追踪核心（非泛型） ----------

var (
	mu        sync.Mutex
	recalc    = sync.NewCond(&mu)  // computed 重算完成广播，供并发读者等待（H-9）
	observers = map[uint64]*node{} // 各 goroutine 当前的观察者栈顶，按 goroutine id 隔离（H-8）
)

// goid 返回当前 goroutine 的 id。通过 runtime.Stack 首行 "goroutine N ..." 解析。
// 用于把观察者栈按 goroutine 隔离——Go 没有官方 goroutine-local 存储，这是
// 社区通用的取值方式。栈缓冲 64 字节足够容纳首行的 goroutine 编号。
func goid() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// 形如 "goroutine 123 [running]:\n..."
	const prefix = "goroutine "
	if n < len(prefix) || string(buf[:len(prefix)]) != prefix {
		return 0
	}
	var id uint64
	for i := len(prefix); i < n; i++ {
		c := buf[i]
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + uint64(c-'0')
	}
	return id
}

// node 是一个依赖图节点：源 signal、computed 或 effect 共用。
type node struct {
	subs        map[*node]struct{} // 下游：谁依赖我
	deps        map[*node]struct{} // 上游：我依赖谁
	dirty       bool               // computed 的缓存是否失效
	computing   bool               // computed 是否正在重算（循环依赖检测）
	computingBy uint64             // 正在重算的 goroutine id（区分自身重入 vs 其它 goroutine）
	isEffect    bool               // 是否为 effect（写时直接重跑，而非仅标脏）
	run         func()             // computed 的重算 / effect 的函数体
	prev        *node              // 观察者栈中前一个观察者
}

func newNode(isEffect bool) *node {
	return &node{
		subs:     make(map[*node]struct{}),
		deps:     make(map[*node]struct{}),
		dirty:    true,
		isEffect: isEffect,
	}
}

// track 在 mu 持有期间调用：把当前 goroutine 的观察者登记为 n 的下游。
func track(n *node) {
	if ob := observers[goid()]; ob != nil {
		ob.deps[n] = struct{}{}
		n.subs[ob] = struct{}{}
	}
}

// clearDeps 在 mu 持有期间调用：清空节点的上游依赖（重跑前调用，避免陈旧依赖）。
func clearDeps(n *node) {
	for dep := range n.deps {
		delete(dep.subs, n)
	}
	n.deps = make(map[*node]struct{})
}

// collectDirty 在 mu 持有期间调用：从 n 向下游传播脏标记，返回需要重跑的 effect。
// computed 节点被标脏后继续向下传播；effect 节点收集去重后返回（锁外执行）。
func collectDirty(n *node) []*node {
	var effects []*node
	seen := make(map[*node]bool)
	var walk func(*node)
	walk = func(cur *node) {
		for sub := range cur.subs {
			if seen[sub] {
				continue
			}
			seen[sub] = true
			if sub.isEffect {
				effects = append(effects, sub)
			} else {
				sub.dirty = true
				walk(sub)
			}
		}
	}
	walk(n)
	return effects
}

// withObserver 在锁外执行 fn，执行期间把 n 设为「当前 goroutine」的观察者并重建其依赖集合。
// 观察者栈按 goroutine id 隔离：并发下各 goroutine 的 withObserver 操作各自的栈顶，
// 不会互相覆盖导致依赖登记到错误节点（H-8）。
// 恢复用 defer 保证：即便 fn panic（如循环依赖检测触发），当前 goroutine 的栈也不会被污染（M-10）。
func withObserver(n *node, fn func()) {
	id := goid()
	mu.Lock()
	n.prev = observers[id]
	observers[id] = n
	clearDeps(n)
	mu.Unlock()

	defer func() {
		mu.Lock()
		observers[id] = n.prev
		n.prev = nil
		mu.Unlock()
	}()

	fn() // 用户回调在锁外执行，回调内读写 signal 会再次获取锁——不会自锁
}

// ---------- 公开 API ----------

// New 创建一个值为 v 的源 signal。
func New[T any](v T) Signal[T] {
	st := &state[T]{n: newNode(false), value: v}
	return func(args ...T) T {
		if len(args) > 0 {
			mu.Lock()
			st.value = args[0]
			effects := collectDirty(st.n)
			mu.Unlock()
			for _, e := range effects {
				e.run()
			}
		}
		mu.Lock()
		track(st.n)
		v := st.value
		mu.Unlock()
		return v
	}
}

// Computed 创建一个派生 signal：f 的结果被缓存，仅在依赖变化时惰性重算。
// 派生 signal 是只读的（带参调用写入被忽略）。
func Computed[T any](f func() T) Signal[T] {
	st := &state[T]{n: newNode(false)}
	recompute := func() {
		id := goid()
		mu.Lock()
		for st.n.computing && st.n.computingBy != id {
			// 别的 goroutine 正在重算：等待其完成，而不是重复执行 f()（H-9）。
			// cond 唤醒后回到循环重新判定 dirty——若对方已完成且已清 dirty，直接返回。
			recalc.Wait()
		}
		if !st.n.dirty {
			mu.Unlock()
			return
		}
		if st.n.computing && st.n.computingBy == id {
			// 同一 goroutine 重入到这里说明 f() 直接或间接读取了自身（如
			// c = Computed(func() int { return c()+1 })），形成环。立即 panic
			// 阻止无限递归导致的栈溢出（栈溢出不可 recover，普通 panic 可）。
			mu.Unlock()
			panic("gx/signal: 检测到循环依赖：computed 直接或间接读取了自身")
		}
		st.n.computing = true
		st.n.computingBy = id
		mu.Unlock()

		// result 在锁外计算（f 内会读其它 signal，需能再次获取 mu），
		// 但写回 st.value 必须在锁内，避免与并发读者的数据竞争（H-9）。
		var result T
		panicked := true
		func() {
			defer func() {
				mu.Lock()
				st.n.computing = false
				st.n.computingBy = 0
				recalc.Broadcast()
				mu.Unlock()
			}()
			withObserver(st.n, func() { result = f() })
			panicked = false
		}()
		if panicked {
			return // f() panic：result 无效，不写回；computing 已由 defer 复位
		}

		mu.Lock()
		st.value = result
		st.n.dirty = false
		mu.Unlock()
	}
	st.n.run = recompute
	return func(args ...T) T {
		if len(args) > 0 {
			mu.Lock()
			v := st.value // 只读：忽略写入
			mu.Unlock()
			return v
		}
		recompute()
		mu.Lock()
		track(st.n)
		v := st.value
		mu.Unlock()
		return v
	}
}

// Effect 立即执行一次 f，并在其依赖的任意 signal 变化时自动重跑。
// f 在每次执行前会重建依赖集合，因此条件分支里的依赖会被正确增删。
func Effect(f func()) {
	n := newNode(true)
	run := func() { withObserver(n, f) }
	n.run = run
	run() // 首次立即执行
}

// state 保存 signal 的节点与当前值。
type state[T any] struct {
	n     *node
	value T
}
