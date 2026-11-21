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

// componentTpl 模拟编译器为 `<div><Card title={t}/></div>` 生成的模板：
// 一个 OpComp 插槽承载 Card 子组件实例。
func parentWithCompTpl() Template {
	return Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpComp, Arg: "Card", Slot: 0},
		{Code: OpClose},
	}, N: 1}
}

// cardTplFor 是 Card 组件体 `<h1>{title}</h1>` 编译出的模板（一个动态标题插槽）。
func cardTplFor() Template {
	return Template{Ops: []Op{
		{Code: OpElem, Arg: "h1"},
		{Code: OpDynChild, Slot: 0},
		{Code: OpClose},
	}, N: 1}
}

func TestCompositeComponentMountAndRecursiveUpdate(t *testing.T) {
	title := "first"
	calls := 0

	// 模拟 JSX: <div><Card title={title}/></div>
	// 组件 getter 只在首次渲染调用一次，返回常驻子组件实例。
	inst := Render(parentWithCompTpl(), func() any {
		calls++
		return Render(cardTplFor(), func() any { return title })
	})

	if calls != 1 {
		t.Fatalf("组件函数应只调用一次, got %d", calls)
	}
	out := (TextBackend{}).Paint(inst.Root())
	if !strings.Contains(out, "<Card>") || !strings.Contains(out, "<h1>") || !strings.Contains(out, `"first"`) {
		t.Fatalf("组件子树未正确挂载:\n%s", out)
	}

	// 改 title：父 Update 应递归驱动子组件，只写 1 个节点
	title = "second"
	if w := inst.Update(); w != 1 {
		t.Fatalf("递归更新应写 1 节点, got %d", w)
	}
	if calls != 1 {
		t.Fatalf("Update 不应重新调用组件函数, got %d", calls)
	}
	out = (TextBackend{}).Paint(inst.Root())
	if !strings.Contains(out, `"second"`) || strings.Contains(out, `"first"`) {
		t.Fatalf("子组件未更新:\n%s", out)
	}

	// 值未变：零写入
	if w := inst.Update(); w != 0 {
		t.Fatalf("值未变应写 0 节点, got %d", w)
	}
}

func TestCompositeComponentPropsGetter(t *testing.T) {
	// 验证 Props/Prop/Get 的 getter 语义：组件读的是 getter，不是快照值。
	n := 1
	p := Props{"n": func() any { return n }}
	getN := Get[int](p, "n")
	if getN() != 1 {
		t.Fatalf("初始值应为 1, got %d", getN())
	}
	n = 9
	if getN() != 9 {
		t.Fatalf("getter 应读到最新值 9, got %d", getN())
	}
	// 静态属性 + 缺失属性
	p2 := Props{"label": "hi"}
	if Get[string](p2, "label")() != "hi" {
		t.Fatal("静态属性应可读")
	}
	if Get[int](p2, "missing")() != 0 {
		t.Fatal("缺失属性应返回零值")
	}
}

func TestCompositeComponentChildrenGraft(t *testing.T) {
	// children 是一个返回 *Instance 的 getter，作为 dyn child 嵌入时应摊平透明节点。
	child := Render(cardTplFor(), func() any { return "kid" })
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "section"},
		{Code: OpDynChild, Slot: 0},
		{Code: OpClose},
	}, N: 1}
	inst := Render(tpl, func() any { return child })
	out := (TextBackend{}).Paint(inst.Root())
	if !strings.Contains(out, "<section>") || !strings.Contains(out, "<h1>") || !strings.Contains(out, `"kid"`) {
		t.Fatalf("children 未正确嫁接:\n%s", out)
	}
	if strings.Contains(out, "#frag") || strings.Contains(out, "#root") {
		t.Fatalf("透明包装层不应出现在输出:\n%s", out)
	}
}

// ---- 节点类型 + 事件透传协议 ----

func TestNodeTypesAndEventTranspass(t *testing.T) {
	called := 0
	customCalled := 0
	h := func(EventContext) { called++ }
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "button"},
		{Code: OpEvent, Key: "onClick", Slot: 0},
		{Code: OpEvent, Key: "onCustomThing", Slot: 1}, // 后端自定义事件，前端不拦
		{Code: OpAttr, Key: "id", Arg: "btn"},
		{Code: OpChildText, Arg: "go"},
		{Code: OpClose},
	}, N: 2}
	inst := Render(tpl,
		func() any { return Handler(h) },
		func() any { return func() { customCalled++ } }, // 自定义签名前端不强制
	)
	btn := inst.Root().Children[0]

	// 节点类型：元素=NodeDiv，文本=NodeText
	if btn.Type != NodeDiv || btn.Kind != "button" {
		t.Fatalf("button: Type=%v Kind=%q", btn.Type, btn.Kind)
	}
	if n := btn.Children[0]; n.Type != NodeText {
		t.Fatalf("text child: Type=%v", n.Type)
	}

	// 事件进 Handlers，不进 Props；静态属性仍在 Props
	if _, ok := btn.Props["onClick"]; ok {
		t.Fatal("事件不应进 Props")
	}
	if btn.Props["id"] != "btn" {
		t.Fatalf("静态属性丢失: %v", btn.Props)
	}
	if btn.Handler("onClick") == nil || btn.Handler("onCustomThing") == nil {
		t.Fatal("标准与自定义事件都应在 Handlers")
	}

	// 标准事件按协议签名派发
	if !btn.Dispatch("onClick", EventContext{Raw: "native"}) || called != 1 {
		t.Fatalf("Dispatch: ok=%v called=%d", true, called)
	}
	// 自定义签名取原值自行调用
	if fn, ok := btn.Handler("onCustomThing").(func()); ok {
		fn()
	}
	if customCalled != 1 {
		t.Fatalf("custom handler: %d", customCalled)
	}
	// 未绑定事件返回 false
	if btn.Dispatch("onNone", EventContext{}) {
		t.Fatal("未绑定事件应返回 false")
	}
}

func TestStandardEventTableIsHintNotWhitelist(t *testing.T) {
	// 表外事件照样透传——证明标准表只是提示，不是白名单。
	inst := Render(Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpEvent, Key: "onTotallyCustom", Slot: 0},
		{Code: OpClose},
	}, N: 1}, func() any { return Handler(func(EventContext) {}) })
	if inst.Root().Children[0].Handler("onTotallyCustom") == nil {
		t.Fatal("表外自定义事件必须透传")
	}
	if len(StandardEvents) == 0 {
		t.Fatal("标准事件表不应为空（供 IDE 提示）")
	}
}
