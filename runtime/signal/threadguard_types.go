package signal

// ThreadViolation 是一次「后台线程触碰响应式系统」的诊断记录。
// 由 gxthreadcheck 构建下的线程守卫产生，供宿主上报到控制台 / UI。
type ThreadViolation struct {
	Op      string // 触碰点：signal.write / signal.read / observer.rebuild / node.dispose
	Goid    uint64 // 违规 goroutine id
	UIGoid  uint64 // 绑定的 UI goroutine id
	Stack   string // 违规调用栈（抓栈字符串）
	Message string // 人类可读摘要
}

// ThreadViolationHook 接收线程违规诊断。回调在违规 goroutine 上执行，
// 实现须轻量、非阻塞、不可递归写 signal（否则再次触发守卫）。
type ThreadViolationHook func(ThreadViolation)
