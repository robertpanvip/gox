// Package signal 的 goroutine id 获取，集中在此文件（TX.1）。
//
// Go 没有官方的 goroutine-local 存储，而响应式依赖追踪需要按 goroutine 隔离
// 观察者栈、批处理深度、帧边界深度与 Owner 栈。这里通过 runtime.Stack 首行
// "goroutine N ..." 解析 id——社区通用做法。
//
// 之所以独立成文件、并用一个可替换的函数变量：goid 的获取方式可能随 Go 版本
// 或运行时环境变化（未来可用汇编/cgo 更快的方案），而与依赖图核心逻辑无关。
// 把「怎么取 id」与「用 id 做什么」分开，替换实现不动一行核心代码。
//
// 性能：读快路径（activeObservers==0）完全不调用 goid，因此这里的间接调用
// 不 hot-path；抓栈本身只在写信号/重算/接线时出现。
package signal

import "runtime"

// goidProvider 是 goid 的实现，可被 SetGoidProvider 替换（TX.1）。
var goidProvider = stackGoid

// goid 返回当前 goroutine id。核心逻辑只依赖这个函数，不关心实现来源。
func goid() uint64 { return goidProvider() }

// Goid 是 goid 的导出版本，供相邻包（如 gxapp 的 QueueDispatcher）与测试获取
// 当前 goroutine id——避免各处重复实现栈解析。
func Goid() uint64 { return goid() }

// SetGoidProvider 替换 goid 实现（传 nil 恢复默认的栈解析）。
// 非线程安全，应在程序初始化阶段调用。返回旧实现便于恢复。
func SetGoidProvider(fn func() uint64) (old func() uint64) {
	old = goidProvider
	if fn == nil {
		fn = stackGoid
	}
	goidProvider = fn
	return old
}

// stackGoid 是默认实现：从 runtime.Stack 首行解析 goroutine 编号。
func stackGoid() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// 形如 "goroutine 123 [running]:\n..."
	const prefix = "goroutine "
	if n < len(prefix) || string(buf[:len(prefix)]) != prefix {
		return 0
	}
	var id uint64
	for i := len(prefix); i < n; i++ {
		c := buf[i]
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + uint64(c-'0')
	}
	return id
}
