package gxscene

import (
	"strings"
	"testing"

	"github.com/robertpanvip/gox/runtime/signal"
)

// Bug 03 回归：child 插槽产出 *Instance 后，Solid <Dynamic> 语义必须有出口。
// 普通 children 插槽（OpDynChild）为保局部状态一劳永逸稳定（Issue #1）；但"确实
// 想按条件切换组件"的场景此前无出口。新增 OpDynComp/slotDynamic：每次求值 getter，
// 按场景模板身份比较——同一模板的新构造复用旧子树（保局部状态、props 由实例内
// 响应式 getter 保鲜），模板不同则 Dispose 旧子树、嫁接新子树、共享 sink 并递归接线。
func TestDynamicCompSwitchByTemplateIdentity(t *testing.T) {
	which := signal.New(0)
	// A 与 B 模板不同（span vs b）→ 组件身份不同。
	makeA := func() *Instance {
		return Render(Template{Ops: []Op{{Code: OpElem, Arg: "span"}, {Code: OpChildText, Arg: "A"}, {Code: OpClose}}})
	}
	makeB := func() *Instance {
		return Render(Template{Ops: []Op{{Code: OpElem, Arg: "b"}, {Code: OpChildText, Arg: "B"}, {Code: OpClose}}})
	}
	wrap := Template{Ops: []Op{
		{Code: OpElem, Arg: "div"}, {Code: OpDynComp, Slot: 0}, {Code: OpClose},
	}, N: 1}
	inst := Render(wrap, func() any {
		if which() == 0 {
			return makeA()
		}
		return makeB()
	})

	if out := (TextBackend{}).Paint(inst.Root()); !strings.Contains(out, `"A"`) {
		t.Fatalf("首帧应为 A:\n%s", out)
	}
	which(1)
	inst.Update()
	out := (TextBackend{}).Paint(inst.Root())
	if !strings.Contains(out, `"B"`) || strings.Contains(out, `"A"`) {
		t.Errorf("signal 变化后应切换到 B（旧 A 被替换）:\n%s", out)
	}
}

// 同一模板的新构造（如 getter 每次 Render 同一组件）必须复用旧实例：局部 token
// 保稳、叶子随外部 signal 更新——证明 <Dynamic> 出口不破坏 Issue #1 的组件保稳。
func TestDynamicCompSameTemplateReusesSubtree(t *testing.T) {
	name := signal.New("x")
	token := 0
	subTpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "input"},
		{Code: OpDynAttr, Key: "id", Slot: 0},
		{Code: OpDynAttr, Key: "value", Slot: 1},
		{Code: OpClose},
	}, N: 2}
	build := func() *Instance {
		token++
		return Render(subTpl, func() any { return "input-1" }, func() any { return name() })
	}
	wrap := Template{Ops: []Op{
		{Code: OpElem, Arg: "div"}, {Code: OpDynComp, Slot: 0}, {Code: OpClose},
	}, N: 1}
	inst := Render(wrap, func() any { return build() })

	if token != 1 {
		t.Fatalf("首帧构造一次, token=%d", token)
	}
	inst.Update() // getter 求值 new 新实例，但模板相同应复用
	name("y")
	inst.Update() // 叶子响应式更新
	out := (TextBackend{}).Paint(inst.Root())
	if !strings.Contains(out, `"y"`) || !strings.Contains(out, "input-1") {
		t.Errorf("同模板应复用旧实例：叶子更新为 y、局部 id 保持 input-1:\n%s", out)
	}
}

// 响应式接线路径（BindStructural + BindLeafEffects）：组件切换应触发 onDirty 重绘，
// 且新子树的叶子能继续响应 signal 写入（验证替换后递归接线 + sink 共享）。
func TestDynamicCompReactiveSwitchRedraws(t *testing.T) {
	which := signal.New(0)
	name := signal.New("x")
	sub := func() *Instance {
		tpl := Template{Ops: []Op{
			{Code: OpElem, Arg: "span"}, {Code: OpDynChild, Slot: 0}, {Code: OpClose},
		}, N: 1}
		return Render(tpl, func() any { return name() })
	}
	wrap := Template{Ops: []Op{
		{Code: OpElem, Arg: "div"}, {Code: OpDynComp, Slot: 0}, {Code: OpClose},
	}, N: 1}
	inst := Render(wrap, func() any {
		if which() == 0 {
			return sub()
		}
		return sub()
	})

	dirty := 0
	inst.BindStructural(signal.Effect, func() { dirty++ })
	inst.BindLeafEffects(signal.Effect)

	if out := (TextBackend{}).Paint(inst.Root()); !strings.Contains(out, `"x"`) {
		t.Fatalf("首帧应含 x:\n%s", out)
	}
	which(1)
	out := (TextBackend{}).Paint(inst.Root())
	if !strings.Contains(out, `"x"`) {
		t.Errorf("切换后新子树应正常渲染:\n%s", out)
	}
	if dirty == 0 {
		t.Error("组件切换应触发 onDirty 重绘")
	}
	name("y")
	out = (TextBackend{}).Paint(inst.Root())
	if !strings.Contains(out, `"y"`) {
		t.Errorf("切换后新子树叶子应继续响应 name:\n%s", out)
	}
}
