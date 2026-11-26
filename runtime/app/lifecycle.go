// 销毁期异常隔离助手（评审 P1-02）。
//
// App.Close 的四个阶段（Runtime/租约、响应式树、派发器、窗口）与 teardownTree
// 的两段（owner/inst）各自单点捕获收集 panic——sync.Once 首次执行一旦中断，
// dispatcher / 窗口将永远不会再被关闭，因此「尽力释放、末尾统一上报」是关闭
// 流程的硬约束。语义与 signal 包的销毁隔离一致（单异常保真 re-panic、多异常
// 汇总为 disposeError），独立实现以保持两包自洽。
package gxapp

import (
	"fmt"
	"strings"
)

// runFuncSafely 执行 fn 并捕获其 panic，返回收集到的 panic 值（通常 0 或 1 个）。
func runFuncSafely(fn func()) (caught []any) {
	defer func() {
		if r := recover(); r != nil {
			caught = append(caught, r)
		}
	}()
	fn()
	return nil
}

// repanicDisposeErrors 按销毁语义上报收集到的异常：单个异常原样 re-panic
// （保留类型与值），多个异常包装成 disposeError 汇总。caught 为空时 no-op。
func repanicDisposeErrors(caught []any) {
	switch len(caught) {
	case 0:
		return
	case 1:
		panic(caught[0])
	default:
		panic(disposeError(caught))
	}
}

// disposeError 是 App 销毁期间多个阶段 panic 的汇总载体。
type disposeError []any

func (e disposeError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "app close: %d 个阶段 panic", len(e))
	for i, p := range e {
		fmt.Fprintf(&b, "\n  [%d] %v", i, p)
	}
	return b.String()
}
