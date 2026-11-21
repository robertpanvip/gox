package gxscene

import (
	"testing"

	"github.com/robertpanvip/gox/runtime/signal"
)

func TestChangesDisabledByDefault(t *testing.T) {
	inst := Render(Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpDynAttr, Key: "class", Slot: 0},
		{Code: OpClose},
	}, N: 1}, func() any { return "a" })
	inst.SetPart(0, "b")
	if got := inst.Changes(); len(got) != 0 {
		t.Fatalf("未启用时应无变更记录: %v", got)
	}
}

func TestChangesSetAttrAndText(t *testing.T) {
	inst := Render(Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpDynAttr, Key: "class", Slot: 0},
		{Code: OpDynChild, Slot: 1},
		{Code: OpClose},
	}, N: 2},
		func() any { return "a" },
		func() any { return "old" },
	)
	inst.EnableChanges()
	inst.TakeChanges() // 清空接线期可能产生的记录

	inst.SetPart(0, "b")   // 属性变化
	inst.SetPart(1, "new") // 文本变化

	changes := inst.TakeChanges()
	if len(changes) != 2 {
		t.Fatalf("应有 2 条变更, got %d: %v", len(changes), changes)
	}
	if changes[0].Kind != ChangeSetAttr || changes[0].Key != "class" || changes[0].Value != "b" {
		t.Fatalf("属性变更不符: %+v", changes[0])
	}
	if changes[1].Kind != ChangeSetText || changes[1].Value != "new" {
		t.Fatalf("文本变更不符: %+v", changes[1])
	}
	// NodeID 应指向有 ID 的节点
	if changes[0].NodeID == "" {
		t.Fatal("变更应携带 Node.ID")
	}
	// TakeChanges 后应清空
	if got := inst.TakeChanges(); len(got) != 0 {
		t.Fatalf("TakeChanges 后应清空: %v", got)
	}
}

func TestChangesEqualValueNoRecord(t *testing.T) {
	inst := Render(Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpDynAttr, Key: "class", Slot: 0},
		{Code: OpClose},
	}, N: 1}, func() any { return "same" })
	inst.EnableChanges()
	inst.TakeChanges()
	inst.SetPart(0, "same") // 同值不产生写入
	if got := inst.Changes(); len(got) != 0 {
		t.Fatalf("同值不应记录变更: %v", got)
	}
}

func TestChangesSetHandler(t *testing.T) {
	inst := Render(Template{Ops: []Op{
		{Code: OpElem, Arg: "button"},
		{Code: OpEvent, Key: "onClick", Slot: 0},
		{Code: OpClose},
	}, N: 1}, func() any { return Handler(func(EventContext) {}) })
	inst.EnableChanges()
	inst.TakeChanges()
	newH := Handler(func(EventContext) {})
	i := inst
	i.slots[0].kind = slotEvent // 确保按事件槽处理
	inst.SetGetter(0, func() any { return newH })
	inst.Update()
	changes := inst.Changes()
	found := false
	for _, c := range changes {
		if c.Kind == ChangeSetHandler && c.Key == "onClick" {
			found = true
		}
	}
	if !found {
		t.Fatalf("应有 handler 换绑变更: %v", changes)
	}
}

func TestChangesWithComponentChild(t *testing.T) {
	// 组件子实例的变更应汇入同一 sink（adoptSink 生效）。
	childInst := Render(Template{Ops: []Op{
		{Code: OpElem, Arg: "span"},
		{Code: OpDynAttr, Key: "title", Slot: 0},
		{Code: OpClose},
	}, N: 1}, func() any { return "x" })

	root := Render(Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpComp, Arg: "Child", Slot: 0},
		{Code: OpClose},
	}, N: 1}, func() any { return childInst })
	root.EnableChanges()
	root.TakeChanges()

	childInst.SetPart(0, "y") // 子组件内部变更
	changes := root.Changes()
	if len(changes) == 0 {
		t.Fatal("组件子实例的变更应汇入根 sink")
	}
	found := false
	for _, c := range changes {
		if c.Kind == ChangeSetAttr && c.Key == "title" && c.Value == "y" {
			found = true
		}
	}
	if !found {
		t.Fatalf("子组件变更未出现在根变更流: %v", changes)
	}
}

func TestChangesTakeOnlyOnceAcrossTree(t *testing.T) {
	// 根 TakeChanges 应一次取走整棵树（含子实例）的变更，且不会重复。
	child := Render(Template{Ops: []Op{
		{Code: OpElem, Arg: "span"},
		{Code: OpDynAttr, Key: "id", Slot: 0},
		{Code: OpClose},
	}, N: 1}, func() any { return "a" })
	root := Render(Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpComp, Arg: "Child", Slot: 0},
		{Code: OpClose},
	}, N: 1}, func() any { return child })
	root.EnableChanges()
	root.TakeChanges()

	child.SetPart(0, "b")             // 子实例一条
	root.slots[0].node.Props["x"] = 1 // 直接改不记录（未走 SetPart）
	got := root.TakeChanges()
	if len(got) != 1 {
		t.Fatalf("整棵树一次应取到 1 条, got %d: %v", len(got), got)
	}
	if got2 := root.TakeChanges(); len(got2) != 0 {
		t.Fatalf("再次取应为空: %v", got2)
	}
}

// 列表项子实例的内部变更应通过 For.sink 汇入根 sink。
func TestChangesWithForKeyedItems(t *testing.T) {
	val := signal.New("v1")
	f := NewFor(
		func() []any { return []any{1} },
		func(v any) any { return v },
		func(v any) *Instance {
			return Render(Template{Ops: []Op{
				{Code: OpElem, Arg: "li"},
				{Code: OpDynChild, Slot: 0},
				{Code: OpClose},
			}, N: 1}, func() any { return val() })
		},
	)
	root := Render(Template{Ops: []Op{
		{Code: OpElem, Arg: "ul"},
		{Code: OpFor, Slot: 0},
		{Code: OpClose},
	}, N: 1}, func() any { return f })
	root.EnableChanges()
	root.TakeChanges()

	val("v2") // 触发列表项子实例的响应式 Effect
	changes := root.TakeChanges()
	found := false
	for _, c := range changes {
		if c.Kind == ChangeSetText && c.Value == "v2" {
			found = true
		}
	}
	if !found {
		t.Fatalf("列表项内部变更应汇入根 sink: %v", changes)
	}
}

// ---------------- 逐节点增删/移动细粒度指令 ----------------

func countKind(changes []Change, kind ChangeKind) int {
	n := 0
	for _, c := range changes {
		if c.Kind == kind {
			n++
		}
	}
	return n
}

func nodeIDs(nodes []*Node) []string {
	out := make([]string, len(nodes))
	for i, n := range nodes {
		out[i] = nodeID(n)
	}
	return out
}

func sameIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 纯重排应只产生 MoveChild，不产生增删；apply 后顺序与目标一致。
func TestDiffChildrenReorderEmitsMoveOnly(t *testing.T) {
	parent := &Node{ID: "p", Kind: "#frag"}
	a := &Node{ID: "a", Kind: "div"}
	b := &Node{ID: "b", Kind: "div"}
	c := &Node{ID: "c", Kind: "div"}
	old := []*Node{a, b, c}
	next := []*Node{c, a, b} // 循环右移：LIS 保留 a,b，只移动 c
	parent.Children = old

	sink := newChangeSink()
	sink.enable()
	diffChildren(sink, parent, old, next)
	changes := sink.take()

	if countKind(changes, ChangeMoveChild) == 0 {
		t.Fatalf("重排应产生 MoveChild: %v", changes)
	}
	if countKind(changes, ChangeRemoveChild) != 0 || countKind(changes, ChangeInsertBefore) != 0 {
		t.Fatalf("纯重排不应产生增删: %v", changes)
	}
	applyChanges(parent, changes)
	if !sameIDs(nodeIDs(parent.Children), []string{"c", "a", "b"}) {
		t.Fatalf("apply 后顺序不符: %v", nodeIDs(parent.Children))
	}
}

// 增删应产生 InsertBefore / RemoveChild，apply 后与目标 children 一致。
func TestDiffChildrenAddRemove(t *testing.T) {
	parent := &Node{ID: "p", Kind: "#frag"}
	a := &Node{ID: "a", Kind: "div"}
	b := &Node{ID: "b", Kind: "div"}
	c := &Node{ID: "c", Kind: "div"}
	d := &Node{ID: "d", Kind: "div"}
	old := []*Node{a, b, c}
	next := []*Node{a, c, d} // 删 b，末尾加 d
	parent.Children = old

	sink := newChangeSink()
	sink.enable()
	diffChildren(sink, parent, old, next)
	changes := sink.take()

	if countKind(changes, ChangeRemoveChild) != 1 {
		t.Fatalf("应产生 1 条 RemoveChild: %v", changes)
	}
	if countKind(changes, ChangeInsertBefore) != 1 {
		t.Fatalf("应产生 1 条 InsertBefore: %v", changes)
	}
	applyChanges(parent, changes)
	if !sameIDs(nodeIDs(parent.Children), []string{"a", "c", "d"}) {
		t.Fatalf("apply 后顺序不符: %v", nodeIDs(parent.Children))
	}
}

// 同位置内容变化（标量/文本节点）应表现为删+插，而非被同位置 ID 误判为未变。
func TestDiffChildrenTextChangeIsRemoveInsert(t *testing.T) {
	parent := &Node{ID: "p", Kind: "#frag"}
	oldText := &Node{ID: "p/t0", Kind: "#text", Type: NodeText, Text: "old"}
	newText := &Node{ID: "p/t0", Kind: "#text", Type: NodeText, Text: "new"}
	old := []*Node{oldText}
	next := []*Node{newText} // ID 相同但指针不同（值变了）

	sink := newChangeSink()
	sink.enable()
	diffChildren(sink, parent, old, next)
	changes := sink.take()

	if countKind(changes, ChangeRemoveChild) != 1 || countKind(changes, ChangeInsertBefore) != 1 {
		t.Fatalf("文本变化应表现为删+插: %v", changes)
	}
}

type fgUser struct {
	id   int
	name string
}

func fgForTpl() Template {
	return Template{Ops: []Op{
		{Code: OpElem, Arg: "ul"},
		{Code: OpFor, Slot: 0},
		{Code: OpClose},
	}, N: 1}
}

func fgItemTpl() Template {
	return Template{Ops: []Op{
		{Code: OpElem, Arg: "li"},
		{Code: OpDynChild, Slot: 0},
		{Code: OpClose},
	}, N: 1}
}

// For 列表增删/重排应真正发出细粒度指令（而非一条 ReplaceChildren）。
func TestForEmitsFineGrainedChanges(t *testing.T) {
	users := signal.New([]fgUser{{1, "Tom"}, {2, "Jack"}})
	f := NewFor(
		func() []any {
			out := []any{}
			for _, u := range users() {
				out = append(out, u)
			}
			return out
		},
		func(v any) any { return v.(fgUser).id },
		func(v any) *Instance {
			u := v.(fgUser)
			return Render(fgItemTpl(), func() any { return u.name })
		},
	)
	view := Render(fgForTpl(), func() any { return f })
	view.BindReactive(signal.Effect)
	view.EnableChanges()
	view.TakeChanges() // 清掉接线期记录

	// 新增第三项。
	users([]fgUser{{1, "Tom"}, {2, "Jack"}, {3, "Ann"}})
	added := view.TakeChanges()
	if countKind(added, ChangeInsertBefore) == 0 {
		t.Fatalf("新增列表项应产生 InsertBefore: %v", added)
	}

	// 删除中间项。
	users([]fgUser{{1, "Tom"}, {3, "Ann"}})
	removed := view.TakeChanges()
	if countKind(removed, ChangeRemoveChild) == 0 {
		t.Fatalf("删除列表项应产生 RemoveChild: %v", removed)
	}

	// 重排。
	users([]fgUser{{3, "Ann"}, {1, "Tom"}})
	reordered := view.TakeChanges()
	if countKind(reordered, ChangeMoveChild) == 0 {
		t.Fatalf("列表重排应产生 MoveChild: %v", reordered)
	}
}
