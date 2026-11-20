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
// 并发安全：全局互斥锁保护观察者栈与各节点的订阅/依赖集合；用户回调
// （compute/effect 函数体）在锁外执行，回调内仍可安全地读写 signal。
package signal

import "sync"

// Signal[T] 是读写一体的 signal。无参调用读，返回当前值；带参调用写第一个参数。
type Signal[T any] func(...T) T

// ---------- 依赖追踪核心（非泛型） ----------

var (
	mu       sync.Mutex
	observer *node // 当前正在收集依赖的观察者（栈顶），通过 node.prev 串成栈
)

// node 是一个依赖图节点：源 signal、computed 或 effect 共用。
type node struct {
	subs     map[*node]struct{} // 下游：谁依赖我
	deps     map[*node]struct{} // 上游：我依赖谁
	dirty    bool               // computed 的缓存是否失效
	isEffect bool               // 是否为 effect（写时直接重跑，而非仅标脏）
	run      func()             // computed 的重算 / effect 的函数体
	prev     *node              // 观察者栈中前一个观察者
}

func newNode(isEffect bool) *node {
	return &node{
		subs:     make(map[*node]struct{}),
		deps:     make(map[*node]struct{}),
		dirty:    true,
		isEffect: isEffect,
	}
}

// track 在 mu 持有期间调用：把当前观察者登记为 n 的下游。
func track(n *node) {
	if observer != nil {
		observer.deps[n] = struct{}{}
		n.subs[observer] = struct{}{}
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

// withObserver 在锁外执行 fn，执行期间把 n 设为当前观察者并重建其依赖集合。
func withObserver(n *node, fn func()) {
	mu.Lock()
	n.prev = observer
	observer = n
	clearDeps(n)
	mu.Unlock()

	fn() // 用户回调在锁外执行，回调内读写 signal 会再次获取锁——不会自锁

	mu.Lock()
	observer = n.prev
	n.prev = nil
	mu.Unlock()
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
		mu.Lock()
		if !st.n.dirty {
			mu.Unlock()
			return
		}
		mu.Unlock()
		withObserver(st.n, func() {
			st.value = f()
		})
		mu.Lock()
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
