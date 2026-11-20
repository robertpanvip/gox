package signal

import (
	"sync"
	"sync/atomic"
	"testing"
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
	count(9) // 相同值也触发（本运行时不做相等裁剪）
	if len(seen) != 4 {
		t.Fatalf("after 2 writes = %v, want 4 runs", seen)
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

func TestConcurrentReadWrite(t *testing.T) {
	count := New(0)
	Effect(func() { _ = count() })
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(base int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				count(base*100 + j)
				_ = count()
			}
		}(i)
	}
	wg.Wait()
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

func TestComputedConcurrentReadComputesOnce(t *testing.T) {
	// 回归 H-9：并发读同一个被标脏的 computed，f() 对每个脏周期只应执行一次，
	// 且不能因其它 goroutine 正在重算而误判为循环依赖 panic。
	src := New(1)
	var calls int64
	doubled := Computed(func() int {
		atomic.AddInt64(&calls, 1)
		return src() * 2
	})
	const readers = 16
	var wg sync.WaitGroup
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := doubled(); got != 2 {
				t.Errorf("doubled() = %d, want 2", got)
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Fatalf("compute calls after concurrent first read = %d, want 1", got)
	}

	// 写脏后再并发读：仍只重算一次。
	src(5)
	atomic.StoreInt64(&calls, 0)
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if got := doubled(); got != 10 {
				t.Errorf("doubled() = %d, want 10", got)
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Fatalf("compute calls after concurrent invalidated read = %d, want 1", got)
	}
}

func TestConcurrentEffectsIsolatedDeps(t *testing.T) {
	// 回归 H-8：两个 goroutine 各自建立 effect 依赖，观察者栈必须隔离——
	// 写 B 只应触发 B 的 effect，不得因共享全局 observer 而登记到 A 的 effect。
	a := New(0)
	b := New(0)
	var runsA, runsB int64
	ready := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		Effect(func() { atomic.AddInt64(&runsA, 1); _ = a() })
		<-ready
	}()
	go func() {
		defer wg.Done()
		Effect(func() { atomic.AddInt64(&runsB, 1); _ = b() })
		<-ready
	}()
	close(ready)
	wg.Wait()

	atomic.StoreInt64(&runsA, 0)
	atomic.StoreInt64(&runsB, 0)
	b(1)
	if got := atomic.LoadInt64(&runsA); got != 0 {
		t.Fatalf("effect A ran %d times on b write, want 0 (deps crossed goroutines)", got)
	}
	if got := atomic.LoadInt64(&runsB); got != 1 {
		t.Fatalf("effect B ran %d times on b write, want 1", got)
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

// TestComputedNoStaleOnConcurrentWrite 回归 P1-2：computed 的 f() 在锁外计算期间，
// 若另一 goroutine 写了源信号并标脏，旧结果不得写回冲掉失效标记。
// 用 channel 屏障确定性复现该竞态窗口。
func TestComputedNoStaleOnConcurrentWrite(t *testing.T) {
	src := New(2)
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan int, 1)

	d := Computed(func() int {
		v := src() // 读旧值 2，登记依赖
		select {
		case started <- struct{}{}:
		default:
		}
		<-release // 期间放行另一 goroutine 写源
		return v * 2
	})

	go func() { done <- d() }()
	<-started // f() 已读到 2
	src(5)    // 写 5 → collectDirty 标脏 d（version++）
	close(release)
	got := <-done

	if got != 10 {
		t.Errorf("计算期间写入被吞：d()=%d, want=10", got)
	}
	if again := d(); again != 10 {
		t.Errorf("后续读取仍是陈旧值：d()=%d, want=10", again)
	}
}

// TestEffectSerialUnderConcurrentWrites 回归 P1-3：多个 goroutine 并发写同一 signal
// 触发 effect 时，effect 回调不得并发执行（否则用户回调里的共享状态会数据竞争）。
// 在 -race 下运行此测试可验证无竞争；无 -race 时验证回调串行（用原子计数检测重入）。
func TestEffectSerialUnderConcurrentWrites(t *testing.T) {
	count := New(0)
	var active, maxActive, total int32
	Effect(func() {
		cur := atomic.AddInt32(&active, 1)
		for {
			m := atomic.LoadInt32(&maxActive)
			if cur <= m || atomic.CompareAndSwapInt32(&maxActive, m, cur) {
				break
			}
		}
		_ = count() // 登记依赖
		atomic.AddInt32(&total, 1)
		atomic.AddInt32(&active, -1)
	})

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			count(v)
		}(i)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&maxActive); got != 1 {
		t.Errorf("effect 回调发生并发执行：maxActive=%d, want=1", got)
	}
	if atomic.LoadInt32(&total) == 0 {
		t.Error("effect 从未执行")
	}
}

// TestNestedComputedConcurrentRead 回归 P1-3：嵌套 computed（观察者栈多层）在并发读写下
// 栈的 push/pop 不共享 node.prev，不破坏彼此的栈结构。
func TestNestedComputedConcurrentRead(t *testing.T) {
	a := New(1)
	b := New(2)
	outer := Computed(func() int {
		inner := Computed(func() int { return b() })
		return a() + inner()
	})
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(v int) {
			defer wg.Done()
			b(v)
			_ = outer()
		}(i)
	}
	wg.Wait()
}
