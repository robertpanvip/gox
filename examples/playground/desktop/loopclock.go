package main

import (
	"sync"
	"time"

	"github.com/robertpanvip/gox/runtime/scheduler"

	"github.com/go-gl/glfw/v3.3/glfw"
)

// loopClock 是 GLFW 主循环驱动的统一帧时钟（BACKEND_ARCHITECTURE_V2 阶段 1
// 的桌面实现）。
//
// RequestFrame 置 pending 并经 wake 即时唤醒 WaitEventsTimeout——Signal 更新
// 不再受 20ms 轮询延迟，且多次更新在同一主循环轮次内合并为一次绘制
// （Scheduler dirty 门控）。帧回调（OnFrame → drawNowErr）在 Run 主循环里
// emit 时执行，天然满足 GLFW 主线程亲和性；vsync 对齐由 SwapInterval 承担。
//
// 时钟本体零平台依赖：唤醒（wake）与单调时间源（now）均为注入点——装配层
// 传入 glfw.PostEmptyEvent / time.Now，测试注入计数器与可控时钟，即可无头
// 验证 60/120/144Hz 下的合并、门控与 Delta 语义。
//
// 对齐显示刷新的进一步演进（DXGI swap-wait / CVDisplayLink）见架构文档
// §FrameClock 平台矩阵；当前主循环轮询 + 帧级短路已达成「无变化零绘制」。
type loopClock struct {
	mu      sync.Mutex
	pending bool
	fn      func(scheduler.FrameTime)
	last    time.Duration
	frameID uint64

	// wake 唤醒平台事件循环（装配层注入 glfw.PostEmptyEvent；测试注入
	// 计数器）。pending 从无到有时调用一次——同周期内的重复请求合并，
	// 不重复打断 WaitEventsTimeout。
	wake func()
	// now 单调时间源（默认 time.Now；测试注入可控时钟推演各刷新率）。
	now func() time.Duration
}

// FrameClock 返回主循环驱动的统一帧时钟。必须在 Run 前接线（Run 内 emit）。
func (g *glWindow) FrameClock() scheduler.FrameClock {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.frameClock == nil {
		g.frameClock = &loopClock{
			wake: glfw.PostEmptyEvent,
			now:  func() time.Duration { return time.Duration(time.Now().UnixNano()) },
		}
	}
	return g.frameClock
}

func (c *loopClock) Now() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

func (c *loopClock) OnFrame(fn func(scheduler.FrameTime)) {
	c.mu.Lock()
	c.fn = fn
	c.mu.Unlock()
}

// RequestFrame 置 pending 并唤醒主循环。可任意高频调用——pending 合并，
// 主循环下一轮 emit 时一次性带走全部变更。同周期重复请求不重复唤醒。
func (c *loopClock) RequestFrame() {
	c.mu.Lock()
	needWake := !c.pending
	c.pending = true
	wake := c.wake
	c.mu.Unlock()
	if needWake && wake != nil {
		wake()
	}
}

func (c *loopClock) CancelFrame() {
	c.mu.Lock()
	c.pending = false
	c.mu.Unlock()
}

// Pending 报告是否有未消费的帧请求（诊断用）。
func (c *loopClock) Pending() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pending
}

// emit 在主循环每轮调用：pending 时派发一次 OnFrame（drawNowErr），并返回
// true 表示「本帧需要绘制」（Run 据此决定是否调 paintNow——无脏零 swap）。
func (c *loopClock) emit() bool {
	c.mu.Lock()
	nowFn := c.now
	c.mu.Unlock()
	var now time.Duration
	if nowFn != nil {
		now = nowFn()
	} else {
		now = time.Duration(time.Now().UnixNano())
	}
	c.mu.Lock()
	pending := c.pending
	c.pending = false
	fn := c.fn
	last := c.last
	if pending {
		c.frameID++
	}
	c.last = now
	c.mu.Unlock()
	if !pending || fn == nil {
		return false
	}
	ft := scheduler.FrameTime{Timestamp: now, FrameID: c.frameID}
	if last != 0 && now > last {
		ft.Delta = now - last
	}
	fn(ft)
	return true
}
