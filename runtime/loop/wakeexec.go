// WakeExecutor 把一个"唤醒宿主"回调适配为完整的 loop.Executor。
//
// 用途：某些宿主只提供"投递到 UI 线程"的能力（如 gxweb.Window.Post 走 RAF），
// 没有直接的 PostMicro。UIPromise 的 continuation 依赖 PostMicro，且要求
// Post/PostMicro 只入队、不在调用栈同步执行。WakeExecutor 内部持一个 Queue
// 提供完整的宏/微任务语义，任务入队后经 wake 通知宿主尽快 drain。
//
// 典型用法（Web/WASM，宿主只有 RAF 投递）：
//
//	var exec *loop.WakeExecutor
//	exec = loop.NewWakeExecutor(func() {
//	    _ = win.Post(func() { exec.DrainTurn() }) // 下一帧 drain 队列
//	})
//	p := fetch.UIFetch(exec, fetch.Request{URL: "..."})
//
// 桌面宿主已有 loop.Queue 时无需本适配器，直接用 *Queue 即可。
package loop

// WakeExecutor 实现 Executor，并暴露 Queue 的 drain / 生命周期。
// 它不启动任何 goroutine 或事件循环——何时 drain 完全由宿主决定。
type WakeExecutor struct {
	q *Queue
}

// NewWakeExecutor 用 wake 回调创建一个 WakeExecutor。
// wake 在任务从"无待处理"变为"有待处理"时于投递方 goroutine 上被调用一次；
// 实现必须轻量、非阻塞、可重入（内部已合并高频唤醒）。
func NewWakeExecutor(wake func()) *WakeExecutor {
	return &WakeExecutor{q: New(Config{Wake: wake})}
}

// Post 投递一个宏任务（只入队，不在调用栈执行）。
func (e *WakeExecutor) Post(fn func()) error { return e.q.Post(fn) }

// PostMicro 投递一个微任务（只入队，由宿主在检查点 drain）。
func (e *WakeExecutor) PostMicro(fn func()) error { return e.q.PostMicro(fn) }

// DrainTurn 执行一个宿主轮次：一个宏任务 + 微任务检查点，预算触顶时让出。
// 宿主应在 wake 触发的 UI 轮次里调用；返回 Backlog=true 时尽快再调度一轮。
func (e *WakeExecutor) DrainTurn() TurnReport { return e.q.DrainTurn() }

// RunMicrotasks 只跑微任务检查点（宿主已有独立宏任务来源时用）。
func (e *WakeExecutor) RunMicrotasks() DrainReport { return e.q.RunMicrotasks() }

// HasPending 报告待处理任务数，供宿主判断是否需要继续调度。
func (e *WakeExecutor) HasPending() (macro, micro int) { return e.q.HasPending() }

// Stop 关闭投递面（幂等），丢弃未执行任务。
func (e *WakeExecutor) Stop() error { return e.q.Stop() }

// Closed 报告是否已关闭。
func (e *WakeExecutor) Closed() bool { return e.q.Closed() }

var _ Executor = (*WakeExecutor)(nil)
