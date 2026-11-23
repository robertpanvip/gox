package gxscene_test

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// T4.5：Perf() 暴露 getter 求值次数、写入数、接线插槽数，可观测插槽级更新的精确性。
func TestPerfStatsObservable(t *testing.T) {
	a := signal.New("A")
	b := signal.New("B")
	tpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpDynChild, Slot: 1},
		{Code: gxscene.OpClose},
	}, N: 2}
	view := gxscene.Render(tpl, func() any { return a() }, func() any { return b() })
	view.BindReactive(signal.Effect)

	base := view.Perf()
	if base.WiredSlots != 2 || base.Slots != 2 {
		t.Fatalf("应接线 2 个插槽, Perf=%+v", base)
	}

	// 只写 b：b 的 getter 多求值一次，a 不变。
	b("B2")
	after := view.Perf()
	if after.GetterEvals <= base.GetterEvals {
		t.Errorf("getter 求值计数应增长, base=%d after=%d", base.GetterEvals, after.GetterEvals)
	}
	if after.Writes != base.Writes+1 {
		t.Errorf("应只写 1 个节点, base=%d after=%d", base.Writes, after.Writes)
	}
}
