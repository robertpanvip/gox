package signal_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/robertpanvip/gox/runtime/signal"
)

// 严格单线程模型：后台 goroutine 不得直接读写 signal，必须经 Signal.Safe 投递。
// 这些测试验证生命周期（Dispose）与帧边界在单线程/投递模型下的正确性。
func TestEffectDisposeStopsReruns(t *testing.T) {
	src := signal.New(0)
	var runs atomic.Int64
	h := signal.Effect(func() {
		runs.Add(1)
		_ = src() // 建立依赖
	})
	if runs.Load() != 1 {
		t.Fatalf("首次执行 runs=%d, want 1", runs.Load())
	}

	h.Dispose() // 幂等销毁
	src(1)
	src(2)
	if got := runs.Load(); got != 1 {
		t.Fatalf("dispose 后不应再重跑, runs=%d", got)
	}
}

// 验证帧边界 hook 在 Batch 合并写入后只触发一次（单线程）。
func TestBatchWithFlushHooks(t *testing.T) {
	var hookFires atomic.Int64
	un := signal.AfterEffects(func() { hookFires.Add(1) })
	defer un()

	s := signal.New(0)
	signal.Effect(func() { _ = s() })

	const batches = 100
	for i := 1; i <= batches; i++ {
		v := i
		signal.Batch(func() {
			s(v)
			s(v) // 写相同值被短路，不额外触发
		})
	}
	if got := hookFires.Load(); got != batches {
		t.Fatalf("帧边界触发次数=%d, want=%d", got, batches)
	}
}

// Owner.Dispose 后再写信号，级联销毁的 effect 不重跑，无 panic。
func TestEffectNoRunAfterOwnerDispose(t *testing.T) {
	src := signal.New(0)
	var runs atomic.Int64
	o := signal.CreateRoot(func(o *signal.Owner) {
		signal.Effect(func() { runs.Add(1); _ = src() })
	})
	base := runs.Load()
	o.Dispose()
	src(1)
	src(2)
	if got := runs.Load(); got != base {
		t.Fatalf("Owner 销毁后 effect 不应重跑, base=%d got=%d", base, got)
	}
}

// OnCleanup 注册后 effect 停止，不留下永久运行的回调。
func TestCleanupAfterDispose(t *testing.T) {
	src := signal.New(0)
	var cleaned atomic.Bool
	h := signal.Effect(func() {
		_ = src()
		signal.OnCleanup(func() { cleaned.Store(true) })
	})
	src(1) // 重跑前执行上一轮 cleanup
	if !cleaned.Load() {
		t.Fatal("重跑前应触发 cleanup")
	}
	h.Dispose()
	cleaned.Store(false)
	h.Dispose() // 幂等
	time.Sleep(time.Millisecond) // 给任何异步收尾留窗口
	if cleaned.Load() {
		t.Fatal("重复 dispose 不应再次触发 cleanup")
	}
}
