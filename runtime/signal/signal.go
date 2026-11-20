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
	recalc    = sync.NewCond(&mu)    // computed 重算完成广播，供并发读者等待（H-9）
	observers = map[uint64][]*node{} // 各 goroutine 的观察者栈，按 goroutine id 隔离（H-8/P1-3）
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
	version     uint64             // 失效版本号：每次被标脏递增，用于检测「计算期间又变脏」（P1-2）
	computing   bool               // computed 是否正在重算（循环依赖检测）
	computingBy uint64             // 正在重算的 goroutine id（区分自身重入 vs 其它 goroutine）
	isEffect    bool               // 是否为 effect（写时直接重跑，而非仅标脏）
	run         func()             // computed 的重算 / effect 的函数体
	// effect 串行执行调度（P1-3）：running 表示该 effect 正在某 goroutine 上执行，
	// queued 表示执行期间又收到重跑请求。合并重复请求，保证同一 effect 不并发。
	running bool
	queued  bool
}

func newNode(isEffect bool) *node {
	return &node{
		subs:     make(map[*node]struct{}),
		deps:     make(map[*node]struct{}),
		dirty:    true,
		isEffect: isEffect,
	}
}

// track 在 mu 持有期间调用：把当前 goroutine 观察者栈的栈顶登记为 n 的下游。
func track(n *node) {
	if st := observers[goid()]; len(st) > 0 {
		ob := st[len(st)-1]
		ob.deps[n] = struct{}{}
		n.subs[ob] = struct{}{}
	}
}

// markDirty 在 mu 持有期间调用：把一个 computed 节点标脏并递增失效版本号。
// version 让重算能发现「计算期间又脏了」——旧结果不得写回（P1-2）。
func markDirty(n *node) {
	n.dirty = true
	n.version++
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
				markDirty(sub)
				walk(sub)
			}
		}
	}
	walk(n)
	return effects
}

// withObserver 在锁外执行 fn，执行期间把 n 压入「当前 goroutine」的观察者栈并重建依赖集合。
// 观察者栈按 goroutine id 隔离且每个 goroutine 持有独立的栈切片（P1-3）：彻底移除共享的
// node.prev —— 之前两个 goroutine 同时执行同一节点时会互相覆盖 prev，破坏对方的栈恢复。
// 恢复用 defer 保证：即便 fn panic（如循环依赖检测触发），当前 goroutine 的栈也不会被污染（M-10）。
func withObserver(n *node, fn func()) {
	id := goid()
	mu.Lock()
	observers[id] = append(observers[id], n)
	clearDeps(n)
	mu.Unlock()

	defer func() {
		mu.Lock()
		st := observers[id]
		if len(st) <= 1 {
			delete(observers, id) // 栈空即删除条目，避免长期积累空栈（goroutine id 不复用时尤其明显）
		} else {
			observers[id] = st[:len(st)-1] // 弹出栈顶（n 必为最后压入者）
		}
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
				scheduleEffect(e)
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
		// 关键：在同一个临界区内完成「检查 dirty」和「占用计算权」。
		// 旧实现在这两步之间解锁了一次，形成竞争窗口：A、B 都能越过 dirty
		// 检查，随后各自 computing=true（后者覆盖 computingBy），双双进入 f()。
		// 现在于首次持锁期间即置 computing/computingBy，窗口消除（P1-1 修复）。
		st.n.computing = true
		st.n.computingBy = id
		mu.Unlock()

		// 计算直到「版本稳定」：若 f() 执行期间依赖又变化（被标脏 → version++），
		// 则本次结果基于过期输入，丢弃并重试。这是 P1-2 的核心：不能让旧结果
		// 写回时把计算期间新产生的 dirty 标记冲掉。
		// computing 在整个重算期间保持为 true，defer 保证任何退出路径都复位。
		defer func() {
			mu.Lock()
			st.n.computing = false
			st.n.computingBy = 0
			recalc.Broadcast()
			mu.Unlock()
		}()

		for {
			mu.Lock()
			startVersion := st.n.version
			mu.Unlock()

			var result T
			panicked := true
			func() {
				// withObserver 内部重建依赖集合；f 在锁外执行（会读其它 signal）。
				withObserver(st.n, func() { result = f() })
				panicked = false
			}()
			if panicked {
				return // f() panic：result 无效，不写回
			}

			mu.Lock()
			if st.n.version != startVersion {
				// 计算期间又脏了：丢弃旧结果，保持 dirty 并重试。
				// 此时 computing 仍为 true（本 goroutine 持有），继续下一次循环。
				mu.Unlock()
				continue
			}
			st.value = result
			st.n.dirty = false
			mu.Unlock()
			return
		}
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

// scheduleEffect 串行调度一个 effect 的执行（P1-3）。
// 同一 effect 的回调绝不在两个 goroutine 上并发执行——用户回调常写共享状态，
// 并发执行会引入数据竞争。执行期间若又收到重跑请求，只记 queued，待本次结束后
// 合并重跑一次，避免重复抖动。
func scheduleEffect(e *node) {
	mu.Lock()
	if e.running {
		e.queued = true
		mu.Unlock()
		return
	}
	e.running = true
	mu.Unlock()

	// 用 defer 保证调度状态在「正常结束」和「用户回调 panic」两条路径上都复位。
	// 旧实现只在循环正常退出时清 running；若 e.run()（用户 effect 回调）panic，
	// running 会永久停留在 true，之后所有写信号都只会置 queued 而不再重跑（P2-2）。
	// panic 本身继续向上传播（由调用方决定是否 recover），但本 effect 的调度状态
	// 必须干净，否则该节点被永久「卡死」。queued 一并清除：panic 期间的请求丢弃，
	// 与「回调未跑完」语义一致；后续新写入会重新触发一次正常调度。
	defer func() {
		mu.Lock()
		e.running = false
		e.queued = false
		mu.Unlock()
	}()

	for {
		e.run() // 用户回调在锁外执行
		mu.Lock()
		if e.queued {
			e.queued = false
			mu.Unlock()
			continue // 合并执行期间的请求，再跑一次
		}
		mu.Unlock()
		return // defer 负责清 running/queued（含正常路径）
	}
}

// Effect 立即执行一次 f，并在其依赖的任意 signal 变化时自动重跑。
// f 在每次执行前会重建依赖集合，因此条件分支里的依赖会被正确增删。
// 重跑由 scheduleEffect 串行调度，保证同一 effect 的回调不并发（P1-3）。
func Effect(f func()) {
	n := newNode(true)
	n.run = func() { withObserver(n, f) }
	scheduleEffect(n) // 首次立即执行
}

// state 保存 signal 的节点与当前值。
type state[T any] struct {
	n     *node
	value T
}
