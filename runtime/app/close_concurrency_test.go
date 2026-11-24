package gxapp

import (
	"sync"
	"sync/atomic"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// closeRaceWindow 统计 Draw 次数，用于验证关闭后不再有新帧提交。
type closeRaceWindow struct {
	cfg   WindowConfig
	draws atomic.Int64
	mu    sync.Mutex
}

func (w *closeRaceWindow) Open(c WindowConfig) error { w.cfg = c; return nil }
func (w *closeRaceWindow) Draw(r *gxscene.Node) error {
	w.draws.Add(1)
	return nil
}
func (w *closeRaceWindow) Run() error           { return nil }
func (w *closeRaceWindow) Close() error         { return nil }
func (w *closeRaceWindow) Config() WindowConfig { return w.cfg }

// chanExecutor 是一个测试用 UIExecutor：把任务投递到单一 UI goroutine 执行。
type chanExecutor struct {
	ch   chan func()
	done chan struct{}
}

func newChanExecutor() *chanExecutor {
	e := &chanExecutor{ch: make(chan func(), 1<<16), done: make(chan struct{})}
	go func() {
		defer close(e.done)
		for fn := range e.ch {
			fn()
		}
	}()
	return e
}

func (e *chanExecutor) Post(fn func()) error {
	select {
	case e.ch <- fn:
		return nil
	default:
		return nil // 队列满则丢弃，模拟关闭
	}
}

// 严格单线程模型：后台 goroutine 只通过 Signal.Safe 投递更新，经 UIExecutor
// 到单一 UI goroutine 执行。验证 Close 与持续 Safe 更新并发时无 panic、无数据竞争，
// 且关闭后不再提交新帧、投递的任务被丢弃。
func TestCloseConcurrentWithSafeMutationAndDraw(t *testing.T) {
	for round := 0; round < 20; round++ {
		val := signal.New(0)
		inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "div"},
			{Code: gxscene.OpDynChild, Slot: 0},
			{Code: gxscene.OpClose},
		}, N: 1}, func() any { return val() })

		w := &closeRaceWindow{}
		app, err := OpenWith(DefaultConfig(), w)
		if err != nil {
			t.Fatal(err)
		}
		exec := newChanExecutor()
		app.SetUIExecutor(exec) // 注入 UI Runtime：Safe 投递到这里串行执行
		if err := app.Render(func() any { return inst }); err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		// 3 个后台写者：只通过 Signal.Safe 投递，绝不直接写 signal。
		for k := 0; k < 3; k++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := 0; i < 500; i++ {
					v := i
					_ = app.Dispatch(func() { val(v) })
				}
			}()
		}
		// 1 个绘制者：Draw 经派发器投递，是并发安全入口；Update 会重算 getter
		// （读 signal），必须经 Safe 投递到 UI Runtime 执行，不能后台直接调。
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				app.Draw()
				_ = app.Dispatch(func() { app.Update() })
			}
		}()
		// 关闭者：制造与 Safe 更新的交错。Close 会销毁响应式树（Owner.Dispose
		// 摘订阅表），属于 UI Runtime 的工作，必须同样经 Safe 投递执行——否则
		// 后台直接 Close 会与 UI goroutine 的 signal 写入并发（普通变量需自己同步）。
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				v := i
				_ = app.Dispatch(func() { val(v) })
			}
			_ = app.Dispatch(func() { _ = app.Close() })
		}()

		wg.Wait()
		// 不在主 goroutine 直接 Close——那会与 UI goroutine 正在执行的 signal
		// 写并发。关闭者已把 Close 作为最后一个闭包投递到同一 FIFO 队列，这里
		// 用屏障等它（及其之前所有任务）执行完。
		barrier := make(chan struct{})
		exec.ch <- func() { close(barrier) }
		<-barrier

		// 关闭后 Draw/Update 不得再触发任何帧。
		before := w.draws.Load()
		app.Draw()
		app.Update()
		if after := w.draws.Load(); after != before {
			t.Fatalf("关闭后不应再提交新帧: before=%d after=%d", before, after)
		}

		// 清理 UI goroutine。
		close(exec.ch)
		<-exec.done
		// 恢复全局 UI post 为 nil，避免影响下一轮/其它测试。
		signal.SetUIPost(nil)
	}
}

// closeErrWindow 返回指定关闭错误，用于验证 App.Close 传递底层错误（评审 P2）。
type closeErrWindow struct {
	closeRaceWindow
	err   error
	calls int
}

func (w *closeErrWindow) Close() error { w.calls++; return w.err }

type sentinelCloseErr struct{}

func (sentinelCloseErr) Error() string { return "close failed" }

// TestClosePropagatesWindowError 验收评审 P2：底层 win.Close() 的错误必须返回给调用方，
// 且并发/重复 Close 的所有调用方得到一致结果（sync.Once + 保存 closeErr）。
func TestClosePropagatesWindowError(t *testing.T) {
	sentinel := sentinelCloseErr{}
	w := &closeErrWindow{err: sentinel}
	app, err := OpenWith(DefaultConfig(), w)
	if err != nil {
		t.Fatal(err)
	}
	// 并发调用 Close，全部应拿到同一个错误。
	const n = 8
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = app.Close()
		}(i)
	}
	wg.Wait()
	for i, e := range errs {
		if e != sentinel {
			t.Errorf("Close#%d 应返回底层错误，got %v", i, e)
		}
	}
	if w.calls != 1 {
		t.Fatalf("win.Close 应只调用一次，got %d", w.calls)
	}
}
