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
