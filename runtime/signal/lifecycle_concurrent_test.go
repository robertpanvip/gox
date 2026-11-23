package signal_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robertpanvip/gox/runtime/signal"
)

// T1.3：effect 回调执行期间被另一个 goroutine Dispose，不得 panic、不得卡死
// 调度状态；dispose 后该 effect 不再重跑。
func TestEffectDisposedWhileRunning(t *testing.T) {
	src := signal.New(0)
	inBody := make(chan struct{}) // 第二次执行（重跑）进入回调时通知
	release := make(chan struct{})
	var runs atomic.Int64

	h := signal.Effect(func() {
		n := runs.Add(1)
		_ = src() // 建立依赖
		if n == 2 {
			// 重跑（后台 goroutine 调度）卡在回调里，等待被 dispose
			close(inBody)
			<-release
		}
	})

	// 首次执行已在 signal.Effect 内同步完成（runs==1）。触发一次重跑，
	// 该重跑在独立 goroutine 的调度里进入回调并阻塞。
	go func() { src(1) }()
	<-inBody // 等重跑进入回调

	disposed := make(chan struct{})
	go func() {
		h.Dispose() // 回调执行中从另一 goroutine 销毁
		close(disposed)
	}()
	select {
	case <-disposed:
	case <-time.After(2 * time.Second):
		t.Fatal("Dispose 未能在回调执行期间完成（疑似死锁）")
	}
	close(release) // 放行回调

	if got := runs.Load(); got != 2 {
		t.Fatalf("dispose 后不应再重跑, runs=%d", got)
	}
}

// T1.3：多个后台 goroutine 并发写 + Batch，帧边界 hook 计数无数据竞争，
// 且 -race 下无 race。各 goroutine 的 Batch 只在自己的帧边界触发一次 hook。
func TestConcurrentBatchWithFlushHooks(t *testing.T) {
	var hookFires atomic.Int64
	un := signal.AfterEffects(func() { hookFires.Add(1) })
	defer un()

	// 每个 goroutine 一个独立 signal，避免彼此干扰；都订阅一个 effect 使其有下游。
	const workers = 4
	const writes = 100
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := signal.New(0)
			signal.Effect(func() { _ = s() })
			for i := 1; i <= writes; i++ {
				v := i
				signal.Batch(func() {
					s(v)
					s(v) // 写相同值被短路，不额外触发
				})
			}
		}()
	}
	wg.Wait()
	// AfterEffects 是进程级 hook：某个 goroutine 的帧边界会通知所有订阅者，
	// 因此这里的计数是「本测试期间所有 goroutine 的帧边界总数」。每个 Batch
	// 至少触发一次，故下界为 workers*writes；精确值依赖调度，不作强断言。
	// 本测试的核心价值是 -race 下无数据竞争、无死锁。
	minFires := int64(workers * writes)
	if got := hookFires.Load(); got < minFires {
		t.Fatalf("帧边界触发次数不足: got %d want >= %d", got, minFires)
	}
}

// T1.3：Owner.Dispose 后再写信号，级联销毁的 effect 不重跑，无 panic。
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
