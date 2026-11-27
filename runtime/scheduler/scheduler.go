// Package scheduler 实现统一帧调度（BACKEND_ARCHITECTURE_V2 阶段 1）。
//
// 核心问题是「Signal 更新频率 ≠ 屏幕刷新频率」：同一刷新周期内多次 Signal
// 更新必须合并为一次提交；没有变化时必须彻底停止绘制；动画必须经由同一
// 时钟续帧，而不是每个后端各写一套（host goroutine 续帧 / skia dirty 维持 /
// Android paint.Event 三套并存的历史遗留）。
//
// 职责切分（评审文档 §三）：
//
//	Scene Commit：保证状态更新正确——由 gxapp 的 commit gate / AfterEffects 负责。
//	Frame Present：决定什么时候把最新状态显示出来——由本包负责。
//
// FrameClock 是平台提供的时钟（Web=requestAnimationFrame、Android=Choreographer、
// iOS=CADisplayLink、桌面=显示同步或对齐事件循环的 TickerClock），不是让 Go
// 自己开固定频率的 Timer 抢刷——实际刷新节奏由平台决定，天然支持 60/90/120/144Hz。
package scheduler

import (
	"sync"
	"time"
)

// FrameTime 是一次帧回调的时间信息（对齐 Web rAF / Android Choreographer 语义）。
type FrameTime struct {
	Timestamp time.Duration // 本帧开始时间（时钟单调基准）
	Delta     time.Duration // 与上一帧的时间差（动画进度计算用）
	FrameID   uint64        // 单调递增帧号
}

// FrameClock 是平台帧时钟。实现契约：
//   - RequestFrame 可并发调用：pending 未消费前的重复请求必须合并（幂等）。
//   - OnFrame 注册的回调在平台的帧时机执行（RAF / Choreographer / vsync 对齐），
//     每次 pending 只回调一次；回调里再次 RequestFrame 形成动画循环。
//   - CancelFrame 撤销未消费的 pending；已排队但平台的取消能力缺失时实现方
//     应保证回调幂等无害（Scheduler 对多余的回调自身也幂等）。
//   - Now 返回单调时钟基准，供 Delta 计算与动画插值。
type FrameClock interface {
	RequestFrame()
	CancelFrame()
	Now() time.Duration
	OnFrame(fn func(FrameTime))
}

// Scheduler 把「重绘请求」与「平台帧时机」连接起来：
//
//	Signal 更新 ──→ RequestFrame（标脏 + 注册一帧）   可任意高频，永不直接绘制
//	平台帧时机 ──→ OnFrame：脏则执行一次绘制回调      无脏 → 不绘制（彻底停绘）
//
// 同一刷新周期内的多次 RequestFrame 合并为一次绘制（dirty 门控 + clock 的
// pending 去重双重保证）；绘制回调里再次 RequestFrame（动画续帧）只会预约
// 下一帧，不会在当前帧内重入。
type Scheduler struct {
	clock FrameClock

	mu       sync.Mutex
	dirty    bool            // 有未绘制的变更
	queued   bool            // clock 侧已有 pending 帧（去重，防回调堆积）
	paused   bool            // 暂停：RequestFrame 只标脏不预约（窗口后台时用）
	onFrame  func(FrameTime) // 用户绘制回调（gxapp.drawNow）
	lastTime time.Duration
	frameID  uint64
	closed   bool
}

// New 创建绑定到 clock 的调度器。clock 不能为 nil。
// 通过 clock.OnFrame 注册内部回调；用户绘制回调用 SetOnFrame 设置。
func New(clock FrameClock) *Scheduler {
	s := &Scheduler{clock: clock}
	clock.OnFrame(s.handleFrame)
	return s
}

// SetOnFrame 设置绘制回调。必须在首帧前设置；nil 回调时帧时机只清脏标志。
func (s *Scheduler) SetOnFrame(fn func(FrameTime)) {
	s.mu.Lock()
	s.onFrame = fn
	s.mu.Unlock()
}

// RequestFrame 请求在下一个平台帧时机绘制一次。可任意并发/高频调用：
// 多次请求在同一刷新周期内合并为一次绘制。
func (s *Scheduler) RequestFrame() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.dirty = true
	if s.queued || s.paused {
		// 已有 pending 帧：本周期内的绘制会带上本次变更（dirty 在回调时读取），
		// 无需重复预约。暂停期间只标脏，恢复时统一补帧。
		s.mu.Unlock()
		return
	}
	s.queued = true
	s.mu.Unlock()
	s.clock.RequestFrame()
}

// CancelFrame 撤销未消费的重绘请求（脏标志与平台 pending 一并清除）。
// 用于窗口不可见等场景：恢复时由 Resume/Paint 事件重新请求。
func (s *Scheduler) CancelFrame() {
	s.mu.Lock()
	s.dirty = false
	s.queued = false
	s.mu.Unlock()
	s.clock.CancelFrame()
}

// Pause 暂停帧预约（窗口进入后台）：RequestFrame 只标脏不预约，恢复后
// 由 Resume 统一补一帧。StopPainting 语义的一部分——后台窗口不占 GPU。
func (s *Scheduler) Pause() {
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
}

// Resume 恢复帧调度：若暂停期间有脏变更，立即预约一帧。
func (s *Scheduler) Resume() {
	s.mu.Lock()
	s.paused = false
	dirty := s.dirty
	queued := s.queued
	if dirty && !queued {
		s.queued = true
	}
	s.mu.Unlock()
	if dirty && !queued {
		s.clock.RequestFrame()
	}
}

// HasPending 报告是否有未绘制的变更（诊断/测试用）。
func (s *Scheduler) HasPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dirty
}

// FrameID 返回已执行的帧号。
func (s *Scheduler) FrameID() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.frameID
}

// Close 停止调度。关闭后 RequestFrame 是 no-op，已排队的帧回调也只会清脏。
func (s *Scheduler) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

// handleFrame 是 clock 的帧回调入口：只有脏时才执行一次绘制。
// 绘制回调内的新 RequestFrame 标脏 + 重新预约，形成下一帧的动画循环。
func (s *Scheduler) handleFrame(ft FrameTime) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.queued = false
	dirty := s.dirty
	s.dirty = false
	last := s.lastTime
	if dirty {
		s.frameID++
	}
	fn := s.onFrame
	s.mu.Unlock()
	if dirty && fn != nil {
		if last == 0 || ft.Timestamp < last {
			// 首帧或时钟回退：Delta 记 0，避免动画大步跳变
			ft.Delta = 0
		} else {
			ft.Delta = ft.Timestamp - last
		}
		ft.FrameID = s.FrameID()
		fn(ft)
	}
	s.mu.Lock()
	s.lastTime = ft.Timestamp
	s.mu.Unlock()
}
