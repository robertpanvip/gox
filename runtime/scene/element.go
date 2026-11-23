package gxscene

// 本文件实现 ref 命令式逃生口的运行期侧：Element 句柄 + 宿主能力注入。
//
// 设计要点（Host 架构评审）：
//   - ref 回调拿到的是 *Element 句柄，**不是**裸 NodeID。业务只见到 DOM 风格的
//     focus() / boundingClientRect() / measure()，节点身份（整数 NodeID）留在
//     host 内部，不流向业务代码。
//   - Element 的方法走一条可选的宿主能力通道（挂在共享 changeSink 上，由 gxapp
//     在 Render 后注入）。没有注入（纯 scene / headless）时全部安全降级为 no-op /
//     零值——后端零改动。
//   - 能力沿 sink 共享：组件子树、条件分支、keyed 列表的子实例都共用同一 sink，
//     因此任意深度节点上的 ref 句柄都能访问同一份宿主能力。

// Rect 是 ref 句柄读到的布局矩形，单位为后端坐标系（像素）。
// 它刻意是纯值类型，不含 NodeID 或节点指针——业务拿到的是一份快照。
type Rect struct {
	X, Y   float32
	Width  float32
	Height float32
}

// hostCaps 是 gxapp 注入到场景树的宿主能力集合。所有字段为 nil 时为空实现：
// 纯 scene 用法（RenderText / 单测）与 headless 后端都不需要实现它。
type hostCaps struct {
	// focus 让指定节点获得焦点；返回是否已被后端接受（headless 返回 false）。
	focus func(NodeID) bool
	// measure 同步读取布局；ok=false 表示后端没有该节点的布局（headless / 未布局）。
	measure func(NodeID) (w, h float32, ok bool)
}

// SetElementCaps 由 gxapp 在 Render 拿到实例后调用，把窗口后端能力注入整棵共享树。
// 传 nil 函数表示该项能力缺失，对应 Element 方法安全降级。
func (i *Instance) SetElementCaps(focus func(NodeID) bool, measure func(NodeID) (float32, float32, bool)) {
	if i == nil || i.changes == nil {
		return
	}
	i.changes.mu.Lock()
	i.changes.caps = hostCaps{focus: focus, measure: measure}
	i.changes.mu.Unlock()
}

// Element 是 ref 回调收到的命令式句柄。
//
// 它内部只保存节点身份（NodeID）与共享 sink，不向业务暴露 ID：
//   - Focus()              请求后端把焦点移到该节点；
//   - BoundingClientRect() 同步读最近一次布局矩形（无布局时返回零值）；
//   - Measure(cb)          登记异步读布局：下一帧绘制后由 gxapp 用最新布局回填 cb。
//
// GX 侧按小写书写（el.focus() / el.boundingClientRect() / el.measure(...)），
// 由编译器的 transformExternal 还原为上述首字母大写的方法。
type Element struct {
	id    NodeID
	sink  *changeSink
	node  *Node          // 对应场景节点，供命令式写回（如 canvas context）改 Props
	owner *Instance      // 记录变更所用的实例（按节点发 ChangeSetAttr）
	ctx   *CanvasContext // getContext 的缓存句柄，同一节点多次调用返回同一上下文
	// dead 在 ref 收到 nil（Dispose）后置位，之后所有方法变 no-op。
	dead bool
}

// newElement 为一个已分配 ID 的节点构造句柄。ID==NoNode 时返回 nil——
// 编译期保证 ref 挂在真实元素节点上（clone 阶段已分配 ID），这里是防御。
// node/owner 仅内部使用，供命令式绘制写回节点 Props 与变更流；不向业务暴露。
func newElement(id NodeID, sink *changeSink, node *Node, owner *Instance) *Element {
	if id == NoNode {
		return nil
	}
	return &Element{id: id, sink: sink, node: node, owner: owner}
}

// caps 读取当前注入的宿主能力。句柄方法在事件回调里调用，此时 Render 已完成、
// 能力已注入；读操作加锁，避免与注入并发。
func (e *Element) caps() hostCaps {
	if e == nil || e.dead || e.sink == nil {
		return hostCaps{}
	}
	e.sink.mu.Lock()
	c := e.sink.caps
	e.sink.mu.Unlock()
	return c
}

// Focus 请求后端把键盘焦点移到该节点。未注入能力 / 已销毁时为 no-op，
// 返回后端是否接受（headless 恒为 false）。
func (e *Element) Focus() bool {
	c := e.caps()
	if c.focus == nil {
		return false
	}
	return c.focus(e.id)
}

// readMeasured 是该节点最近一次布局尺寸的**统一数据源**：
//  1. 优先读后端回流注册表（MeasuredSize）——Skia/Rust 在 Draw 后 PublishMeasured，
//     虚拟列表自动测量那套回流数据直接成为 ref 句柄的尺寸来源，无需后端额外实现接口；
//  2. 其次走 gxapp 注入的可选 Measurer.measure（给不接回流、愿意同步提供尺寸的后端）；
//  3. 两者都没有时 ok=false。
func (e *Element) readMeasured() (w, h float32, ok bool) {
	if e == nil || e.dead {
		return 0, 0, false
	}
	if m, exists := MeasuredSize(e.id); exists {
		return float32(m.W), float32(m.H), true
	}
	if c := e.caps(); c.measure != nil {
		return c.measure(e.id)
	}
	return 0, 0, false
}

// BoundingClientRect 同步读该节点最近一次布局矩形。后端没有该节点布局、
// 或尚未注入能力时返回零值 Rect（Width/Height 均为 0）。
// 需要「等待下一帧布局」的语义请用 Measure。
func (e *Element) BoundingClientRect() Rect {
	w, h, ok := e.readMeasured()
	if !ok {
		return Rect{}
	}
	return Rect{Width: w, Height: h}
}

// Measure 登记一次异步布局读取。gxapp 在下一帧 Present/Draw 后调用 cb 回填
// (w, h)；后端未实现 Measurer（含 headless）时以 (0, 0) 回填，不悬挂调用方。
// cb 为 nil 时忽略。
func (e *Element) Measure(cb func(w, h float32)) {
	if cb == nil {
		return
	}
	if w, h, ok := e.readMeasured(); ok {
		cb(w, h)
		return
	}
	// 同步拿不到布局：交给宿主在帧后重试/回填。未注入宿主时直接零值回调，
	// 保证纯 scene 用法也不会让 cb 悬挂。
	if hook := e.sink.measureHook(); hook != nil {
		hook(e.id, cb)
		return
	}
	cb(0, 0)
}

// RequestAnimationFrame 登记一个下一帧回调（Web rAF 语义）。回调在下一帧
// Draw 之前执行；回调里可继续登记形成动画循环。登记后请求宿主重绘一帧，
// 保证「下一帧」真的发生（build 期 hook 未注入时由首帧 Render 兜底）。
// 已销毁句柄忽略。
//
// GX 小写写法：el.requestAnimationFrame(fn)
func (e *Element) RequestAnimationFrame(fn func()) {
	if e == nil || e.dead || fn == nil || e.sink == nil {
		return
	}
	e.sink.addRAF(fn)
	e.sink.dirty() // 请求宿主安排下一帧；纯 scene/headless 下 no-op
}

// disposeRef 把句柄标记为销毁态：之后所有方法 no-op。ref(nil) 由 applyAll 的
// 插槽清理路径触发，句柄本身不需要再回传。
func (e *Element) disposeRef() {
	if e != nil {
		e.dead = true
	}
}
