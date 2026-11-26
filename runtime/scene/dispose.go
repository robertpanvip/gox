// 销毁期异常隔离助手（评审 P1-01/P1-02 的 scene 层配套）。
//
// Instance.Dispose / For.Dispose 遍历多个 slot / entry，每个都可能执行用户代码
// （Owner cleanup、ref 回调）。销毁只有一次：某个 panic 不得阻断其余资源的释放
// ——被跳过的清单已从容器上摘除，中断即永久泄漏。语义与 signal/app 包的销毁
// 隔离一致：单点捕获收集，全部释放完毕后统一上报（单异常保真 re-panic，多个
// 汇总为 disposeError）。三层各自独立实现以保持包自洽。
package gxscene

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

// repanicDisposeErrors 按销毁语义上报收集到的异常。caught 为空时 no-op。
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

// disposeError 是 scene 销毁期间多个阶段 panic 的汇总载体。
type disposeError []any

func (e disposeError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "dispose: %d 个阶段 panic", len(e))
	for i, p := range e {
		fmt.Fprintf(&b, "\n  [%d] %v", i, p)
	}
	return b.String()
}
