package scheduler

import (
	"sync"
	"time"
)

// LoopClock 是「平台事件循环轮次」驱动的统一帧时钟（BACKEND_ARCHITECTURE_V2
// 阶段 1 的桌面实现，从 playground loopClock 沉淀为通用实现）。
//
// RequestFrame 置 pending 并经 wake 即时唤醒事件循环——Signal 更新不受轮询
// 延迟，且多次更新在同一循环轮次内合并为一次绘制（pending 门控）。帧回调
// （OnFrame → 装配层绘制入口）由宿主在主循环每轮调用 Poll 时执行，天然满足
// 主线程亲和；vsync 对齐由平台（SwapInterval / DXGI swap-wait）承担。
//
// 时钟本体零平台依赖：唤醒（wake）与单调时间源（now）均为注入点——桌面装配
// 传入窗口唤醒函数 / time.Now，测试注入计数器与可控时钟，即可无头验证
// 60/120/144Hz 下的合并、门控与 Delta 语义。
type LoopClock struct {
	mu      sync.Mutex
	pending bool
	fn      func(FrameTime)
	last    time.Duration
	frameID uint64

	// wake 唤醒平台事件循环（装配层注入：窗口泵唤醒 / glfw.PostEmptyEvent）。
	// pending 从无到有时调用一次——同周期内的重复请求合并，不重复打断循环。
	wake func()
	// now 单调时间源（默认 time.Now；测试注入可控时钟推演各刷新率）。
	now func() time.Duration
}

// 确保满足 FrameClock 契约。
var _ FrameClock = (*LoopClock)(nil)

// NewLoopClock 构造事件循环轮次帧时钟。wake 可为 nil（纯轮询模式，靠宿主
// 周期性 Poll）；now 为 nil 时用 time.Now。
func NewLoopClock(wake func(), now func() time.Duration) *LoopClock {
	return &LoopClock{wake: wake, now: now}
}

// Now 实现帧时钟契约（FrameTime 的时钟基准）。
func (c *LoopClock) Now() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

// OnFrame 注册帧回调（在宿主主循环轮次内执行；回调里再次 RequestFrame 形成
// 动画循环）。
func (c *LoopClock) OnFrame(fn func(FrameTime)) {
	c.mu.Lock()
	c.fn = fn
	c.mu.Unlock()
}

// RequestFrame 置 pending 并唤醒主循环。可任意高频调用——pending 合并，
// 主循环下一轮 emit 时一次性带走全部变更。同周期重复请求不重复唤醒。
func (c *LoopClock) RequestFrame() {
	c.mu.Lock()
	needWake := !c.pending
	c.pending = true
	wake := c.wake
	c.mu.Unlock()
	if needWake && wake != nil {
		wake()
	}
}

// CancelFrame 清除 pending（撤销未消费的帧请求）。
func (c *LoopClock) CancelFrame() {
	c.mu.Lock()
	c.pending = false
	c.mu.Unlock()
}

// Pending 报告是否有未消费的帧请求（诊断用）。
func (c *LoopClock) Pending() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pending
}

// Poll 在宿主主循环每轮调用：pending 时派发一次 OnFrame（装配层绘制入口），
// 返回 true 表示「本帧有绘制」。无 pending 零回调零分配（无变化不触发
// GPU Present 的桌面路径）。
func (c *LoopClock) Poll() bool {
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
	ft := FrameTime{Timestamp: now, FrameID: c.frameID}
	if last != 0 && now > last {
		ft.Delta = now - last
	}
	fn(ft)
	return true
}
