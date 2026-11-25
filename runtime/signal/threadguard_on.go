//go:build gxthreadcheck

// gxthreadcheck 构建：启用响应式系统的 UI 线程亲和守卫。
//
// 它只在「触碰共享响应式状态」的结构变更点检查（写信号、重建依赖、标脏、
// 销毁节点），不在只读当前值的热路径上抓栈——即便如此，本文件整体只在
// -tags gxthreadcheck 下参与构建，默认发布构建连调用都被内联消除。
//
// 违规处理是「报告 + 继续」，不 panic：后台野写生产环境本就罕见，调试构建
// 的目的是把偶发的 fatal error: concurrent map writes 提前变成可读的栈报告。
package signal

import (
	"fmt"
	"os"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

var (
	guardUIGoid atomic.Uint64 // 绑定的 UI goroutine id；0 = 未绑定（守卫静默）

	hookMu sync.RWMutex
	hook   ThreadViolationHook

	// 去重：同一 (goid, op) 只报一次，避免后台 ticker 每 530ms 刷屏。
	reportedMu sync.Mutex
	reported   = map[uint64]map[string]struct{}{}
)

// BindUIThread 把当前 goroutine 登记为唯一合法的 UI 线程。由宿主在启动
// 事件循环前调用（app.SetUIExecutor 会自动调用）。重复调用以最后一次为准。
func BindUIThread() { guardUIGoid.Store(goid()) }

// SetThreadViolationHook 注册违规回调，返回幂等注销函数。nil 表示注销。
func SetThreadViolationHook(fn ThreadViolationHook) func() {
	hookMu.Lock()
	hook = fn
	hookMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			hookMu.Lock()
			hook = nil
			hookMu.Unlock()
		})
	}
}

// ThreadGuardEnabled 报告守卫是否启用。
func ThreadGuardEnabled() bool { return true }

// checkUIThread 校验当前 goroutine 是否为绑定的 UI 线程。
func checkUIThread(op string) {
	want := guardUIGoid.Load()
	if want == 0 {
		return // 未绑定：headless / 纯测试，无从判断，静默
	}
	got := goid()
	if got == want {
		return
	}
	// 去重后再抓栈（抓栈昂贵，重复违规只报一次）
	reportedMu.Lock()
	ops := reported[got]
	if ops == nil {
		ops = map[string]struct{}{}
		reported[got] = ops
	}
	if _, seen := ops[op]; seen {
		reportedMu.Unlock()
		return
	}
	ops[op] = struct{}{}
	reportedMu.Unlock()

	v := ThreadViolation{
		Op:     op,
		Goid:   got,
		UIGoid: want,
		Stack:  string(debug.Stack()),
	}
	v.Message = fmt.Sprintf(
		"gx/signal: 后台 goroutine %d 触碰响应式系统(%s)，UI 线程为 %d；请经 Runtime.Dispatch/Signal.safe 投递",
		got, op, want,
	)
	fmt.Fprintln(os.Stderr, v.Message)
	fmt.Fprintln(os.Stderr, v.Stack)

	hookMu.RLock()
	h := hook
	hookMu.RUnlock()
	if h != nil {
		h(v)
	}
}
