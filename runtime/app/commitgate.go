package gxapp

import (
	"sync"

	"github.com/robertpanvip/gox/runtime/signal"
)

// commitGate 是 Host 的串行提交门：把「Scene Mutation」（响应式 effect 改节点）
// 与「Backend Draw」（后端遍历节点）收敛到同一互斥上下文，消除数据竞争。
//
// 背景（Host 架构评审 P0）：QueueDispatcher 只保证 Draw 发生在 UI 线程，却没有
// 保证 Node 树的修改也在那里。多个后台 goroutine 写不同 signal 时，各自的 effect
// body 会并发执行 SetPart / graft，与 UI 线程的 Draw 遍历竞争。
//
// 设计：
//   - 基于 goroutine id 的**可重入**互斥：signal 的 effect 支持同步嵌套写入（一个
//     effect body 写 signal 会同步触发另一个 effect），若用 sync.Mutex 会自锁死锁；
//     同一 goroutine 重入只累加计数，不同 goroutine 才真正等待。
//   - Mutation 侧用 Lock/Unlock 包住 effect body；Draw 侧用同一把门包住 win.Draw。
//     于是「一批 mutation 全部提交后」才可能轮到 Draw，且二者绝不并发。
//   - goid 复用安全：持锁期间 goroutine 必然存活，id 不会被复用；释放后 owner 归零。
//
// 它复用 signal.Goid()（全仓 goid 集中在一处，TX.1），不引入新的 goid 实现。
type commitGate struct {
	mu    sync.Mutex
	cond  *sync.Cond
	owner uint64 // 当前持有者的 goroutine id；0 表示空闲
	depth int    // 同一 goroutine 的重入深度
}

func newCommitGate() *commitGate {
	g := &commitGate{}
	g.cond = sync.NewCond(&g.mu)
	return g
}

// lock 进入串行提交上下文。同一 goroutine 可重入。
func (g *commitGate) lock() {
	id := signal.Goid()
	g.mu.Lock()
	for g.owner != 0 && g.owner != id {
		g.cond.Wait()
	}
	if g.owner == id {
		g.depth++
	} else {
		g.owner = id
		g.depth = 1
	}
	g.mu.Unlock()
}

// unlock 退出一层提交上下文，与 lock 严格配对。
func (g *commitGate) unlock() {
	id := signal.Goid()
	g.mu.Lock()
	// 防御：只有持有者能解锁（正常路径必成立；异常路径避免破坏互斥）。
	if g.owner != id || g.depth == 0 {
		g.mu.Unlock()
		return
	}
	g.depth--
	if g.depth == 0 {
		g.owner = 0
		g.cond.Signal()
	}
	g.mu.Unlock()
}
