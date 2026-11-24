package signal_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robertpanvip/gox/runtime/signal"
)

// testUIRuntime 是一个测试用的单线程 UI Runtime：所有 Safe 投递的闭包都在
// 唯一的 run goroutine 上串行执行，模拟真实后端 UIExecutor 的线程亲和性。
type testUIRuntime struct {
	ch     chan func()
	done   chan struct{}
	closed atomic.Bool
}

func newTestUIRuntime(t *testing.T) *testUIRuntime {
	t.Helper()
	rt := &testUIRuntime{
		ch:   make(chan func(), 1<<16),
		done: make(chan struct{}),
	}
	old := signal.SetUIPost(func(fn func()) bool {
		if rt.closed.Load() {
			return false // UI 已关闭：丢弃
		}
		select {
		case rt.ch <- fn:
			return true
		default:
			return false
		}
	})
	go func() {
		defer close(rt.done)
		for fn := range rt.ch {
			fn()
		}
	}()
	t.Cleanup(func() {
		rt.closed.Store(true)
		close(rt.ch)
		<-rt.done
		signal.SetUIPost(old)
	})
	return rt
}

// drain 等待当前已投递的所有任务执行完（用 FIFO 屏障保证）。
func (rt *testUIRuntime) drain() {
	barrier := make(chan struct{})
	select {
	case rt.ch <- func() { close(barrier) }:
	case <-time.After(2 * time.Second):
		panic("drain: 投递超时")
	}
	<-barrier
}

// TestSafeDeliversToUIRuntime 验证后台 goroutine 的 Safe 更新被投递到 UI Runtime
// 执行，且所有响应式操作都在同一个 goroutine（无并发依赖追踪）。
func TestSafeDeliversToUIRuntime(t *testing.T) {
	rt := newTestUIRuntime(t)

	// signal/effect 在测试主线程（即 UI Runtime 角色）创建；后台 goroutine
	// 只通过 Safe 投递更新，绝不直接触碰 signal。
	src := signal.New(0)
	var runs atomic.Int64
	signal.Effect(func() { runs.Add(1); _ = src() })
	base := runs.Load()

	const writers, perWriter = 8, 100
	var delivered atomic.Int64 // 在 UI Runtime 内执行的任务数
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWriter; j++ {
				signal.Safe(func() {
					delivered.Add(1)
					src(int(delivered.Load())) // 每个任务写不同值
				})
			}
		}()
	}
	wg.Wait()
	rt.drain() // 等所有投递执行完

	// 核心契约：所有后台 Safe 投递都到达 UI Runtime 并执行（无丢弃、无丢失）。
	if got := delivered.Load(); got != writers*perWriter {
		t.Fatalf("投递执行数=%d, want=%d（有任务被丢弃）", got, writers*perWriter)
	}
	// effect 因这些写入而重跑（至少一次）；不要求精确次数——并发投递顺序不确定，
	// 且串行执行下可能触发多次。
	if runs.Load() <= base {
		t.Fatalf("effect 未因 Safe 投递而重跑: base=%d got=%d", base, runs.Load())
	}
	if got := src(); got < 1 {
		t.Fatalf("最终值应被更新, got=%d", got)
	}
}

// TestSafeDroppedAfterClose 验证 UI Runtime 关闭后 Safe 投递的任务被丢弃，
// 不会在已销毁的 Runtime 上执行。
func TestSafeDroppedAfterClose(t *testing.T) {
	rt := newTestUIRuntime(t)

	var executed atomic.Bool
	// 关闭 UI Runtime（模拟窗口关闭）。
	rt.closed.Store(true)
	signal.CloseUITasks()

	signal.Safe(func() { executed.Store(true) })
	time.Sleep(10 * time.Millisecond)
	if executed.Load() {
		t.Fatal("UI 关闭后 Safe 任务不应执行")
	}

	// 恢复：newTestUIRuntime 的 cleanup 会再次 SetUIPost(old)，这里手动恢复 rt 的 post
	// 以便 cleanup 的 close(rt.ch) 正常（无人再投递）。
}

// TestSafeWithoutRuntimeReturnsError 验收评审 P0：未注入 UI Runtime 时 Safe
// 不得静默在调用方（可能是后台）goroutine 同步执行响应式代码。
func TestSafeWithoutRuntimeReturnsError(t *testing.T) {
	old := signal.SetUIPost(nil)
	defer signal.SetUIPost(old)

	called := false
	if err := signal.Safe(func() { called = true }); !errors.Is(err, signal.ErrUIRuntimeNotInitialized) {
		t.Fatalf("无 UI Runtime 时 Safe 应返回 ErrUIRuntimeNotInitialized, got=%v", err)
	}
	if called {
		t.Fatal("无 UI Runtime 时 Safe 的闭包绝不能被执行")
	}

	// nil fn 是 no-op。
	if err := signal.Safe(nil); err != nil {
		t.Fatalf("Safe(nil) 应无错误, got=%v", err)
	}
}

// TestSyncRuntimeRunsInline 验证 headless/测试可以**显式**选择同步执行，
// 而不是靠正式 API 自动降级。
func TestSyncRuntimeRunsInline(t *testing.T) {
	rt := signal.NewSyncRuntime()
	called := false
	if err := rt.Dispatch(func() { called = true }); err != nil {
		t.Fatalf("SyncRuntime.Dispatch 应成功, got=%v", err)
	}
	if !called {
		t.Fatal("SyncRuntime 应同步执行闭包")
	}
	if !rt.InUI() {
		t.Fatal("SyncRuntime.InUI 应为 true")
	}
}

// TestRuntimeIsolation 验收评审 P0：每个 Runtime 的投递器与关闭状态互相隔离，
// 关闭一个不影响另一个（模拟 app1.Close 不影响 app2 的后台更新）。
func TestRuntimeIsolation(t *testing.T) {
	rt1 := newDispatchRuntime()
	rt2 := newDispatchRuntime()

	var a, b atomic.Int64
	for i := 0; i < 10; i++ {
		if err := rt1.rt.Dispatch(func() { a.Add(1) }); err != nil {
			t.Fatal(err)
		}
		if err := rt2.rt.Dispatch(func() { b.Add(1) }); err != nil {
			t.Fatal(err)
		}
	}
	rt1.drain()
	rt2.drain()

	// 关闭 rt1：rt2 必须仍可投递。
	rt1.rt.Close()
	if err := rt2.rt.Dispatch(func() { b.Add(1) }); err != nil {
		t.Fatalf("rt1 关闭后 rt2 仍应可投递, got=%v", err)
	}
	if err := rt1.rt.Dispatch(func() { a.Add(1) }); !errors.Is(err, signal.ErrUIRuntimeClosed) {
		t.Fatalf("已关闭的 rt1 应返回 ErrUIRuntimeClosed, got=%v", err)
	}
	rt2.drain()
	if a.Load() != 10 || b.Load() != 11 {
		t.Fatalf("投递计数错误: rt1=%d rt2=%d", a.Load(), b.Load())
	}
}

// TestRuntimeNotInitializedDoesNotExecute 验证空 Runtime（未注入投递器）时
// Dispatch 返回错误且闭包不执行——正式 App 未接 UIExecutor 的行为。
func TestRuntimeNotInitializedDoesNotExecute(t *testing.T) {
	rt := signal.NewRuntime()
	called := false
	if err := rt.Dispatch(func() { called = true }); !errors.Is(err, signal.ErrUIRuntimeNotInitialized) {
		t.Fatalf("未初始化 Runtime 应返回 ErrUIRuntimeNotInitialized, got=%v", err)
	}
	if called {
		t.Fatal("未初始化 Runtime 的闭包绝不能执行")
	}
	if rt.InUI() {
		t.Fatal("未初始化 Runtime 的 InUI 应为 false")
	}
}

// TestRuntimeErrorHandler 验证错误回调：GX 语句形式忽略 Dispatch 返回值时，
// 未初始化 / 已关闭等失败仍能通过 Runtime 错误处理器上报。
func TestRuntimeErrorHandler(t *testing.T) {
	rt := signal.NewRuntime()
	var got error
	rt.SetErrorHandler(func(err error) { got = err })
	defer rt.SetErrorHandler(nil)

	_ = rt.Dispatch(func() {})
	if !errors.Is(got, signal.ErrUIRuntimeNotInitialized) {
		t.Fatalf("未初始化错误未上报, got=%v", got)
	}

	got = nil
	rt.SetPoster(signal.SyncPoster)
	rt.Close()
	_ = rt.Dispatch(func() {})
	if !errors.Is(got, signal.ErrUIRuntimeClosed) {
		t.Fatalf("已关闭错误未上报, got=%v", got)
	}
}

// dispatchRuntime 是绑定独立 goroutine 的投递型 Runtime 测试夹具。
type dispatchRuntime struct {
	rt   *signal.Runtime
	ch   chan func()
	done chan struct{}
}

func newDispatchRuntime() *dispatchRuntime {
	d := &dispatchRuntime{rt: signal.NewRuntime(), ch: make(chan func(), 1<<16), done: make(chan struct{})}
	d.rt.SetPoster(func(fn func()) bool {
		select {
		case d.ch <- fn:
			return true
		default:
			return false
		}
	})
	go func() {
		defer close(d.done)
		for fn := range d.ch {
			fn()
		}
	}()
	return d
}

func (d *dispatchRuntime) drain() {
	barrier := make(chan struct{})
	d.ch <- func() { close(barrier) }
	<-barrier
}

// TestBatchDoesNotSwitchThread 验证 Batch 只做合并、不做线程切换：在 UI Runtime
// 内调用 Batch 时，effect 在 Batch 返回前已同步跑完（不投递到下一帧）。
func TestBatchDoesNotSwitchThread(t *testing.T) {
	s := signal.New(0)
	var runs int
	signal.Effect(func() { runs++; _ = s() })
	base := runs

	signal.Batch(func() {
		s(1)
		s(2)
		s(3)
	})
	// Batch 同步冲刷：返回时 effect 已跑完（合并为一次或按 queued 合并）。
	if runs <= base {
		t.Fatalf("Batch 返回后 effect 应已重跑: base=%d got=%d", base, runs)
	}
	if got := s(); got != 3 {
		t.Fatalf("最终值=%d, want=3", got)
	}
}
