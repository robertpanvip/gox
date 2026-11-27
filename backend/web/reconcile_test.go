package gxweb

import (
	"testing"

	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
	"github.com/robertpanvip/gox-core/runtime/signal"
)

// TestWindowStructuralChanges 直接构造 INSERT_BEFORE / REMOVE_CHILD 变更，
// 验证后端的结构补丁路径（真实 gxapp 下这些变更由 Branch/For 响应式产生）。
func TestWindowStructuralChanges(t *testing.T) {
	d := newMemDriver()
	w := NewWithDriver(d)
	_ = w.Open(gxapp.DefaultConfig())

	root := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpClose},
	}, N: 0})
	if err := w.Draw(root.Root()); err != nil {
		t.Fatal(err)
	}

	countKind := func(kind string) int {
		n := 0
		for _, m := range w.elems {
			if m.kind == kind {
				n++
			}
		}
		return n
	}
	if countKind("span") != 0 {
		t.Fatalf("首帧 span=%d want 0", countKind("span"))
	}

	divID := root.Root().Children[0].ID
	// 构造一个 span 子树插入 div
	span := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpChildText, Arg: "x"},
		{Code: gxscene.OpClose},
	}, N: 0})
	spanNode := span.Root().Children[0]

	changes := []gxscene.Change{{
		Kind:    gxscene.ChangeInsertBefore,
		NodeID:  divID.String(),
		ChildID: spanNode.ID.String(),
		Value:   spanNode,
	}}
	if err := w.Present(&gxapp.Frame{Root: root.Root(), Changes: changes, Version: 2}); err != nil {
		t.Fatal(err)
	}
	if countKind("span") != 1 {
		t.Fatalf("插入后 span=%d want 1", countKind("span"))
	}
	if _, ok := w.nodes[spanNode.ID]; !ok {
		t.Fatal("插入的 span 未登记镜像")
	}

	// 移除 span
	changes = []gxscene.Change{{
		Kind:    gxscene.ChangeRemoveChild,
		NodeID:  divID.String(),
		ChildID: spanNode.ID.String(),
	}}
	if err := w.Present(&gxapp.Frame{Root: root.Root(), Changes: changes, Version: 3}); err != nil {
		t.Fatal(err)
	}
	if countKind("span") != 0 {
		t.Fatalf("移除后 span=%d want 0", countKind("span"))
	}
}

func TestPresentFullSyncRecoversAfterMissedChanges(t *testing.T) {
	d := newMemDriver()
	w := NewWithDriver(d)
	_ = w.Open(gxapp.DefaultConfig())

	txt := signal.New("v1")
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return txt() })
	inst.EnableChanges()

	present := func(ver uint64, full bool) {
		frame := &gxapp.Frame{Root: inst.Root(), Changes: inst.Changes(), Version: ver, FullSync: full}
		if err := w.Present(frame); err != nil {
			t.Fatal(err)
		}
		inst.TakeChanges()
	}
	findText := func() string {
		for _, e := range w.elems {
			if e.kind == "#text" {
				return e.text
			}
		}
		return ""
	}

	present(1, false)
	if findText() != "v1" {
		t.Fatalf("首帧（空 changes）未建树: %q", findText())
	}

	// 模拟「后端丢帧」：改了 signal 但不 Present，直接 full sync 重建
	txt("v2")
	inst.Update()
	_ = inst.TakeChanges() // 丢弃增量
	present(2, true)
	if findText() != "v2" {
		t.Fatalf("FullSync 未从 Root 恢复: %q", findText())
	}
}

// 复现：For 增删行时父节点是 #frag 透明包装（无独立 DOM 元素）。
// 新行必须插到该透明父挂靠的真实 DOM 容器内，而不是页面根。
func TestInsertBeforeIntoTransparentFragParent(t *testing.T) {
	d := newMemDriver()
	w := NewWithDriver(d)
	_ = w.Open(gxapp.DefaultConfig())

	// 首帧：<div id="editor"> 内含一个透明 frag 容器，frag 里一个行节点。
	// 用 Render 构造更真实的结构：外层 div > For frag > 行
	root := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpBranch, Slot: 0}, // #frag 透明父
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return nil })
	editorNode := root.Root().Children[0]
	fragNode := editorNode.Children[0]
	if fragNode.Kind != "#frag" {
		t.Fatalf("slot 节点应为 #frag, got %q", fragNode.Kind)
	}
	if err := w.Draw(root.Root()); err != nil {
		t.Fatal(err)
	}
	editorElem := w.nodes[editorNode.ID].elem

	// 往透明 frag 父下插入一个新行子树
	newRow := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpChildText, Arg: "new"},
		{Code: gxscene.OpClose},
	}, N: 0}).Root().Children[0]
	changes := []gxscene.Change{{
		Kind:    gxscene.ChangeInsertBefore,
		NodeID:  fragNode.ID.String(), // 父是透明 frag
		ChildID: newRow.ID.String(),
		Value:   newRow,
	}}
	if err := w.Present(&gxapp.Frame{Root: root.Root(), Changes: changes, Version: 2}); err != nil {
		t.Fatal(err)
	}
	// 新行必须挂在 editor 容器内（editorElem 的后代），而非页面根 w.root。
	isDesc := func(child ElemID) bool {
		for e := child; e != 0; {
			if e == editorElem { return true }
			el := d.elements[e]
			if el == nil { return false }
			e = el.parent
		}
		return false
	}
	if !isDesc(w.nodes[newRow.ID].elem) {
		t.Fatalf("新行未插入透明父挂靠的容器: editor=%v newRow=%v", editorElem, w.nodes[newRow.ID].elem)
	}
}

// For 从右往左发 InsertBefore（succ 为右侧已就位项），子树插到锚点前时
// 必须保持顺序，不能 append 到末尾导致倒序（新行 8,7,6）。
func TestInsertBeforeSubtreeOrder(t *testing.T) {
	d := newMemDriver()
	w := NewWithDriver(d)
	_ = w.Open(gxapp.DefaultConfig())

	root := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpBranch, Slot: 0}, // #frag 透明父，初始为空
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return nil })
	editorNode := root.Root().Children[0]
	fragNode := editorNode.Children[0]
	if err := w.Draw(root.Root()); err != nil {
		t.Fatal(err)
	}
	editorElem := w.nodes[editorNode.ID].elem

	mkRow := func(label string) (gxscene.NodeID, *gxscene.Node) {
		n := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "div"},
			{Code: gxscene.OpChildText, Arg: label},
			{Code: gxscene.OpClose},
		}, N: 0}).Root().Children[0]
		return n.ID, n
	}
	// 从右往左插 C（append）、B（before C）、A（before B）
	idC, nodeC := mkRow("C")
	idB, nodeB := mkRow("B")
	idA, nodeA := mkRow("A")
	ver := uint64(2)
	for _, ins := range []struct {
		id   gxscene.NodeID
		node *gxscene.Node
		before string
	}{
		{idC, nodeC, ""},
		{idB, nodeB, idC.String()},
		{idA, nodeA, idB.String()},
	} {
		changes := []gxscene.Change{{
			Kind: gxscene.ChangeInsertBefore, NodeID: fragNode.ID.String(),
			ChildID: ins.id.String(), BeforeID: ins.before, Value: ins.node,
		}}
		if err := w.Present(&gxapp.Frame{Root: root.Root(), Changes: changes, Version: ver}); err != nil {
			t.Fatal(err)
		}
		ver++
	}
	// 读 editor 容器的直接子节点文本，应为 A,B,C 顺序。
	got := []string{}
	for _, e := range d.elements[editorElem].children {
		// 行 div 的文本在其 #text 子节点里。
		txt := d.elements[e].text
		if txt == "" && len(d.elements[e].children) > 0 {
			txt = d.elements[d.elements[e].children[0]].text
		}
		got = append(got, txt)
	}
	if len(got) != 3 || got[0] != "A" || got[1] != "B" || got[2] != "C" {
		t.Fatalf("插入顺序错误: %v want [A B C]", got)
	}
}
