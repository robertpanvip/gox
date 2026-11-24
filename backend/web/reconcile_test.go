package gxweb

import (
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
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
