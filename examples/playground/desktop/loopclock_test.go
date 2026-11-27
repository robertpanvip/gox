package main

import (
	"sync"
	"testing"
	"time"

	"github.com/robertpanvip/gox/runtime/scheduler"
)

// ---- 阶段 4：Desktop 迁移验证（无头）----
//
// 沙箱无显示器，无法真开 GLFW 窗口实测 60/120/144Hz 的 vsync 节拍；但主循环
// 时钟的全部可观测语义——pending 合并、唤醒去重、空闲零绘制、Delta 精度、
// 动画续帧不重入——都可以用注入的可控时钟 + 模拟主循环在无头环境验证。
// vsync 硬件对齐由 SwapInterval 承担（平台职责，见架构文档平台矩阵）。

// fakeClock 是可控单调时钟：advance 推进时间，emit 读取。
type fakeClock struct {
	mu sync.Mutex
	t  time.Duration
}

func (f *fakeClock) now() time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeClock) advance(d time.Duration) {
	f.mu.Lock()
	f.t += d
	f.mu.Unlock()
}

// newTestClock 构造注入可控时钟与唤醒计数的 loopClock。
func newTestClock() (*loopClock, *fakeClock, *int) {
	fc := &fakeClock{}
	wakes := 0
	c := &loopClock{
		wake: func() { wakes++ },
		now:  fc.now,
	}
	return c, fc, &wakes
}

// runMainLoopRounds 模拟 GLFW 主循环：每轮 = 一拍（1/Hz 秒）→ emit。
// 返回派发（=绘制）次数与累计 Delta 列表。
func runMainLoopRounds(c *loopClock, fc *fakeClock, hz int, rounds int) (paints int, deltas []time.Duration) {
	c.OnFrame(func(ft scheduler.FrameTime) {
		paints++
		deltas = append(deltas, ft.Delta)
	})
	period := time.Second / time.Duration(hz)
	for i := 0; i < rounds; i++ {
		c.RequestFrame() // 每帧都有请求（稳态绘制）
		fc.advance(period)
		if c.emit() {
			// emit 返回 true 表示本帧有绘制（真实主循环里随后调 paintNow）。
		}
	}
	return paints, deltas
}

// TestLoopClockMergesRequestsInSameCycle：同一刷新周期内任意多次请求合并为
// 一次派发 + 一次唤醒（三个刷新率下语义一致）。
func TestLoopClockMergesRequestsInSameCycle(t *testing.T) {
	for _, hz := range []int{60, 120, 144} {
		c, fc, wakes := newTestClock()
		var paints int
		c.OnFrame(func(scheduler.FrameTime) { paints++ })

		// 同周期内狂发 100 次请求。
		for i := 0; i < 100; i++ {
			c.RequestFrame()
		}
		if w := *wakes; w != 1 {
			t.Errorf("%dHz: 同周期 %d 次请求应只唤醒 1 次，实际 %d 次", hz, 100, w)
		}
		fc.advance(time.Second / time.Duration(hz))
		if !c.emit() {
			t.Fatalf("%dHz: 有 pending 的轮次 emit 应返回 true", hz)
		}
		if paints != 1 {
			t.Errorf("%dHz: 合并后应恰好绘制 1 帧，实际 %d", hz, paints)
		}
		// 消费后再空转若干轮：pending 已清，零绘制零唤醒。
		fc.advance(time.Second / time.Duration(hz))
		for i := 0; i < 5; i++ {
			if c.emit() {
				t.Errorf("%dHz: 空闲轮 emit 不应返回 true", hz)
			}
		}
		if *wakes != 1 {
			t.Errorf("%dHz: 空闲轮不应产生新唤醒", hz)
		}
	}
}

// TestLoopClockDeltaAcrossRefreshRates：60/120/144Hz 节拍下 Delta 应等于
// 刷新周期（±5% 容差），动画插值直接可用。
func TestLoopClockDeltaAcrossRefreshRates(t *testing.T) {
	cases := []struct {
		hz     int
		period time.Duration
	}{
		{60, 16666 * time.Microsecond},
		{120, 8333 * time.Microsecond},
		{144, 6944 * time.Microsecond},
	}
	for _, tc := range cases {
		c, fc, _ := newTestClock()
		_, deltas := runMainLoopRounds(c, fc, tc.hz, 20)
		if len(deltas) < 19 {
			t.Fatalf("%dHz: 帧数不足（%d）", tc.hz, len(deltas))
		}
		// 首帧 Delta=0（last 未初始化），其余每帧应等于周期。
		for i, d := range deltas[1:19] {
			want := tc.period
			tol := want / 20
			if d > want+tol || d+tol < want {
				t.Errorf("%dHz: 第 %d 帧 Delta=%v，期望 ≈%v（±%v）", tc.hz, i+1, d, want, tol)
			}
		}
	}
}

// TestLoopClockFrameIDMonotonic：FrameID 只在有 pending 的轮次递增。
func TestLoopClockFrameIDMonotonic(t *testing.T) {
	c, fc, _ := newTestClock()
	var ids []uint64
	c.OnFrame(func(ft scheduler.FrameTime) { ids = append(ids, ft.FrameID) })

	c.RequestFrame()
	fc.advance(8333 * time.Microsecond)
	c.emit() // 帧 1
	c.RequestFrame()
	fc.advance(8333 * time.Microsecond)
	c.emit() // 帧 2
	// 连续两个 pending 轮（两轮各一次请求）。
	c.RequestFrame()
	fc.advance(8333 * time.Microsecond)
	c.emit() // 帧 3
	c.RequestFrame()
	fc.advance(8333 * time.Microsecond)
	c.emit() // 帧 4

	if len(ids) != 4 {
		t.Fatalf("应派发 4 帧，实际 %d", len(ids))
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] != ids[i-1]+1 {
			t.Errorf("FrameID 应严格递增：ids=%v", ids)
		}
	}
}

// TestLoopClockCancelFrameDropsPending：Cancel 后 emit 不派发。
func TestLoopClockCancelFrameDropsPending(t *testing.T) {
	c, fc, _ := newTestClock()
	var paints int
	c.OnFrame(func(scheduler.FrameTime) { paints++ })
	c.RequestFrame()
	c.CancelFrame()
	fc.advance(16666 * time.Microsecond)
	if c.emit() {
		t.Fatal("Cancel 后 emit 不应派发")
	}
	if paints != 0 {
		t.Fatalf("Cancel 后不应绘制，实际 %d", paints)
	}
}

// TestSchedulerDesktopClockLowRate：请求速率低于刷新率时，每请求各得一个
// 独立帧（不丢帧），绘制次数 = 请求次数。
func TestSchedulerDesktopClockLowRate(t *testing.T) {
	for _, hz := range []int{60, 120, 144} {
		c, fc, _ := newTestClock()
		s := scheduler.New(c)
		var paints int
		s.SetOnFrame(func(scheduler.FrameTime) { paints++ })

		rounds := 90
		period := time.Second / time.Duration(hz)
		requests := 30 // 前 90 轮每 3 轮请求一次（间隔远大于刷新周期，不丢帧）
		for i := 0; i < rounds; i++ {
			if i%3 == 0 {
				s.RequestFrame()
			}
			fc.advance(period)
			c.emit()
		}
		if paints != requests {
			t.Errorf("%dHz: 低速率请求 %d 次应绘制 %d 帧，实际 %d（不得丢帧）", hz, requests, requests, paints)
		}
		s.Close()
	}
}

// TestSchedulerDesktopClockHighRate：请求速率高于刷新率（2x/10x）时，绘制
// 次数被刷新率上限钳制——同周期请求合并，绝不超帧。
func TestSchedulerDesktopClockHighRate(t *testing.T) {
	for _, tc := range []struct{ hz, mult int }{{60, 2}, {60, 10}, {120, 2}, {144, 2}, {144, 10}} {
		c, fc, _ := newTestClock()
		s := scheduler.New(c)
		var paints int
		s.SetOnFrame(func(scheduler.FrameTime) { paints++ })

		rounds := 120
		period := time.Second / time.Duration(tc.hz)
		for i := 0; i < rounds; i++ {
			for k := 0; k < tc.mult; k++ {
				s.RequestFrame() // 每轮 mult 次请求
			}
			fc.advance(period)
			c.emit()
		}
		// 每轮至多 1 次绘制（合并上限 = 刷新率）。
		if paints > rounds {
			t.Errorf("%dHz×%d: 绘制 %d 次超过轮数上限 %d（超帧）", tc.hz, tc.mult, paints, rounds)
		}
		if paints < rounds/2 {
			t.Errorf("%dHz×%d: 绘制 %d 次远低于预期（丢帧）", tc.hz, tc.mult, paints)
		}
		s.Close()
	}
}

// TestSchedulerDesktopAnimationLoop：动画续帧语义——回调内 RequestFrame 形成下一
// 帧循环，每帧恰好一次绘制；停止续帧后自动停绘（无空转）。
func TestSchedulerDesktopAnimationLoop(t *testing.T) {
	for _, hz := range []int{60, 144} {
		c, fc, _ := newTestClock()
		s := scheduler.New(c)
		var paints int
		period := time.Second / time.Duration(hz)

		s.SetOnFrame(func(scheduler.FrameTime) {
			paints++
			if paints < 10 {
				s.RequestFrame() // 动画续帧
			}
			// paints==10 起不再续帧 → 自动停绘
		})

		s.RequestFrame() // 启动动画
		for i := 0; i < 200 && paints < 10; i++ {
			fc.advance(period)
			c.emit()
		}
		if paints != 10 {
			t.Fatalf("%dHz: 动画应恰好绘制 10 帧后停止，实际 %d", hz, paints)
		}
		// 停止后空转 30 轮：零绘制。
		stop := paints
		for i := 0; i < 30; i++ {
			fc.advance(period)
			c.emit()
		}
		if paints != stop {
			t.Errorf("%dHz: 停止续帧后不应继续绘制（+%d 帧空转）", hz, paints-stop)
		}
		s.Close()
	}
}

// TestSchedulerDesktopPauseResume：后台窗口暂停期间请求只标脏；恢复后统一补
// 一帧（不多不少）。
func TestSchedulerDesktopPauseResume(t *testing.T) {
	c, fc, _ := newTestClock()
	s := scheduler.New(c)
	var paints int
	period := 16666 * time.Microsecond
	s.SetOnFrame(func(scheduler.FrameTime) { paints++ })

	s.Pause()
	for i := 0; i < 5; i++ {
		s.RequestFrame() // 暂停期间多次请求
		fc.advance(period)
		c.emit()
	}
	if paints != 0 {
		t.Fatalf("暂停期间不应绘制，实际 %d", paints)
	}
	s.Resume() // 补一帧
	fc.advance(period)
	c.emit()
	if paints != 1 {
		t.Fatalf("恢复后应补恰好 1 帧，实际 %d", paints)
	}
	// 再空转若干轮不再绘制。
	for i := 0; i < 10; i++ {
		fc.advance(period)
		c.emit()
	}
	if paints != 1 {
		t.Fatalf("补帧后不应继续绘制，实际 %d", paints)
	}
	s.Close()
}
