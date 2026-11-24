// Signal.Safe：跨线程进入 UI Runtime 的安全更新入口。
//
// GX 的核心线程规则：
//
//	响应式系统是单线程的，业务任务可以是多线程的。
//
// Go 负责并发执行网络请求、IO、计算；UI Runtime 负责单线程响应式更新。
// 后台 goroutine 不得直接读写响应式 Signal，必须通过 Runtime.Dispatch（或旧名
// Signal.Safe）把更新闭包投递给 UI Runtime。这样 Signal 内部才能彻底去掉
// goroutine id、观察者栈按线程分桶与全局锁，让读取热路径保持最轻。
//
// 职责分离：
//   - Runtime.Dispatch —— 只负责线程切换（后台 → UI Runtime），异步不等待；
//   - Batch            —— 只负责合并同一上下文内的多次写入，不做线程切换。
//
// 多 App 隔离（评审 P0）：
//
//	跨线程任务队列属于 UI Runtime，不属于 Signal 算法本身。每个 App 持有自己的
//	signal.Runtime：app.SetUIExecutor 注入该 App 的投递器，app.Close 只关闭该
//	Runtime。多个 App 关闭互不影响，也不会互相覆盖投递器。
//
//	注意：依赖追踪核心（observers / ownerStack / batch）仍是单线程唯一的。
//	多个 App 应共享同一个 UI 事件循环（同一执行器）——这也正是评审认可的模型：
//	共享一个事件循环的多个窗口共用一个响应式 Runtime。Runtime 的实例隔离解决的
//	是「投递器被覆盖 / 关闭状态误伤」这两个具体缺陷，而不是让响应式核心支持多
//	线程并发。
//
// 安全降级（评审 P0）：
//
//	正式 UI Runtime 未初始化时，Dispatch 返回 ErrUIRuntimeNotInitialized，
//	绝不静默在调用方（可能是后台）goroutine 同步执行响应式代码。需要同步语义的
//	headless/测试场景必须显式创建 NewSyncRuntime 或注入 SyncPoster。
//
// 后台：
//
//	go func() {
//	    data := fetch()
//	    _ = app.Dispatch(func() { count(data) })  // 投递，不等待执行
//	}()
//
// UI Runtime 内（事件回调、effect、Render 等）：
//
//	Signal.Batch(func() { count = 100; name = "Pan" })  // 直接执行 + 合并
package signal

import (
	"errors"
	"sync"
)

// 任务投递失败的原因（评审 P0：不允许静默失败）。
var (
	// ErrUIRuntimeNotInitialized 表示尚未注入 UI 投递器。后台 goroutine 收到它
	// 说明宿主忘记在 Render 前调用 app.SetUIExecutor；此时 fn **不会**执行。
	ErrUIRuntimeNotInitialized = errors.New("gx/signal: UI Runtime 未初始化：请在 Render 前调用 app.SetUIExecutor")
	// ErrUIRuntimeClosed 表示 UI Runtime 已关闭（窗口销毁后到达的投递）。
	ErrUIRuntimeClosed = errors.New("gx/signal: UI Runtime 已关闭")
	// ErrUITaskRejected 表示投递器拒绝了任务（队列满或后端已停止）。
	ErrUITaskRejected = errors.New("gx/signal: UI 任务被投递器拒绝")
)

// TaskPoster 把任务投递到 UI Runtime 执行。返回 false 表示 UI 已关闭、
// 任务被丢弃（窗口关闭后的投递必须安全失效，不能在已销毁的 Runtime 上跑）。
type TaskPoster func(fn func()) bool

// SyncPoster 是 headless / 测试专用的同步投递器：在当前 goroutine 立即执行 fn。
// 它必须被**显式**选用（NewSyncRuntime 或 SetUIPost(SyncPoster)），正式 App
// 不会自动退化为它——这正是评审 P0「禁止无 Executor 时静默同步」的要求。
var SyncPoster TaskPoster = func(fn func()) bool {
	if fn != nil {
		fn()
	}
	return true
}

// Runtime 是一个 UI 运行时的任务投递上下文。每个 App 持有独立实例，
// 从而多个 App 的投递器与关闭状态互不干扰。
//
// Runtime 只负责「跨线程任务进入 UI」这一件事；依赖追踪 / Owner / 批处理
// 仍是单线程的响应式核心，约定全部在 UI Runtime 线程上执行。
type Runtime struct {
	mu      sync.RWMutex
	post    TaskPoster
	closed  bool
	errHook func(error) // 可选：投递失败回调（GX 语句形式忽略返回值时的上报通道）
}

// NewRuntime 创建一个尚未注入投递器的 Runtime。此时 Dispatch 返回
// ErrUIRuntimeNotInitialized，不会在调用方线程执行闭包。
func NewRuntime() *Runtime { return &Runtime{} }

// NewSyncRuntime 创建一个同步执行的 Runtime（headless / 测试用）。
func NewSyncRuntime() *Runtime { return &Runtime{post: SyncPoster} }

// SetPoster 注入/替换投递器并重置关闭状态。返回旧的投递器，便于成对恢复。
// 重复调用以最后一次为准；post 为 nil 表示注销（此后 Dispatch 返回未初始化）。
func (rt *Runtime) SetPoster(post TaskPoster) (old TaskPoster) {
	if rt == nil {
		return nil
	}
	rt.mu.Lock()
	old = rt.post
	rt.post = post
	rt.closed = false
	rt.mu.Unlock()
	return old
}

// Close 关闭 Runtime：此后所有 Dispatch 一律返回 ErrUIRuntimeClosed，
// 闭包不会在任何线程执行。幂等。
func (rt *Runtime) Close() {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	rt.closed = true
	rt.post = nil
	rt.mu.Unlock()
}

// SetErrorHandler 设置投递失败回调。GX 里 `app.dispatch(...)` 是语句、会忽略
// Go 返回值，注册此回调可让未初始化 / 已关闭 / 被拒绝等错误仍被上层观测到。
// fn 为 nil 表示注销。回调在 Dispatch 的锁外执行。
func (rt *Runtime) SetErrorHandler(fn func(error)) {
	if rt == nil {
		return
	}
	rt.mu.Lock()
	rt.errHook = fn
	rt.mu.Unlock()
}

// fail 记录一次投递失败：调用 errHook（锁外），再返回 err。
func (rt *Runtime) fail(err error) error {
	rt.mu.RLock()
	hook := rt.errHook
	rt.mu.RUnlock()
	if hook != nil {
		hook(err)
	}
	return err
}

// Closed 报告 Runtime 是否已关闭。
func (rt *Runtime) Closed() bool {
	if rt == nil {
		return true
	}
	rt.mu.RLock()
	c := rt.closed
	rt.mu.RUnlock()
	return c
}

// Dispatch 把 fn 投递到 UI Runtime 执行——后台 goroutine 修改响应式状态的唯一
// 合法入口。异步投递，调用后立即返回，不等待 fn 执行。
//
// 返回 nil 表示成功投递；任何非 nil 错误都表示 fn **未被执行**，调用方应据
// 此处理结果（GX 语句形式忽略返回值时，运行时会通过错误处理器上报）。
func (rt *Runtime) Dispatch(fn func()) error {
	if fn == nil {
		return nil
	}
	if rt == nil {
		return ErrUIRuntimeNotInitialized
	}
	rt.mu.RLock()
	post, closed := rt.post, rt.closed
	rt.mu.RUnlock()
	if closed {
		return rt.fail(ErrUIRuntimeClosed)
	}
	if post == nil {
		return rt.fail(ErrUIRuntimeNotInitialized)
	}
	if !post(fn) {
		return rt.fail(ErrUITaskRejected)
	}
	return nil
}

// InUI 报告该 Runtime 是否已注入投递器且未关闭（诊断用）。
func (rt *Runtime) InUI() bool {
	if rt == nil {
		return false
	}
	rt.mu.RLock()
	p := rt.post
	c := rt.closed
	rt.mu.RUnlock()
	return !c && p != nil
}

// ---------- 全局默认 Runtime：Signal.Safe 的兼容层 ----------
//
// 推荐路径是 app.Dispatch（绑定具体 App 的 Runtime）。保留全局默认 Runtime 仅为
// 兼容纯 signal 测试与 GX 的 `Signal.safe` 语法糖：单 App 场景下，
// app.SetUIExecutor 会通过 lease 桥接安装到这里；多 App 请使用各自的
// app.Dispatch，不要依赖全局入口。

var defaultRuntime = NewRuntime()

// globalLease 跟踪有多少 App 桥接到了默认 Runtime。只有最后一个 lease 释放时
// 才真正关闭默认 Runtime——避免 app1.Close 误伤 app2 的全局 Safe 投递。
var (
	globalLeaseMu sync.Mutex
	globalLeases  int
)

// GlobalLease 是 App 对默认 Runtime 的桥接租约。释放幂等。
type GlobalLease struct {
	released bool
}

// InstallGlobalPost 由 App.SetUIExecutor 调用：注入投递器并占用一份租约。
// 多 App 时以最后一次注入为准（与旧行为一致），但关闭按租约计数，互不误伤。
func InstallGlobalPost(post TaskPoster) *GlobalLease {
	globalLeaseMu.Lock()
	defer globalLeaseMu.Unlock()
	globalLeases++
	defaultRuntime.SetPoster(post)
	return &GlobalLease{}
}

// Release 释放租约。最后一个租约释放时才关闭默认 Runtime。
func (l *GlobalLease) Release() {
	if l == nil {
		return
	}
	globalLeaseMu.Lock()
	defer globalLeaseMu.Unlock()
	if l.released {
		return
	}
	l.released = true
	globalLeases--
	if globalLeases <= 0 {
		globalLeases = 0
		defaultRuntime.Close()
	}
}

// SetUIPost 配置默认 Runtime 的投递器（兼容旧 API / 测试），返回旧投递器。
// 注意：它不占用租约；生产代码请优先用 app.Dispatch，App 内部会通过
// InstallGlobalPost 维护兼容桥接。
func SetUIPost(post TaskPoster) (old TaskPoster) {
	return defaultRuntime.SetPoster(post)
}

// CloseUITasks 关闭默认 Runtime（兼容旧 API / 测试）。生产 App 关闭走
// 各自的 Runtime.Close + GlobalLease.Release，不再调用这里。
func CloseUITasks() { defaultRuntime.Close() }

// InUIRuntime 报告默认 Runtime 是否已接入 UI（诊断用）。
func InUIRuntime() bool { return defaultRuntime.InUI() }

// Safe 是默认 Runtime.Dispatch 的兼容别名（GX `Signal.safe` 的落点）。
//
// 语义变更（评审 P0）：未注入 UI Runtime 时**不再静默同步执行**，而是返回
// ErrUIRuntimeNotInitialized；UI 关闭返回 ErrUIRuntimeClosed。返回非 nil 表示
// fn 未执行。需要同步语义请显式 SetUIPost(SyncPoster) 或使用 NewSyncRuntime。
func Safe(fn func()) error { return defaultRuntime.Dispatch(fn) }
