package gxapp_test

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	"github.com/robertpanvip/gox/runtime/scheduler"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// ---- 阶段 1 验收（BACKEND_ARCHITECTURE_V2 §九）：统一帧调度 ----
//
// 核心验收：同一刷新周期内多次 Signal 更新只提交一次 Frame；
// 无变化时零绘制；动画经同一时钟续帧、结束后自动停止。

// sigTextInst 构造一个响应式文本视图：<b>{n}</b>，n 来自 signal。
func sigTextInst(n signal.Signal[int]) *gxscene.Instance {
	return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "b"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return n() })
}

// clockWindow 记录绘制次数与输出文本（TextBackend 全量），供帧数与内容双重断言。
type clockWindow struct {
	countingWindow
	buf strings.Builder
}

func (w *clockWindow) Draw(r *gxscene.Node) error {
	w.draws++
	w.lastRoot = r
	_, err := w.buf.WriteString((gxscene.TextBackend{}).Paint(r))
	return err
}

// TestFrameClockBatchesSignalsInSameTick：同一刷新周期内多次 Signal 更新只提交一帧。
func TestFrameClockBatchesSignalsInSameTick(t *testing.T) {
	clock := scheduler.NewManualClock()
	win := &clockWindow{}
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()
	app.SetFrameClock(clock)
	if app.FrameScheduler() == nil {
		t.Fatal("SetFrameClock 后 FrameScheduler 应可用")
	}

	n := signal.New(0)
	if err := app.Render(func() any { return sigTextInst(n) }); err != nil {
		t.Fatal(err)
	}
	framesAfterRender := win.draws

	// 同一刷新周期内连续 5 次 Signal 更新（之间不 Tick）：一次都不应绘制。
	for i := 1; i <= 5; i++ {
		n(i)
	}
	if got := win.draws; got != framesAfterRender {
		t.Fatalf("平台帧时机未到就绘制了：draws %d → %d（Signal 与 Present 未解耦）",
			framesAfterRender, got)
	}

	// 一次 Tick = 一次屏幕刷新：恰好提交一帧。
	if !clock.Tick(time.Millisecond * 17) {
		t.Fatal("有脏变更却未派发帧")
	}
	if got := win.draws; got != framesAfterRender+1 {
		t.Fatalf("同一刷新周期内 5 次 Signal 更新提交了 %d 帧，应为 1 帧（draws=%d）",
			got-framesAfterRender, got)
	}

	// 空闲 Tick：无变化零绘制。
	if clock.Tick(time.Millisecond * 17) {
		t.Fatal("无变化时仍派发了帧")
	}
	if got := win.draws; got != framesAfterRender+1 {
		t.Fatalf("空闲 Tick 后 draws 前进到 %d", got)
	}

	// 合并帧内容 = 最后一次更新值。
	if !strings.Contains(win.buf.String(), "5") {
		t.Fatalf("合并帧应包含最后一次更新值 5:\n%s", win.buf.String())
	}
}

// TestFrameClockIdleStopsPainting：无变化时连续 Tick 不产生任何帧。
func TestFrameClockIdleStopsPainting(t *testing.T) {
	clock := scheduler.NewManualClock()
	win := &clockWindow{}
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()
	app.SetFrameClock(clock)
	if err := app.Render(func() any { return sigTextInst(signal.New(1)) }); err != nil {
		t.Fatal(err)
	}
	base := win.draws
	for i := 0; i < 100; i++ {
		clock.Tick(time.Millisecond * 17)
	}
	if win.draws != base {
		t.Fatalf("100 次空闲 Tick 产生了 %d 帧绘制", win.draws-base)
	}
}

// serialExecutor 是串行任务队列：模拟真实 GUI 的 UI 线程队列——Dispatch 并发
// 投递、单线程串行执行（保持 Signal 单线程语义），drain 显式落定队列。
type serialExecutor struct {
	mu sync.Mutex
	q  []func()
}

func (e *serialExecutor) Post(fn func()) error {
	e.mu.Lock()
	e.q = append(e.q, fn)
	e.mu.Unlock()
	return nil
}

func (e *serialExecutor) drain() {
	for {
		e.mu.Lock()
		if len(e.q) == 0 {
			e.mu.Unlock()
			return
		}
		fns := e.q
		e.q = nil
		e.mu.Unlock()
		for _, fn := range fns {
			fn()
		}
	}
}

// TestFrameClockConcurrentWritersBatch：后台任务经 app.Dispatch（合法并发路径）
// 投递到 UI 队列，多个来源的变更在 UI 上下文串行落定后合并为一帧提交。
//
// 注意：Signal 严格单线程（signal.go 线程模型原则），后台 goroutine 不得直接
// 写 Signal——那会并发损坏依赖追踪 map。经 Dispatch 投递到 UI 执行上下文。
func TestFrameClockConcurrentWritersBatch(t *testing.T) {
	clock := scheduler.NewManualClock()
	exec := &serialExecutor{}
	win := &clockWindow{}
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()
	app.SetFrameClock(clock)
	app.SetUIExecutor(exec)

	n := signal.New(0)
	if err := app.Render(func() any { return sigTextInst(n) }); err != nil {
		t.Fatal(err)
	}
	base := win.draws

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			_ = app.Dispatch(func() { n(k + 1) })
		}(i)
	}
	wg.Wait()
	exec.drain() // UI 上下文串行落定全部变更（每次写入触发帧边界 → RequestFrame）
	clock.Tick(time.Millisecond * 17)
	if got := win.draws - base; got != 1 {
		t.Fatalf("8 个后台 Dispatch 更新串行落定后应只提交 1 帧，实际 %d 帧", got)
	}
	// 投递顺序不定：合并帧的文本应是 8 个更新值之一（buf 里除首帧 0 外恰好一个新值）。
	for v := 1; v <= 8; v++ {
		if strings.Contains(win.buf.String(), itoaTest(v)) {
			return
		}
	}
	t.Fatalf("合并帧未包含任何更新值 1..8:\n%s", win.buf.String())
}

// itoaTest 最小整数转字符串（避免仅为测试引 strconv 的名字冲突）。
func itoaTest(v int) string {
	if v == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

// TestFrameClockRAFAnimationLoopStopsWhenIdle：RAF 动画经由统一时钟续帧，
// 回调停止登记后循环自动终止。
func TestFrameClockRAFAnimationLoopStopsWhenIdle(t *testing.T) {
	clock := scheduler.NewManualClock()
	win := &clockWindow{}
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()
	app.SetFrameClock(clock)

	var ticks atomic.Int64
	cap := &atomic.Value{}
	if err := app.Render(func() any { return canvasRAFInst(cap) }); err != nil {
		t.Fatal(err)
	}
	el := cap.Load().(*gxscene.Element)

	// 动画：每帧登记一次 RAF，共 3 次续帧后停止。
	var tick func()
	tick = func() {
		if ticks.Add(1) <= 3 {
			el.RequestAnimationFrame(tick)
		}
	}
	el.RequestAnimationFrame(tick)

	base := win.draws
	for i := 0; i < 20; i++ {
		clock.Tick(time.Millisecond * 17)
	}
	// 首次 RAF + 3 次续帧 = 恰好 4 次绘制；多余空闲 Tick 不产生帧。
	if got := win.draws - base; got != 4 {
		t.Fatalf("动画应恰好执行 4 帧（1 次 RAF + 3 次续帧），实际 %d", got)
	}
	if ticks.Load() < 4 {
		t.Fatalf("动画回调执行次数不足：%d", ticks.Load())
	}
	// 停止后：不再预约、不再绘制。
	if clock.Pending() {
		t.Fatal("动画结束后仍预约帧（未停止绘制）")
	}
	idle := win.draws
	for i := 0; i < 10; i++ {
		clock.Tick(time.Millisecond * 17)
	}
	if win.draws != idle {
		t.Fatalf("动画结束后 Tick 产生了 %d 帧绘制", win.draws-idle)
	}
}

// TestFrameClockPauseResume：暂停期间只标脏不预约，恢复后补一帧。
func TestFrameClockPauseResume(t *testing.T) {
	clock := scheduler.NewManualClock()
	win := &clockWindow{}
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()
	app.SetFrameClock(clock)
	if err := app.Render(func() any { return sigTextInst(signal.New(0)) }); err != nil {
		t.Fatal(err)
	}
	sched := app.FrameScheduler()

	sched.Pause()
	base := win.draws
	app.Update()
	if clock.Pending() {
		t.Fatal("暂停期间仍向平台预约帧")
	}
	sched.Resume()
	if !clock.Pending() {
		t.Fatal("恢复后未补约帧")
	}
	if !clock.Tick(time.Millisecond * 17) {
		t.Fatal("恢复后的帧未派发")
	}
	if got := win.draws - base; got != 1 {
		t.Fatalf("暂停期间积累的变更应在恢复后一次提交，实际 %d 帧", got)
	}
}

// TestTickerClockIdleStops：兜底时钟空闲后停止派发（无变化零持续绘制）。
func TestTickerClockIdleStops(t *testing.T) {
	clock := scheduler.NewTickerClock(time.Millisecond * 4)
	defer clock.Close()

	var fired atomic.Int64
	clock.OnFrame(func(scheduler.FrameTime) { fired.Add(1) })
	clock.RequestFrame()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && fired.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	if fired.Load() == 0 {
		t.Fatal("TickerClock 未派发帧")
	}
	// 无新请求：超过 3 个周期后不应再派发。
	time.Sleep(time.Millisecond * 40)
	if after := fired.Load(); after > 2 {
		t.Fatalf("空闲后仍持续派发帧：%d 次", after)
	}
}
