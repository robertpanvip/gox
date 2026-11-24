package signal

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSignalReadWrite(t *testing.T) {
	count := New(1)
	if got := count(); got != 1 {
		t.Fatalf("initial = %d, want 1", got)
	}
	count(5)
	if got := count(); got != 5 {
		t.Fatalf("after write = %d, want 5", got)
	}
}

func TestEffectRerunsOnWrite(t *testing.T) {
	count := New(0)
	var seen []int
	Effect(func() { seen = append(seen, count()) })
	if len(seen) != 1 || seen[0] != 0 {
		t.Fatalf("initial effect runs = %v, want [0]", seen)
	}
	count(5)
	if len(seen) != 2 || seen[1] != 5 {
		t.Fatalf("after write = %v, want [0 5]", seen)
	}
	count(9)
	count(9) // 相等性裁剪：值未变化，不重复触发下游
	if len(seen) != 3 {
		t.Fatalf("after 2 writes = %v, want 3 runs (相等写入被裁剪)", seen)
	}
}

func TestComputedDerivedAndCached(t *testing.T) {
	count := New(2)
	computeCalls := 0
	doubled := Computed(func() int {
		computeCalls++
		return count() * 2
	})
	if got := doubled(); got != 4 {
		t.Fatalf("doubled() = %d, want 4", got)
	}
	if got := doubled(); got != 4 {
		t.Fatalf("doubled() again = %d, want 4", got)
	}
	if computeCalls != 1 {
		t.Fatalf("compute calls = %d, want 1 (cached)", computeCalls)
	}
	count(3)
	if got := doubled(); got != 6 {
		t.Fatalf("doubled() after write = %d, want 6", got)
	}
	if computeCalls != 2 {
		t.Fatalf("compute calls = %d, want 2 (invalidated once)", computeCalls)
	}
}

func TestComputedChain(t *testing.T) {
	a := New(1)
	b := Computed(func() int { return a() + 1 })
	c := Computed(func() int { return b() * 10 })
	if got := c(); got != 20 {
		t.Fatalf("c() = %d, want 20", got)
	}
	a(4)
	if got := c(); got != 50 {
		t.Fatalf("c() after a=4 = %d, want 50", got)
	}
}

func TestEffectRebuildsDeps(t *testing.T) {
	useA := New(true)
	a := New(1)
	b := New(10)
	var got int
	Effect(func() {
		if useA() {
			got = a()
		} else {
			got = b()
		}
	})
	if got != 1 {
		t.Fatalf("initial got = %d, want 1", got)
	}
	useA(false) // 切换分支：effect 重跑，依赖应从 a 变为 b
	if got != 10 {
		t.Fatalf("after switch got = %d, want 10", got)
	}
	a(100) // 已不再依赖 a，不应触发
	if got != 10 {
		t.Fatalf("after a write got = %d, want 10 (a no longer tracked)", got)
	}
	b(20) // 现在依赖 b，应触发
	if got != 20 {
		t.Fatalf("after b write got = %d, want 20", got)
	}
}

func TestComputedInEffect(t *testing.T) {
	count := New(1)
	doubled := Computed(func() int { return count() * 2 })
	var seen []int
	Effect(func() { seen = append(seen, doubled()) })
	count(2)
	count(3)
	want := []int{2, 4, 6}
	if len(seen) != len(want) {
		t.Fatalf("seen = %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("seen = %v, want %v", seen, want)
		}
	}
}

// 严格单线程模型：signal 读写只属于 UI Runtime。后台 goroutine 不得直接读写，
// 必须经 Signal.Safe 投递（见 safe_test.go）。这里验证单线程顺序读写的正确性。
func TestSerialReadWrite(t *testing.T) {
	count := New(0)
	Effect(func() { _ = count() })
	for i := 0; i < 8; i++ {
		for j := 0; j < 100; j++ {
			count(i*100 + j)
			if got := count(); got != i*100+j {
				t.Fatalf("读回 %d, want %d", got, i*100+j)
			}
		}
	}
}

func TestEffectCounterAtomic(t *testing.T) {
	count := New(0)
	var runs int64
	Effect(func() { atomic.AddInt64(&runs, 1); _ = count() })
	for i := 1; i <= 50; i++ {
		count(i)
	}
	if got := atomic.LoadInt64(&runs); got != 51 {
		t.Fatalf("effect runs = %d, want 51", got)
	}
}

func TestComputedCyclePanicsRecoverably(t *testing.T) {
	// 回归 B-2：computed 自引用曾导致不可 recover 的 stack overflow；
	// 现在应立即 panic 出可读错误，且可被 recover。
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("循环依赖应 panic")
		}
		msg, _ := r.(string)
		if msg == "" || !containsChineseSubstr(msg, "循环依赖") {
			t.Fatalf("panic 信息应说明循环依赖, got: %v", r)
		}
	}()
	var c Signal[int]
	c = Computed(func() int { return c() + 1 })
	_ = c()
}

func TestComputedMutualCyclePanicsRecoverably(t *testing.T) {
	// 间接环 a→b→a 同样应被检测。
	defer func() {
		if recover() == nil {
			t.Fatal("相互循环依赖应 panic")
		}
	}()
	var a, b Signal[int]
	a = Computed(func() int { return b() + 1 })
	b = Computed(func() int { return a() + 1 })
	_ = a()
}

func TestComputedReadComputesOnce(t *testing.T) {
	// 惰性求值：读同一个 computed，f() 对每个脏周期只执行一次——单线程下反复读
	// 不会重复重算，也不会误判为循环依赖。
	src := New(1)
	calls := 0
	doubled := Computed(func() int {
		calls++
		return src() * 2
	})
	for i := 0; i < 16; i++ {
		if got := doubled(); got != 2 {
			t.Errorf("doubled() = %d, want 2", got)
		}
	}
	if calls != 1 {
		t.Fatalf("compute calls after repeated reads = %d, want 1", calls)
	}

	// 写脏后再读多次：仍只重算一次。
	src(5)
	calls = 0
	for i := 0; i < 16; i++ {
		if got := doubled(); got != 10 {
			t.Errorf("doubled() = %d, want 10", got)
		}
	}
	if calls != 1 {
		t.Fatalf("compute calls after invalidated reads = %d, want 1", calls)
	}
}

func TestEffectsIsolatedDeps(t *testing.T) {
	// 两个 effect 各自建立依赖：写 b 只应触发依赖 b 的 effect，不得串到依赖 a 的那个。
	a := New(0)
	b := New(0)
	runsA, runsB := 0, 0
	Effect(func() { runsA++; _ = a() })
	Effect(func() { runsB++; _ = b() })

	runsA, runsB = 0, 0
	b(1)
	if runsA != 0 {
		t.Fatalf("effect A ran %d times on b write, want 0", runsA)
	}
	if runsB != 1 {
		t.Fatalf("effect B ran %d times on b write, want 1", runsB)
	}
}

func containsChineseSubstr(s, sub string) bool {
	if len(sub) == 0 || len(s) < len(sub) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestComputedNoStale 验证 computed 在源信号变化后惰性重算得到新值、不返回陈旧缓存。
// 严格单线程模型下计算不会被其它 goroutine 打断，「计算期间写脏」的竞态窗口不复存在，
// version 校验仍保留作为防御；这里验证对外的正确性契约。
func TestComputedNoStale(t *testing.T) {
	src := New(2)
	d := Computed(func() int { return src() * 2 })
	if got := d(); got != 4 {
		t.Fatalf("initial d()=%d, want 4", got)
	}
	src(5) // 标脏
	if got := d(); got != 10 {
		t.Errorf("写脏后 d()=%d, want=10", got)
	}
	if again := d(); again != 10 {
		t.Errorf("再次读取 d()=%d, want=10", again)
	}
}

// TestEffectSerialUnderWrites 验证 effect 回调不并发执行（单线程模型下天然串行），
// 且每次写都触发一次重跑。
func TestEffectSerialUnderWrites(t *testing.T) {
	count := New(0)
	active, maxActive, total := 0, 0, 0
	Effect(func() {
		active++
		if active > maxActive {
			maxActive = active
		}
		_ = count() // 登记依赖
		total++
		active--
	})

	for i := 1; i <= 200; i++ {
		count(i) // 从 1 开始，避免首写与初值 0 相等被短路
	}

	if maxActive != 1 {
		t.Errorf("effect 回调发生并发执行：maxActive=%d, want=1", maxActive)
	}
	if total != 201 {
		t.Errorf("effect 执行次数=%d, want=201", total)
	}
}

// TestNestedComputedRead 验证嵌套 computed（观察者栈多层）读写正确、栈结构不被破坏。
func TestNestedComputedRead(t *testing.T) {
	a := New(1)
	b := New(2)
	outer := Computed(func() int {
		inner := Computed(func() int { return b() })
		return a() + inner()
	})
	for i := 0; i < 100; i++ {
		b(i)
		if got := outer(); got != 1+i {
			t.Fatalf("outer()=%d, want=%d", got, 1+i)
		}
	}
}

// TestComputedComputesOncePerInvalidation 验证每个失效周期内 f() 只执行一次：
// 写脏一次后反复读，f() 只在首次读时重算。单线程模型下计算权天然唯一。
func TestComputedComputesOncePerInvalidation(t *testing.T) {
	src := New(0)
	calls := 0
	d := Computed(func() int {
		calls++
		return src()
	})

	const rounds, readers = 200, 8
	for r := 0; r < rounds; r++ {
		src(r)
		before := calls
		for i := 0; i < readers; i++ {
			if got := d(); got != r {
				t.Fatalf("d()=%d, want %d", got, r)
			}
		}
		if calls != before+1 {
			t.Fatalf("第 %d 轮 f() 执行 %d 次，want 1", r, calls-before)
		}
	}
}

// TestEffectRecoversAfterPanic 回归 P2-2（effect 回调 panic 永久卡死）：
// effect 回调 panic 被外部 recover 后，该 effect 必须仍能在后续 signal 写入时重跑。
// 旧实现 e.running 在 panic 路径不会复位，导致效果永久停留在 running=true。
func TestEffectRecoversAfterPanic(t *testing.T) {
	count := New(0)
	var runs int64
	Effect(func() {
		atomic.AddInt64(&runs, 1)
		v := count()
		if v == 42 {
			panic("effect panic")
		}
	})
	if got := atomic.LoadInt64(&runs); got != 1 {
		t.Fatalf("initial runs=%d, want 1", got)
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("effect 回调 panic 应向上传播")
			}
		}()
		count(42) // 触发重跑，回调 panic
	}()

	// panic 恢复后，effect 调度状态必须干净：后续写入仍能触发重跑。
	count(7)
	if got := atomic.LoadInt64(&runs); got != 3 {
		t.Fatalf("panic 后 effect 应能重跑：runs=%d, want 3", got)
	}
}

// TestFastPathNoLostDependency 验证读快路径（非 observer 上下文的普通读）不会
// 破坏已建立的依赖：高频普通读混在写之间后，effect 的依赖仍完整，再写恰好重跑一次。
func TestFastPathNoLostDependency(t *testing.T) {
	src := New(0)
	runs := 0
	Effect(func() { runs++; _ = src() })
	base := runs

	for i := 0; i < 4; i++ {
		for j := 1; j <= 100; j++ {
			_ = src() // 普通读者走读快路径
			src(i*100 + j)
		}
	}

	before := runs
	src(99999)
	if got := runs; got != before+1 {
		t.Fatalf("快路径疑似丢失依赖：写后 effect runs %d→%d, want +1 (base=%d)", before, got, base)
	}
}

// TestSerialValueConsistency 验证顺序读写时值始终一致——单线程模型下 value 是
// 普通字段，不存在撕裂读写，这里确认读回的总是最后一次写入的值。
func TestSerialValueConsistency(t *testing.T) {
	s := New(uint64(0))
	for i := uint64(1); i <= 20000; i++ {
		s(i)
		if v := s(); v != i {
			t.Fatalf("读写不一致: wrote %d, read %d", i, v)
		}
	}
}

// 第十轮 P1-2：LockedGetter 使"读跟随/写落回变量"的 getter 并发安全。
// 旧裸闭包 v=arg[0]/return v 无锁，两个 goroutine 并发读写同一字段会 data race；
// LockedGetter 用实例级互斥锁包住 get/set，消除竞态，且跟随语义不变。
func TestLockedGetterConcurrentReadWrite(t *testing.T) {
	v := "init"
	g := LockedGetter(
		func() string { return v },
		func(x string) { v = x },
	)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if n == 0 {
					g("written")
				} else {
					_ = g()
				}
			}
		}(i)
	}
	wg.Wait()
	// 跟随语义：写落回变量，最终 v 是某个写入值而非空。
	if v == "" {
		t.Fatal("写应落回被捕获变量")
	}
}

// 第十轮 P2-3：Computed 是只读的，带参写入应显式 panic 而非静默丢弃。
func TestComputedWritePanics(t *testing.T) {
	src := New(1)
	d := Computed(func() int { return src() * 2 })
	if got := d(); got != 2 {
		t.Fatalf("initial = %d, want 2", got)
	}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("Computed 带参写入应 panic，而不是静默丢弃")
		}
	}()
	d(99) // 应 panic
}

// 第十一轮 N-3：同一变量被多个 getter 引用时（同实例多字段/跨实例），
// 用同一 key 的 LockedGetterShared 必须共享锁——并发写 a、读 b 不得 data race。
// 旧实现每个 getter 各持一把 sync.Mutex，同变量多字段彼此不互斥，仍报 race。
func TestLockedGetterSharedSameKeyConcurrent(t *testing.T) {
	v := "init"
	// 模拟 P{a: v, b: v}：两个字段都跟随同一变量 v，生成时拿到相同 key。
	ga := LockedGetterShared("sf:shared-var", func() string { return v }, func(x string) { v = x })
	gb := LockedGetterShared("sf:shared-var", func() string { return v }, func(x string) { v = x })
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 300; j++ {
				if n == 0 {
					ga("w")
				} else {
					_ = gb()
				}
			}
		}(i)
	}
	wg.Wait()
	if v == "" {
		t.Fatal("写应落回被捕获变量")
	}
}

// 第十一轮 N-3：LockedGetterShared 的 get/set panic 时锁须释放（不死锁）。
func TestLockedGetterSharedUnlockOnPanic(t *testing.T) {
	g := LockedGetterShared("sf:panic", func() string { return "x" }, func(string) { panic("boom") })
	func() {
		defer func() { _ = recover() }()
		g("trigger")
	}()
	// 若上一把锁未释放，下面这次读取会永久阻塞；能返回即证明已解锁。
	done := make(chan string, 1)
	go func() { done <- g() }()
	select {
	case got := <-done:
		if got != "x" {
			t.Fatalf("want x, got %s", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("panic 后锁未释放，读被永久阻塞")
	}
}
