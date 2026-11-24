package gxapp

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// newTextAppWith 在给定 UIExecutor 上建一个 App，返回 (app, signal)。
//
// 注意：响应式核心（observers/ownerStack/batch）是单线程唯一的，多个 App 必须
// 共享同一个 UI 事件循环（同一执行器）——评审也认可「共享一个 UI 事件循环的
// 多个窗口共用一个 Runtime」。本测试验证的是**投递器 / 关闭状态**的实例隔离。
func newTextAppWith(t *testing.T, exec *chanExecutor) (*App, signal.Signal[int]) {
	t.Helper()
	val := signal.New(0)
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return val() })

	app, err := OpenWith(DefaultConfig(), &closeRaceWindow{})
	if err != nil {
		t.Fatal(err)
	}
	app.SetUIExecutor(exec)
	if err := app.Render(func() any { return inst }); err != nil {
		t.Fatal(err)
	}
	return app, val
}

func drainExecutor(exec *chanExecutor) {
	barrier := make(chan struct{})
	exec.ch <- func() { close(barrier) }
	<-barrier
}

// TestMultipleAppsIsolatedClose 验收评审 P0：两个 App 各自持有 Runtime，
// app1.Close 后 app2 的后台 Dispatch 仍然正常，且不会访问已销毁的 Scene。
func TestMultipleAppsIsolatedClose(t *testing.T) {
	// 两个 App 共享同一 UI 事件循环（响应式核心单线程的契约）。
	exec := newChanExecutor()
	app1, _ := newTextAppWith(t, exec)
	app2, val2 := newTextAppWith(t, exec)

	// 关闭 app1：应只影响 app1 的 Runtime。
	if err := app1.Close(); err != nil {
		t.Fatal(err)
	}

	// app1 的 Dispatch 必须被拒绝，且闭包不执行（不会触碰已销毁的树）。
	var staleExecuted atomic.Bool
	if err := app1.Dispatch(func() { staleExecuted.Store(true) }); !errors.Is(err, signal.ErrUIRuntimeClosed) {
		t.Fatalf("已关闭 App 的 Dispatch 应返回 ErrUIRuntimeClosed, got=%v", err)
	}
	if err := app1.Dispatch(nil); err != nil {
		t.Fatalf("Dispatch(nil) 应无错误, got=%v", err)
	}

	// app2 的 Dispatch 仍正常（投递器未被 app1.Close 的全局关闭误伤）。
	if err := app2.Dispatch(func() { val2(42) }); err != nil {
		t.Fatalf("app1 关闭不应影响 app2 的 Dispatch, got=%v", err)
	}
	drainExecutor(exec)
	if got := val2(); got != 42 {
		t.Fatalf("app2 的后台更新应生效, got=%d", got)
	}

	if err := app2.Close(); err != nil {
		t.Fatal(err)
	}
	drainExecutor(exec)
	close(exec.ch)
	if staleExecuted.Load() {
		t.Fatal("已关闭 App 的 Dispatch 闭包不应执行")
	}
}

// TestDispatchAfterCloseUnderConcurrency 验收评审第三优先级第 11 项：关闭过程中
// 连续投递任务，关闭后到达的闭包一律不执行，不允许访问已销毁的 Scene。
func TestDispatchAfterCloseUnderConcurrency(t *testing.T) {
	exec := newChanExecutor()
	app, _ := newTextAppWith(t, exec)
	var executed atomic.Int64

	const goroutines, per = 4, 200
	closeDone := make(chan struct{})
	// Close 本身是响应式生命周期操作，投递到 UI 线程执行，避免与已投递的更新并发。
	go func() {
		defer close(closeDone)
		_ = app.Dispatch(func() { _ = app.Close() })
	}()

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < per; j++ {
				// 关闭与投递交错，部分被拒绝是预期的；只关心已接受的闭包。
				_ = app.Dispatch(func() { executed.Add(1) })
			}
		}()
	}
	wg.Wait()
	<-closeDone

	// 等所有已接受的任务执行完，再断言关闭后投递的闭包绝不执行。
	drainExecutor(exec)
	before := executed.Load()
	if err := app.Dispatch(func() { executed.Add(1) }); !errors.Is(err, signal.ErrUIRuntimeClosed) {
		t.Fatalf("关闭后 Dispatch 应返回 ErrUIRuntimeClosed, got=%v", err)
	}
	drainExecutor(exec)
	if got := executed.Load(); got != before {
		t.Fatalf("关闭后投递的闭包不应执行, before=%d got=%d", before, got)
	}
	close(exec.ch)
}

// Runtime()/Dispatch() 的基本契约：未注入 Executor 的 App 不允许静默同步执行。
func TestDispatchRequiresExecutor(t *testing.T) {
	app, err := OpenWith(DefaultConfig(), &closeRaceWindow{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	if app.Runtime() == nil || app.Runtime().InUI() {
		t.Fatal("未注入 Executor 时 Runtime 应存在但未接入 UI")
	}
	var executed atomic.Bool
	if err := app.Dispatch(func() { executed.Store(true) }); !errors.Is(err, signal.ErrUIRuntimeNotInitialized) {
		t.Fatalf("未注入 Executor 应返回 ErrUIRuntimeNotInitialized, got=%v", err)
	}
	if executed.Load() {
		t.Fatal("未注入 Executor 时 Dispatch 闭包绝不能同步执行")
	}
}
