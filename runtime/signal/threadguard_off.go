//go:build !gxthreadcheck

// 默认构建：线程守卫完全关闭。checkUIThread 是空函数，Go 编译器会把它和
// 各调用点一起内联消除——响应式读写热路径不增加任何指令，性能零影响。
//
// 需要检测后台线程违规访问时，用 `-tags gxthreadcheck` 构建/测试：
//
//	go test -tags gxthreadcheck ./...
//	go build -tags gxthreadcheck ./...
package signal

// checkUIThread 在默认构建下是空操作，被编译器内联消除。
func checkUIThread(op string) {}

// BindUIThread 在默认构建下是空操作。
func BindUIThread() {}

// SetThreadViolationHook 在默认构建下不注册任何回调，返回空注销函数。
func SetThreadViolationHook(fn ThreadViolationHook) func() { return func() {} }

// ThreadGuardEnabled 报告守卫是否启用（默认构建恒为 false）。
func ThreadGuardEnabled() bool { return false }
