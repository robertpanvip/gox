package signal

import (
	"reflect"
	"sync/atomic"
	"testing"
)

// TestEffectDisposeStopsReruns 验证 Effect 句柄可停止订阅。
func TestEffectDisposeStopsReruns(t *testing.T) {
	count := New(0)
	var runs int64
	h := Effect(func() { atomic.AddInt64(&runs, 1); _ = count() })
	if got := atomic.LoadInt64(&runs); got != 1 {
		t.Fatalf("initial runs=%d, want 1", got)
	}
	count(1)
	if got := atomic.LoadInt64(&runs); got != 2 {
		t.Fatalf("runs after write=%d, want 2", got)
	}
	h.Dispose()
	h.Dispose() // 幂等
	count(2)
	count(3)
	if got := atomic.LoadInt64(&runs); got != 2 {
		t.Fatalf("runs after dispose=%d, want 2 (已停止)", got)
	}
}

// TestOwnerDisposeCascades 验证 Owner 销毁级联清理其下所有 Effect。
func TestOwnerDisposeCascades(t *testing.T) {
	a := New(0)
	b := New(0)
	var runsA, runsB int64
	var owner *Owner
	CreateRoot(func(o *Owner) {
		owner = o
		Effect(func() { atomic.AddInt64(&runsA, 1); _ = a() })
		Effect(func() { atomic.AddInt64(&runsB, 1); _ = b() })
	})
	owner.Dispose()
	a(1)
	b(1)
	if atomic.LoadInt64(&runsA) != 1 || atomic.LoadInt64(&runsB) != 1 {
		t.Fatalf("owner 销毁后 effect 仍在跑: A=%d B=%d", runsA, runsB)
	}
}

// TestOnCleanupOnRerunAndDispose 验证 onCleanup 在重跑前与销毁时执行。
func TestOnCleanupOnRerunAndDispose(t *testing.T) {
	src := New(0)
	var cleanups int64
	var h Computation
	h = Effect(func() {
		OnCleanup(func() { atomic.AddInt64(&cleanups, 1) })
		_ = src()
	})
	if got := atomic.LoadInt64(&cleanups); got != 0 {
		t.Fatalf("initial cleanups=%d, want 0", got)
	}
	src(1) // 触发重跑：先执行上一轮的 cleanup
	if got := atomic.LoadInt64(&cleanups); got != 1 {
		t.Fatalf("cleanups after rerun=%d, want 1", got)
	}
	h.Dispose() // 销毁：执行本轮的 cleanup
	if got := atomic.LoadInt64(&cleanups); got != 2 {
		t.Fatalf("cleanups after dispose=%d, want 2", got)
	}
}

// TestOwnerCleanupOnDispose 验证 Owner 级 cleanup 在销毁时逆序执行。
func TestOwnerCleanupOnDispose(t *testing.T) {
	var order []int
	o := CreateRoot(func(o *Owner) {
		OnCleanup(func() { order = append(order, 1) })
		OnCleanup(func() { order = append(order, 2) })
	})
	o.Dispose()
	if !reflect.DeepEqual(order, []int{2, 1}) {
		t.Fatalf("cleanup order=%v, want [2 1] (逆序)", order)
	}
}

// TestOnMountOnce 验证 onMount 在挂载后只执行一次。
func TestOnMountOnce(t *testing.T) {
	var mounts int64
	CreateRoot(func(o *Owner) {
		OnMount(func() { atomic.AddInt64(&mounts, 1) })
		OnMount(func() { atomic.AddInt64(&mounts, 1) })
	})
	if got := atomic.LoadInt64(&mounts); got != 2 {
		t.Fatalf("mounts=%d, want 2 (两个回调各一次)", got)
	}
}

// TestBatchCoalescesEffects 验证批处理把同一轮多次写入合并成一次 effect 执行。
func TestBatchCoalescesEffects(t *testing.T) {
	a := New(0)
	b := New(0)
	var runs int64
	Effect(func() { atomic.AddInt64(&runs, 1); _, _ = a(), b() })
	if got := atomic.LoadInt64(&runs); got != 1 {
		t.Fatalf("initial runs=%d, want 1", got)
	}
	Batch(func() {
		a(1)
		a(2)
		b(1)
		b(2)
	})
	if got := atomic.LoadInt64(&runs); got != 2 {
		t.Fatalf("runs after batch=%d, want 2 (合并为一次)", got)
	}
}

// TestEqualValueNoNotify 验证相等值不触发（含 slice 的深度相等）。
func TestEqualValueNoNotify(t *testing.T) {
	s := New([]int{1, 2})
	var runs int64
	Effect(func() { atomic.AddInt64(&runs, 1); _ = s() })
	s([]int{1, 2}) // 深度相等：不触发
	if got := atomic.LoadInt64(&runs); got != 1 {
		t.Fatalf("runs=%d, want 1 (深度相等被裁剪)", got)
	}
	s([]int{1, 2, 3}) // 不同：触发
	if got := atomic.LoadInt64(&runs); got != 2 {
		t.Fatalf("runs=%d, want 2", got)
	}
}

// TestAlwaysNotifySignal 验证 NewWithEquals(nil) 实现每次写都通知。
func TestAlwaysNotifySignal(t *testing.T) {
	s := NewWithEquals(5, nil)
	var runs int64
	Effect(func() { atomic.AddInt64(&runs, 1); _ = s() })
	s(5) // 相同值也触发
	if got := atomic.LoadInt64(&runs); got != 2 {
		t.Fatalf("runs=%d, want 2 (always notify)", got)
	}
}

// TestNestedOwnerDispose 验证父 Owner 销毁级联销毁子 Owner。
func TestNestedOwnerDispose(t *testing.T) {
	src := New(0)
	var runs int64
	var parent *Owner
	CreateRoot(func(o *Owner) {
		parent = o
		RunWithOwner(o, func() {
			CreateRoot(func(child *Owner) {
				Effect(func() { atomic.AddInt64(&runs, 1); _ = src() })
			})
		})
	})
	parent.Dispose()
	src(1)
	if got := atomic.LoadInt64(&runs); got != 1 {
		t.Fatalf("runs=%d, want 1 (子 Owner 已级联销毁)", got)
	}
}

// 阶段4 压力测试：反复创建/销毁 Owner，验证 cleanup 严格配对，无泄漏、无重复。
func TestOwnerStressNoLeak(t *testing.T) {
	const rounds = 500
	totalCleaned := 0
	for i := 0; i < rounds; i++ {
		src := New(0)
		cleaned := 0
		o := CreateRoot(func(o *Owner) {
			Effect(func() {
				_ = src()
				OnCleanup(func() { cleaned++ })
			})
		})
		// 每轮内多次重跑该 effect，cleanup 应在每次重跑前执行（配对）。
		for j := 1; j <= 5; j++ {
			src(j)
		}
		o.Dispose()
		// 该 effect：首次 + 5 次重跑 = 6 次执行；每次重跑前清理上一次，
		// Dispose 再清理最后一次 → 共 6 次 cleanup。
		if cleaned != 6 {
			t.Fatalf("轮 %d: cleanup 应严格配对为 6, got %d", i, cleaned)
		}
		// Dispose 后写信号不应再触发任何 cleanup。
		src(999)
		if cleaned != 6 {
			t.Fatalf("轮 %d: Dispose 后不应再 cleanup, got %d", i, cleaned)
		}
		totalCleaned += cleaned
	}
	if totalCleaned != rounds*6 {
		t.Fatalf("总 cleanup 数不符: %d", totalCleaned)
	}
}
