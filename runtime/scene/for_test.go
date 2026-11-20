package gxscene_test

import (
	"strings"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

type testUser struct {
	id   int
	name string
}

func forTpl() gxscene.Template {
	return gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "ul"},
		{Code: gxscene.OpFor, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
}

func itemTpl() gxscene.Template {
	return gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "li"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
}

func TestForRendersItems(t *testing.T) {
	users := signal.New([]testUser{{1, "Tom"}, {2, "Jack"}})
	f := gxscene.NewFor(
		func() []any {
			out := []any{}
			for _, u := range users() {
				out = append(out, u)
			}
			return out
		},
		func(v any) any { return v.(testUser).id },
		func(v any) *gxscene.Instance {
			u := v.(testUser)
			return gxscene.Render(itemTpl(), func() any { return u.name })
		},
	)
	view := gxscene.Render(forTpl(), func() any { return f })
	view.BindReactive(signal.Effect)

	out := view.String()
	if !strings.Contains(out, "Tom") || !strings.Contains(out, "Jack") {
		t.Fatalf("初始应渲染两项:\n%s", out)
	}
}

func TestForKeyedReuseOnReorder(t *testing.T) {
	users := signal.New([]testUser{{1, "Tom"}, {2, "Jack"}})
	renderCalls := map[int]int{}
	f := gxscene.NewFor(
		func() []any {
			out := []any{}
			for _, u := range users() {
				out = append(out, u)
			}
			return out
		},
		func(v any) any { return v.(testUser).id },
		func(v any) *gxscene.Instance {
			u := v.(testUser)
			renderCalls[u.id]++
			return gxscene.Render(itemTpl(), func() any { return u.name })
		},
	)
	view := gxscene.Render(forTpl(), func() any { return f })
	view.BindReactive(signal.Effect)

	// 重排：不应重新调用 render（节点按 key 复用）。
	users([]testUser{{2, "Jack"}, {1, "Tom"}})
	if renderCalls[1] != 1 || renderCalls[2] != 1 {
		t.Fatalf("重排不应重建节点, renderCalls=%v", renderCalls)
	}
	out := view.String()
	// Jack 应出现在 Tom 之前。
	if strings.Index(out, "Jack") > strings.Index(out, "Tom") {
		t.Fatalf("重排后顺序错误:\n%s", out)
	}
}

func TestForAddRemoveWithOwnerCleanup(t *testing.T) {
	users := signal.New([]testUser{{1, "Tom"}, {2, "Jack"}})
	cleaned := map[int]int{}
	f := gxscene.NewFor(
		func() []any {
			out := []any{}
			for _, u := range users() {
				out = append(out, u)
			}
			return out
		},
		func(v any) any { return v.(testUser).id },
		func(v any) *gxscene.Instance {
			u := v.(testUser)
			name := signal.New(u.name)
			signal.Effect(func() {
				_ = name()
				id := u.id
				signal.OnCleanup(func() { cleaned[id]++ })
			})
			return gxscene.Render(itemTpl(), func() any { return name() })
		},
	)
	view := gxscene.Render(forTpl(), func() any { return f })
	view.BindReactive(signal.Effect)

	// 删除 id=2：其 Owner 应销毁，cleanup 执行。
	users([]testUser{{1, "Tom"}})
	out := view.String()
	if strings.Contains(out, "Jack") {
		t.Fatalf("删除后不应有 Jack:\n%s", out)
	}
	if cleaned[2] < 1 {
		t.Fatalf("删除元素的 Owner 应清理其 Effect, cleaned=%v", cleaned)
	}
	if cleaned[1] != 0 {
		t.Fatalf("保留元素的 Effect 不应被清理, cleaned=%v", cleaned)
	}
}

func TestForInnerSignalUpdatesOnlyItem(t *testing.T) {
	// 每个元素的 name 是 signal，改一个元素只更新对应 li，不重建列表。
	names := map[int]signal.Signal[string]{
		1: signal.New("Tom"),
		2: signal.New("Jack"),
	}
	renderCalls := 0
	f := gxscene.NewFor(
		func() []any { return []any{testUser{1, ""}, testUser{2, ""}} },
		func(v any) any { return v.(testUser).id },
		func(v any) *gxscene.Instance {
			u := v.(testUser)
			renderCalls++
			return gxscene.Render(itemTpl(), func() any { return names[u.id]() })
		},
	)
	view := gxscene.Render(forTpl(), func() any { return f })
	view.BindReactive(signal.Effect)

	names[1]("Thomas")
	out := view.String()
	if !strings.Contains(out, "Thomas") || !strings.Contains(out, "Jack") {
		t.Fatalf("元素内 signal 应更新对应项:\n%s", out)
	}
	if renderCalls != 2 {
		t.Fatalf("元素内 signal 不应重建列表, renderCalls=%d", renderCalls)
	}
}

func TestForDisposeCleansAll(t *testing.T) {
	cleaned := 0
	f := gxscene.NewFor(
		func() []any { return []any{testUser{1, "a"}, testUser{2, "b"}} },
		func(v any) any { return v.(testUser).id },
		func(v any) *gxscene.Instance {
			signal.Effect(func() {
				signal.OnCleanup(func() { cleaned++ })
			})
			return gxscene.Render(itemTpl(), func() any { return "x" })
		},
	)
	view := gxscene.Render(forTpl(), func() any { return f })
	view.Dispose()
	if cleaned != 2 {
		t.Fatalf("Dispose 应清理所有元素 Owner, cleaned=%d", cleaned)
	}
}
