package gxapp

import (
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// 本文件定义 ref Element 句柄所需的「宿主可选能力」契约：Focuser / Measurer。
//
// 两者都是**可选接口**：后端实现即获得能力，不实现则对应功能安全降级——
// Focus 变 no-op、Measure 以 (0,0) 回填。headless 与现有 Skia / Rust 后端
// 零改动即可继续工作（它们不实现这两个接口时行为完全不变）。
//
// 依赖方向：gxapp 只声明契约并适配到 gxscene 的共享 sink，不 import 任何具体后端。

// Focuser 是可选的焦点能力。后端实现后，ref 句柄的 el.Focus() 会把键盘焦点
// 移到该 NodeID 对应的原生控件；未实现时 Focus() 为 no-op 并返回 false。
type Focuser interface {
	Focus(id gxscene.NodeID)
}

// Measurer 是可选的布局读取能力。后端在 Draw/Present 完成后应能按 NodeID
// 返回该节点最近一次布局的宽高。ok=false 表示没有该节点的布局
// （headless / 尚未布局 / 节点已销毁）。
//
// 实现是可选的：未实现的后端走 gxapp 的零值回填（w=h=0, ok=false），
// el.BoundingClientRect() 得到零矩形，el.Measure(cb) 以 (0,0) 回调。
type Measurer interface {
	Measure(id gxscene.NodeID) (w, h float32, ok bool)
}

// ---- App 侧：把后端能力适配进场景树 ----

// measureReq 是一条待回填的异步布局读取请求。
type measureReq struct {
	id gxscene.NodeID
	cb func(w, h float32)
}

// installElementCaps 在 Render 拿到实例后调用：把窗口后端的 Focuser/Measurer
// 适配成 gxscene 的能力函数注入共享 sink，使任意深度节点上的 ref 句柄可用。
// 同时注入异步布局回填钩子：Element.Measure 同步读不到时登记到 a.pendingMeasures，
// 帧后由 flushMeasures 回填。
func (a *App) installElementCaps(inst *gxscene.Instance) {
	if inst == nil {
		return
	}
	var focus func(gxscene.NodeID) bool
	if _, ok := a.win.(Focuser); ok {
		focus = func(id gxscene.NodeID) bool {
			a.win.(Focuser).Focus(id)
			return true
		}
	}
	var measure func(gxscene.NodeID) (float32, float32, bool)
	if _, ok := a.win.(Measurer); ok {
		measure = func(id gxscene.NodeID) (float32, float32, bool) {
			return a.win.(Measurer).Measure(id)
		}
	}

	// 异步回填钩子：Element.Measure 同步拿不到布局时把请求挂到这里。
	inst.SetMeasureHook(func(id gxscene.NodeID, cb func(w, h float32)) {
		if cb == nil {
			return
		}
		a.pendingMu.Lock()
		a.pendingMeasures = append(a.pendingMeasures, measureReq{id: id, cb: cb})
		a.pendingMu.Unlock()
	})

	// 需要回填钩子时把 focus/measure 一起注入；没有 Measurer 时 measure 仍注入
	// （返回 ok=false），Element.BoundingClientRect 得到零矩形——语义一致。
	inst.SetElementCaps(focus, measure)
}

// flushMeasures 在一帧 Present/Draw 成功后调用：把挂起的异步布局读取用最新
// 布局回填。后端实现 Measurer 且能给出布局时用真实值；否则（含 headless、
// 节点尚未布局）以 (0,0) 回填，保证回调不悬挂。回调在锁外执行——用户代码
// 可能写 signal 触发新的 effect，持 pendingMu 会造成重入死锁。
func (a *App) flushMeasures() {
	a.pendingMu.Lock()
	reqs := a.pendingMeasures
	a.pendingMeasures = nil
	a.pendingMu.Unlock()
	if len(reqs) == 0 {
		return
	}
	var m Measurer
	if mm, ok := a.win.(Measurer); ok {
		m = mm
	}
	for _, r := range reqs {
		// 优先读后端回流注册表（Draw 里已 PublishMeasured），其次可选 Measurer，
		// 都没有则 (0,0)——headless 与未接回流的后端零改动。
		var w, h float32
		if measured, exists := gxscene.MeasuredSize(r.id); exists {
			w, h = float32(measured.W), float32(measured.H)
		} else if m != nil {
			mw, mh, ok := m.Measure(r.id)
			if ok {
				w, h = mw, mh
			}
		}
		r.cb(w, h)
	}
}

// Measure 登记一次异步布局读取：下一帧 Present/Draw 后用最新布局回调 cb。
// 这是 App 级的公开 API，供不用 ref 句柄、只知道 NodeID 的高级调用方使用
// （如组件内部想读某个子节点尺寸）。cb 为 nil 时忽略。
func (a *App) Measure(id gxscene.NodeID, cb func(w, h float32)) {
	if a == nil || cb == nil {
		return
	}
	a.pendingMu.Lock()
	a.pendingMeasures = append(a.pendingMeasures, measureReq{id: id, cb: cb})
	a.pendingMu.Unlock()
}
