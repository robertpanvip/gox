package gxvdom

import (
	"reflect"
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	node := CreateElement("div", map[string]any{"class": "box", "disabled": true}, "hi ", "there")
	got := Render(node)
	want := `<div class="box" disabled>hi there</div>`
	if got != want {
		t.Fatalf("Render = %q, want %q", got, want)
	}
}

func TestDiff(t *testing.T) {
	a := CreateElement("div", map[string]any{"class": "box"}, "hello")
	b := CreateElement("div", map[string]any{"class": "card"}, "world", "!")
	patches := Diff(a, b)

	seen := map[PatchOp]int{}
	for _, p := range patches {
		seen[p.Op]++
		if p.Op == OpSetProp && p.Key != "class" {
			t.Errorf("set-prop 的 key 应为 class，实际 %q", p.Key)
		}
		if p.Op == OpSetText && (len(p.Path) != 1 || p.Path[0] != 0) {
			t.Errorf("set-text 路径应为 [0]，实际 %v", p.Path)
		}
	}
	if seen[OpSetProp] != 1 || seen[OpSetText] != 1 || seen[OpAppend] != 1 {
		t.Fatalf("期望 set-prop/set-text/append 各一条，实际 %+v", seen)
	}
}

func TestRenderVoidTags(t *testing.T) {
	node := CreateElement("br", nil)
	if got := Render(node); got != "<br>" {
		t.Errorf("void 标签渲染 = %q, want <br>", got)
	}
	// void 标签即使有 children 也不渲染闭合标签
	node2 := CreateElement("img", map[string]any{"src": "x.png"}, "ignored")
	want := `<img src="x.png">`
	if got := Render(node2); got != want {
		t.Errorf("void 标签带子节点 = %q, want %q", got, want)
	}
}

func TestRenderEscaping(t *testing.T) {
	node := CreateElement("div", map[string]any{"title": `a"b&c<d`}, `<script>alert("x")</script>`)
	got := Render(node)
	want := `<div title="a&#34;b&amp;c&lt;d">&lt;script&gt;alert(&#34;x&#34;)&lt;/script&gt;</div>`
	if got != want {
		t.Errorf("HTML 转义 = %q, want %q", got, want)
	}
}

func TestRenderSkipsProps(t *testing.T) {
	fn := func() {}
	node := CreateElement("div", map[string]any{
		"children": "should-not-render",
		"onClick":  fn,
		"hidden":   false,
		"z":        "last",
		"a":        "first",
	}, "body")
	got := Render(node)
	want := `<div a="first" z="last">body</div>`
	if got != want {
		t.Errorf("属性过滤/排序 = %q, want %q", got, want)
	}
}

func TestRenderScalarChildren(t *testing.T) {
	node := CreateElement("p", nil, "n=", 42, " ", true, []any{"a", "b"})
	got := Render(node)
	want := "<p>n=42 trueab</p>"
	if got != want {
		t.Errorf("标量/切片子节点 = %q, want %q", got, want)
	}
}

func TestRenderNilNode(t *testing.T) {
	var nilNode *VNode
	cases := []any{nil, nilNode, ""}
	for _, c := range cases {
		if got := Render(c); got != "" {
			t.Errorf("Render(%v) = %q, want empty", c, got)
		}
	}
}

func TestDiffReplaceDifferentTag(t *testing.T) {
	a := CreateElement("div", nil, "x")
	b := CreateElement("span", nil, "x")
	patches := Diff(a, b)
	if len(patches) != 1 || patches[0].Op != OpReplace || len(patches[0].Path) != 0 {
		t.Fatalf("不同标签应整体替换: %+v", patches)
	}
}

func TestDiffTextToElementReplace(t *testing.T) {
	a := CreateElement("div", nil, "text")
	b := CreateElement("div", nil, CreateElement("span", nil))
	patches := Diff(a, b)
	found := false
	for _, p := range patches {
		if p.Op == OpReplace && len(p.Path) == 1 && p.Path[0] == 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("文本→元素应产生 replace at [0]: %+v", patches)
	}
}

func TestDiffRemovePropAndChild(t *testing.T) {
	a := CreateElement("div", map[string]any{"class": "box", "id": "x"}, "keep", "drop")
	b := CreateElement("div", map[string]any{"class": "box"}, "keep")
	patches := Diff(a, b)
	seen := map[PatchOp]int{}
	for _, p := range patches {
		seen[p.Op]++
		if p.Op == OpRemoveProp && p.Key != "id" {
			t.Errorf("应删除 id 属性, got %q", p.Key)
		}
		if p.Op == OpRemove && (len(p.Path) != 1 || p.Path[0] != 1) {
			t.Errorf("应删除子节点 [1], got %v", p.Path)
		}
	}
	if seen[OpRemoveProp] != 1 || seen[OpRemove] != 1 {
		t.Fatalf("期望 remove-prop/remove 各一条: %+v", seen)
	}
}

func TestDiffNestedPath(t *testing.T) {
	childA := CreateElement("span", nil, "old")
	childB := CreateElement("span", nil, "new")
	a := CreateElement("div", nil, CreateElement("section", nil, childA))
	b := CreateElement("div", nil, CreateElement("section", nil, childB))
	patches := Diff(a, b)
	found := false
	for _, p := range patches {
		if p.Op == OpSetText && reflect.DeepEqual(p.Path, []int{0, 0, 0}) {
			found = true
		}
	}
	if !found {
		t.Fatalf("嵌套文本变化路径应为 [0,0,0]: %+v", patches)
	}
}

func TestDiffIdenticalTreesNoPatches(t *testing.T) {
	a := CreateElement("div", map[string]any{"class": "box"}, "hello", CreateElement("span", nil, "x"))
	b := CreateElement("div", map[string]any{"class": "box"}, "hello", CreateElement("span", nil, "x"))
	if patches := Diff(a, b); len(patches) != 0 {
		t.Fatalf("相同树应无补丁, got %+v", patches)
	}
}

func TestDiffSetPropValueChange(t *testing.T) {
	a := CreateElement("input", map[string]any{"value": "1"})
	b := CreateElement("input", map[string]any{"value": "2"})
	patches := Diff(a, b)
	if len(patches) != 1 || patches[0].Op != OpSetProp || patches[0].Key != "value" || patches[0].Value != "2" {
		t.Fatalf("属性变更应产生 set-prop: %+v", patches)
	}
}

// ---- 函数组件（JSX <Foo/>）回归测试 ----

func TestCreateElementComponentVsTag(t *testing.T) {
	native := CreateElement("div", nil)
	if native.Tag != "div" || native.Component != nil {
		t.Fatalf("string 参数应生成原生元素节点: %+v", native)
	}
	comp := CreateElement(func(map[string]any) any { return nil }, nil)
	if comp.Tag != "" || comp.Component == nil {
		t.Fatalf("函数参数应生成组件节点: %+v", comp)
	}
}

func TestRenderComponent(t *testing.T) {
	// 组件签名与 GX 生成的函数组件一致：func(map[string]any) *Element
	greet := func(props map[string]any) *Element {
		name, _ := props["name"].(string)
		return CreateElement("span", nil, "hello ", name)
	}
	node := CreateElement(greet, map[string]any{"name": "gx"})
	got := Render(node)
	want := "<span>hello gx</span>"
	if got != want {
		t.Fatalf("组件渲染 = %q, want %q", got, want)
	}
}

func TestRenderComponentWithChildren(t *testing.T) {
	wrap := func(props map[string]any) any {
		kids, _ := props["children"].([]any)
		return CreateElement("div", map[string]any{"class": "wrap"}, kids...)
	}
	node := CreateElement(wrap, nil, "a", CreateElement("b", nil))
	got := Render(node)
	want := `<div class="wrap">a<b></b></div>`
	if got != want {
		t.Fatalf("组件 children 渲染 = %q, want %q", got, want)
	}
}

func TestRenderComponentDoesNotMutateProps(t *testing.T) {
	noop := func(props map[string]any) any { return CreateElement("i", nil, "x") }
	props := map[string]any{"id": "k"}
	node := CreateElement(noop, props, "child")
	_ = Render(node)
	if _, ok := props["children"]; ok {
		t.Fatalf("渲染不应把 children 写回调用方 props: %+v", props)
	}
}

func TestDiffExpandsComponent(t *testing.T) {
	// 组件随 props 变化产出不同子树；Diff 必须展开后再比较。
	mk := func(text string) any {
		return func(map[string]any) any { return CreateElement("p", nil, text) }
	}
	a := CreateElement(mk("old"), nil)
	b := CreateElement(mk("new"), nil)
	patches := Diff(a, b)
	found := false
	for _, p := range patches {
		if p.Op == OpSetText && p.Value == "new" {
			found = true
		}
	}
	if !found {
		t.Fatalf("组件展开后应产生 set-text(new), got %+v", patches)
	}
}

func TestRenderNonFuncComponentIsSafe(t *testing.T) {
	// 组件位不是函数时不应 panic，渲染为空。
	node := CreateElement(42, nil)
	if got := Render(node); got != "" {
		t.Fatalf("非函数组件应渲染为空, got %q", got)
	}
}

func TestComponentInvokedOnce(t *testing.T) {
	calls := 0
	counter := func(map[string]any) any {
		calls++
		return CreateElement("span", nil, "x")
	}
	node := CreateElement(counter, nil)
	_ = Render(node) // 第一次展开
	_ = Render(node) // 应命中缓存
	_ = ExpandComponent(node)
	if calls != 1 {
		t.Fatalf("组件应只执行一次（惰性+缓存）, got %d", calls)
	}
}

func TestComponentExplicitExpand(t *testing.T) {
	comp := func(props map[string]any) any {
		return CreateElement("b", nil, props["text"])
	}
	node := CreateElement(comp, map[string]any{"text": "hi"})
	expanded := ExpandComponent(node)
	native, ok := expanded.(*Element)
	if !ok || native.Tag != "b" {
		t.Fatalf("ExpandComponent 应返回组件产出的节点树: %#v", expanded)
	}
	if got := Render(expanded); got != "<b>hi</b>" {
		t.Fatalf("展开后渲染 = %q", got)
	}
}

func TestExpandComponentOnNativeReturnsNil(t *testing.T) {
	if got := ExpandComponent(CreateElement("div", nil)); got != nil {
		t.Fatalf("原生元素的 ExpandComponent 应为 nil, got %v", got)
	}
}

func TestComponentRejectsWrongSignature(t *testing.T) {
	// 组件签名不符合 func(map) T 时应安全降级为空，不 panic。
	bad := func(int) any { return CreateElement("div", nil) }
	node := CreateElement(bad, nil)
	if got := Render(node); got != "" {
		t.Fatalf("错误签名组件应渲染为空, got %q", got)
	}
}

func TestDiffEqualValueKeepsType(t *testing.T) {
	// true(bool) 与 "true"(string) 渲染不同，必须产生 set-prop，不能漏更新。
	a := CreateElement("button", map[string]any{"disabled": true})
	b := CreateElement("button", map[string]any{"disabled": "true"})
	patches := Diff(a, b)
	found := false
	for _, p := range patches {
		if p.Op == OpSetProp && p.Key == "disabled" && p.Value == "true" {
			found = true
		}
	}
	if !found {
		t.Fatalf("布尔 true→字符串 true 应产生 set-prop: %+v", patches)
	}
}

func TestDiffNilNilNoPatch(t *testing.T) {
	if patches := Diff(nil, nil); len(patches) != 0 {
		t.Fatalf("Diff(nil,nil) 应无补丁, got %+v", patches)
	}
}

func TestIsFuncAnySignature(t *testing.T) {
	// 任意签名的事件处理器都应被识别为函数，不渲染成 HTML 属性。
	if !isFunc(func(string) {}) {
		t.Error("func(string) 应识别为函数")
	}
	if !isFunc(func(int, string) error { return nil }) {
		t.Error("多参数有返回值应识别为函数")
	}
	if isFunc("not a func") {
		t.Error("字符串不应识别为函数")
	}
	node := CreateElement("button", map[string]any{"onClick": func(string) {}})
	if got := Render(node); strings.Contains(got, "onClick") {
		t.Errorf("函数属性不应渲染成 HTML 属性: %s", got)
	}
}

func TestRenderSkipsKeyProp(t *testing.T) {
	node := CreateElement("li", map[string]any{"key": "a", "id": "x"}, "hi")
	if got := Render(node); strings.Contains(got, "key=") {
		t.Errorf("key 不应渲染为 HTML 属性: %s", got)
	}
}

func TestDiffKeyedReorder(t *testing.T) {
	mk := func(k, text string) *Element {
		return CreateElement("li", map[string]any{"key": k}, text)
	}
	old := CreateElement("ul", nil, mk("a", "A"), mk("b", "B"))
	new := CreateElement("ul", nil, mk("b", "B"), mk("a", "A"))
	// 重排：按 key 对齐后，每个 key 命中的节点内容相同，不应产生 set-text（
	// 只有顺序变化）。这里断言没有把 A/B 内容对错位的补丁。
	for _, p := range Diff(old, new) {
		if p.Op == OpSetText {
			t.Errorf("key 重排不应产生文本错位补丁: %+v", p)
		}
	}
}

func TestDiffKeyedInsert(t *testing.T) {
	mk := func(k, text string) *Element {
		return CreateElement("li", map[string]any{"key": k}, text)
	}
	old := CreateElement("ul", nil, mk("a", "A"))
	new := CreateElement("ul", nil, mk("a", "A"), mk("b", "B"))
	patches := Diff(old, new)
	found := false
	for _, p := range patches {
		if p.Op == OpAppend {
			found = true
		}
	}
	if !found {
		t.Fatalf("key 新增应产生 append: %+v", patches)
	}
}
