package gxapp

import (
	"sync"

	"github.com/robertpanvip/gox/runtime/signal"
)

// FrameDispatcher 决定「帧重绘请求」在哪个线程/goroutine 上执行。
//
// 响应式 effect 可能在任意 goroutine（如后台数据拉取）中因 signal 写入而重跑；
// 真实 GUI 后端通常要求绘制与事件处理都在 UI 线程。FrameDispatcher 把「谁请求
// 重绘」与「在哪重绘」解耦：effect 侧只调用 RequestFrame，dispatcher 负责把真正
// 的 Draw 回调派发到合适的执行上下文（T2.1）。
//
// 实现必须保证：同一时刻的多次 RequestFrame 合并为一次执行（帧去抖），与 signal
// 的 AfterEffects 帧边界形成两级合并——signal 层合并「effect 批次」，dispatcher
// 合并「跨 goroutine / 跨批次的窗口刷新」。
type FrameDispatcher interface {
	// RequestFrame 请求一次重绘。可并发调用，实现须合并重复请求。
	RequestFrame()
	// SetSink 设置真正执行重绘的回调（通常是 app.drawNow）。
	SetSink(fn func())
	// Close 停止派发并释放资源。幂等。
	Close()
}

// InlineDispatcher 是默认派发器：RequestFrame 立即同步执行 sink。
//
// 它保持 headless / 测试场景的同步语义——写完 signal 立刻可见一帧重绘，无需
// 等待 UI 线程。单线程模型（GUI 事件循环本身就在同一 goroutine 驱动 signal）
// 下，它也是正确且零开销的选择。
type InlineDispatcher struct {
	mu   sync.Mutex
	sink func()
}

func (d *InlineDispatcher) SetSink(fn func()) {
	d.mu.Lock()
	d.sink = fn
	d.mu.Unlock()
}

func (d *InlineDispatcher) RequestFrame() {
	d.mu.Lock()
	fn := d.sink
	d.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (d *InlineDispatcher) Close() {}

// QueueDispatcher 把帧请求派发到创建时绑定的「UI goroutine」执行。
//
// 构造时调用方提供 uiGoid（UI 线程的 goroutine id）与一个 post 函数——post 负责
// 把任务安全地投递到该 goroutine（真实后端用窗口事件循环的 postMessage；测试用
// channel）。RequestFrame 在任意 goroutine 被调用：若当前就是 UI goroutine，直接
// 执行 sink；否则通过 post 投递。多次请求在 UI 线程侧合并为一次 Draw（pending 标志）。
//
// 它解决报告问题 4：后台 goroutine 写 signal 触发的 effect 不会在非 UI 线程 Draw。
type QueueDispatcher struct {
	uiGoid uint64
	post   func(func())

	mu      sync.Mutex
	sink    func()
	pending bool
	closed  bool
}

// NewQueueDispatcher 创建派发到 uiGoid 的队列派发器。
//   - uiGoid: UI 线程的 goroutine id（用 signal.Goid() 取得当前 goroutine id）；
//   - post:   把 func() 投递到 UI 线程执行的函数，必须非 nil。
func NewQueueDispatcher(uiGoid uint64, post func(func())) *QueueDispatcher {
	if post == nil {
		post = func(f func()) { f() }
	}
	return &QueueDispatcher{uiGoid: uiGoid, post: post}
}

func (d *QueueDispatcher) SetSink(fn func()) {
	d.mu.Lock()
	d.sink = fn
	d.mu.Unlock()
}

func (d *QueueDispatcher) RequestFrame() {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return
	}
	fn := d.sink
	if d.pending {
		d.mu.Unlock()
		return // 已有排队帧，去抖合并
	}
	d.pending = true
	d.mu.Unlock()

	if fn == nil {
		d.mu.Lock()
		d.pending = false
		d.mu.Unlock()
		return
	}
	if currentGoid() == d.uiGoid {
		d.runQueued() // UI 线程：直接执行，避免绕一圈 post 造成延迟
		return
	}
	d.post(d.runQueued) // 后台线程：投递到 UI 线程
}

// runQueued 清标志并执行 sink（在 UI 线程上）。
func (d *QueueDispatcher) runQueued() {
	d.mu.Lock()
	if !d.pending || d.closed {
		d.mu.Unlock()
		return
	}
	d.pending = false
	fn := d.sink
	d.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// Drain 供 UI 线程在事件循环里显式冲刷一帧（可选；RequestFrame 通常已自动投递）。
func (d *QueueDispatcher) Drain() { d.runQueued() }

func (d *QueueDispatcher) Close() {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
}

// CurrentGoid 返回当前 goroutine id。真实后端创建 QueueDispatcher 时用它捕获 UI
// 线程 id；测试也可据此验证 Draw 是否发生在预期线程。
// 实现复用 signal 包的 goid（TX.1：goid 获取全仓集中一处）。
func CurrentGoid() uint64 { return signal.Goid() }

// currentGoid 返回当前 goroutine id（包内使用的简写）。
func currentGoid() uint64 { return signal.Goid() }
