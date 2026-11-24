// 帧边界 hook：在一批 effect 全部同步执行完毕后触发。
//
// 这是响应式层与「消费方」（如 gxapp 的窗口重绘）之间的通用衔接点：
//   - 插槽级接线下，多个叶子/结构 effect 会因同一轮 signal 写入而重跑，
//     各自只更新自己的节点；重绘不应跟着每个 effect 各画一帧，而应在
//     这一批 effect 全部跑完后统一画一帧（帧合并，T1.2）。
//   - effect 回调内若又写 signal（递归写入），只在最外层边界触发一次 hook，
//     避免中途重绘出「半成品」帧。
//
// 单线程模型：帧边界深度是唯一实例——只有 UI Runtime 会进入边界。
// hook 注册表用 RWMutex 保护，因为 AfterEffects 允许在初始化阶段从任意
// goroutine 注册/注销（这是低频控制面，不属 Signal 读取热路径）。
package signal

import "sync"

var (
	flushMu     sync.RWMutex
	flushHooks  = map[uint64]func(){}
	nextFlushID uint64
	flushDepth  int // 当前帧边界嵌套深度（单实例）
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

// withFlush 在帧边界内执行 fn：fn 调度的所有 effect 跑完后，若当前处于
// 最外层边界（深度归零），触发所有 AfterEffects 回调。
func withFlush(fn func()) {
	flushDepth++
	// defer 保证即便 fn（effect 回调链）panic，帧深度也能复位——否则
	// flushDepth 永久卡在非零，后续所有写入都不再触发帧边界 hook。
	defer func() {
		flushDepth--
		if flushDepth == 0 {
			runFlushHooks()
		}
	}()
	fn()
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
