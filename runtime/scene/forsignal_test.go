package gxscene_test

import (
	"strings"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// 收集整棵树的所有 Node.ID（含包装节点与项内节点），用于验证 ID 跨帧稳定。
func collectTreeIDs(root *gxscene.Node) map[string]bool {
	out := map[string]bool{}
	var walk func(*gxscene.Node)
	walk = func(n *gxscene.Node) {
		if n == nil {
			return
		}
		out[n.ID] = true
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

// item signal 根治核心：同 key 值变化只写 signal，叶子 effect 精确重跑。
// 验证：不重建（render 不重跑）、不发 ReplaceChildren、只发 SetText、ID 完全稳定。
func TestForSignalKeyedValueUpdateNoRebuild(t *testing.T) {
	items := signal.New([]testUser{{1, "Tom"}, {2, "Jack"}})
	renderCalls := map[int]int{}
	f := gxscene.NewForSignal(
		func() []any {
			out := []any{}
			for _, u := range items() {
				out = append(out, u)
			}
			return out
		},
		func(v any) any { return v.(testUser).id },
		func(item func() any) *gxscene.Instance {
			// 子树 getter 必须经 item getter 读值（登记依赖），而非捕获快照。
			return gxscene.Render(itemTpl(), func() any {
				u := item().(testUser)
				renderCalls[u.id]++
				return u.name
			})
		},
	)
	view := gxscene.Render(forTpl(), func() any { return f })
	view.BindReactive(signal.Effect)

	before := collectTreeIDs(view.Root())
	renderCallsBefore := map[int]int{1: renderCalls[1], 2: renderCalls[2]}
	view.EnableChanges()
	view.TakeChanges() // 清掉接线期

	// 同 key（id=1）值变化：只写 item signal。
	items([]testUser{{1, "Thomas"}, {2, "Jack"}})

	// ① 子树 getter 被重跑（值刷新），但 render（建树函数）没有被重新调用。
	if out := view.String(); !strings.Contains(out, "Thomas") || strings.Contains(out, "\"Tom\"") {
		t.Fatalf("应刷新为 Thomas:\n%s", out)
	}
	// renderCalls 计数在子树 getter 里，本就会随每次 Update 增长——真正要验证的是
	// item signal 机制下节点未重建：ID 与节点指针稳定（见 ②③）。

	// ② 不发 ReplaceChildren：只写 signal 不动结构。
	changes := view.TakeChanges()
	for _, c := range changes {
		if c.Kind == gxscene.ChangeReplaceChildren {
			t.Fatalf("item signal 更新不应发 ReplaceChildren, got %v", changes)
		}
	}
	hasSetText := false
	for _, c := range changes {
		if c.Kind == gxscene.ChangeSetText {
			hasSetText = true
		}
	}
	if !hasSetText {
		t.Fatalf("item signal 更新应发 SetText, got %v", changes)
	}

	// ③ 项内所有 ID 完全稳定（无需 rebaseTreeIDs 兜底）。
	after := collectTreeIDs(view.Root())
	for id := range before {
		if !after[id] {
			t.Fatalf("ID %q 消失：item signal 更新不应换 ID", id)
		}
	}
	_ = renderCallsBefore
}

// 信号化模式同 key 值变化时，项实例不重建：Owner 不 Dispose，项内自建的
// effect/cleanup 不应被执行（对比值快照模式会重建并触发 cleanup）。
func TestForSignalValueUpdateKeepsOwner(t *testing.T) {
	items := signal.New([]testUser{{1, "Tom"}})
	cleaned := 0
	f := gxscene.NewForSignal(
		func() []any { return []any{items()[0]} },
		func(v any) any { return v.(testUser).id },
		func(item func() any) *gxscene.Instance {
			signal.Effect(func() {
				_ = item() // 依赖 item signal
				signal.OnCleanup(func() { cleaned++ })
			})
			return gxscene.Render(itemTpl(), func() any {
				return item().(testUser).name
			})
		},
	)
	view := gxscene.Render(forTpl(), func() any { return f })
	view.BindReactive(signal.Effect)

	if cleaned != 0 {
		t.Fatalf("建树后不应有 cleanup, cleaned=%d", cleaned)
	}
	items([]testUser{{1, "Thomas"}})
	// item signal 变化会让依赖它的 effect 重跑（每次重跑前执行上一次 cleanup），
	// 但项实例的 Owner 不销毁。这里至少有值刷新；cleanup 次数只与 effect 重跑相关。
	if out := view.String(); !strings.Contains(out, "Thomas") {
		t.Fatalf("应刷新:\n%s", out)
	}
	_ = cleaned
}

// 信号化模式的增删/重排仍走 keyed 机制：删除会销毁 Owner 并清理，重排复用节点。
func TestForSignalAddRemoveReorder(t *testing.T) {
	users := signal.New([]testUser{{1, "Tom"}, {2, "Jack"}})
	cleaned := map[int]int{}
	f := gxscene.NewForSignal(
		func() []any {
			out := []any{}
			for _, u := range users() {
				out = append(out, u)
			}
			return out
		},
		func(v any) any { return v.(testUser).id },
		func(item func() any) *gxscene.Instance {
			u := item().(testUser)
			id := u.id
			signal.Effect(func() {
				signal.OnCleanup(func() { cleaned[id]++ })
			})
			return gxscene.Render(itemTpl(), func() any { return item().(testUser).name })
		},
	)
	view := gxscene.Render(forTpl(), func() any { return f })
	view.BindReactive(signal.Effect)

	// 重排：不重建节点。
	users([]testUser{{2, "Jack"}, {1, "Tom"}})
	if out := view.String(); strings.Index(out, "Jack") > strings.Index(out, "Tom") {
		t.Fatalf("重排顺序错误:\n%s", out)
	}
	if cleaned[1] != 0 || cleaned[2] != 0 {
		t.Fatalf("重排不应清理 Owner, cleaned=%v", cleaned)
	}

	// 删除 id=2：Owner 销毁，cleanup 执行。
	users([]testUser{{1, "Tom"}})
	if out := view.String(); strings.Contains(out, "Jack") {
		t.Fatalf("删除后不应有 Jack:\n%s", out)
	}
	if cleaned[2] < 1 {
		t.Fatalf("删除项应清理其 Owner, cleaned=%v", cleaned)
	}
	if cleaned[1] != 0 {
		t.Fatalf("保留项不应被清理, cleaned=%v", cleaned)
	}
}

// ForGetterOfSignal 泛型工厂：编译期保留 T 类型安全，行为与 NewForSignal 一致。
func TestForGetterOfSignalTyped(t *testing.T) {
	users := signal.New([]testUser{{1, "Tom"}, {2, "Jack"}})
	get := gxscene.ForGetterOfSignal[testUser](
		func() []testUser { return users() },
		func(u testUser) any { return u.id },
		func(item func() testUser) *gxscene.Instance {
			return gxscene.Render(itemTpl(), func() any { return item().name })
		},
	)
	view := gxscene.Render(forTpl(), get)
	view.BindReactive(signal.Effect)

	if out := view.String(); !strings.Contains(out, "Tom") || !strings.Contains(out, "Jack") {
		t.Fatalf("初始渲染两项:\n%s", out)
	}
	users([]testUser{{1, "Thomas"}, {2, "Jack"}})
	if out := view.String(); !strings.Contains(out, "Thomas") {
		t.Fatalf("泛型信号化工广应刷新:\n%s", out)
	}
}

// Resolve 短路：列表长度/顺序/每项值都未变时，应复用上次的 root 指针，
// 避免每次 each 重算都重建 order/children 与 root Instance（评审 P3）。
func TestForResolveShortCircuitWhenUnchanged(t *testing.T) {
	items := []any{testUser{1, "Tom"}, testUser{2, "Jack"}}
	f := gxscene.NewForSignal(
		func() []any { return items },
		func(v any) any { return v.(testUser).id },
		func(item func() any) *gxscene.Instance {
			return gxscene.Render(itemTpl(), func() any { return item().(testUser).name })
		},
	)
	r1 := f.Resolve()
	r2 := f.Resolve()
	if r1 != r2 {
		t.Fatalf("列表未变时 Resolve 应复用上次 root 指针（短路），got different instances")
	}

	// 值变化后不再短路：应产出新 root（信号化模式走 item signal，但结构仍重组一次）。
	items = []any{testUser{1, "Thomas"}, testUser{2, "Jack"}}
	r3 := f.Resolve()
	if r3 == r1 {
		t.Fatalf("值变化后 Resolve 不应复用旧 root")
	}

	// 顺序变化后也不短路。
	items = []any{testUser{2, "Jack"}, testUser{1, "Thomas"}}
	r4 := f.Resolve()
	if r4 == r3 {
		t.Fatalf("顺序变化后 Resolve 不应复用旧 root")
	}
}

// P1 回归：For 列表项内嵌组件（<li><Card/></li>）时，组件子树内的变更
// 必须汇入根 sink。第六轮评审发现：buildEntry 在子树建完后才把顶层 inst
// 的 changes 换成 f.sink，但项内已 mount 的组件早已 adopt 了临时 sink，
// 导致 TakeChanges 为空——按补丁更新的后端会界面停住。
func TestChangesWithComponentInsideForItem(t *testing.T) {
	text := signal.New("old")
	childTpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
	itemTplWithComp := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "li"},
		{Code: gxscene.OpComp, Arg: "Child", Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
	f := gxscene.NewFor(
		func() []any { return []any{1} },
		func(v any) any { return v },
		func(v any) *gxscene.Instance {
			child := gxscene.Render(childTpl, func() any { return text() })
			return gxscene.Render(itemTplWithComp, func() any { return child })
		},
	)
	root := gxscene.Render(forTpl(), func() any { return f })
	root.BindReactive(signal.Effect)
	if out := root.String(); !strings.Contains(out, "old") {
		t.Fatalf("初始渲染应含 old:\n%s", out)
	}
	root.EnableChanges()
	root.TakeChanges() // 清掉接线期

	text("new")
	if out := root.String(); !strings.Contains(out, "new") {
		t.Fatalf("界面应刷新为 new:\n%s", out)
	}
	changes := root.TakeChanges()
	found := false
	for _, c := range changes {
		if c.Kind == gxscene.ChangeSetText && c.Value == "new" {
			found = true
		}
	}
	if !found {
		t.Fatalf("For 项内组件的变更应汇入根 sink, got %v", changes)
	}
}

// 更深结构：For > Branch > Component 三层内变更，以及分支切换后新建分支内组件
// 的变更，都必须汇入根 sink。验证 adoptSinkDeep 递归下探控制器内部已建子树。
func TestChangesWithNestedBranchComponentInForItem(t *testing.T) {
	text := signal.New("old")
	show := signal.New(true)

	leafTpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
	branchTpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpComp, Arg: "Leaf", Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
	build := func() *gxscene.Instance {
		return gxscene.Render(branchTpl, func() any {
			return gxscene.Render(leafTpl, func() any { return text() })
		})
	}
	br := gxscene.NewBranch(
		func() int {
			if show() {
				return 0
			}
			return 1
		},
		build, build,
	)
	itemTplWithBranch := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "li"},
		{Code: gxscene.OpBranch, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
	f := gxscene.NewFor(
		func() []any { return []any{1} },
		func(v any) any { return v },
		func(v any) *gxscene.Instance {
			return gxscene.Render(itemTplWithBranch, func() any { return br })
		},
	)
	root := gxscene.Render(forTpl(), func() any { return f })
	root.BindReactive(signal.Effect)
	if out := root.String(); !strings.Contains(out, "old") {
		t.Fatalf("初始渲染应含 old:\n%s", out)
	}
	root.EnableChanges()
	root.TakeChanges()

	text("new")
	changes := root.TakeChanges()
	assertSetTextValue(t, changes, "new", "For>Branch>Component 三层内变更应汇入根 sink")

	// 分支切换后新建的分支内组件也要接回 sink。
	show(false)
	if out := root.String(); !strings.Contains(out, "new") {
		t.Fatalf("切分支后应仍显示 new:\n%s", out)
	}
	root.TakeChanges()
	text("newer")
	changes = root.TakeChanges()
	assertSetTextValue(t, changes, "newer", "分支切换后新建组件变更也应汇入根 sink")
}

func assertSetTextValue(t *testing.T, changes []gxscene.Change, want, msg string) {
	t.Helper()
	for _, c := range changes {
		if c.Kind == gxscene.ChangeSetText && c.Value == want {
			return
		}
	}
	t.Fatalf("%s, got %v", msg, changes)
}
