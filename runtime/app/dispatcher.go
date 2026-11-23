package gxapp

import (
	"errors"
	"sync"

	"github.com/robertpanvip/gox/runtime/signal"
)

var errNoUIExecutor = errors.New("gxapp: UIExecutor 投递函数未提供")

// FrameDispatcher 决定「帧重绘请求」在哪个线程/goroutine 上执行。
//
// 响应式 effect 可能在任意 goroutine（如后台数据拉取）中因 signal 写入而重跑；
// 真实 GUI 后端通常要求绘制与事件处理都在 UI 线程。FrameDispatcher 把「谁请求
// 重绘」与「在哪重绘」解耦：effect 侧只调用 RequestFrame，dispatcher 负责把真正
// 的 Draw 回调派发到合适的执行上下文（T2.1）。
//
// 实现契约：
//   - RequestFrame 可并发调用；实现须保证同一时刻的多次请求合并为一次执行（帧去抖）。
//     例外：InlineDispatcher 是同步执行器，明确不承诺跨 goroutine 合并（见其注释）。
//   - SetSink 在 Render 前设置，Render 后 sink 稳定。
//   - Close 后 RequestFrame 必须 no-op，已排队但未执行的回调必须安全失效。
type FrameDispatcher interface {
	// RequestFrame 请求一次重绘。可并发调用，实现须合并重复请求（同步执行器除外）。
	RequestFrame()
	// SetSink 设置真正执行重绘的回调（通常是 app.drawNow）。
	SetSink(fn func())
	// Close 停止派发并释放资源。幂等；关闭后排队的回调不得执行。
	Close()
}

// UIExecutor 是后端提供的「投递到 UI 执行环境」能力（Host 评审 P0：线程亲和性）。
//
// 线程契约：谁保证 UI 操作发生在正确的 OS Thread？答案是后端，不是 Host。
// Go 的 goroutine 可在不同 OS Thread 间迁移，goroutine id 相等并不能证明当前
// 位于正确的 OS Thread。因此 Host 不再用 goid 判断线程，而是把投递能力抽象为
// UIExecutor：后端用窗口事件循环的 postMessage / glfw.PostEmptyEvent / 平台
// run loop 等机制保证 fn 在正确的 UI 线程执行；需要锁定 OS Thread 的框架由
// 后端自己的事件循环负责线程绑定。
//
// Post 成功返回 nil；返回非 nil 表示投递失败（如 UI 已关闭），上层可据此忽略。
type UIExecutor interface {
	Post(fn func()) error
}

// InlineDispatcher 是默认派发器：RequestFrame 立即同步执行 sink。
//
// 它保持 headless / 测试场景的同步语义——写完 signal 立刻可见一帧重绘，无需
// 等待 UI 线程。单线程模型（GUI 事件循环本身就在同一 goroutine 驱动 signal）
// 下，它也是正确且零开销的选择。
//
// 契约澄清（Host 评审 P1）：InlineDispatcher 是同步执行器，不承诺「跨 goroutine /
// 跨批次合并」。若多个后台 goroutine 并发写 signal，每个 RequestFrame 都会同步
// 执行一次 Draw；需要合并语义的 GUI 后端请使用 UIExecutorDispatcher。
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

// UIExecutorDispatcher 把帧请求交给后端 UIExecutor 投递到正确的 UI 环境。
//
// 这是真实 GUI 后端的推荐派发器：RequestFrame 在任意 goroutine 调用都只做一件
// 事——通过 exec.Post 把一次合并后的 runQueued 投递给 UIExecutor。它不比较
// goroutine id，也不提供「当前线程直接执行」的快捷路径，因此不会在事件处理
// 过程中同步重入 Draw（下一次帧回调比立即重入更安全）。多次请求在 pending
// 标志下去抖合并为一次投递。
type UIExecutorDispatcher struct {
	exec UIExecutor

	mu      sync.Mutex
	sink    func()
	pending bool
	closed  bool
}

// NewUIExecutorDispatcher 创建基于后端 UIExecutor 的派发器。exec 不能为 nil。
func NewUIExecutorDispatcher(exec UIExecutor) *UIExecutorDispatcher {
	return &UIExecutorDispatcher{exec: exec}
}

func (d *UIExecutorDispatcher) SetSink(fn func()) {
	d.mu.Lock()
	d.sink = fn
	d.mu.Unlock()
}

func (d *UIExecutorDispatcher) RequestFrame() {
	d.mu.Lock()
	if d.closed || d.pending || d.sink == nil {
		d.mu.Unlock()
		return
	}
	d.pending = true
	d.mu.Unlock()

	// 由后端保证 runQueued 在 UI 线程执行；投递失败则清标志，允许下次重试。
	if err := d.exec.Post(d.runQueued); err != nil {
		d.mu.Lock()
		d.pending = false
		d.mu.Unlock()
	}
}

// runQueued 清标志并执行 sink（在 UIExecutor 投递的执行环境里）。
func (d *UIExecutorDispatcher) runQueued() {
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
func (d *UIExecutorDispatcher) Drain() { d.runQueued() }

func (d *UIExecutorDispatcher) Close() {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
}

// ---------- 兼容层：旧 QueueDispatcher ----------

// QueueDispatcher 是旧版基于 goroutine id 的派发器，保留仅为源码兼容。
//
// Deprecated: goroutine id 相等不能证明位于正确的 OS Thread，post==nil 时的
// 同步降级会静默破坏线程保证。新后端请实现 UIExecutor 并用 NewUIExecutorDispatcher。
type QueueDispatcher struct {
	uiGoid uint64
	post   func(func())

	mu      sync.Mutex
	sink    func()
	pending bool
	closed  bool
}

// NewQueueDispatcher 创建旧版队列派发器。post 为 nil 时不再静默降级为「直接执行」，
// 而是保留空 post——调用方必须提供正确的投递函数，否则帧永远不会执行（安全失败，
// 而非在不正确的线程上绘制）。
//
// Deprecated: 使用 NewUIExecutorDispatcher + UIExecutor。
func NewQueueDispatcher(uiGoid uint64, post func(func())) *QueueDispatcher {
	return &QueueDispatcher{uiGoid: uiGoid, post: post}
}

// PostFunc 把一个 post 函数适配为 UIExecutor，便于旧后端迁移：
//
//	app.SetDispatcher(NewUIExecutorDispatcher(PostFunc(win.PostEmptyEvent)))
//
// fn 为 nil 时 Post 返回错误而非静默降级——避免在不正确的线程执行绘制。
type PostFunc func(func())

func (p PostFunc) Post(fn func()) error {
	if p == nil {
		return errNoUIExecutor
	}
	p(fn)
	return nil
}

func (d *QueueDispatcher) SetSink(fn func()) {
	d.mu.Lock()
	d.sink = fn
	d.mu.Unlock()
}

func (d *QueueDispatcher) RequestFrame() {
	d.mu.Lock()
	if d.closed || d.pending || d.sink == nil {
		d.mu.Unlock()
		return
	}
	d.pending = true
	d.mu.Unlock()

	// 兼容策略：当前 goroutine 就是绑定的 UI goroutine 时直接执行（保留旧语义），
	// 否则用 post 投递。注意这仍是 goroutine 级判断，不能证明 OS Thread 亲和性——
	// 这正是推荐迁移到 UIExecutorDispatcher 的原因。
	if currentGoid() == d.uiGoid {
		d.runQueued()
		return
	}
	if d.post == nil {
		// 不再静默在同一（错误）线程执行；清标志安全失败。
		d.mu.Lock()
		d.pending = false
		d.mu.Unlock()
		return
	}
	d.post(d.runQueued)
}

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

func (d *QueueDispatcher) Drain() { d.runQueued() }

func (d *QueueDispatcher) Close() {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
}

// CurrentGoid 返回当前 goroutine id。保留供旧代码捕获 UI goroutine id；
// 新代码应优先实现 UIExecutor，不依赖 goroutine id 判断线程。
func CurrentGoid() uint64 { return signal.Goid() }

func currentGoid() uint64 { return signal.Goid() }
