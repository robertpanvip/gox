// 帧边界 hook：在一批 effect 全部同步执行完毕后触发。
//
// 这是响应式层与「消费方」（如 gxapp 的窗口重绘）之间的通用衔接点：
//   - 插槽级接线下，多个叶子/结构 effect 会因同一轮 signal 写入而重跑，
//     各自只更新自己的节点；重绘不应跟着每个 effect 各画一帧，而应在
//     这一批 effect 全部跑完后统一画一帧（帧合并，T1.2）。
//   - effect 回调内若又写 signal（递归写入），只在最外层边界触发一次 hook，
//     避免中途重绘出「半成品」帧。
//
// 设计要点：
//   - flushDepth 按 goroutine 隔离（复用 signal 的 goid），保证并发写入的
//     后台 goroutine 不干扰 UI 所在 goroutine 的边界判断；
//   - hook 回调在 mu 锁外执行，回调内不应再写 signal（只做绘制/通知这类
//     终端动作），否则会开启新的帧边界；
//   - 与 Batch 协同：Batch 期间写入只收集 effect，最外层 Batch 结束时统一
//     调度并在其后 flush 一次。
package signal

import "sync"

var (
	flushMu     sync.RWMutex
	flushHooks  = map[uint64]func(){}
	nextFlushID uint64
	flushDepth  = map[uint64]int{}
)

// AfterEffects 注册一个「帧边界」回调，返回注销函数（幂等）。
//
// 回调在每批被调度的 effect 同步执行完毕后触发；嵌套写入只在最外层触发一次。
// 典型用法：gxapp 注册它来把多个插槽级 effect 的变化合并为一次 Draw。
func AfterEffects(fn func()) func() {
	if fn == nil {
		return func() {}
	}
	flushMu.Lock()
	id := nextFlushID
	nextFlushID++
	flushHooks[id] = fn
	flushMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			flushMu.Lock()
			delete(flushHooks, id)
			flushMu.Unlock()
		})
	}
}

// withFlush 在帧边界内执行 fn：fn 调度的所有 effect 跑完后，若当前 goroutine
// 处于最外层边界（深度归零），触发所有 AfterEffects 回调。
func withFlush(gid uint64, fn func()) {
	mu.Lock()
	flushDepth[gid]++
	mu.Unlock()

	fn()

	mu.Lock()
	flushDepth[gid]--
	last := flushDepth[gid] == 0
	if last {
		delete(flushDepth, gid)
	}
	mu.Unlock()
	if last {
		runFlushHooks()
	}
}

// runFlushHooks 在锁外快照并执行所有注册的帧边界回调。
func runFlushHooks() {
	flushMu.RLock()
	if len(flushHooks) == 0 {
		flushMu.RUnlock()
		return
	}
	fns := make([]func(), 0, len(flushHooks))
	for _, fn := range flushHooks {
		fns = append(fns, fn)
	}
	flushMu.RUnlock()
	for _, fn := range fns {
		fn()
	}
}
