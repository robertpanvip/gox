package gxscene

import (
	"strconv"
	"sync"
)

// Measured 是后端布局完成后回流的单个节点实测尺寸（像素）。
//
// 回流方向为 Backend → Host → Component，与输入事件同向但用途不同：
// 输入事件走 DispatchBubble 命中节点上的 handler；实测尺寸走本文件的注册表，
// 由关心该节点的组件（如虚拟列表）按 NodeID 订阅。
//
// NodeID 由 host 全局单调分配，故包级注册表即可正确区分不同应用/不同列表项，
// 不会串号。W/H 为后端布局器（Skia 的 Yoga / Rust 的 Taffy）算出的最终尺寸。
type Measured struct {
	ID NodeID
	W  float64
	H  float64
}

// measureSub 是一条订阅记录。token 让退订精确：同一节点允许多个订阅者，
// 每个订阅持有独立 token，销毁时只移除自己那条。
type measureSub struct {
	token uint64
	fn    func(Measured)
}

// measuredRegistry 按 NodeID 保存最近一次实测尺寸与订阅者。
//
// 去重是回流防环的收敛条件：只有尺寸变化超过 measureEpsilon 才更新并通知。
// 稳定后 Publish 返回 false，App 不再请求重绘，布局→回流→布局环自然终止。
type measuredRegistry struct {
	mu      sync.Mutex
	nextTok uint64
	latest  map[NodeID]Measured
	subs    map[NodeID][]measureSub
}

const measureEpsilon = 0.5 // px：小于此的抖动视为未变化，避免浮点噪声导致抖动重排

var measurements = measuredRegistry{
	latest: make(map[NodeID]Measured),
	subs:   make(map[NodeID][]measureSub),
}

func mabs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// ParseNodeID 是 NodeID.String 的逆操作，供后端回传流把字符串 ID 还原为 NodeID。
// 空串/非法值归一为 NoNode（会被 PublishMeasured 忽略）。
func ParseNodeID(s string) NodeID {
	if s == "" {
		return NoNode
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return NoNode
	}
	return NodeID(v)
}

// SubscribeMeasured 订阅某节点的实测尺寸，返回退订函数（幂等）。
// 若该节点已有回流数据，会立即用当前值回调一次——避免"回流先于订阅到达"丢失首值。
// 回调在 PublishMeasured 的锁外执行，故回调内写 signal / 触发重排不会死锁。
func SubscribeMeasured(id NodeID, fn func(Measured)) func() {
	if id == NoNode || fn == nil {
		return func() {}
	}
	measurements.mu.Lock()
	measurements.nextTok++
	tok := measurements.nextTok
	measurements.subs[id] = append(measurements.subs[id], measureSub{token: tok, fn: fn})
	cur, ok := measurements.latest[id]
	measurements.mu.Unlock()
	if ok {
		fn(cur)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			measurements.mu.Lock()
			list := measurements.subs[id]
			for i, s := range list {
				if s.token == tok {
					measurements.subs[id] = append(list[:i], list[i+1:]...)
					break
				}
			}
			if len(measurements.subs[id]) == 0 {
				delete(measurements.subs, id)
			}
			measurements.mu.Unlock()
		})
	}
}

// PublishMeasured 发布一批实测尺寸。返回是否有任何尺寸发生了实质变化。
// 后端布局后调用；App 收到变化后请求下一帧，让组件按新尺寸重排。
func PublishMeasured(ms []Measured) bool {
	changed := false
	var notifyVals []Measured
	var notifyFns [][]func(Measured)

	measurements.mu.Lock()
	for _, m := range ms {
		if m.ID == NoNode {
			continue
		}
		old, exists := measurements.latest[m.ID]
		if exists && mabs(old.W-m.W) < measureEpsilon && mabs(old.H-m.H) < measureEpsilon {
			continue
		}
		measurements.latest[m.ID] = m
		subs := measurements.subs[m.ID]
		if len(subs) > 0 {
			notifyVals = append(notifyVals, m)
			cp := make([]func(Measured), len(subs))
			for i, s := range subs {
				cp[i] = s.fn
			}
			notifyFns = append(notifyFns, cp)
		}
		changed = true
	}
	measurements.mu.Unlock()

	for i := range notifyVals {
		for _, fn := range notifyFns[i] {
			fn(notifyVals[i])
		}
	}
	return changed
}

// MeasuredSize 查询某节点当前实测尺寸；ok=false 表示尚无回流数据。
func MeasuredSize(id NodeID) (Measured, bool) {
	measurements.mu.Lock()
	defer measurements.mu.Unlock()
	m, ok := measurements.latest[id]
	return m, ok
}

// ForgetMeasured 删除某节点的实测尺寸（订阅由各自的退订函数管理）。
// 供列表项滚出窗口、节点销毁时清理，防止注册表随滚动无限增长。
func ForgetMeasured(id NodeID) {
	if id == NoNode {
		return
	}
	measurements.mu.Lock()
	delete(measurements.latest, id)
	measurements.mu.Unlock()
}

// ResetMeasurements 清空整个回流注册表（仅测试用：隔离不同用例的全局状态）。
// 生产代码不应调用——窗口销毁由各订阅者的 OnCleanup 逐项清理。
func ResetMeasurements() {
	measurements.mu.Lock()
	measurements.latest = make(map[NodeID]Measured)
	// 订阅不清：正在活跃订阅的组件仍持有退订函数；清空 latest 只重置"已知尺寸"。
	measurements.mu.Unlock()
}
