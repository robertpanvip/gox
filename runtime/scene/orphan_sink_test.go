package gxscene_test

import (
	"fmt"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// TestBindStructureAdoptsNestedSink 回归 playground 虚拟滚动实测缺陷：
// 根 --OpComp--> 内层组件 --OpDynChild--> 含 OpFor 的列表实例。首帧建树时
// 各层 Render 内部互相 adopt 的是创建期临时 sink，上层换共享 sink 只 adopt
// 一层，嵌套实例（含 For 控制器）的 changes 仍指向孤立 sink——BindStructural
// 之后列表扩展产生的 InsertBefore/RemoveChild 全部写进无人消费的缓冲，
// 增量后端永远收不到（现象：虚拟滚动窗口在 scene 层扩展了、DOM 不动）。
//
// 修复后 bindStructure 入口把访问到的实例统一换到根 sink，For/Branch 的
// adoptBind 把 ctx.sink 下推到控制器。本测试锁定该行为：列表扩展后根实例的
// Changes() 必须出现结构变更指令。
func TestBindStructureAdoptsNestedSink(t *testing.T) {
	items := signal.New([]any{0, 1, 2})

	// 列表实例：div > OpFor，每项渲染一个 div 文本。
	list := func() *gxscene.Instance {
		forCtl := gxscene.NewFor(
			func() []any {
				src := items()
				out := make([]any, len(src))
				copy(out, src)
				return out
			},
			func(v any) any { return v },
			func(v any) *gxscene.Instance {
				return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpChildText, Arg: fmt.Sprint(v)},
					{Code: gxscene.OpClose},
				}, N: 0})
			},
		)
		return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "ul"},
			{Code: gxscene.OpFor, Slot: 0},
			{Code: gxscene.OpClose},
		}, N: 1}, func() any { return forCtl })
	}

	// 内层组件：div > OpDynChild 承载列表实例（模拟 Editor 嵌 VirtualList）。
	inner := func() *gxscene.Instance {
		return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "section"},
			{Code: gxscene.OpDynChild, Slot: 0},
			{Code: gxscene.OpClose},
		}, N: 1}, func() any { return list() })
	}

	// 根实例：div > OpComp 承载内层组件（模拟 playground 树挂 Editor()）。
	root := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "main"},
		{Code: gxscene.OpComp, Arg: "Inner", Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return inner() })

	// app 场景接线顺序：EnableChanges → BindStructural + BindLeafEffects。
	root.EnableChanges()
	root.BindStructural(signal.Effect, func() {})
	root.BindLeafEffects(signal.Effect)
	_ = root.Changes() // 清掉接线期可能产生的初始变更

	// 列表扩展 3 → 8：结构变更必须汇入根 sink。
	items([]any{0, 1, 2, 3, 4, 5, 6, 7})
	changes := root.Changes()
	t.Logf("changes=%d", len(changes))
	// 场景树是否扩展（区分「effect 没重跑」vs「变更进孤立 sink」）
	nText := 0
	var walk func(*gxscene.Node)
	walk = func(n *gxscene.Node) {
		if n.Type == gxscene.NodeText {
			nText++
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root.Root())
	t.Logf("treeTextNodes=%d", nText)
	structural := 0
	for _, c := range changes {
		switch c.Kind {
		case gxscene.ChangeInsertBefore, gxscene.ChangeRemoveChild, gxscene.ChangeMoveChild:
			structural++
		}
	}
	if structural == 0 {
		t.Fatalf("列表扩展后根实例 Changes() 无结构变更（%d 条全部是叶子类）——结构变更写进了孤立 sink", len(changes))
	}

	// 收缩 8 → 1：同理必须出现 RemoveChild。
	items([]any{0})
	changes = root.Changes()
	removed := false
	for _, c := range changes {
		if c.Kind == gxscene.ChangeRemoveChild {
			removed = true
		}
	}
	if !removed {
		t.Fatalf("列表收缩后根实例 Changes() 无 RemoveChild——结构变更写进了孤立 sink")
	}
}
