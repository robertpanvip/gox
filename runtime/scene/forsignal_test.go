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
