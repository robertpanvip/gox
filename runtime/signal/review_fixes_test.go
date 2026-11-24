package signal_test

import (
	"sync/atomic"
	"testing"

	"github.com/robertpanvip/gox/runtime/signal"
)

// TestWithObserverCleanupPanicRestoresStack 验收评审 P0：
// Cleanup（用户代码）panic 时，观察者栈必须已由前置 defer 恢复，否则后续无关
// Signal 读取会错误地把新依赖登记到这个「幽灵观察者」上，污染依赖图。
func TestWithObserverCleanupPanicRestoresStack(t *testing.T) {
	src := signal.New(0)
	var runs atomic.Int64
	registered := false

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("期望 cleanup panic 向上传播")
			}
		}()
		signal.Effect(func() {
			_ = src()
			if !registered {
				registered = true
				signal.OnCleanup(func() { panic("cleanup boom") })
			}
			runs.Add(1)
		})
		// 触发重跑：上一轮注册的 cleanup 先执行并 panic，effect body 不再执行。
		src(1)
	}()

	base := runs.Load()
	if base != 1 {
		t.Fatalf("首次执行应只有 1 次, got=%d", base)
	}

	// 关键断言：panic 后观察者栈已清空。下面这次普通读取不应登记任何依赖。
	other := signal.New(0)
	_ = other() // 若栈被污染，这里会把幽灵 effect 登记为 other 的订阅者
	other(1)    // 写入无关 signal 不应触发之前的 effect
	if got := runs.Load(); got != base {
		t.Fatalf("观察者栈被污染：无关 signal 写入触发了旧 effect, base=%d got=%d", base, got)
	}
}

// TestBatchEffectDeduplicatedPerEpoch 验收评审 P1：同一 Batch 内多次写入只把
// Effect 收集一次（epoch 打标去重），且不同 Batch 各自独立去重。
func TestBatchEffectDeduplicatedPerEpoch(t *testing.T) {
	src := signal.New(0)
	var runs atomic.Int64
	signal.Effect(func() { _ = src(); runs.Add(1) })
	base := runs.Load()

	signal.Batch(func() {
		src(1)
		src(2)
		src(3)
	})
	if got := runs.Load(); got != base+1 {
		t.Fatalf("单批多次写入应合并为 1 次 effect, base=%d got=%d", base, runs.Load())
	}

	signal.Batch(func() {
		src(4)
		src(5)
	})
	if got := runs.Load(); got != base+2 {
		t.Fatalf("第二批应再触发 1 次 effect, base=%d got=%d", base, runs.Load())
	}
}

// TestBatchPanicStillFlushesWrites 验收评审 P1：Batch 内 panic 时 defer 仍冲刷
// 已收集的 Effect，已写入的 Signal 值保留，随后 panic 原样向上传播。
func TestBatchPanicStillFlushesWrites(t *testing.T) {
	src := signal.New(0)
	var runs atomic.Int64
	signal.Effect(func() { _ = src(); runs.Add(1) })
	base := runs.Load()

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("期望 Batch 内 panic 向上传播")
			}
		}()
		signal.Batch(func() {
			src(1)
			src(2)
			panic("batch boom")
		})
	}()

	if got := src(); got != 2 {
		t.Fatalf("Batch 内已写入的值应保留, got=%d", got)
	}
	if got := runs.Load(); got <= base {
		t.Fatalf("panic 后已收集的 effect 仍应冲刷, base=%d got=%d", base, runs.Load())
	}
}

// TestComputedPanicPropagatesAndRecovers 验收评审 P1：Computed 内 panic 原样向外
// 传播（不吞用户异常），且 computing / observers / ownerStack 等内部状态由各层
// defer 复位——下一次读取能正常重算，而不是误报「循环依赖」。
func TestComputedPanicPropagatesAndRecovers(t *testing.T) {
	src := signal.New(1)
	boom := true
	c := signal.Computed(func() int {
		v := src()
		if boom {
			panic("computed boom")
		}
		return v * 2
	})

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("期望 Computed 内 panic 向上传播")
			}
		}()
		_ = c()
	}()

	// panic 后观察者栈与 computing 标志都必须复位。
	boom = false
	if got := c(); got != 2 {
		t.Fatalf("panic 后 Computed 应能正常重算, got=%d", got)
	}

	// 再验一次观察者栈：普通读取不得污染依赖。
	other := signal.New(10)
	_ = other()
	var runs atomic.Int64
	signal.Effect(func() { _ = other(); runs.Add(1) })
	base := runs.Load()
	other(11)
	if got := runs.Load(); got <= base {
		t.Fatalf("Computed panic 后依赖追踪应仍正常, base=%d got=%d", base, got)
	}
}
