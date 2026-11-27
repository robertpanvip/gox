package gxapp

import (
	"sync"
	"testing"
	"time"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// ---- P4 目标态验收：单一 UI Loop + Host 轮询主循环（ui-loop-p4-multiwindows.md §3/§5/§9）----

// hostedWindowStub 是 bareWindow：只实现 Window、不实现 HostedWindow 的后端，
// 验证目标态硬约束——Host 拒绝不可轮询的窗口，没有静默降级路径。
type bareWindow struct{ cfg WindowConfig }

func (w *bareWindow) Open(cfg WindowConfig) error { w.cfg = cfg; return nil }
func (w *bareWindow) Draw(r *gxscene.Node) error  { return nil }
func (w *bareWindow) Run() error                  { return nil }
func (w *bareWindow) Close() error                { return nil }
func (w *bareWindow) Config() WindowConfig        { return w.cfg }

// Host 只接受 HostedWindow：未实现轮询接口的后端开窗即报错（无自驱回退）。
func TestOpenWindowRequiresHostedWindow(t *testing.T) {
	host := NewHost()
	_, err := host.OpenWindow(WindowConfig{Title: "legacy"}, &bareWindow{})
	if err == nil {
		t.Fatal("未实现 HostedWindow 的窗口应被拒绝")
	}
	if len(host.Windows()) != 0 {
		t.Fatal("被拒绝的窗口不应残留")
	}
}

// 单一 UI Loop：两扇窗的 Dispatch 任务 FIFO 进同一队列，由 Host 主循环串行执行。
func TestHostSharedSingleLoop(t *testing.T) {
	host := NewHost(WithIdleWait(time.Millisecond))
	wA, wB := &stubWindow{closeAfter: 3}, &stubWindow{closeAfter: 3}
	aA, err := host.OpenWindow(WindowConfig{Title: "A", Backend: "stub"}, wA)
	if err != nil {
		t.Fatalf("open A: %v", err)
	}
	aB, err := host.OpenWindow(WindowConfig{Title: "B", Backend: "stub"}, wB)
	if err != nil {
		t.Fatalf("open B: %v", err)
	}

	var mu sync.Mutex
	var order []string
	_ = aA.Dispatch(func() { mu.Lock(); order = append(order, "A1"); mu.Unlock() })
	_ = aB.Dispatch(func() { mu.Lock(); order = append(order, "B1"); mu.Unlock() })
	_ = aA.Dispatch(func() { mu.Lock(); order = append(order, "A2"); mu.Unlock() })

	if err := host.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	got := append([]string(nil), order...)
	mu.Unlock()
	want := []string{"A1", "B1", "A2"}
	if len(got) != len(want) {
		t.Fatalf("跨窗任务应按 FIFO 串行执行, want %v got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("跨窗任务应按 FIFO 串行执行, want %v got %v", want, got)
		}
	}
	// 任务确实跑在 Host 唯一 Loop 上（而非窗口自驱泵）。
	st := host.Loop().Stats()
	if st.RanMacro < 3 {
		t.Fatalf("Host Loop 应执行 >=3 个宏任务, got %+v", st)
	}
	if got := wA.pollCount(); got < 3 {
		t.Fatalf("A 窗应被主循环轮询到请求关闭, got %d", got)
	}
	if !wA.closed || !wB.closed {
		t.Fatal("两扇窗都应被主循环关闭")
	}
}

// 窗口任务 guard：A 窗 Dispatch 的任务 panic → 归因 Phase=task 并只熔断 A，B 继续。
func TestHostedTaskPanicIsolated(t *testing.T) {
	var mu sync.Mutex
	var faults []Fault
	host := NewHost(
		WithFaultHandler(func(f Fault) { mu.Lock(); faults = append(faults, f); mu.Unlock() }),
		WithIdleWait(time.Millisecond),
	)
	wA, wB := &stubWindow{}, &stubWindow{closeAfter: 3}
	aA, err := host.OpenWindow(WindowConfig{Title: "A", Backend: "stub"}, wA)
	if err != nil {
		t.Fatalf("open A: %v", err)
	}
	if _, err := host.OpenWindow(WindowConfig{Title: "B", Backend: "stub"}, wB); err != nil {
		t.Fatalf("open B: %v", err)
	}
	_ = aA.Dispatch(func() { panic("task boom") })

	if err := host.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	mu.Lock()
	got := append([]Fault(nil), faults...)
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("应归因上报一次故障, got %+v", got)
	}
	f := got[0]
	if f.Phase != "task" || f.Panic != "task boom" || !f.Recovered {
		t.Fatalf("任务 panic 应归因 Phase=task, got %+v", f)
	}
	// 直接 OpenWindow 注入窗口实例（未走 Backend）：Backend 标识为空串。
	if f.WindowID != 1 || f.Backend != "" {
		t.Fatalf("故障应归因到窗口 A (id=1), got %+v", f)
	}
	if !wA.closed {
		t.Fatal("任务 panic 的窗口应被熔断关闭")
	}
	if got := wB.pollCount(); got != 3 {
		t.Fatalf("B 窗应不受影响继续轮询, got %d 次 Poll", got)
	}
}

// Host.Go 受保护入口：后台 goroutine panic 被归因 Phase=go，不带崩进程。
func TestHostGoProtected(t *testing.T) {
	faultCh := make(chan Fault, 1)
	host := NewHost(WithFaultHandler(func(f Fault) { faultCh <- f }))
	done := make(chan struct{})
	host.Go(func() {
		defer close(done)
		panic("goroutine boom")
	})
	<-done
	select {
	case f := <-faultCh:
		if f.Phase != "go" || f.Panic != "goroutine boom" || !f.Recovered {
			t.Fatalf("Host.Go panic 应归因 Phase=go, got %+v", f)
		}
	case <-time.After(time.Second):
		t.Fatal("Host.Go 的 panic 应归因上报")
	}
	// 正常 fn 不产生故障。
	host.Go(func() { /* ok */ })
}

// Wake 即时打断：主循环空闲等待（300ms）期间任务入队应立即唤醒执行，
// 感知延迟远小于空闲间隔——这是「单一 Loop + Wake」的调度不变量。
func TestHostWakeInterruptsIdle(t *testing.T) {
	host := NewHost(WithIdleWait(300 * time.Millisecond))
	w := &stubWindow{}
	a, err := host.OpenWindow(WindowConfig{Title: "A", Backend: "stub"}, w)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ran := make(chan struct{})
	runErr := make(chan error, 1)
	go func() { runErr <- host.Run() }()
	time.Sleep(80 * time.Millisecond) // 等主循环进入空闲等待

	start := time.Now()
	_ = a.Dispatch(func() { close(ran) })
	select {
	case <-ran:
		if d := time.Since(start); d > 200*time.Millisecond {
			t.Fatalf("任务应被 Wake 即时唤醒执行, 耗时 %v", d)
		}
	case <-time.After(time.Second):
		t.Fatal("任务未被执行")
	}
	if err := host.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := <-runErr; err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// 混合验收：Poll 请求关闭的窗口被主循环逐出，Host.Run 随最后一个窗口关闭返回。
func TestHostRunEndsOnLastWindowClose(t *testing.T) {
	host := NewHost(WithIdleWait(time.Millisecond))
	w1, w2 := &stubWindow{closeAfter: 2}, &stubWindow{closeAfter: 5}
	if _, err := host.OpenWindow(WindowConfig{Title: "one", Backend: "stub"}, w1); err != nil {
		t.Fatalf("open one: %v", err)
	}
	if _, err := host.OpenWindow(WindowConfig{Title: "two", Backend: "stub"}, w2); err != nil {
		t.Fatalf("open two: %v", err)
	}
	if err := host.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !w1.closed || !w2.closed {
		t.Fatal("全部窗口应被主循环关闭")
	}
	if got := w2.pollCount(); got != 5 {
		t.Fatalf("后关闭的窗口应轮询满自己的节奏, got %d", got)
	}
}

// ---- IdleWaker 注入验收（P4）：帧时钟 wake 即时打断主循环空闲等待 ----

// wakeWindow 是实现 IdleWaker 的装配桩：记录 Host 注入的空闲打断函数。
type wakeWindow struct {
	stubWindow
	mu   sync.Mutex
	hook func()
}

func (w *wakeWindow) SetWakeHook(fn func()) {
	w.mu.Lock()
	w.hook = fn
	w.mu.Unlock()
}

func (w *wakeWindow) getHook() func() {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.hook
}

// OpenWindow 装配时向实现 IdleWaker 的窗口注入 h.wakeUp——帧时钟 RequestFrame
// 经此钩子即时唤醒主循环（帧节奏不被 idleWait 量化，高刷前提）。
func TestOpenWindowInjectsWakeHook(t *testing.T) {
	host := NewHost(WithIdleWait(time.Millisecond))
	w := &wakeWindow{stubWindow: stubWindow{closeAfter: 2}}
	if _, err := host.OpenWindow(WindowConfig{Title: "wake", Backend: "stub"}, w); err != nil {
		t.Fatalf("open: %v", err)
	}
	if w.getHook() == nil {
		t.Fatal("实现 IdleWaker 的窗口应在装配时拿到 wakeHook")
	}
	// 注入的钩子必须非阻塞且能真实唤醒空闲中的主循环：调用后 Run 正常推进到
	// 窗口请求关闭并退出（hook 即 h.wakeUp，唤醒即消耗一次空闲等待）。
	if err := host.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !w.closed {
		t.Fatal("窗口应被主循环关闭")
	}
}

// 未实现 IdleWaker 的窗口不受影响（可选扩展，无强制）。
func TestOpenWindowWithoutIdleWakerStillWorks(t *testing.T) {
	host := NewHost(WithIdleWait(time.Millisecond))
	w := &stubWindow{closeAfter: 2}
	if _, err := host.OpenWindow(WindowConfig{Title: "plain", Backend: "stub"}, w); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := host.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !w.closed {
		t.Fatal("窗口应被主循环关闭")
	}
}
