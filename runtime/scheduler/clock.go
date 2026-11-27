package scheduler

import (
	"sync"
	"sync/atomic"
	"time"
)

// ManualClock 是受控时钟：测试与 headless 用。Tick 才推进帧时机，帧的产生
// 完全确定——验收「同一刷新周期内多次 Signal 更新只提交一次 Frame」的核心工具。
type ManualClock struct {
	mu      sync.Mutex
	now     time.Duration
	pending bool
	fn      func(FrameTime)
	frames  uint64 // 已派发的帧次数（含非脏空帧？不含——handleFrame 由 Tick 直达）
}

// NewManualClock 创建受控时钟。
func NewManualClock() *ManualClock { return &ManualClock{} }

func (c *ManualClock) Now() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *ManualClock) OnFrame(fn func(FrameTime)) {
	c.mu.Lock()
	c.fn = fn
	c.mu.Unlock()
}

// RequestFrame 记录 pending。可重入（幂等）。
func (c *ManualClock) RequestFrame() {
	c.mu.Lock()
	c.pending = true
	c.mu.Unlock()
}

// CancelFrame 清除 pending。
func (c *ManualClock) CancelFrame() {
	c.mu.Lock()
	c.pending = false
	c.mu.Unlock()
}

// Pending 报告是否有未消费的帧请求（测试断言用）。
func (c *ManualClock) Pending() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pending
}

// Tick 推进一个帧时机：若有 pending，派发一次 FrameTime 回调并返回 true；
// 无 pending（空闲）则什么都不发生并返回 false——这正是「无变化不绘制」
// 的可观测行为。
func (c *ManualClock) Tick(step time.Duration) bool {
	c.mu.Lock()
	c.now += step
	pending := c.pending
	c.pending = false
	fn := c.fn
	c.mu.Unlock()
	if !pending || fn == nil {
		return false
	}
	fn(FrameTime{Timestamp: c.now})
	return true
}

// TickerClock 是桌面兜底时钟：goroutine 内部 time.Ticker 按周期对齐帧时机。
//
// 平台没有直接暴露显示同步（Windows DXGI swap-wait / macOS CVDisplayLink）时
// 用它兜底；周期取显示刷新周期的近似值。空闲时自动停止 goroutine（无变化
// 彻底零绘制零唤醒），RequestFrame 重新启动——「无变化不触发 GPU Present」
// 的桌面实现路径。
type TickerClock struct {
	interval time.Duration

	stopCh  chan struct{}
	mu      sync.Mutex
	started bool
	pending atomic.Bool
	fn      atomic.Value // func(FrameTime)
	closed  atomic.Bool

	lastNow atomic.Int64 // 纳秒，Delta 计算基准（时钟内部单调推进）
}

// NewTickerClock 创建周期为 interval 的兜底时钟（如 16.67ms ≈ 60Hz）。
func NewTickerClock(interval time.Duration) *TickerClock {
	if interval <= 0 {
		interval = time.Second / 60
	}
	return &TickerClock{interval: interval, stopCh: make(chan struct{})}
}

func (c *TickerClock) Now() time.Duration { return time.Duration(c.lastNow.Load()) }

func (c *TickerClock) OnFrame(fn func(FrameTime)) { c.fn.Store(fn) }

// RequestFrame 标脏并确保 ticker goroutine 在跑。
func (c *TickerClock) RequestFrame() {
	c.pending.Store(true)
	if c.closed.Load() {
		return
	}
	c.mu.Lock()
	needStart := !c.started
	c.started = true
	c.mu.Unlock()
	if needStart {
		go c.loop()
	}
}

// CancelFrame 清脏（goroutine 下一次 tick 自然空转）。
func (c *TickerClock) CancelFrame() { c.pending.Store(false) }

// Close 停止 goroutine，释放资源。幂等。
func (c *TickerClock) Close() {
	if c.closed.CompareAndSwap(false, true) {
		close(c.stopCh)
	}
}

func (c *TickerClock) loop() {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case t := <-ticker.C:
			c.mu.Lock()
			c.started = c.pending.Load() // 空闲即停：无 pending 则 goroutine 退出
			keepRunning := c.started
			c.mu.Unlock()
			if c.closed.Load() {
				return
			}
			if !c.pending.CompareAndSwap(true, false) {
				if !keepRunning {
					return
				}
				continue
			}
			now := time.Duration(t.UnixNano())
			c.lastNow.Store(int64(now))
			if fn, ok := c.fn.Load().(func(FrameTime)); ok && fn != nil {
				fn(FrameTime{Timestamp: now})
			}
			// 派发后若无新 pending，下一 tick 退出 goroutine（彻底空闲停绘）
			c.mu.Lock()
			if !c.pending.Load() {
				c.started = false
			}
			c.mu.Unlock()
		}
	}
}
