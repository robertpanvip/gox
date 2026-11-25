// P5 验收测试：对应提案 §10.4「Watchdog 测试」与 §12 P5。
//   - 使用可控的阻塞回调验证超时事件（分级、每级别一次）
//   - 任务结束后不再报告该任务
//   - 正常任务不误报
//   - Loop 关闭 / Stop 后监测 goroutine 退出
//   - 栈采集失败不会导致监测崩溃，UI 照常执行
//   - Report 回调 panic 隔离；诊断本身不阻塞 UI 任务
//
// 注意：Watchdog 只报告、不声称恢复（§10.1）；本文件不做任何「恢复」类断言。

package loop

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchdogSlowSevereStuckProgression(t *testing.T) {
	l := New(Config{})
	events := make(chan WatchEvent, 32)
	var stackCalls int32
	release := make(chan struct{})
	w := NewWatchdog(l, WatchConfig{
		Slow:          30 * time.Millisecond,
		Severe:        120 * time.Millisecond,
		Stuck:         240 * time.Millisecond,
		CheckInterval: 5 * time.Millisecond,
		StackCapture: func() string {
			atomic.AddInt32(&stackCalls, 1)
			return "stack-dump"
		},
		Report: func(ev WatchEvent) { events <- ev },
	})
	w.Start()

	done := make(chan struct{})
	go func() { // 宿主：取出并执行这个受控阻塞宏任务
		defer close(done)
		l.Post(func() { <-release })
		for !l.RunOneMacro() {
			time.Sleep(time.Millisecond)
		}
	}()

	var got []WatchEvent
	deadline := time.After(3 * time.Second)
	for len(got) < 3 {
		select {
		case ev := <-events:
			got = append(got, ev)
		case <-deadline:
			close(release)
			t.Fatalf("超时事件不足: %v", got)
		}
	}
	close(release)
	<-done

	// 结束后继续过几个轮询周期：不得再报告该任务
	time.Sleep(40 * time.Millisecond)
	select {
	case ev := <-events:
		t.Fatalf("任务结束后仍在报告: %v", ev)
	default:
	}

	if got[0].Level != LevelSlow || got[1].Level != LevelSevere || got[2].Level != LevelStuck {
		t.Fatalf("分级进度错误: %v %v %v", got[0].Level, got[1].Level, got[2].Level)
	}
	for _, ev := range got {
		if ev.Task.Kind != KindMacro || ev.Task.ID == 0 {
			t.Fatalf("事件缺任务身份: %+v", ev)
		}
	}
	if got[0].Stack != "" {
		t.Fatal("slow 级不应采集栈")
	}
	if got[1].Stack != "stack-dump" {
		t.Fatal("severe 级应采集栈")
	}
	if atomic.LoadInt32(&stackCalls) != 1 {
		t.Fatalf("同一任务栈应只采集一次: %d", stackCalls)
	}
	w.Stop()
}

func TestWatchdogFastTaskNoFalsePositive(t *testing.T) {
	l := New(Config{})
	var events int32
	w := NewWatchdog(l, WatchConfig{
		Slow: 20 * time.Millisecond, Severe: 40 * time.Millisecond, Stuck: 60 * time.Millisecond,
		CheckInterval: 2 * time.Millisecond,
		Report:        func(WatchEvent) { atomic.AddInt32(&events, 1) },
	})
	w.Start()
	for i := 0; i < 100; i++ {
		l.Post(func() {})
		l.RunOneMacro()
	}
	time.Sleep(60 * time.Millisecond) // 让监测多跑几轮
	if got := atomic.LoadInt32(&events); got != 0 {
		t.Fatalf("快速任务被误报 %d 次", got)
	}
	if _, _, active := l.CurrentTask(); active {
		t.Fatal("任务结束后观测位应复位")
	}
	w.Stop()
}

func TestWatchdogExitsOnLoopStop(t *testing.T) {
	l := New(Config{})
	w := NewWatchdog(l, WatchConfig{Report: func(WatchEvent) {}})
	w.Start()
	if err := l.Stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Loop 关闭后监测 goroutine 未退出")
	}
	w.Stop() // 自动退出后再 Stop 必须不挂起（幂等）
	w.Stop()
}

func TestWatchdogStackCaptureFailureIsolated(t *testing.T) {
	l := New(Config{})
	var events int32
	w := NewWatchdog(l, WatchConfig{
		Slow: 20 * time.Millisecond, Severe: 60 * time.Millisecond, Stuck: 100 * time.Millisecond,
		CheckInterval: 5 * time.Millisecond,
		StackCapture:  func() string { panic("采集器炸了") },
		Report: func(ev WatchEvent) {
			if ev.Stack != "" {
				t.Errorf("采集失败时栈应为空: %q", ev.Stack)
			}
			atomic.AddInt32(&events, 1)
		},
	})
	w.Start()
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.Post(func() { <-release })
		for !l.RunOneMacro() {
			time.Sleep(time.Millisecond)
		}
	}()
	deadline := time.After(3 * time.Second)
	for atomic.LoadInt32(&events) < 2 { // severe 与 stuck 都到达 → 采集失败未中断监测
		select {
		case <-deadline:
			close(release)
			t.Fatalf("采集 panic 后监测停止: %d", atomic.LoadInt32(&events))
		case <-time.After(5 * time.Millisecond):
		}
	}
	close(release)
	<-done
	w.Stop()
	// Loop 本身不受影响
	l.Post(func() {})
	if !l.RunOneMacro() {
		t.Fatal("栈采集失败不应影响 Loop 任务执行")
	}
}

func TestWatchdogReportPanicIsolated(t *testing.T) {
	l := New(Config{})
	var reports int32
	w := NewWatchdog(l, WatchConfig{
		Slow: 20 * time.Millisecond, Severe: 40 * time.Millisecond, Stuck: 60 * time.Millisecond,
		CheckInterval: 5 * time.Millisecond,
		Report: func(WatchEvent) {
			atomic.AddInt32(&reports, 1)
			panic("reporter boom")
		},
	})
	w.Start()

	// 第一个阻塞任务：事件触发 → Report panic → 监测必须存活
	release1 := make(chan struct{})
	done1 := make(chan struct{})
	go func() {
		defer close(done1)
		l.Post(func() { <-release1 })
		for !l.RunOneMacro() {
			time.Sleep(time.Millisecond)
		}
	}()
	time.Sleep(50 * time.Millisecond)
	close(release1)
	<-done1
	if atomic.LoadInt32(&reports) == 0 {
		t.Fatal("第一个任务没有产生事件")
	}
	// 期间宿主照常执行其他宏任务
	for i := 0; i < 20; i++ {
		l.Post(func() {})
		l.RunOneMacro()
	}
	// 第二个阻塞任务：Report 依然 panic，但事件继续产生 → 监测循环没死
	release2 := make(chan struct{})
	done2 := make(chan struct{})
	go func() {
		defer close(done2)
		l.Post(func() { <-release2 })
		for !l.RunOneMacro() {
			time.Sleep(time.Millisecond)
		}
	}()
	deadline := time.After(3 * time.Second)
	before := atomic.LoadInt32(&reports)
	for atomic.LoadInt32(&reports) <= before {
		select {
		case <-deadline:
			close(release2)
			t.Fatal("Report panic 后监测不再产生事件")
		case <-time.After(5 * time.Millisecond):
		}
	}
	close(release2)
	<-done2
	w.Stop()
}

// 诊断在监测 goroutine 上运行（含慢栈采集），不得阻塞宿主执行 UI 任务。
func TestWatchdogDiagnosticsDoNotBlockUI(t *testing.T) {
	l := New(Config{})
	var mu sync.Mutex
	var firstReport = true
	w := NewWatchdog(l, WatchConfig{
		Slow: 10 * time.Millisecond, Severe: time.Second, Stuck: time.Second,
		CheckInterval: time.Millisecond,
		StackCapture:  func() string { time.Sleep(80 * time.Millisecond); return "" }, // 慢诊断
		Report: func(ev WatchEvent) {
			mu.Lock()
			defer mu.Unlock()
			_ = firstReport
		},
	})
	w.Start()

	release := make(chan struct{})
	done := make(chan struct{})
	go func() { // 宿主 goroutine 正在跑那个触发诊断的任务
		defer close(done)
		l.Post(func() { <-release })
		for !l.RunOneMacro() {
			time.Sleep(time.Millisecond)
		}
	}()
	time.Sleep(20 * time.Millisecond) // 诊断已开跑（慢栈采集睡 80ms）

	// 另一个宿主序列：执行 100 个快任务，不应被诊断拖慢
	start := time.Now()
	for i := 0; i < 100; i++ {
		l.Post(func() {})
		l.RunOneMacro()
	}
	cost := time.Since(start)
	close(release)
	<-done
	if cost > 100*time.Millisecond {
		t.Fatalf("诊断阻塞了 UI 任务执行: %v", cost)
	}
	w.Stop()
}

// P1 §9 观测扩展：等待时间、拒绝计数、drain 耗时。
func TestStatsObservability(t *testing.T) {
	l := New(Config{})
	for i := 0; i < 3; i++ {
		l.Post(func() {})
	}
	for l.RunOneMacro() {
	}
	s := l.Stats()
	if s.WaitedCount != 3 || s.PostedMacro != 3 || s.RanMacro != 3 {
		t.Fatalf("等待统计缺失: %+v", s)
	}
	if s.MacroPending != 0 || s.MicroPending != 0 {
		t.Fatalf("pending 字段未清零: %+v", s)
	}
	l.PostMicro(func() {})
	rep := l.RunMicrotasks()
	if rep.Ran != 1 || rep.Took < 0 {
		t.Fatalf("drain 报告/耗时异常: %+v", rep)
	}
	l.Stop()
	if err := l.Post(func() {}); err != ErrClosed {
		t.Fatal(err)
	}
	if err := l.PostMicro(func() {}); err != ErrClosed {
		t.Fatal(err)
	}
	if s := l.Stats(); s.Rejected != 2 {
		t.Fatalf("Rejected=%d want 2", s.Rejected)
	}
}
