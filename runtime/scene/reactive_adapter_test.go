package gxscene_test

import (
	"sync/atomic"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// T1.4 验收：替换 ReactiveAdapter 后，For/Branch 仍跑通，且注入的实现确实被调用。
// 这里用一个「委托给默认实现 + 计数」的适配器，证明 scene 不再硬编码 signal 包。
func TestReplaceReactiveAdapterRunsScene(t *testing.T) {
	var rootCount, signalCount atomic.Int64
	def := gxscene.DefaultReactiveAdapter
	custom := gxscene.ReactiveAdapter{
		Effect: def.Effect, // effect 委托默认（否则大量接线语义要重写）
		Root: func(f func(o gxscene.Owner)) gxscene.Owner {
			rootCount.Add(1)
			return def.Root(f)
		},
		NewSignal: func(v any) gxscene.Signal {
			signalCount.Add(1)
			return def.NewSignal(v)
		},
	}
	old := gxscene.SetReactiveAdapter(custom)
	defer gxscene.SetReactiveAdapter(old)

	// signal 化 For（走 NewSignal + Root）+ 普通 Branch（走 Root）。
	items := signal.New([]int{1, 2})
	f := gxscene.NewForSignal(
		func() []any {
			out := []any{}
			for _, v := range items() {
				out = append(out, v)
			}
			return out
		},
		func(v any) any { return v },
		func(getItem func() any) *gxscene.Instance {
			return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				{Code: gxscene.OpElem, Arg: "li"},
				{Code: gxscene.OpDynChild, Slot: 0},
				{Code: gxscene.OpClose},
			}, N: 1}, func() any { return getItem() })
		},
	)
	view := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "ul"}, {Code: gxscene.OpFor, Slot: 0}, {Code: gxscene.OpClose},
	}, N: 1}, func() any { return f })
	view.BindReactive(signal.Effect)

	if rootCount.Load() == 0 {
		t.Error("For 应通过适配器创建 Owner（rootCount>0）")
	}
	if signalCount.Load() == 0 {
		t.Error("signal 化 For 应通过适配器创建 item signal（signalCount>0）")
	}
	items([]int{1, 2, 3})
	if out := view.String(); !containsSub(out, "3") {
		t.Errorf("替换适配器后 For 仍应更新到新值 3: %s", out)
	}
}

// 确认 scene 的默认适配器仍等价于直接用 signal 包（行为不回归）。
func TestDefaultReactiveAdapterMatchesSignal(t *testing.T) {
	a := gxscene.DefaultReactiveAdapter
	s := a.NewSignal(1)
	if got := s(); got != 1 {
		t.Fatalf("默认 NewSignal 初值应为 1, got %v", got)
	}
	s(5)
	if got := s(); got != 5 {
		t.Fatalf("默认 NewSignal 写后应读到 5, got %v", got)
	}
	runs := 0
	h := a.Effect(func() { runs++; _ = s() })
	base := runs
	s(6)
	if runs != base+1 {
		t.Fatalf("默认 Effect 应随 signal 重跑, base=%d got=%d", base, runs)
	}
	h.Dispose()
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
