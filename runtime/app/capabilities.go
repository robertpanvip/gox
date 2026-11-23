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

	// 命令式写回（CanvasContext / el.requestAnimationFrame）的重绘请求通道。
	inst.SetDirtyHook(a.requestCAPaintDirty)
}

// ---- RAF（requestAnimationFrame）帧调度 ----

// drainRAF 在帧首执行所有已登记的「下一帧」回调。放在 Present/Draw 之前，
// 于是回调里 ctx.fillRect / el.requestAnimationFrame 的效果本帧即可见。
//
// inRAF 置位期间抑制回调内部的重绘请求（见 requestCAPaintDirty）：本帧 Drain
// 后紧接着就 Present，写回已包含在本次绘制里，无需再派发一帧——这也避免在
// drawNowErr 持 commit gate 时因同步派发而递归重入。
func (a *App) drainRAF(inst *gxscene.Instance) {
	if inst == nil {
		return
	}
	// 上一帧的预约已被本次绘制消费，释放去抖标志，使帧末能为下一帧重新预约。
	// 必须在 drain 回调之前清：InlineDispatcher 下 RequestFrame 同步完成整帧，
	// 帧末 scheduleRAFNext 就在同一调用栈内，标志此时须为 false。
	a.rafPending.Store(false)
	fns := inst.TakeRAF()
	if len(fns) == 0 {
		return
	}
	a.inRAF.Store(true)
	for _, fn := range fns {
		if fn == nil {
			continue
		}
		func() {
			// 单个回调 panic 不拖垮整帧（与浏览器 rAF 语义一致）。
			defer func() { _ = recover() }()
			fn()
		}()
	}
	a.inRAF.Store(false)
}

// scheduleRAFNext 在帧末检查回调是否登记了下一帧，有则预约一帧。
// 用 goroutine 异步派发：此时仍在 drawNowErr 的 commit gate 内，同步派发会让
// InlineDispatcher 立即重入 drawNowErr；异步则等本帧解锁后再画下一帧。
// rafPending 去抖，防止事件里高频登记 RAF 导致帧堆积。
func (a *App) scheduleRAFNext(inst *gxscene.Instance) {
	if inst == nil || a.closed.Load() || !inst.HasRAF() {
		return
	}
	if !a.rafPending.CompareAndSwap(false, true) {
		return
	}
	go func() {
		if a.closed.Load() {
			a.rafPending.Store(false)
			return
		}
		a.dispatcher.RequestFrame()
	}()
}

// requestCAPaintDirty 是命令式写回（ctx.fillRect / el.requestAnimationFrame）
// 触发的重绘请求（注入到 sink 的 onDirty）。drain 期间抑制：本帧会画出写回结果。
func (a *App) requestCAPaintDirty() {
	if a.closed.Load() || a.inRAF.Load() {
		return
	}
	a.dispatcher.RequestFrame()
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

// RequestAnimationFrame 登记一个下一帧回调（Web rAF 语义），App 级入口。
// 等价于在任意拿得到 Element 的地方调 el.requestAnimationFrame——队列挂在
// 场景树的共享 sink 上，回调在下一帧 Draw 之前执行，可再次登记形成动画循环。
// Render 之前（inst 为 nil）为 no-op。
func (a *App) RequestAnimationFrame(fn func()) {
	if a == nil || fn == nil || a.inst == nil {
		return
	}
	a.inst.RequestAnimationFrame(fn)
}
