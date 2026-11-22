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
