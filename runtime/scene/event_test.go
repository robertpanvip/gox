package gxscene

import (
	"reflect"
	"testing"
)

func TestNormalizeEventName(t *testing.T) {
	cases := map[string]Event{
		"onClick":       EvClick,
		"onMouseEnter":  "mouseEnter", // 保留内部驼峰
		"onMouseLeave":  EvMouseLeave, // 与标准常量直接相等
		"onKeyDown":     EvKeyDown,
		"onFoo":         "foo",
		"onCustomThing": "customThing",   // 自定义事件驼峰不丢
		"click":         "click",         // 已规范化
		"on":            "on",            // 太短，不去前缀
		"onclick":       "onclick",       // on 后小写，不去前缀（避免误伤）
		"CustomerEvent": "customerEvent", // 首字母小写，其余保留
	}
	for in, want := range cases {
		if got := NormalizeEventName(in); got != want {
			t.Errorf("NormalizeEventName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEventIsCaseInsensitive(t *testing.T) {
	// 驼峰 Type 与小写写法应互相匹配（兼容后端传入不同大小写）。
	if !Event("mouseEnter").Is(Event("mouseenter")) || !Event("mouseenter").Is("mouseEnter") {
		t.Fatal("Event.Is 应大小写不敏感")
	}
	if Event("click").Is("mouseEnter") {
		t.Fatal("不同事件名不应匹配")
	}
	// 精确比较仍成立
	if NormalizeEventName("onMouseEnter") != EvMouseEnter {
		t.Fatal("Type 应与标准常量驼峰值直接相等")
	}
}

func TestModifierHas(t *testing.T) {
	m := ModCtrl | ModShift
	if !m.Has(ModCtrl) || !m.Has(ModShift) || m.Has(ModAlt) {
		t.Fatalf("Modifier.Has 错误: %b", m)
	}
	if !(ModCtrl | ModAlt).Has(ModCtrl | ModAlt) {
		t.Fatal("组合判断应全部置位才为真")
	}
}

func TestNewSyntheticEvent(t *testing.T) {
	n := &Node{Kind: "button"}
	raw := "native-click"
	ev := NewSyntheticEvent("onClick", n, raw)
	if ev.Type != EvClick || ev.NativeName != "onClick" {
		t.Fatalf("Type/NativeName: %q / %q", ev.Type, ev.NativeName)
	}
	if ev.Target != n || ev.CurrentTarget != n || ev.Raw != raw {
		t.Fatal("Target/CurrentTarget/Raw 未正确填充")
	}
	if ev.IsPropagationStopped() || ev.IsDefaultPrevented() {
		t.Fatal("初始不应有停止/阻止标记")
	}
	ev.StopPropagation()
	ev.PreventDefault()
	if !ev.IsPropagationStopped() || !ev.IsDefaultPrevented() {
		t.Fatal("StopPropagation/PreventDefault 未生效")
	}
}

// 构造三层树：root(#root) > outer(div) > inner(button)，每层绑定同事件 handler。
// 返回实例与记录调用顺序的切片指针。
func buildBubbleTree(t *testing.T, order *[]string) *Instance {
	t.Helper()
	rec := func(name string) Handler {
		return Handler(func(ctx EventContext) {
			*order = append(*order, name)
			if ctx.Synthetic == nil {
				t.Error("冒泡派发应自动补 Synthetic")
			}
		})
	}
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpElem, Arg: "button"},
		{Code: OpEvent, Key: "onClick", Slot: 0},
		{Code: OpClose},
		{Code: OpEvent, Key: "onClick", Slot: 1},
		{Code: OpClose},
	}, N: 2}
	return Render(tpl,
		func() any { return rec("inner") },
		func() any { return rec("outer") },
	)
}

func TestDispatchBubbleOrder(t *testing.T) {
	var order []string
	inst := buildBubbleTree(t, &order)
	inner := inst.Root().Children[0].Children[0]

	if !inner.DispatchBubble("onClick", EventContext{}) {
		t.Fatal("冒泡应触发 handler")
	}
	// inner(button) → outer(div)；#root 无 handler，跳过
	if !reflect.DeepEqual(order, []string{"inner", "outer"}) {
		t.Fatalf("冒泡顺序: %v", order)
	}
}

func TestDispatchBubbleStopPropagation(t *testing.T) {
	var order []string
	inst := Render(Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpElem, Arg: "button"},
		{Code: OpEvent, Key: "onClick", Slot: 0},
		{Code: OpClose},
		{Code: OpEvent, Key: "onClick", Slot: 1},
		{Code: OpClose},
	}, N: 2},
		func() any {
			return Handler(func(ctx EventContext) {
				order = append(order, "inner")
				ctx.Synthetic.StopPropagation()
			})
		},
		func() any {
			return Handler(func(EventContext) { order = append(order, "outer") })
		},
	)
	inner := inst.Root().Children[0].Children[0]
	inner.DispatchBubble("onClick", EventContext{})
	if !reflect.DeepEqual(order, []string{"inner"}) {
		t.Fatalf("StopPropagation 后不应到 outer: %v", order)
	}
}

func TestDispatchCaptureOrder(t *testing.T) {
	var order []string
	inst := Render(Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpElem, Arg: "button"},
		{Code: OpEvent, Key: "onClick", Slot: 0},
		{Code: OpClose},
		{Code: OpEvent, Key: "onClick", Slot: 1},
		{Code: OpClose},
	}, N: 2},
		func() any {
			return Handler(func(ctx EventContext) { order = append(order, "inner") })
		},
		func() any {
			return Handler(func(ctx EventContext) { order = append(order, "outer") })
		},
	)
	inner := inst.Root().Children[0].Children[0]
	inner.DispatchCapture("onClick", EventContext{})
	if !reflect.DeepEqual(order, []string{"outer", "inner"}) {
		t.Fatalf("捕获顺序: %v", order)
	}
}

// 合成事件不影响自定义事件透传：表外事件照常进 Handlers，且能冒泡。
func TestBubbleCustomEventStillTranspass(t *testing.T) {
	called := 0
	inst := Render(Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpElem, Arg: "span"},
		{Code: OpEvent, Key: "onFoo", Slot: 0},
		{Code: OpClose},
		{Code: OpClose},
	}, N: 1}, func() any {
		return Handler(func(ctx EventContext) {
			called++
			if ctx.Synthetic.Type != "foo" {
				t.Errorf("自定义事件应规范化为 foo, got %q", ctx.Synthetic.Type)
			}
		})
	})
	span := inst.Root().Children[0].Children[0]
	if !span.DispatchBubble("onFoo", EventContext{}) || called != 1 {
		t.Fatalf("自定义事件冒泡: called=%d", called)
	}
}

// 后端已构造 Synthetic 时应原样沿用，不被 ensureSynthetic 覆盖。
func TestBubbleRespectsBackendSynthetic(t *testing.T) {
	gotX := 0.0
	inst := Render(Template{Ops: []Op{
		{Code: OpElem, Arg: "button"},
		{Code: OpEvent, Key: "onClick", Slot: 0},
		{Code: OpClose},
	}, N: 1}, func() any {
		return Handler(func(ctx EventContext) { gotX = ctx.Synthetic.X })
	})
	btn := inst.Root().Children[0]
	se := NewSyntheticEvent("onClick", btn, "native")
	se.X = 42.5
	se.Mods = ModCtrl
	btn.DispatchBubble("onClick", EventContext{Synthetic: se})
	if gotX != 42.5 {
		t.Fatalf("后端 Synthetic 被覆盖: X=%v", gotX)
	}
	if !se.Mods.Has(ModCtrl) {
		t.Fatal("后端填的 Mods 丢失")
	}
}
