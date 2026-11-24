// Signal.Safe：跨线程进入 UI Runtime 的安全更新入口。
//
// GX 的核心线程规则：
//
//	响应式系统是单线程的，业务任务可以是多线程的。
//
// Go 负责并发执行网络请求、IO、计算；UI Runtime 负责单线程响应式更新。
// 后台 goroutine 不得直接读写响应式 Signal，必须通过 Safe 把更新闭包投递给 UI
// Runtime。这样 Signal 内部才能彻底去掉 goroutine id、观察者栈按线程分桶与全局锁，
// 让读取热路径保持最轻。
//
// 职责分离：
//   - Safe  —— 只负责线程切换（后台 → UI Runtime），异步不等待；
//   - Batch —— 只负责合并同一上下文内的多次写入，不做线程切换。
//
// 后台：
//
//	go func() {
//	    data := fetch()
//	    Signal.Safe(func() { count = data })   // 投递，不等待执行
//	}()
//
// UI Runtime 内（事件回调、effect、Render 等）：
//
//	Signal.Batch(func() { count = 100; name = "Pan" })  // 直接执行 + 合并
package signal

import "sync"

// TaskPoster 把任务投递到 UI Runtime 执行。返回 false 表示 UI 已关闭、
// 任务被丢弃（窗口关闭后的投递必须安全失效，不能在已销毁的 Runtime 上跑）。
type TaskPoster func(fn func()) bool

var (
	taskMu     sync.RWMutex
	taskPost   TaskPoster
	taskClosed bool
)

// SetUIPost 由宿主（gxapp）注入 UI Runtime 的任务投递函数。
// 应在 Runtime 就绪后、响应式接线前调用；post 为 nil 时注销并恢复「无 UI Runtime」
// 状态（Safe 退化为同步执行，供纯 headless 测试用）。返回旧 post 便于成对恢复。
//
// 单线程模型下 UI Runtime 全局唯一，重复注入以最后一次为准。
func SetUIPost(post TaskPoster) (old TaskPoster) {
	taskMu.Lock()
	old = taskPost
	taskPost = post
	taskClosed = false
	taskMu.Unlock()
	return old
}

// CloseUITasks 标记 UI Runtime 关闭：此后所有 Safe 投递一律丢弃。
// gxapp.Close 调用它，保证关闭过程中 / 关闭后到达的后台更新不会触碰已销毁的树。
func CloseUITasks() {
	taskMu.Lock()
	taskClosed = true
	taskPost = nil
	taskMu.Unlock()
}

// InUIRuntime 报告当前是否由注入的 TaskPoster 驱动（即已接入 UI Runtime）。
// 仅用于诊断；业务代码不需要判断线程——后台一律用 Safe，UI 内一律用 Batch/直接读写。
func InUIRuntime() bool {
	taskMu.RLock()
	p := taskPost
	c := taskClosed
	taskMu.RUnlock()
	return !c && p != nil
}

// Safe 把更新闭包投递到 UI Runtime 执行——这是后台 goroutine 修改响应式状态的
// 唯一合法入口。
//
// 语义：
//   - 异步投递，调用后立即返回，不等待 fn 执行（第一版不提供 SafeSync）；
//   - 已注入 UI Runtime：fn 在 UI Runtime 线程执行，响应式更新单线程串行；
//   - 未注入 UI Runtime（纯 headless/测试）：退化为同步执行，调用方须自行保证
//     单线程顺序；
//   - UI Runtime 已关闭：任务被丢弃。
//
// fn 内可安全调用 Batch 合并多次写入；不需要（也不应该）在 fn 内再调 Safe。
func Safe(fn func()) {
	if fn == nil {
		return
	}
	taskMu.RLock()
	post, closed := taskPost, taskClosed
	taskMu.RUnlock()
	if closed {
		return // UI 已关闭：丢弃
	}
	if post == nil {
		fn() // 无 UI Runtime：同步执行（headless/测试）
		return
	}
	post(fn) // 返回 false（关闭）时任务由 poster 侧丢弃
}
