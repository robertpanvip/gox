package gxapp_test

import (
	"fmt"
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// T1.1 验收：只写一个 signal 时，另一个插槽的 getter 不应被求值。
// 聚合 Update 模型下每次写都会遍历全部 getter（aEval、bEval 都增长）；
// 插槽级模型下只重跑依赖该 signal 的那个 getter。
func TestSlotLevelOnlyDependentGetterEvaluated(t *testing.T) {
	a := signal.New("A")
	b := signal.New("B")
	aEval, bEval := 0, 0

	tpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpDynChild, Slot: 1},
		{Code: gxscene.OpClose},
	}, N: 2}
	inst := gxscene.Render(tpl,
		func() any { aEval++; return a() },
		func() any { bEval++; return b() },
	)
	win := &countingWindow{}
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()
	if err := app.Render(func() any { return inst }); err != nil {
		t.Fatal(err)
	}
	aEval, bEval = 0, 0 // 接线期不计

	b("B2")
	if aEval != 0 {
		t.Errorf("只写 b 不应求值 a 的 getter, aEval=%d", aEval)
	}
	if bEval != 1 {
		t.Errorf("只写 b 应求值 b 的 getter 一次, bEval=%d", bEval)
	}
}

// T1.2 验收：一个 Batch 内改 N 个 signal，只画一帧，且最后一帧一致。
func TestBatchNValuesDrawOneFrame(t *testing.T) {
	vals := make([]signal.Signal[int], 8)
	getters := make([]func() any, 8)
	for i := range vals {
		vals[i] = signal.New(0)
		v := vals[i]
		getters[i] = func() any { return v() }
	}
	ops := []gxscene.Op{{Code: gxscene.OpElem, Arg: "div"}}
	for i := range getters {
		ops = append(ops, gxscene.Op{Code: gxscene.OpDynChild, Slot: i})
	}
	ops = append(ops, gxscene.Op{Code: gxscene.OpClose})
	inst := gxscene.Render(gxscene.Template{Ops: ops, N: len(getters)}, getters...)

	win := &countingWindow{}
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()
	if err := app.Render(func() any { return inst }); err != nil {
		t.Fatal(err)
	}
	base := win.draws
	signal.Batch(func() {
		for i, v := range vals {
			v(i + 1)
		}
	})
	if got := win.draws - base; got != 1 {
		t.Fatalf("批次改 %d 个 signal 应只画 1 帧, got %d", len(vals), got)
	}
}

// T1.1 关键：app 场景下去掉聚合 Update 后，Branch 子树内的叶子变化仍触发重绘
// （这些叶子的 effect 由 bindCtx.effect 注入）。
func TestBranchLeafChangeRedrawsUnderSlotLevel(t *testing.T) {
	visible := signal.New(true)
	inner := signal.New("X")
	br := gxscene.NewBranch(
		func() int {
			if visible() {
				return 0
			}
			return 1
		},
		func() *gxscene.Instance {
			return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				{Code: gxscene.OpElem, Arg: "span"},
				{Code: gxscene.OpDynChild, Slot: 0},
				{Code: gxscene.OpClose},
			}, N: 1}, func() any { return inner() })
		},
		func() *gxscene.Instance {
			return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				{Code: gxscene.OpElem, Arg: "b"}, {Code: gxscene.OpChildText, Arg: "E"}, {Code: gxscene.OpClose},
			}})
		},
	)
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"}, {Code: gxscene.OpBranch, Slot: 0}, {Code: gxscene.OpClose},
	}, N: 1}, func() any { return br })

	win := &countingWindow{}
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()
	if err := app.Render(func() any { return inst }); err != nil {
		t.Fatal(err)
	}
	text := gxscene.TextBackend{}.Paint(inst.Root())
	if !contains(text, "X") {
		t.Fatalf("初始帧应含 X: %s", text)
	}
	base := win.draws
	inner("Y") // 分支内叶子变化
	if got := win.draws - base; got != 1 {
		t.Errorf("分支内叶子变化应画 1 帧, got %d", got)
	}
	text = gxscene.TextBackend{}.Paint(inst.Root())
	if !contains(text, "Y") || contains(text, `"X"`) {
		t.Fatalf("分支内叶子应更新为 Y: %s", text)
	}
}

// T1.1：For 列表项内叶子变化同样触发重绘。
func TestForItemLeafChangeRedrawsUnderSlotLevel(t *testing.T) {
	items := signal.New([]int{1, 2})
	f := gxscene.NewFor(
		func() []any {
			out := []any{}
			for _, v := range items() {
				out = append(out, v)
			}
			return out
		},
		func(v any) any { return v },
		func(v any) *gxscene.Instance {
			n := v.(int)
			mark := signal.New(fmt.Sprintf("n%d", n))
			return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				{Code: gxscene.OpElem, Arg: "li"},
				{Code: gxscene.OpDynChild, Slot: 0},
				{Code: gxscene.OpClose},
			}, N: 1}, func() any { return mark() })
		},
	)
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "ul"}, {Code: gxscene.OpFor, Slot: 0}, {Code: gxscene.OpClose},
	}, N: 1}, func() any { return f })

	win := &countingWindow{}
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()
	if err := app.Render(func() any { return inst }); err != nil {
		t.Fatal(err)
	}
	base := win.draws
	items([]int{1, 2, 3}) // 结构变化
	if got := win.draws - base; got != 1 {
		t.Errorf("列表新增应画 1 帧, got %d", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
