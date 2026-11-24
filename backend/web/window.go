package gxweb

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// Window 是 Web 后端窗口，实现 gxapp.Window + gxapp.FramePresenter，以及可选的
// Focuser / Measurer / ComputedStyler。所有方法并发安全。
//
// 它持有一棵「镜像」：NodeID → ElemID，以及每条 DOM 元素上写入过的属性/handler，
// 用于增量重绘时只下传真正变化的字段（与 skia 的 framecache 同思路）。
type Window struct {
	mu      sync.Mutex
	driver  Driver
	cfg     gxapp.WindowConfig
	opened  bool
	closed  bool
	frames  uint64
	version uint64

	// 镜像：场景 NodeID → DOM 元素；DOM 元素 → 场景节点信息。
	nodes map[gxscene.NodeID]*mirrorNode
	elems map[ElemID]*mirrorNode
	root  ElemID
}

// mirrorNode 记录一个场景节点在 DOM 侧的镜像状态。
type mirrorNode struct {
	id       gxscene.NodeID
	elem     ElemID
	kind     string
	text     string
	node     *gxscene.Node                 // 最新场景节点指针（结构变化后刷新，供事件回投）
	attrs    map[string]string            // 最近一次下传的普通属性/样式（含空串=已移除）
	handlers map[string]func(FiredEvent)  // 已注册的事件监听（key=规范化事件名）
}

func newMirrorNode(id gxscene.NodeID, elem ElemID, kind string) *mirrorNode {
	return &mirrorNode{
		id: id, elem: elem, kind: kind,
		attrs: map[string]string{}, handlers: map[string]func(FiredEvent){},
	}
}

// Open 构造 Web 后端窗口。selector 指定挂载点（如 "#root"），空串用 document.body。
//
//	win, _ := gxweb.Open("#root")
//	app, _ := gxapp.OpenWith(gxapp.WindowConfig{Backend: "web"}, win)
func Open(selector string) (*Window, error) {
	w := &Window{
		driver: newPlatformDriver(selector),
		nodes:  map[gxscene.NodeID]*mirrorNode{},
		elems:  map[ElemID]*mirrorNode{},
	}
	return w, nil
}

// MustOpen 同 Open，失败时 panic（适合初始化期确定性装配）。
func MustOpen(selector string) *Window {
	w, err := Open(selector)
	if err != nil {
		panic(err)
	}
	return w
}

// NewWithDriver 用自定义驱动构造窗口——主要给单测注入 memDriver，也给宿主
// 自定义 DOM 实现（如 SSR / 虚拟 DOM）留出扩展点。
func NewWithDriver(d Driver) *Window {
	return &Window{
		driver: d,
		nodes:  map[gxscene.NodeID]*mirrorNode{},
		elems:  map[ElemID]*mirrorNode{},
	}
}

// Register 把 "web" 后端注册到 gxapp 注册表，装配层随后可用
// gxapp.OpenBackend(gxapp.WindowConfig{Backend:"web"})。selector 同 Open。
func Register(selector string) {
	gxapp.MustRegister("web", func(cfg gxapp.WindowConfig) (gxapp.Window, error) {
		return Open(selector)
	})
}

// ---- gxapp.Window ----

func (w *Window) Open(cfg gxapp.WindowConfig) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errClosed
	}
	w.cfg = cfg
	w.cfg.Backend = "web"
	w.opened = true
	// 根元素：透明容器，挂到宿主 selector 节点下；场景树的顶层节点插进来。
	if w.root == 0 {
		w.root = w.driver.Create("#root", gxscene.NoNode)
		w.driver.Mount(w.root)
	}
	return nil
}

// Draw 老式全量绘制路径：按 Root 重建整棵 DOM 镜像。FramePresenter 后端优先走
// Present，Draw 仅在不实现 Present 的场景被调用；这里仍实现以保证接口完整。
func (w *Window) Draw(root *gxscene.Node) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.opened {
		return errNotOpen
	}
	if w.closed {
		return errClosed
	}
	w.rebuildLocked(root)
	w.frames++
	w.version++
	return nil
}

// Run 立即返回：Web 后端没有自己的事件循环，事件由浏览器 DOM 驱动、经
// FireEvent 回投到场景树。
func (w *Window) Run() error { return nil }

func (w *Window) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	w.nodes = map[gxscene.NodeID]*mirrorNode{}
	w.elems = map[ElemID]*mirrorNode{}
	w.root = 0
	return nil
}

func (w *Window) Config() gxapp.WindowConfig {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cfg
}

// Frames 返回已绘制帧数（观测面）。
func (w *Window) Frames() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.frames
}

// Version 返回最近一次帧版本（观测面）。
func (w *Window) Version() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.version
}

// ---- gxapp.FramePresenter ----

// Present 实现增量/全量统一提交。FullSync=true 或首帧时按 Root 全量重建；
// 否则按 Changes 逐条打补丁。返回前 DOM 已同步完成（同步消费契约）。
func (w *Window) Present(frame *gxapp.Frame) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.opened {
		return errNotOpen
	}
	if w.closed {
		return errClosed
	}
	if frame == nil {
		return fmt.Errorf("gxweb: frame 为 nil")
	}
	if frame.FullSync || len(w.nodes) == 0 {
		w.rebuildLocked(frame.Root)
	} else {
		w.applyChangesLocked(frame.Changes, frame.Root)
	}
	w.frames++
	w.version = frame.Version
	return nil
}

// ---- 可选能力（gxapp.Focuser / Measurer / ComputedStyler） ----

func (w *Window) Focus(id gxscene.NodeID) {
	w.mu.Lock()
	e := w.nodes[id]
	w.mu.Unlock()
	if e != nil {
		w.driver.Focus(e.elem)
	}
}

func (w *Window) Measure(id gxscene.NodeID) (float32, float32, bool) {
	w.mu.Lock()
	e := w.nodes[id]
	w.mu.Unlock()
	if e == nil {
		return 0, 0, false
	}
	x, y, width, height, ok := w.driver.Rect(e.elem)
	_ = x
	_ = y
	return width, height, ok
}

func (w *Window) ComputedStyle(id gxscene.NodeID, state string) map[string]string {
	w.mu.Lock()
	e := w.nodes[id]
	w.mu.Unlock()
	if e == nil {
		return map[string]string{}
	}
	return w.driver.ComputedStyle(e.elem, state)
}

// NodeIDForElem 反查 DOM 元素对应的场景节点（集成层处理原生事件时用）。
func (w *Window) NodeIDForElem(e ElemID) (gxscene.NodeID, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	m := w.elems[e]
	if m == nil {
		return 0, false
	}
	return m.id, true
}

// ---- 事件回投 ----

// FireEvent 把浏览器事件回投到场景树。ev.NodeID 由驱动创建元素时绑定，
// 后端据此定位 gxscene.Node 并构造 SyntheticEvent 冒泡派发。
func (w *Window) FireEvent(ev FiredEvent) bool {
	w.mu.Lock()
	m := w.nodes[ev.NodeID]
	w.mu.Unlock()
	if m == nil {
		return false
	}
	native := ev.Name
	se := gxscene.NewSyntheticEvent(native, nil, ev.Raw)
	se.X, se.Y = ev.X, ev.Y
	se.Button = ev.Button
	se.Key = ev.Key
	se.Mods = gxscene.Modifier(ev.Mods)
	if ev.Value != "" {
		if se.Data == nil {
			se.Data = map[string]any{}
		}
		se.Data["value"] = ev.Value
	}
	ctx := gxscene.EventContext{Raw: ev.Raw, Synthetic: se}
	// 命中节点由镜像反查：Node 本身不在镜像里存指针，而是从当前场景树拿。
	// 这里用一个轻量索引：mirrorNode 持有最新 *Node。
	if m.node == nil {
		return false
	}
	se.Target = m.node
	se.CurrentTarget = m.node
	return m.node.DispatchBubble(native, ctx)
}

// ---- 内部：镜像重建与增量补丁 ----

func (w *Window) rebuildLocked(root *gxscene.Node) {
	// 清空旧镜像并销毁元素（保留 root 容器）。
	for id, m := range w.elems {
		if m.elem != w.root {
			w.driver.Destroy(m.elem)
		}
		delete(w.elems, id)
	}
	w.nodes = map[gxscene.NodeID]*mirrorNode{}
	if root == nil {
		return
	}
	for _, c := range root.Children {
		w.buildLocked(c, w.root)
	}
}

// buildLocked 递归把节点建成 DOM 并登记镜像。返回该节点对应的元素。
func (w *Window) buildLocked(n *gxscene.Node, parent ElemID) ElemID {
	if n == nil {
		return 0
	}
	if n.Kind == "#text" || n.Type == gxscene.NodeText {
		e := w.driver.Create("#text", n.ID)
		m := newMirrorNode(n.ID, e, "#text")
		m.node = n
		m.text = n.Text
		w.registerLocked(m)
		w.driver.SetText(e, n.Text)
		w.driver.InsertBefore(parent, e, 0)
		return e
	}
	if n.Kind == "#frag" || n.Kind == "#root" || n.Type == gxscene.NodeFragment {
		// 透明节点：子节点直接挂到 parent，不产生 DOM 元素。
		for _, c := range n.Children {
			w.buildLocked(c, parent)
		}
		return 0
	}
	e := w.driver.Create(n.Kind, n.ID)
	m := newMirrorNode(n.ID, e, n.Kind)
	m.node = n
	w.registerLocked(m)

	// 属性/样式：展平后的标量写属性，事件名单独注册监听。
	for k, v := range n.Props {
		s := scalarString(v)
		if s == "" {
			continue
		}
		w.driver.SetAttr(e, k, s)
		m.attrs[k] = s
	}
	for name := range n.Handlers {
		ev := string(gxscene.NormalizeEventName(name))
		w.attachHandlerLocked(m, ev, name)
	}
	if ops, ok := n.Props["canvas-ops"].(string); ok {
		w.driver.CanvasOps(e, ops)
	}
	w.driver.InsertBefore(parent, e, 0)
	for _, c := range n.Children {
		w.buildLocked(c, e)
	}
	return e
}

// registerLocked 登记镜像（重名节点先销毁旧的，避免泄漏）。
func (w *Window) registerLocked(m *mirrorNode) {
	if old := w.nodes[m.id]; old != nil {
		delete(w.elems, old.elem)
		w.driver.Destroy(old.elem)
	}
	w.nodes[m.id] = m
	w.elems[m.elem] = m
}

// attachHandlerLocked 为节点注册一个事件监听。闭包捕获 NodeID 与原生事件名，
// 回调时走 FireEvent 统一回投路径。
func (w *Window) attachHandlerLocked(m *mirrorNode, ev, native string) {
	if _, ok := m.handlers[ev]; ok {
		return
	}
	id := m.id
	cb := func(f FiredEvent) {
		f.Name = native
		f.NodeID = id
		w.FireEvent(f)
	}
	m.handlers[ev] = cb
	w.driver.Listen(m.elem, ev, cb)
}

// applyChangesLocked 按 Change 序列打增量补丁。结构指令按 ChildID 定位元素，
// 属性/文本/事件按 NodeID 定位。遇到无法定位的 ID 安全跳过（等下帧 FullSync）。
func (w *Window) applyChangesLocked(changes []gxscene.Change, root *gxscene.Node) {
	for _, c := range changes {
		id := gxscene.ParseNodeID(c.NodeID)
		m := w.nodes[id]
		switch c.Kind {
		case gxscene.ChangeSetAttr:
			if m == nil {
				continue
			}
			s := scalarString(c.Value)
			w.driver.SetAttr(m.elem, c.Key, s)
			m.attrs[c.Key] = s
			if c.Key == "canvas-ops" {
				w.driver.CanvasOps(m.elem, s)
			}
		case gxscene.ChangeSetText:
			if m == nil {
				continue
			}
			s := scalarString(c.Value)
			w.driver.SetText(m.elem, s)
			m.text = s
		case gxscene.ChangeSetHandler:
			if m == nil {
				continue
			}
			ev := string(gxscene.NormalizeEventName(c.Key))
			w.attachHandlerLocked(m, ev, c.Key)
		case gxscene.ChangeRemoveChild:
			cid := gxscene.ParseNodeID(c.ChildID)
			if child := w.nodes[cid]; child != nil {
				w.driver.Destroy(child.elem)
				delete(w.elems, child.elem)
				delete(w.nodes, cid)
			}
		case gxscene.ChangeReplaceChildren:
			// 兜底：整体重建该节点的子树。该指令语义是「Children 被整体重建，
			// 按 NodeID 重读」——最稳妥的做法是从 Root 重建整棵树；增量属性路径
			// 已覆盖叶子热更新，结构变更频率低，全量重建的代价可接受。
			if m == nil {
				continue
			}
			w.rebuildLocked(root)
			return
		case gxscene.ChangeInsertBefore:
			cid := gxscene.ParseNodeID(c.ChildID)
			before := gxscene.ParseNodeID(c.BeforeID)
			var beforeElem ElemID
			if bm := w.nodes[before]; bm != nil {
				beforeElem = bm.elem
			}
			parentElem := w.root
			if m != nil {
				parentElem = m.elem
			}
			if sub, ok := c.Value.(*gxscene.Node); ok && sub != nil {
				w.buildLocked(sub, parentElem)
			} else if child := w.nodes[cid]; child != nil {
				w.driver.InsertBefore(parentElem, child.elem, beforeElem)
			}
		case gxscene.ChangeMoveChild:
			cid := gxscene.ParseNodeID(c.ChildID)
			before := gxscene.ParseNodeID(c.BeforeID)
			child := w.nodes[cid]
			if child == nil || m == nil {
				continue
			}
			var beforeElem ElemID
			if bm := w.nodes[before]; bm != nil {
				beforeElem = bm.elem
			}
			w.driver.InsertBefore(m.elem, child.elem, beforeElem)
		}
	}
	// 刷新 node 指针（结构变化后 m.node 可能指向旧节点）。
	w.refreshNodePtrs(root)
}

func (w *Window) refreshNodePtrs(root *gxscene.Node) {
	var walk func(*gxscene.Node)
	walk = func(n *gxscene.Node) {
		if n == nil {
			return
		}
		if m := w.nodes[n.ID]; m != nil {
			m.node = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
}

// scalarString 把 props/change 的标量值转为属性字符串。函数/complex/结构体等非
// 标量返回空串（事件走单独通道，组件子树不写属性）。
func scalarString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'g', -1, 32)
	default:
		// 某些样式的数字以 json.Number / fmt.Stringer 形态出现，兜底处理。
		if s, ok := v.(fmt.Stringer); ok {
			return s.String()
		}
		return ""
	}
}

// isStyleAttr 供测试/调试：判断一个键是否是受限样式属性。
func isStyleAttr(key string) bool {
	return isStyleKey(strings.TrimPrefix(key, "&:"))
}
