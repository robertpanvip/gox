package gxscene_test

import (
	"strings"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// Issue #1 回归：动态 children 插槽（OpDynChild）承载有状态组件子树时，
// 父级无关 signal 更新不应重建子组件实例（identity / 局部状态保稳），
// 只应更新子树内变化的叶子。
//
// 模拟编译器产物：
//
//	<Card><MyInput value={name}/></Card>
//	  → Card 模板 <div>{kids()}</div>，kids 是 children getter
//	  → children getter 每次求值都 gxscene.Render(#frag{slotComp: MyInput})
//	  → MyInput 内部持有局部 token（每次构造 +1）与读 name() 的叶子插槽
func TestDynChildrenCompositeInstanceStable(t *testing.T) {
	name := signal.New("a")
	token := 0

	// 子组件 <input id={token} value={name()}/>：token 是局部状态，name 是外部 signal。
	inputTpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "input"},
		{Code: gxscene.OpDynAttr, Key: "id", Slot: 0},
		{Code: gxscene.OpDynAttr, Key: "value", Slot: 1},
		{Code: gxscene.OpClose},
	}, N: 2}
	buildMyInput := func() *gxscene.Instance {
		token++
		myToken := "input-" + string(rune('0'+token))
		return gxscene.Render(inputTpl,
			func() any { return myToken },
			func() any { return name() },
		)
	}

	// children getter：每次求值新建 #frag 包一个 slotComp(MyInput)。
	fragTpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "#frag"},
		{Code: gxscene.OpComp, Arg: "MyInput", Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
	children := func() any {
		return gxscene.Render(fragTpl, func() any { return buildMyInput() })
	}

	// 父组件 <div>{kids()}</div>：children 插槽是 slotChild。
	cardTpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
	card := gxscene.Render(cardTpl, children)

	if token != 1 {
		t.Fatalf("首次渲染应构造一次 MyInput, token=%d", token)
	}
	before := (gxscene.TextBackend{}).Paint(card.Root())
	if !strings.Contains(before, "input-1") {
		t.Fatalf("首帧应含局部 token input-1:\n%s", before)
	}

	// 父级无关更新：children 子树保稳，不重建 MyInput。
	card.Update()
	if token != 1 {
		t.Fatalf("无关 Update 重建了 children 内的组件, token=%d", token)
	}

	// 外部 signal 变化：只更新子树叶子，组件实例仍保稳。
	name("b")
	card.Update()
	if token != 1 {
		t.Fatalf("signal 更新重建了 children 内的组件, token=%d", token)
	}
	out := (gxscene.TextBackend{}).Paint(card.Root())
	if !strings.Contains(out, `"b"`) || !strings.Contains(out, "input-1") {
		t.Fatalf("叶子应更新为 b 且局部 token 保持 input-1:\n%s", out)
	}
}

// Issue #1 另一路：纯 scene 场景用 BindReactive（每叶子一个 effect），children 内的
// 组件子树须保稳——接线时递归进子树给叶子建 effect，而非为 slotChild 建 getter effect
// （getter 重求值会重建）。signal 变化只更新子树叶子，不重建组件、不重跑构造。
func TestDynChildrenCompositeStableUnderBindReactive(t *testing.T) {
	name := signal.New("x")
	token := 0

	inputTpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "input"},
		{Code: gxscene.OpDynAttr, Key: "id", Slot: 0},
		{Code: gxscene.OpDynAttr, Key: "value", Slot: 1},
		{Code: gxscene.OpClose},
	}, N: 2}
	buildMyInput := func() *gxscene.Instance {
		token++
		myToken := "input-" + string(rune('0'+token))
		return gxscene.Render(inputTpl,
			func() any { return myToken },
			func() any { return name() },
		)
	}
	fragTpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "#frag"},
		{Code: gxscene.OpComp, Arg: "MyInput", Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
	children := func() any {
		return gxscene.Render(fragTpl, func() any { return buildMyInput() })
	}
	cardTpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
	card := gxscene.Render(cardTpl, children)

	card.BindReactive(signal.Effect)
	name("y")
	if token != 1 {
		t.Fatalf("BindReactive 下 signal 变化重建了组件, token=%d", token)
	}
	out := (gxscene.TextBackend{}).Paint(card.Root())
	if !strings.Contains(out, `"y"`) || !strings.Contains(out, "input-1") {
		t.Fatalf("叶子应更新为 y 且局部 token 保持:\n%s", out)
	}
}

// Issue #1 协议层根因守护：组件库 demo 的典型形态——父容器 children 插槽里嵌套
// 多个有状态组件，父级 signal（模拟 blink/focus 翻转）变化时：
//  1. 组件实例保稳（构造计数不再增长）；
//  2. 变更流只含叶子动作（SetAttr/SetText/SetHandler），不含任何结构动作
//     （Remove/Insert/Replace/Move）。
//
// 第 2 点是协议层"结构动作阈值退化 GRX1"workaround 可以移除的直接证据：
// 根因修复后正常更新根本不产生结构动作，阈值只在真正的 For 增删时触发，
// 而那类合法结构变化应当走 GRX2 delta，不该被误判为镜像树膨胀。
func TestDynChildrenNoStructuralChangesOnSignalFlip(t *testing.T) {
	blink := signal.New(true)
	constructed := 0

	// 子组件 <input cursor={blink()}/>：局部 token + 读父级 blink 的叶子。
	inputTpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "input"},
		{Code: gxscene.OpDynAttr, Key: "cursor", Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
	buildInput := func() *gxscene.Instance {
		constructed++
		return gxscene.Render(inputTpl, func() any { return blink() })
	}

	// children getter 每次求值产出一个 #frag，内含 7 个组件（demo 的 7 子树）。
	fragOps := []gxscene.Op{{Code: gxscene.OpElem, Arg: "#frag"}}
	for i := 0; i < 7; i++ {
		fragOps = append(fragOps, gxscene.Op{Code: gxscene.OpComp, Arg: "Input", Slot: i})
	}
	fragOps = append(fragOps, gxscene.Op{Code: gxscene.OpClose})
	fragTpl := gxscene.Template{Ops: fragOps, N: 7}
	getters := make([]func() any, 7)
	for i := range getters {
		getters[i] = func() any { return buildInput() }
	}
	children := func() any { return gxscene.Render(fragTpl, getters...) }

	// 父容器 <div>{children}</div>：children 插槽是 slotChild。
	cardTpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
	card := gxscene.Render(cardTpl, children)

	if constructed != 7 {
		t.Fatalf("首帧应构造 7 个组件, constructed=%d", constructed)
	}
	card.EnableChanges()
	card.TakeChanges() // 清掉接线/首帧期变更

	// 连续翻转父级 signal（模拟 blink 定时器），每次都是一次无关更新。
	for k := 0; k < 5; k++ {
		blink(!blink())
		card.Update()
		if constructed != 7 {
			t.Fatalf("第 %d 次翻转后组件被重建, constructed=%d", k+1, constructed)
		}
		changes := card.TakeChanges()
		for _, c := range changes {
			switch c.Kind {
			case gxscene.ChangeRemoveChild, gxscene.ChangeInsertBefore,
				gxscene.ChangeReplaceChildren, gxscene.ChangeMoveChild:
				t.Fatalf("第 %d 次翻转产生结构动作 %v——根因未根治或仍有重建路径", k+1, c.Kind)
			}
		}
	}
}

// 回归守护：slotChild 承载普通标量（{count}）时仍每次更新——保稳逻辑只针对 *Instance。
func TestDynChildScalarStillUpdates(t *testing.T) {
	count := signal.New(1)
	tpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
	inst := gxscene.Render(tpl, func() any { return count() })
	inst.BindReactive(signal.Effect)
	count(2)
	out := (gxscene.TextBackend{}).Paint(inst.Root())
	if !strings.Contains(out, `"2"`) {
		t.Fatalf("标量插槽应更新到 2:\n%s", out)
	}
}

// Issue #3 回归：动态子节点槽位收到「类型化的子树切片」（[]*Instance / []*Node）时，
// 必须和 []any 一样按子树嫁接，不能被 type switch 漏掉后经 valueText 序列化成文本。
func TestDynChildrenTypedSubtreeSlices(t *testing.T) {
	// 一个 <span>"OK"</span> 子树模板。
	spanTpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpChildText, Arg: "OK"},
		{Code: gxscene.OpClose},
	}}
	wrap := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}

	cases := []struct {
		name  string
		value any
	}{
		{"[]any", func() any { return []any{gxscene.Render(spanTpl), gxscene.Render(spanTpl)} }()},
		{"[]*Instance", func() any { return []*gxscene.Instance{gxscene.Render(spanTpl), gxscene.Render(spanTpl)} }()},
		{"[]*Node", func() any { return []*gxscene.Node{gxscene.Render(spanTpl).Root(), gxscene.Render(spanTpl).Root()} }()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := c.value
			inst := gxscene.Render(wrap, func() any { return v })
			out := (gxscene.TextBackend{}).Paint(inst.Root())
			// 期望两棵 span 子树；不应出现 "[<span>" / "[0x..." 这类被 fmt.Sprint
			// 序列化的原文（类型化切片掉进 default 分支的症状）。
			if strings.Count(out, `<span>`) != 2 || strings.Count(out, `"OK"`) != 2 {
				t.Fatalf("类型化子树切片应按节点嫁接，得到:\n%s", out)
			}
			if strings.Contains(out, "[<span>") || strings.Contains(out, `[0x`) {
				t.Fatalf("子树切片被序列化成文本:\n%s", out)
			}
		})
	}
}

// Issue #3 守护：非子树类型的切片（[]string / []int）不能因反射改动被误当成子树列表，
// 仍应退化为文本；嵌套的子树切片应递归展开。
func TestDynChildrenScalarSliceStaysText(t *testing.T) {
	wrap := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
	v := []string{"a", "b"}
	inst := gxscene.Render(wrap, func() any { return v })
	out := (gxscene.TextBackend{}).Paint(inst.Root())
	if !strings.Contains(out, `[a b]`) {
		t.Fatalf("标量切片仍应序列化为文本 [a b]:\n%s", out)
	}
}

func TestDynChildrenNestedTypedSlices(t *testing.T) {
	spanTpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpChildText, Arg: "OK"},
		{Code: gxscene.OpClose},
	}}
	wrap := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
	// [][]any{[]{*Instance}, []{*Instance}} 嵌套切片。
	nested := [][]any{
		{gxscene.Render(spanTpl)},
		{gxscene.Render(spanTpl)},
	}
	inst := gxscene.Render(wrap, func() any { return nested })
	out := (gxscene.TextBackend{}).Paint(inst.Root())
	if strings.Count(out, `<span>`) != 2 || strings.Count(out, `"OK"`) != 2 {
		t.Fatalf("嵌套子树切片应递归展开为两棵 span:\n%s", out)
	}
}

// Issue #3：类型化子树切片在 Update（getter 重求值）时也应正确嫁接，
// 而不是在第二次求值时掉回文本分支。
func TestDynChildrenTypedSliceOnUpdate(t *testing.T) {
	span := func(s string) *gxscene.Instance {
		return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "span"},
			{Code: gxscene.OpChildText, Arg: s},
			{Code: gxscene.OpClose},
		}})
	}
	wrap := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
	kids := []*gxscene.Instance{span("A")}
	inst := gxscene.Render(wrap, func() any { return kids })
	out := (gxscene.TextBackend{}).Paint(inst.Root())
	if !strings.Contains(out, `"A"`) {
		t.Fatalf("首帧应含 A:\n%s", out)
	}
	kids = []*gxscene.Instance{span("B"), span("C")}
	inst.Update()
	out = (gxscene.TextBackend{}).Paint(inst.Root())
	if strings.Count(out, `<span>`) != 2 || !strings.Contains(out, `"B"`) || !strings.Contains(out, `"C"`) {
		t.Fatalf("Update 后类型化切片应按子树嫁接:\n%s", out)
	}
}
