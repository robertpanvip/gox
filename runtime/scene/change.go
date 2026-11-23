package gxscene

import (
	"fmt"
	"sync"
)

// ---------------- 增量变更通道（additive：后端可选消费） ----------------
//
// 解决"后端每次 Draw 都要全量遍历/比对整棵树"的问题。host 在细粒度更新时，
// 把"什么变了"记录成一条有序的 Change 流；后端可选消费（TakeChanges）来做增量
// 重绘，也可以完全忽略、继续每次全量 Draw(root)——后者零改动，行为不变。
//
// 边界（与 backend-integration.md 一致）：
//   - host 只描述"节点的哪个属性/子节点变了"，不解释语义、不做命中/布局/绘制；
//   - 用 Node.ID 定位节点（跨帧稳定），不依赖易失效的 *Node 指针；
//   - 事件回路、命中、绘制仍 100% 归后端。
//
// 这是 opcode 常驻树 + 细粒度 Update 的自然产物：Update 本来就已经算出"哪些
// 插槽变了"，这里只是把该结果暴露出来——不像被移除的 vdom 后端那样每次重建整树。

// ChangeKind 是一条增量变更的类型。
type ChangeKind uint8

const (
	ChangeSetAttr         ChangeKind = iota // 设置属性；Key=属性名（style 也走这里），Value=新值
	ChangeSetText                           // 设置文本；Value=新文本
	ChangeSetHandler                        // 绑定/换绑事件；Key=事件名，Value=handler 原值
	ChangeReplaceChildren                   // 节点的 Children 被整体重建；后端按 NodeID 重读 children（兜底）
	ChangeRemoveChild                       // 移除子节点；ChildID=被移除节点，Value=该 *Node
	ChangeInsertBefore                      // 插入子节点；ChildID/Value=新子树，BeforeID=锚点（空=追加末尾，即 append）
	ChangeMoveChild                         // 移动已有子节点；ChildID=要移动的节点，BeforeID=锚点（空=移到末尾）
)

// 注：曾有 ChangeAppendChild / ChangeDestroy 两个常量，但实现从未发出（追加等价于
// BeforeID="" 的 InsertBefore，销毁等价于 RemoveChild + 后端自行释放缓存）。为避免
// "协议许诺、实现不发"导致后端按表实现时漏更新，已从协议面移除——协议表只列实际
// 会发出的指令。二者语义分别由 InsertBefore(BeforeID="") 与 RemoveChild 覆盖。

// String 返回可读的指令名，便于日志/测试。
func (k ChangeKind) String() string {
	switch k {
	case ChangeSetAttr:
		return "SetAttr"
	case ChangeSetText:
		return "SetText"
	case ChangeSetHandler:
		return "SetHandler"
	case ChangeReplaceChildren:
		return "ReplaceChildren"
	case ChangeRemoveChild:
		return "RemoveChild"
	case ChangeInsertBefore:
		return "InsertBefore"
	case ChangeMoveChild:
		return "MoveChild"
	default:
		return "Unknown"
	}
}

// Change 是一条增量变更指令。NodeID 为目标节点（跨帧稳定的逻辑 id），
// Key/Value 依 Kind 而定；*Node 形态的 Value 让后端拿到完整子树内容。
//
// 细粒度结构指令（InsertBefore/RemoveChild/MoveChild）用 ChildID 定位
// 被操作的子节点、BeforeID 作为顺序锚点，二者均为跨帧稳定的 Node.ID：
//   - InsertBefore：把 Value（完整子树）插到 NodeID 下 BeforeID 之前；BeforeID 空表示追加到末尾；
//   - RemoveChild：移除 ChildID 指向的子节点（Value 带出被移除子树，便于后端释放缓存）；
//   - MoveChild：把 ChildID 子节点移动到 BeforeID 之前（空表示移到末尾），节点指针/内容不变。
//
// 旧字段（NodeID/Key/Value）语义不变，向后兼容——只看旧字段的后端无需改动。
type Change struct {
	Kind     ChangeKind
	NodeID   string // 操作所在的父节点（结构指令）/ 发生变化的节点（属性/文本/事件指令）
	ChildID  string // 结构指令中被操作的子节点 ID
	BeforeID string // 结构指令中的顺序锚点子节点 ID；空=末尾
	Key      string
	Value    any
}

// ChildDiffer 是子节点 diff 策略：给定父节点与嫁接前/后的子节点切片，产出有序的
// 逐节点变更指令（通过 emit 回调）。后端可注入自己的实现覆盖 host 内建算法——
// 例如后端维护了自己的镜像树、已有更高效的 diff，就不必再跑 host 这一遍。
//
// 策略只负责"算出哪些子节点变了"，命中/布局/绘制仍 100% 归后端；产出的仍是
// 协议定义的 Change，因此注入策略不改变协议面，老后端不注入则走内建、零改动。
type ChildDiffer func(parent *Node, old, next []*Node, emit func(Change))

// changeSink 是线程安全的变更缓冲区。根 Instance 创建，所有子实例共享——
// 组件、条件分支、keyed 列表里发生的变更都汇入同一条有序流。
type changeSink struct {
	mu      sync.Mutex
	enabled bool
	changes []Change
	differ  ChildDiffer // nil 表示使用 host 内建 LIS diff
}

func newChangeSink() *changeSink { return &changeSink{} }

// setDiffer 注入/清除子节点 diff 策略（nil 恢复内建）。作用于整棵共享 sink。
func (s *changeSink) setDiffer(d ChildDiffer) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.differ = d
	s.mu.Unlock()
}

// enable 打开记录。默认关闭：不消费增量的后端完全不受影响（零分配、零开销）。
func (s *changeSink) enable() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.enabled = true
	s.mu.Unlock()
}

func (s *changeSink) add(c Change) {
	if s == nil || !s.enabled {
		return
	}
	s.mu.Lock()
	s.changes = append(s.changes, c)
	s.mu.Unlock()
}

// take 取走并清空当前所有变更，保持顺序。
func (s *changeSink) take() []Change {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	out := s.changes
	s.changes = nil
	s.mu.Unlock()
	return out
}

// peek 返回当前变更的副本，不清空。
func (s *changeSink) peek() []Change {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	out := append([]Change(nil), s.changes...)
	s.mu.Unlock()
	return out
}

// ---------------- Instance 的公开变更 API ----------------

// EnableChanges 打开增量变更记录。不调用时完全退回旧行为：不分配、不记录，
// 后端照常全量 Draw(root)。
//
// 首帧语义（重要）：Render 期间（含 applyAll 首渲染与接线时 effect 的首次执行）
// sink 默认为关闭，产生的变更不记录。因此调用方应当：先 Render 首帧 → 再
// EnableChanges → 之后每帧 TakeChanges 拿到的都是"相对首帧"的增量。若后端选择
// 在 Render 之前EnableChanges，则会收到首帧建树的 InsertBefore 流——此时后端必须
// 能从空镜像树逐条 apply 这些指令建起全量树；两种用法都合法，但一个后端应固定一种。
func (i *Instance) EnableChanges() {
	if i != nil {
		i.changes.enable()
	}
}

// SetChildDiffer 注入自定义子节点 diff 策略，覆盖 host 内建算法。
// 传 nil 恢复内建。作用于整棵共享 sink（含组件/分支/列表项），可在任意时刻调用。
//
// 后端若已有自己的镜像树与更高效的 diff，可用它直接产出协议 Change，避免 host
// 再跑一遍内建 LIS；不调用则行为不变。策略只算"哪些子节点变了"，不碰命中/布局/绘制。
func (i *Instance) SetChildDiffer(d ChildDiffer) {
	if i != nil {
		i.changes.setDiffer(d)
	}
}

// adoptSink 让子实例共享父实例的变更缓冲。挂载子实例（组件 / 分支 / 列表项）时
// 调用，确保整棵实时实例树的变更汇入同一个 sink——一次 EnableChanges 即全覆盖。
func (i *Instance) adoptSink(child *Instance) {
	if i == nil || child == nil || i.changes == nil {
		return
	}
	child.changes = i.changes
}

// adoptSinkDeep 把 sink 递归注入一棵「已建成」的实例子树。
//
// 与 adoptSink（只改单个子实例）不同，它还覆盖子树里 slotComp 挂载的组件、
// slotBranch 控制器及其当前子树，并让控制器继续覆盖它内部已建的分支/列表项。
// 用于 For.buildEntry / Branch.Resolve 这类「先建子树、建完才知道所属 sink」的
// 场景：子树在 Render 的 applyAll 阶段就把内部组件 adopt 到了创建期的临时 sink，
// 顶层 inst 随后换成共享 sink 时若不递归，内部组件的变更会写进孤立 sink 而丢失
// （第六轮评审 P1：<li><Card/></li> 内文本变化 TakeChanges 为空，补丁后端界面停住）。
//
// 递归经 deepSinkAdopter 局部接口下探控制器内部——避免改动 subtreeController 接口，
// 对未实现该可选方法的第三方控制器退化为普通 adoptSink。
func adoptSinkDeep(inst *Instance, s *changeSink) {
	if inst == nil || s == nil {
		return
	}
	inst.changes = s
	for _, sl := range inst.slots {
		if sl == nil {
			continue
		}
		switch sl.kind {
		case slotComp, slotDynamic:
			// slotDynamic（<Dynamic>）与 slotComp 一样承载可替换的子组件实例。
			adoptSinkDeep(sl.child, s)
		case slotChild:
			// Issue #1 后 slotChild 也会承载 *Instance（children 保稳子树），
			// 必须递归注入 sink，否则其内部组件变更写进孤立 sink 而丢失。
			adoptSinkDeep(sl.child, s)
		case slotBranch:
			if d, ok := sl.ctrl.(deepSinkAdopter); ok {
				d.adoptSinkDeep(s)
			} else if sl.ctrl != nil {
				sl.ctrl.adoptSink(s)
			}
			adoptSinkDeep(sl.child, s)
		}
	}
}

// deepSinkAdopter 是可选接口：subtreeController 若实现它，adoptSinkDeep 会递归
// 覆盖其内部已建的分支/列表项子树，而不只是控制器自身的 sink。
type deepSinkAdopter interface {
	adoptSinkDeep(*changeSink)
}

// Changes 返回自上次 TakeChanges 以来的所有变更（副本，不清空）。
// 由于整棵实例树共享同一 sink，这里一次拿到全部（含组件/分支/列表项内的变更）。
func (i *Instance) Changes() []Change {
	if i == nil {
		return nil
	}
	return i.changes.peek()
}

// TakeChanges 取走并清空当前所有变更。后端每帧调用一次即可拿到增量补丁。
func (i *Instance) TakeChanges() []Change {
	if i == nil {
		return nil
	}
	return i.changes.take()
}

// record 是内部唯一的变更记录入口。node 为发生变更的节点。
func (i *Instance) record(kind ChangeKind, node *Node, key string, value any) {
	if i == nil || i.changes == nil {
		return
	}
	i.changes.add(Change{Kind: kind, NodeID: nodeID(node), Key: key, Value: value})
}

// recordChild 记录一条细粒度结构指令：parent 为操作所在的父节点，
// child 为被操作的子节点，before 为顺序锚点（可为 nil 表示末尾）。
func (i *Instance) recordChild(kind ChangeKind, parent, child, before *Node, value any) {
	if i == nil || i.changes == nil {
		return
	}
	i.changes.add(Change{
		Kind:     kind,
		NodeID:   nodeID(parent),
		ChildID:  nodeID(child),
		BeforeID: nodeID(before),
		Value:    value,
	})
}

// nodeID 取节点稳定 ID，nil 安全。极少数未预分配 ID 的节点用指针地址兜底——
// 同一节点在一批指令里地址恒定，足以作为内部锚点使用。
func nodeID(n *Node) string {
	if n == nil {
		return ""
	}
	if n.ID != "" {
		return n.ID
	}
	return fmt.Sprintf("ptr/%p", n)
}

// ---------------- 逐节点 children diff ----------------
//
// diffChildren 是策略分派入口：若后端通过 SetChildDiffer 注入了自己的 diff 算法，
// 交由它产出指令；否则走 host 内建的 LIS diff。所有结构更新路径都经过这里。
func diffChildren(sink *changeSink, parent *Node, old, next []*Node) {
	if sink == nil || parent == nil {
		return
	}
	sink.mu.Lock()
	differ := sink.differ
	sink.mu.Unlock()
	if differ != nil {
		differ(parent, old, next, func(c Change) { sink.add(c) })
		return
	}
	builtinDiffChildren(sink, parent, old, next)
}

// builtinDiffChildren 是 host 内建的逐节点 diff：比较 parent 的旧子节点序列 old
// 与新序列 next，把"逐节点增删/移动"记录成有序指令流。它是被移除的 vdom Diff 里
// "哪些子节点变了"那部分的轻量回归，但**只作用于单个动态插槽的 children**，
// 不重建整棵树、不产生 createElement 调用。
//
// 复用判定用 *Node 指针身份（不是仅用 ID）：常驻子树、keyed 列表项复用同一指针，
// 标量/文本值变化会生成新 *Node——于是"同位置内容变了"会被正确识别为删+插，
// 而不是被同位置 ID 误判成未变。输出指令里的 ChildID/BeforeID 仍用跨帧稳定 ID，
// 供后端定位；这样既精确又不受指针失效影响。
//
// 为把移动次数降到最少：对复用节点按旧索引求最长递增子序列（LIS），LIS 上的节点
// 保持原位作骨架，其余复用节点发 MoveChild。然后从右往左发出插入/移动，锚点
// BeforeID 始终是右侧已就位的节点——按序 apply 后即得 next 的顺序。
//
// 边界：host 只描述结构变化，不做命中/布局/绘制；后端可忽略这些指令继续全量 Draw。
func builtinDiffChildren(sink *changeSink, parent *Node, old, next []*Node) {
	if sink == nil || parent == nil {
		return
	}

	oldIndex := make(map[*Node]int, len(old))
	for idx, n := range old {
		oldIndex[n] = idx
	}
	newSet := make(map[*Node]bool, len(next))
	for _, n := range next {
		newSet[n] = true
	}

	// ① 删除：旧有新无。按原顺序发出，便于后端顺序释放缓存。
	for _, n := range old {
		if !newSet[n] {
			sink.add(Change{Kind: ChangeRemoveChild, NodeID: parent.ID, ChildID: nodeID(n), Value: n})
		}
	}

	// ② 复用节点的 LIS：keep[j]=true 表示 next[j] 保持原位、不需要移动。
	prev := make([]int, len(next))
	for j := range prev {
		prev[j] = -1
	}
	tails := make([]int, 0, len(next))  // 递增子序列的末尾旧索引
	tailAt := make([]int, 0, len(next)) // 对应的 next 下标
	for j, n := range next {
		oi, reused := oldIndex[n]
		if !reused {
			continue
		}
		// 二分找 tails 中第一个 >= oi 的位置（下界）。
		lo, hi := 0, len(tails)
		for lo < hi {
			mid := int(uint(lo+hi) >> 1)
			if tails[mid] < oi {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo > 0 {
			prev[j] = tailAt[lo-1]
		}
		if lo == len(tails) {
			tails = append(tails, oi)
			tailAt = append(tailAt, j)
		} else {
			tails[lo] = oi
			tailAt[lo] = j
		}
	}
	keep := make([]bool, len(next))
	if len(tailAt) > 0 {
		for k := tailAt[len(tailAt)-1]; k >= 0; k = prev[k] {
			keep[k] = true
		}
	}

	// ③ 从右往左发出插入/移动，succ 是右侧已就位节点的 ID（空=末尾）。
	succ := ""
	for j := len(next) - 1; j >= 0; j-- {
		n := next[j]
		id := nodeID(n)
		if _, reused := oldIndex[n]; !reused {
			sink.add(Change{Kind: ChangeInsertBefore, NodeID: parent.ID, ChildID: id, BeforeID: succ, Value: n})
		} else if !keep[j] {
			sink.add(Change{Kind: ChangeMoveChild, NodeID: parent.ID, ChildID: id, BeforeID: succ})
		}
		if id != "" {
			succ = id
		}
	}
}

// applyChanges 把一组结构指令应用到一棵 *Node 树上，供测试与后端参考实现使用。
// 它让"增量补丁"可被独立验证：对 root 依次 apply 后的结果应与全量树一致。
// host 自身不用它绘制——后端按自己平台的方式消费即可。
func applyChanges(root *Node, changes []Change) *Node {
	nodes := map[string]*Node{}
	var index func(*Node)
	index = func(n *Node) {
		if n == nil {
			return
		}
		if nid := nodeID(n); nid != "" {
			nodes[nid] = n
		}
		for _, c := range n.Children {
			index(c)
		}
	}
	index(root)

	childrenOf := func(id string) (*Node, []*Node) {
		if id == "" {
			return nil, nil
		}
		p := nodes[id]
		if p == nil {
			return nil, nil
		}
		return p, p.Children
	}
	setChildren := func(parent *Node, cs []*Node) {
		if parent != nil {
			parent.Children = cs
		}
	}
	// 从子节点集合中移除。
	removeFrom := func(parent *Node, childID string) {
		if parent == nil {
			return
		}
		out := parent.Children[:0]
		for _, c := range parent.Children {
			if nodeID(c) == childID {
				continue
			}
			out = append(out, c)
		}
		parent.Children = out
	}

	// insertAt 把节点 n 插到 kids 中 anchorID 之前；anchorID 为空或找不到时追加末尾。
	insertAt := func(kids []*Node, anchorID string, n *Node) []*Node {
		at := len(kids)
		if anchorID != "" {
			for i, c := range kids {
				if nodeID(c) == anchorID {
					at = i
					break
				}
			}
		}
		out := make([]*Node, 0, len(kids)+1)
		out = append(out, kids[:at]...)
		out = append(out, n)
		out = append(out, kids[at:]...)
		return out
	}

	for _, ch := range changes {
		// 属性/文本/事件指令：直接按 NodeID 定位节点，不依赖父子关系。
		switch ch.Kind {
		case ChangeSetText:
			if n := nodes[ch.NodeID]; n != nil {
				n.Text, _ = ch.Value.(string)
			}
			continue
		case ChangeSetAttr:
			if n := nodes[ch.NodeID]; n != nil {
				if n.Props == nil {
					n.Props = map[string]any{}
				}
				n.Props[ch.Key] = ch.Value
			}
			continue
		case ChangeSetHandler:
			if n := nodes[ch.NodeID]; n != nil {
				if n.Handlers == nil {
					n.Handlers = map[string]any{}
				}
				n.Handlers[ch.Key] = ch.Value
			}
			continue
		case ChangeReplaceChildren:
			// 兜底：后端按 NodeID 重读 children；参考实现没有数据源可重读，
			// 保持现状（全量 Draw 路径不受影响）。
			continue
		}

		parent, kids := childrenOf(ch.NodeID)
		if parent == nil {
			continue
		}
		switch ch.Kind {
		case ChangeInsertBefore:
			n, _ := ch.Value.(*Node)
			if n == nil {
				continue
			}
			if nid := nodeID(n); nid != "" {
				nodes[nid] = n
			}
			kids = insertAt(kids, ch.BeforeID, n)
			setChildren(parent, kids)
		case ChangeRemoveChild:
			removeFrom(parent, ch.ChildID)
		case ChangeMoveChild:
			var moving *Node
			for _, c := range kids {
				if c != nil && c.ID == ch.ChildID {
					moving = c
					break
				}
			}
			if moving == nil {
				continue
			}
			removeFrom(parent, ch.ChildID)
			setChildren(parent, insertAt(parent.Children, ch.BeforeID, moving))
		}
	}
	return root
}

// recordChildTransition 记录一次 slotChild 写入后的结构变化。调用方在写入前
// 传入旧 children 快照 old 与写入前是否文本节点 wasText，写入后调用本方法。
//
// 文本↔容器互转时语义变化较大，退化为 ReplaceChildren 让后端整体重读该节点；
// 同类型则走细粒度 diff（列表增删/重排/标量文本更新）。
func (i *Instance) recordChildTransition(parent *Node, old []*Node, wasText bool) {
	if i == nil || i.changes == nil || parent == nil {
		return
	}
	isText := parent.Kind == "#text"
	if isText != wasText {
		i.record(ChangeReplaceChildren, parent, "", nil)
		return
	}
	if isText {
		i.record(ChangeSetText, parent, "", parent.Text)
		return
	}
	diffChildren(i.changes, parent, old, parent.Children)
}

// childrenSnapshot 浅拷贝节点的当前 children（指针切片），用于 diff 的旧侧快照。
func childrenSnapshot(n *Node) []*Node {
	if n == nil {
		return nil
	}
	return append([]*Node(nil), n.Children...)
}
