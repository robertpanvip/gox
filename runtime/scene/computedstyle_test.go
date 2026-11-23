package gxscene

import (
	"testing"
)

// TestComputedStyleFromProps 验证无后端能力时 getComputedStyle 从节点 Props
// 的展平声明样式生成快照：只暴露白名单属性、值字符串化、属性名排序稳定。
func TestComputedStyleFromProps(t *testing.T) {
	var el *Element
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpDynAttr, Key: "style", Slot: 0},
		{Code: OpRef, Slot: 1},
		{Code: OpClose},
	}, N: 2}
	st := Style{
		"bg":       "#fff",
		"color":    "#000",
		"padding":  12,
		"opacity":  0.5,
		"&:hover":  Style{"bg": "#eee"},
		"&:active": Style{"bg": "#ddd", "opacity": 0.9},
	}
	if err := st.Validate(); err != nil {
		t.Fatal(err)
	}
	inst := Render(tpl,
		func() any { return st },
		func() any { return func(e *Element) { el = e } },
	)
	defer inst.Dispose()

	cs := el.GetComputedStyle()
	if cs == nil {
		t.Fatal("GetComputedStyle 不应返回 nil")
	}
	if got := cs.GetPropertyValue("bg"); got != "#fff" {
		t.Fatalf("bg 应为 #fff，got %q", got)
	}
	if got := cs.GetPropertyValue("color"); got != "#000" {
		t.Fatalf("color 应为 #000，got %q", got)
	}
	if got := cs.GetPropertyValue("padding"); got != "12" {
		t.Fatalf("padding 应字符串化为 12，got %q", got)
	}
	if !cs.Has("opacity") {
		t.Fatal("opacity 应在快照中")
	}
	// 伪状态子表不算普通属性，不应出现在基础快照。
	if cs.Has("&:hover") || cs.Has("&:active") {
		t.Fatalf("伪状态子表不应作为普通属性暴露: %v", cs.PropertyNames())
	}
	if cs.Length() != 4 {
		t.Fatalf("基础快照应有 4 个白名单属性，got %d: %v", cs.Length(), cs.PropertyNames())
	}
	// 属性名小写且字典序稳定。
	want := []string{"bg", "color", "opacity", "padding"}
	got := cs.PropertyNames()
	if len(got) != len(want) {
		t.Fatalf("PropertyNames 长度不符: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("PropertyNames[%d] 应为 %q，got %q（完整: %v）", i, want[i], got[i], got)
		}
	}
	// 大小写不敏感、trim 空白。
	if cs.GetPropertyValue("  BG ") != "#fff" {
		t.Fatalf("属性名应大小写不敏感且 trim，got %q", cs.GetPropertyValue("  BG "))
	}
	if cs.GetPropertyValue("not-exist") != "" || cs.Has("not-exist") {
		t.Fatal("不存在的属性应返回空串且 Has=false")
	}
	// 非样式键（如 ref 自己）不进入快照——ref 不写 Props，这里确认白名单过滤生效。
	if cs.Has("class") || cs.Has("id") {
		t.Fatal("非白名单键不应进入快照")
	}
}

// TestComputedStylePseudoState 验证传伪状态时子表同名属性覆盖基础属性，
// 且非冲突的基础属性保留（与 Style.Flatten 同语义）。
func TestComputedStylePseudoState(t *testing.T) {
	var el *Element
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "button"},
		{Code: OpDynAttr, Key: "style", Slot: 0},
		{Code: OpRef, Slot: 1},
		{Code: OpClose},
	}, N: 2}
	st := Style{
		"bg":      "#fff",
		"color":   "#000",
		"&:hover": Style{"bg": "#eee"},
	}
	inst := Render(tpl,
		func() any { return st },
		func() any { return func(e *Element) { el = e } },
	)
	defer inst.Dispose()

	// 三种写法等价：hover / :hover / &:hover。
	for _, p := range []string{"hover", ":hover", "&:hover", "$:hover"} {
		cs := el.GetComputedStyle(p)
		if got := cs.GetPropertyValue("bg"); got != "#eee" {
			t.Fatalf("伪状态 %q 下 bg 应被覆盖为 #eee，got %q", p, got)
		}
		if got := cs.GetPropertyValue("color"); got != "#000" {
			t.Fatalf("伪状态 %q 下非冲突的 color 应保留 #000，got %q", p, got)
		}
	}
	// 未知伪状态：回退到基础样式，不panic。
	if got := el.GetComputedStyle("focus").GetPropertyValue("bg"); got != "#fff" {
		t.Fatalf("未知伪状态应回退基础样式，got %q", got)
	}
	// 基础快照不受伪状态查询影响。
	if got := el.GetComputedStyle().GetPropertyValue("bg"); got != "#fff" {
		t.Fatalf("基础 bg 不应被伪状态查询改写，got %q", got)
	}
}

// TestComputedStyleCapability 验证注入 ComputedStyler 能力后优先读后端权威值；
// 后端返回 nil 表示放弃，回退到 Props；后端可只覆盖部分属性。
func TestComputedStyleCapability(t *testing.T) {
	var el *Element
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpDynAttr, Key: "style", Slot: 0},
		{Code: OpRef, Slot: 1},
		{Code: OpClose},
	}, N: 2}
	st := Style{"bg": "#fff", "color": "#000"}
	inst := Render(tpl,
		func() any { return st },
		func() any { return func(e *Element) { el = e } },
	)
	defer inst.Dispose()

	id := el.id
	gotID, gotState := NoNode, "unset"
	inst.SetComputedStyleCap(func(nid NodeID, state string) map[string]string {
		gotID, gotState = nid, state
		// 后端返回含默认值的权威值，只覆盖 bg，其余交给回退是不支持的——
		// 能力命中即整份采用，故这里显式给出另一属性值。
		return map[string]string{"bg": "rgb(1,2,3)", "font-size": "16"}
	})

	cs := el.GetComputedStyle("active")
	if gotID != id {
		t.Fatalf("后端能力应收到节点 ID %v，got %v", id, gotID)
	}
	if gotState != "active" {
		t.Fatalf("后端能力应收到规整后的 state=active，got %q", gotState)
	}
	if got := cs.GetPropertyValue("bg"); got != "rgb(1,2,3)" {
		t.Fatalf("应优先读后端权威值，got %q", got)
	}
	if got := cs.GetPropertyValue("font-size"); got != "16" {
		t.Fatalf("后端提供的默认值应可见，got %q", got)
	}
	if cs.Has("color") {
		t.Fatal("能力命中时整份采用后端快照，不应混入 Props 声明值")
	}

	// 后端返回 nil 表示放弃 → 回退到 Props。
	inst.SetComputedStyleCap(func(NodeID, string) map[string]string { return nil })
	if got := el.GetComputedStyle().GetPropertyValue("bg"); got != "#fff" {
		t.Fatalf("后端放弃时应回退 Props，got %q", got)
	}

	// 传 nil 清空能力。
	inst.SetComputedStyleCap(nil)
	if got := el.GetComputedStyle().GetPropertyValue("color"); got != "#000" {
		t.Fatalf("清空能力后应回退 Props，got %q", got)
	}
}

// TestComputedStyleDeadHandle 验证失效句柄返回空快照而非 nil，链式取值安全。
func TestComputedStyleDeadHandle(t *testing.T) {
	var el *Element
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpRef, Slot: 0},
		{Code: OpClose},
	}, N: 1}
	inst := Render(tpl, func() any { return func(e *Element) { el = e } })
	inst.Dispose()

	cs := el.GetComputedStyle()
	if cs == nil {
		t.Fatal("失效句柄也应返回非空空快照")
	}
	if cs.Length() != 0 || cs.GetPropertyValue("bg") != "" || cs.Has("bg") {
		t.Fatalf("失效句柄应得到空快照，got %+v", cs)
	}
	// nil 接收者安全。
	var nilEl *Element
	if cs2 := nilEl.GetComputedStyle(); cs2 == nil || cs2.Length() != 0 {
		t.Fatalf("nil 句柄应得到空快照，got %+v", cs2)
	}
}
