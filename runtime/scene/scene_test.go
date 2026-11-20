package gxscene

import (
	"strings"
	"testing"
)

// cardTpl 是编译器应生成的 opcode 模板：
//
//	<div class="card"><h1>{title}</h1><span>{n}</span></div>
//
// 两个动态插槽：0=title(child), 1=n(child)；一个静态属性 class=card。
func cardTpl() Template {
	return Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpAttr, Key: "class", Arg: "card"},
		{Code: OpElem, Arg: "h1"},
		{Code: OpDynChild, Slot: 0},
		{Code: OpClose},
		{Code: OpElem, Arg: "span"},
		{Code: OpDynChild, Slot: 1},
		{Code: OpClose},
		{Code: OpClose},
	}, N: 2}
}

func TestRenderStaticSkeleton(t *testing.T) {
	got := RenderText(cardTpl(),
		func() any { return "Hello" },
		func() any { return 42 },
	)
	for _, want := range []string{"<div class=\"card\">", "<h1>", "\"Hello\"", "<span>", "\"42\"", "</div>"} {
		if !strings.Contains(got, want) {
			t.Errorf("渲染输出应包含 %q, got:\n%s", want, got)
		}
	}
}

func TestUpdateFineGrained(t *testing.T) {
	title := "old"
	n := 1
	inst := Render(cardTpl(),
		func() any { return title },
		func() any { return n },
	)
	if inst.Writes != 0 {
		t.Fatalf("首次渲染不应计入更新写入, got %d", inst.Writes)
	}

	// 只改 title：应只写 1 个节点
	title = "new"
	if w := inst.Update(); w != 1 {
		t.Fatalf("只改 title 应只写 1 节点, got %d", w)
	}
	if inst.Writes != 1 {
		t.Fatalf("累计写入应为 1, got %d", inst.Writes)
	}
	out := (TextBackend{}).Paint(inst.Root())
	if !strings.Contains(out, `"new"`) || !strings.Contains(out, `"1"`) {
		t.Errorf("更新后输出错误:\n%s", out)
	}

	// 值未变：不产生任何写入（细粒度核心）
	if w := inst.Update(); w != 0 {
		t.Fatalf("值未变应写 0 节点, got %d", w)
	}
	if inst.Writes != 1 {
		t.Fatalf("值未变累计写入应保持 1, got %d", inst.Writes)
	}

	// 只改 n
	n = 9
	if w := inst.Update(); w != 1 {
		t.Fatalf("只改 n 应只写 1 节点, got %d", w)
	}
	out = (TextBackend{}).Paint(inst.Root())
	if !strings.Contains(out, `"9"`) {
		t.Errorf("n 更新后输出错误:\n%s", out)
	}
}

func TestTemplateCacheReuse(t *testing.T) {
	// 同一 opcode 序列命中同一静态骨架：两次 Render 的根结构一致但节点相互独立。
	a := Render(cardTpl(), func() any { return "A" }, func() any { return 1 })
	b := Render(cardTpl(), func() any { return "B" }, func() any { return 2 })
	if a.Root() == b.Root() {
		t.Fatal("两个实例不应共享同一根节点")
	}
	if a.Root().Kind != b.Root().Kind || len(a.Root().Children) != len(b.Root().Children) {
		t.Fatal("缓存骨架应产出相同结构")
	}
	// 改 A 不影响 B
	a.SetPart(0, "A2")
	ob := (TextBackend{}).Paint(b.Root())
	if strings.Contains(ob, `"A2"`) || !strings.Contains(ob, `"B"`) {
		t.Errorf("实例间不应串值:\n%s", ob)
	}
}

func TestDynAttr(t *testing.T) {
	// <button disabled={flag}>。动态属性插槽。
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "button"},
		{Code: OpDynAttr, Key: "disabled", Slot: 0},
		{Code: OpClose},
	}, N: 1}
	flag := false
	inst := Render(tpl, func() any { return flag })
	if v := inst.Root().Children[0].Props["disabled"]; v != false {
		t.Fatalf("初始 disabled 应为 false, got %v", v)
	}
	flag = true
	if w := inst.Update(); w != 1 {
		t.Fatalf("flag 变化应写 1 节点, got %d", w)
	}
	if v := inst.Root().Children[0].Props["disabled"]; v != true {
		t.Fatalf("更新后 disabled 应为 true, got %v", v)
	}
}

func TestStaticTextAndAttr(t *testing.T) {
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "a"},
		{Code: OpAttr, Key: "href", Arg: "/home"},
		{Code: OpChildText, Arg: "home"},
		{Code: OpClose},
	}, N: 0}
	out := RenderText(tpl)
	if !strings.Contains(out, `href="/home"`) || !strings.Contains(out, `"home"`) {
		t.Errorf("静态属性/文本渲染错误:\n%s", out)
	}
}
