package main

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/robertpanvip/gox/runtime/scheduler"
)

// paintClock 把 x/mobile 的 paint.Event（系统 Choreographer / vsync 驱动）
// 适配为 scheduler.FrameClock（BACKEND_ARCHITECTURE_V2 阶段 1 的 Android 实现）。
//
// RequestFrame：置 pending 并向事件循环 Send 一次 paint.Event（CAS 去重，
// 高频 Signal 更新不会堆积事件）；paint.Event 到达时主循环调用 emit() 派发
// OnFrame → FrameScheduler 的 dirty 门控决定是否真正绘制（无变化零 blit）。
// 动画续帧：drawNowErr 内的 RAF scheduleRAFNext → Scheduler.RequestFrame →
// 本时钟再次 Send paint.Event，形成 vsync 对齐的动画循环，取代原来
// hasPendingPaint 直发 paint.Event 的旁路机制（后者仍保留兜底，见 main.go）。
type paintClock struct {
	send    func() // 发送 paint.Event 的回调（由 main 注入 a.Send 包装）
	pending atomic.Bool

	mu      sync.Mutex
	fn      func(scheduler.FrameTime)
	last    time.Duration
	frameID uint64
}

func newPaintClock(send func()) *paintClock {
	return &paintClock{send: send}
}

func (c *paintClock) Now() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

func (c *paintClock) OnFrame(fn func(scheduler.FrameTime)) {
	c.mu.Lock()
	c.fn = fn
	c.mu.Unlock()
}

// RequestFrame 置 pending 并 Send 一次 paint.Event；pending 已置位时不再发送
// （已排队的事件到达时会带走本次变更——Scheduler 读 dirty 而非 pending）。
func (c *paintClock) RequestFrame() {
	if c.pending.Swap(true) {
		return
	}
	if c.send != nil {
		c.send()
	}
}

func (c *paintClock) CancelFrame() { c.pending.Store(false) }

// Pending 报告是否有未消费的帧预约（main.go 判断是否需要继续自发 paint）。
func (c *paintClock) Pending() bool { return c.pending.Load() }

// emit 在 paint.Event 处理里调用：派发一次 OnFrame。
// dirty 门控在 Scheduler 内——空闲帧回调直接空跑，paintNow 的 dirty 短路
// 保证不发生 CPU raster 与纹理上传。
func (c *paintClock) emit() {
	c.pending.Store(false)
	now := time.Duration(time.Now().UnixNano())
	c.mu.Lock()
	fn := c.fn
	last := c.last
	ft := scheduler.FrameTime{Timestamp: now}
	if last != 0 && now > last {
		ft.Delta = now - last
	}
	c.last = now
	c.frameID++
	ft.FrameID = c.frameID
	c.mu.Unlock()
	if fn != nil {
		fn(ft)
	}
}
