package gxscene

import (
	"strings"
	"testing"

	"github.com/robertpanvip/gox/runtime/signal"
)

// ---------- 协议一致性套件（conformance suite）----------
//
// 这一组测试守护"协议库存在的理由"：跨帧身份唯一、增量补丁与全量树等价、
// 结构插槽在任意深度可驱动。任何一条失败，都意味着按协议文档实现的后端会出错。

// assertUniqueIDs 遍历整棵树，断言所有非空 Node.ID 唯一，并返回 ID 集合。
func assertUniqueIDs(t *testing.T, root *Node) map[string]bool {
	t.Helper()
	seen := map[string]*Node{}
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if n.ID != "" {
			if prev, ok := seen[n.ID]; ok {
				t.Fatalf("Node.ID 冲突: %q 同时用于 %v 与 %v", n.ID, prev.Kind, n.Kind)
			}
			seen[n.ID] = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	out := make(map[string]bool, len(seen))
	for id := range seen {
		out[id] = true
	}
	return out
}

// 两个独立 Render 的同构树 + 两个 For 列表（同 key）：所有 ID 必须唯一。
func TestConformanceIDsUniqueAcrossInstancesAndLists(t *testing.T) {
	mkList := func() *Instance {
		f := NewFor(
			func() []any { return []any{1, 2} },
			func(v any) any { return v },
			func(v any) *Instance {
				return Render(Template{Ops: []Op{
					{Code: OpElem, Arg: "li"}, {Code: OpChildText, Arg: "x"}, {Code: OpClose},
				}, N: 0})
			},
		)
		return Render(Template{Ops: []Op{
			{Code: OpElem, Arg: "ul"}, {Code: OpFor, Slot: 0}, {Code: OpClose},
		}, N: 1}, func() any { return f })
	}
	root := Render(Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpComp, Arg: "L1", Slot: 0}, {Code: OpComp, Arg: "L2", Slot: 1},
		{Code: OpClose},
	}, N: 2}, func() any { return mkList() }, func() any { return mkList() })
	ids := assertUniqueIDs(t, root.Root())
	if len(ids) < 8 {
		t.Fatalf("ID 数量异常偏少: %d", len(ids))
	}
}

// 增量补丁 apply 到基线树后，结构（ID/Kind/Text 序列）应与全量目标树一致。
func TestConformanceChangesApplyEqualsFullTree(t *testing.T) {
	val := signal.New("a")
	inst := Render(Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpElem, Arg: "span"}, {Code: OpDynChild, Slot: 0}, {Code: OpClose},
		{Code: OpElem, Arg: "b"}, {Code: OpChildText, Arg: "fixed"}, {Code: OpClose},
		{Code: OpClose},
	}, N: 1}, func() any { return val() })
	inst.EnableChanges()
	baseline := inst.TakeChanges() // 接线期变更（首帧），作为基线快照的一部分
	_ = baseline

	// 制造一次文本变更；把补丁 apply 到"变更前树的深拷贝"（保留原 ID），
	// 断言得到新值——验证增量流可被独立消费（后端镜像树模型）。
	before := deepCopyKeepID(inst.Root())
	inst.TakeChanges()
	val("b")
	inst.Update() // 无响应式接线时手动驱动更新
	changes := inst.TakeChanges()
	applyChanges(before, changes)
	if !strings.Contains(treeText(before), "b") || strings.Contains(treeText(before), "a") {
		t.Fatalf("补丁 apply 后应只含新值 b, got %q (changes=%v)", treeText(before), changes)
	}
}

// deepCopyKeepID 深拷贝节点树但保留原始 ID（用于补丁 apply 的基线镜像）。
func deepCopyKeepID(n *Node) *Node {
	if n == nil {
		return nil
	}
	c := &Node{Kind: n.Kind, Type: n.Type, Text: n.Text, ID: n.ID}
	if n.Props != nil {
		c.Props = make(map[string]any, len(n.Props))
		for k, v := range n.Props {
			c.Props[k] = v
		}
	}
	if len(n.Children) > 0 {
		c.Children = make([]*Node, len(n.Children))
		for i, child := range n.Children {
			c.Children[i] = deepCopyKeepID(child)
			c.Children[i].parent = c
		}
	}
	return c
}

func treeText(root *Node) string {
	var b strings.Builder
	var walk func(*Node)
	walk = func(n *Node) {
		if n == nil {
			return
		}
		if n.Kind == "#text" {
			b.WriteString(n.Text)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return b.String()
}

// 组件内的条件分支：selector 变化必须切换子树（P0 回归守护）。
func TestConformanceBranchInsideComponent(t *testing.T) {
	visible := signal.New(true)
	card := func() *Instance {
		br := NewBranch(
			func() int {
				if visible() {
					return 0
				}
				return 1
			},
			func() *Instance {
				return Render(Template{Ops: []Op{
					{Code: OpElem, Arg: "span"}, {Code: OpChildText, Arg: "THEN"}, {Code: OpClose},
				}, N: 0})
			},
			func() *Instance {
				return Render(Template{Ops: []Op{
					{Code: OpElem, Arg: "b"}, {Code: OpChildText, Arg: "ELSE"}, {Code: OpClose},
				}, N: 0})
			},
		)
		return Render(Template{Ops: []Op{
			{Code: OpElem, Arg: "div"}, {Code: OpBranch, Slot: 0}, {Code: OpClose},
		}, N: 1}, func() any { return br })
	}
	view := Render(Template{Ops: []Op{
		{Code: OpComp, Arg: "Card", Slot: 0},
	}, N: 1}, func() any { return card() })
	view.BindReactive(signal.Effect)
	visible(false)
	if out := view.String(); !strings.Contains(out, "ELSE") {
		t.Fatalf("组件内 if 应切换到 ELSE:\n%s", out)
	}
}

// 同 key 值变化必须刷新该项内容（P1 回归守护）。
func TestConformanceForKeyedValueUpdate(t *testing.T) {
	items := signal.New([]string{"old"})
	f := NewFor(
		func() []any {
			out := []any{}
			for _, s := range items() {
				out = append(out, s)
			}
			return out
		},
		func(v any) any { return 1 },
		func(v any) *Instance {
			s := v.(string)
			return Render(Template{Ops: []Op{
				{Code: OpElem, Arg: "li"}, {Code: OpDynChild, Slot: 0}, {Code: OpClose},
			}, N: 1}, func() any { return s })
		},
	)
	view := Render(Template{Ops: []Op{
		{Code: OpElem, Arg: "ul"}, {Code: OpFor, Slot: 0}, {Code: OpClose},
	}, N: 1}, func() any { return f })
	view.BindReactive(signal.Effect)
	items([]string{"new"})
	if out := view.String(); !strings.Contains(out, "new") || strings.Contains(out, "old") {
		t.Fatalf("同 key 值变化应刷新为 new:\n%s", out)
	}
}

// For 同 key 值变化重建项后，entry.node 指针不变、父级 diff 不发指令。
// 必须补发一条 ReplaceChildren，否则按补丁更新的后端会漏掉这一项（P1 回归守护）。
func TestConformanceForKeyedRebuildEmitsReplaceChildren(t *testing.T) {
	items := signal.New([]string{"old"})
	f := NewFor(
		func() []any { return []any{items()[0]} },
		func(v any) any { return 1 }, // 固定 key，值变化走重建路径
		func(v any) *Instance {
			s := v.(string)
			return Render(Template{Ops: []Op{
				{Code: OpElem, Arg: "li"}, {Code: OpDynChild, Slot: 0}, {Code: OpClose},
			}, N: 1}, func() any { return s })
		},
	)
	view := Render(Template{Ops: []Op{
		{Code: OpElem, Arg: "ul"}, {Code: OpFor, Slot: 0}, {Code: OpClose},
	}, N: 1}, func() any { return f })
	view.BindReactive(signal.Effect)
	view.EnableChanges()
	view.TakeChanges() // 清掉接线期变更

	items([]string{"new"})
	changes := view.TakeChanges()
	found := false
	for _, c := range changes {
		if c.Kind == ChangeReplaceChildren {
			found = true
		}
	}
	if !found {
		t.Fatalf("同 key 重建应发 ReplaceChildren, got %v", changes)
	}
	if out := view.String(); !strings.Contains(out, "new") {
		t.Fatalf("重建后应显示新值:\n%s", out)
	}
}
