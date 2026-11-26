package gxweb

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

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

	// 帧诊断（E2E 用）：最近一次 Present 的增量长度 / 全量标志 / 应用条数。
	lastChangesLen int
	lastFullSync   bool
	lastApplied    int
	// lastChangeSample：最近一帧前 5 条 Change 的摘要（Kind/NodeID/Key），
	// 以及镜像未命中（m==nil 被跳过）的条数。
	lastChangeSample []string
	lastSkipped      int
	lastKinds        map[string]any
}

// ProbeFrame 返回最近一次 Present 的帧诊断信息。
// 注意：js.ValueOf 只接受 []any 不接受 []string、不接受 uint64——字段须用这些类型。
func (w *Window) ProbeFrame() map[string]any {
	w.mu.Lock()
	defer w.mu.Unlock()
	sample := make([]any, 0, len(w.lastChangeSample))
	for _, s := range w.lastChangeSample {
		sample = append(sample, s)
	}
	return map[string]any{
		"lastChangesLen": w.lastChangesLen,
		"lastFullSync":   w.lastFullSync,
		"lastApplied":    w.lastApplied,
		"frames":         int64(w.frames),
		"mirrorNodes":    len(w.nodes),
		"sample":         sample,
		"skipped":        w.lastSkipped,
		"kinds":          w.lastKinds,
	}
}

// mirrorNode 记录一个场景节点在 DOM 侧的镜像状态。
type mirrorNode struct {
	id             gxscene.NodeID
	elem           ElemID
	kind           string
	text           string
	node           *gxscene.Node               // 最新场景节点指针（结构变化后刷新，供事件回投）
	firstElem      ElemID                      // 透明节点(#frag)第一个真实 DOM 子元素；作为 before 锚点时用
	attrs          map[string]string           // 最近一次下传的普通属性/样式（含空串=已移除）
	richHTML       string                      // 最近一次下传的富文本 HTML（rich-text 属性）
	selection      gxscene.SelectionStyle      // 最近一次下传的 &::selection 子表（富文本选区配色）
	selectionRange gxscene.SelectionRange      // 最近一次下传的 selection-range（rune 区间）
	handlers       map[string]func(FiredEvent) // 已注册的事件监听（key=规范化事件名）
}

// richHTML 序列化富文本：按 selection-range 给落在 [start,end) rune 区间的分段
// 打上 Selected，再用 &::selection 配色序列化。这样键盘程序化选区与鼠标拖选
// （原生 ::selection）在 Web 端都呈现统一的主题选区色。
func richHTML(rt gxscene.RichText, sel gxscene.SelectionStyle, selRange gxscene.SelectionRange) string {
	if !selRange.Valid() {
		return rt.HTMLWithSelection(sel)
	}
	marked := make(gxscene.RichText, 0, len(rt))
	pos := 0
	for _, span := range rt {
		n := utf8.RuneCountInString(span.Text)
		if n == 0 {
			continue
		}
		span.Selected = span.Selected || (pos < selRange.End && pos+n > selRange.Start)
		marked = append(marked, span)
		pos += n
	}
	return marked.HTMLWithSelection(sel)
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
		w.lastChangesLen = len(frame.Changes)
		w.lastFullSync = true
		w.lastApplied = len(w.nodes)
		w.lastChangeSample = nil
		w.lastKinds = map[string]any{}
		w.lastSkipped = 0
	} else {
		w.lastChangesLen = len(frame.Changes)
		w.lastFullSync = false
		w.lastSkipped = 0
		w.lastChangeSample = make([]string, 0, 5)
		kinds := map[string]any{}
		counts := map[string]int{}
		for i, c := range frame.Changes {
			counts[c.Kind.String()]++
			if i < 40 {
				w.lastChangeSample = append(w.lastChangeSample,
					fmt.Sprintf("%s id=%s key=%s hit=%v", c.Kind, c.NodeID, c.Key, w.nodes[gxscene.ParseNodeID(c.NodeID)] != nil))
			}
			if w.nodes[gxscene.ParseNodeID(c.NodeID)] == nil {
				w.lastSkipped++
			}
		}
		for k, v := range counts {
			kinds[k] = int64(v)
		}
		w.lastKinds = kinds
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
		w.buildLocked(c, w.root, 0)
	}
}

// buildLocked 递归把节点建成 DOM 并登记镜像。返回该节点对应的元素。
// before 非 0 时，本层产生的真实元素插到该锚点之前（用于增量插入子树）；
// 连续插到同一锚点前会保持子树顺序（后插的排在先插的后面）。递归子节点时传 0。
func (w *Window) buildLocked(n *gxscene.Node, parent ElemID, before ElemID) ElemID {
	if n == nil {
		return 0
	}
	// 富文本节点虽是 NodeText（供 skia 测量/无障碍用纯文本），但 web 端必须
	// 建成真实 span 元素才能用 innerHTML 一次写入多段着色，故绕过文本节点分支。
	_, isRich := gxscene.RichTextFromProps(n.Props)
	if (n.Kind == "#text" || n.Type == gxscene.NodeText) && !isRich {
		e := w.driver.Create("#text", n.ID)
		m := newMirrorNode(n.ID, e, "#text")
		m.node = n
		m.text = n.Text
		w.registerLocked(m)
		w.driver.SetText(e, n.Text)
		w.driver.InsertBefore(parent, e, before)
		return e
	}
	if n.Kind == "#frag" || n.Kind == "#root" || n.Type == gxscene.NodeFragment {
		// 透明节点：子节点直接挂到 parent，不产生 DOM 元素。
		// 但仍登记一个指向 parent 的"透明镜像"（只进 nodes、不进 elems）：For/Branch
		// 的增删指令以该透明节点的 NodeID 为父（ChangeInsertBefore.NodeID），定位时
		// 用它拿到真实 DOM 父元素。若不登记，父定位失败会回退到 w.root，导致 For
		// 新增的列表项被插到页面根容器（表现为新行跑到界面最底部）。
		m := newMirrorNode(n.ID, parent, "#frag")
		m.node = n
		w.nodes[n.ID] = m
		for _, c := range n.Children {
			childElem := w.buildLocked(c, parent, before)
			if m.firstElem == 0 {
				m.firstElem = childElem
			}
		}
		return 0
	}
	createKind := n.Kind
	if isRich && (createKind == "#text" || createKind == "") {
		createKind = "span" // 富文本节点必须有真实元素承载 innerHTML
	}
	e := w.driver.Create(createKind, n.ID)
	m := newMirrorNode(n.ID, e, createKind)
	m.node = n
	if isRich {
		m.text = n.Text
	}
	w.registerLocked(m)

	// 属性/样式：展平后的标量写属性，事件名单独注册监听。
	// rich-text 是结构化分段值，走 innerHTML 一次写入（不进标量属性循环）。
	// 富文本节点的选区配色来自节点 &::selection 子表，选中区间由 selection-range
	// 标量给出（rune 索引），后端据此给分段打 Selected 再叠加配色。
	if rt, ok := gxscene.RichTextFromProps(n.Props); ok {
		sel := gxscene.SelectionFromProps(n.Props)
		selRange := gxscene.SelectionRangeFromProps(n.Props)
		m.selection = sel
		m.selectionRange = selRange
		h := richHTML(rt, sel, selRange)
		w.driver.SetRichText(e, h)
		m.richHTML = h
	}
	for k, v := range n.Props {
		if k == gxscene.PropRichText {
			continue
		}
		// 富文本节点的文字颜色已由 innerHTML 分段承载；跳过 gradient-text 与渐变 bg，
		// 否则 background-clip:text + color:transparent 会把 innerHTML 的字全部透明化。
		if isRich && (k == "gradient-text" || (k == "bg" && strings.HasPrefix(strings.TrimSpace(scalarString(v)), "linear-gradient("))) {
			continue
		}
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
	w.driver.InsertBefore(parent, e, before)
	// 富文本节点的全部可见内容已在 rich-text 的 innerHTML 里；其子节点只是
	// 供 skia 测量用的纯文本镜像，渲染到 web 会造成文字重复显示，故跳过。
	if !isRich {
		for _, c := range n.Children {
			w.buildLocked(c, e, 0)
		}
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

// destroyTreeLocked 递归销毁一棵子树在 DOM 侧的镜像与元素。透明 #frag 无独立元素，
// 仅递归其子节点；真实节点销毁 DOM 元素并从 nodes/elems 索引移除。
func (w *Window) destroyTreeLocked(n *gxscene.Node) {
	if n == nil {
		return
	}
	m := w.nodes[n.ID]
	if m != nil && m.kind != "#frag" {
		w.driver.Destroy(m.elem)
		delete(w.elems, m.elem)
	}
	delete(w.nodes, n.ID)
	for _, c := range n.Children {
		w.destroyTreeLocked(c)
	}
}

// beforeElemLocked 把锚点 NodeID 解析为 DOM 元素。透明 #frag 节点本身无 DOM 元素，
// 作为 before 锚点时要落到它的第一个真实子元素（"插到该项之前"= 插到该项首个 DOM 元素前）。
func (w *Window) beforeElemLocked(id gxscene.NodeID) ElemID {
	if id == 0 {
		return 0
	}
	bm := w.nodes[id]
	if bm == nil {
		return 0
	}
	if bm.kind == "#frag" {
		return bm.firstElem
	}
	return bm.elem
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
			if c.Key == gxscene.PropRichText {
				// 富文本：一次 innerHTML 写入所有着色分段（含选区配色叠加）。
				var h string
				if rt, ok := c.Value.(gxscene.RichText); ok {
					h = richHTML(rt, m.selection, m.selectionRange)
				}
				if h != m.richHTML {
					w.driver.SetRichText(m.elem, h)
					m.richHTML = h
				}
				continue
			}
			// selection 子表 / selection-range 变化：更新镜像并重新序列化富文本，
			// 使键盘选区移动与主题切换都即时反映到选区色块。
			if (c.Key == gxscene.PropSelection || c.Key == gxscene.PropSelectionRange) && m.richHTML != "" {
				if c.Key == gxscene.PropSelection {
					m.selection = gxscene.SelectionFromProps(map[string]any{c.Key: c.Value})
				} else {
					m.selectionRange = gxscene.SelectionRangeFromProps(map[string]any{c.Key: c.Value})
				}
				if m.node != nil {
					if rt, ok := gxscene.RichTextFromProps(m.node.Props); ok {
						h := richHTML(rt, m.selection, m.selectionRange)
						if h != m.richHTML {
							w.driver.SetRichText(m.elem, h)
							m.richHTML = h
						}
					}
				}
				// 标量属性仍需下传给驱动（如 &::selection 写 CSS 变量供鼠标拖选用）。
				s := scalarString(c.Value)
				w.driver.SetAttr(m.elem, c.Key, s)
				m.attrs[c.Key] = s
				continue
			}
			// 富文本节点忽略 gradient-text 与渐变 bg（颜色由 innerHTML 承载）。
			if m.richHTML != "" && (c.Key == "gradient-text" || (c.Key == "bg" && strings.HasPrefix(strings.TrimSpace(scalarString(c.Value)), "linear-gradient("))) {
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
			// 富文本节点的内容由 innerHTML 承载；纯文本变化已随 rich-text 一起下发，
			// 此处跳过 SetText，否则会抹掉着色 span。
			if m.richHTML != "" {
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
				if child.kind == "#frag" {
					// 透明节点无独立 DOM 元素（elem 指向父容器，绝不能 Destroy）。
					// 递归销毁其子树里的真实元素并清理镜像。
					if sub, ok := c.Value.(*gxscene.Node); ok && sub != nil {
						w.destroyTreeLocked(sub)
					}
				} else {
					w.driver.Destroy(child.elem)
					delete(w.elems, child.elem)
				}
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
			beforeElem := w.beforeElemLocked(before)
			parentElem := w.root
			if m != nil {
				parentElem = m.elem
			}
			if sub, ok := c.Value.(*gxscene.Node); ok && sub != nil {
				// 子树顶层真实元素必须插到锚点之前，不能 append 到末尾——
				// 否则 For 从右往左发的插入会倒序（新行变 8,7,6）。
				w.buildLocked(sub, parentElem, beforeElem)
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
			beforeElem := w.beforeElemLocked(before)
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
