package interp

import (
	"fmt"
	"os"
)

// 解释器输出通道：用户源码里 println / print / fmt.println / fmt.print /
// fmt.printf 的输出去向。装配层（playground web/desktop）通过 SetOutput
// 把输出接到内置终端面板（widgets.Terminal）与浏览器 console；
// 未注册时回落到标准输出（桌面终端直接可见）。
var outputSink func(string)

// SetOutput 注册输出 sink；传 nil 恢复标准输出。
func SetOutput(fn func(string)) { outputSink = fn }

// emitOutput 把一段用户输出投递到当前 sink。
func emitOutput(text string) {
	if text == "" {
		return
	}
	if outputSink != nil {
		outputSink(text)
		return
	}
	fmt.Fprint(os.Stdout, text)
}

// goSprintf 是 fmt.printf 的格式化实现：直接复用标准库 fmt（解释器是
// 宿主 Go 代码，不受 GX 语法糖限制）。
func goSprintf(format string, args []Value) string {
	if len(args) == 0 {
		return format
	}
	anyArgs := make([]any, len(args))
	for i, a := range args {
		anyArgs[i] = a
	}
	return fmt.Sprintf(format, anyArgs...)
}
