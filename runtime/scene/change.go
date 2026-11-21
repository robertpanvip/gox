package gxscene

import "sync"

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
	ChangeReplaceChildren                   // 节点的 Children 被整体重建；后端按 NodeID 重读 children
	ChangeAppendChild                       // 追加子节点；Value=子节点 *Node（完整子树）
	ChangeRemoveChild                       // 移除子节点；Value=子节点 *Node
	ChangeDestroy                           // 节点从树中移除；Value=节点 *Node
)

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
	case ChangeAppendChild:
		return "AppendChild"
	case ChangeRemoveChild:
		return "RemoveChild"
	case ChangeDestroy:
		return "Destroy"
	default:
		return "Unknown"
	}
}

// Change 是一条增量变更指令。NodeID 为目标节点（跨帧稳定的逻辑 id），
// Key/Value 依 Kind 而定；*Node 形态的 Value 让后端拿到完整子树内容。
type Change struct {
	Kind   ChangeKind
	NodeID string
	Key    string
	Value  any
}

// changeSink 是线程安全的变更缓冲区。根 Instance 创建，所有子实例共享——
// 组件、条件分支、keyed 列表里发生的变更都汇入同一条有序流。
type changeSink struct {
	mu      sync.Mutex
	enabled bool
	changes []Change
}

func newChangeSink() *changeSink { return &changeSink{} }

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

// EnableChanges 打开增量变更记录。必须在产生变更的更新之前调用。
// 不调用时完全退回旧行为：不分配、不记录，后端照常全量 Draw(root)。
func (i *Instance) EnableChanges() {
	if i != nil {
		i.changes.enable()
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
	id := ""
	if node != nil {
		id = node.ID
	}
	i.changes.add(Change{Kind: kind, NodeID: id, Key: key, Value: value})
}
