package widgets

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// fireClick 找到 kind 节点上的 onClick handler 并派发一个最小合成事件。
func fireClick(t *testing.T, n *gxscene.Node) gxscene.EventContext {
	t.Helper()
	if n == nil {
		t.Fatal("节点为空，无法触发点击")
	}
	h, ok := n.Handler("onClick").(gxscene.Handler)
	if !ok || h == nil {
		t.Fatalf("节点 %s 未绑定 onClick handler", n.Kind)
	}
	ctx := gxscene.EventContext{Synthetic: &gxscene.SyntheticEvent{Type: gxscene.EvClick, NativeName: "onClick"}}
	h(ctx)
	return ctx
}

func TestCheckboxToggle(t *testing.T) {
	checked := signal.New(false)
	inst := Checkbox(gxscene.Props{
		"checked":  func() any { return checked() },
		"onChange": BindBool(func(v bool) { checked(v) }),
		"label":    func() any { return "同意条款" },
	})
	root := inst.Root()
	box := findByRole(root, "checkbox")
	if box == nil {
		t.Fatal("未生成 checkbox 节点")
	}

	// 未选中：勾 opacity=0，bg 为输入底色。
	if got := findAttrDeep(root, "opacity"); got != "0" {
		t.Errorf("未选中勾 opacity = %v, want 0", got)
	}
	txt := gxscene.TextBackend{}.Paint(root)
	if !containsStr(txt, "同意条款") {
		t.Errorf("应显示标签: %q", txt)
	}

	fireClick(t, box)
	if !checked() {
		t.Fatal("点击后应选中")
	}
	inst.Update()
	if got := findAttrDeep(root, "opacity"); got != "1" {
		t.Errorf("选中后勾 opacity = %v, want 1", got)
	}
	if got := findAttrDeep(root, "bg"); got != ThemeAccent {
		t.Errorf("选中后应有强调色底, got %v", got)
	}

	fireClick(t, box)
	if checked() {
		t.Fatal("再次点击应取消选中")
	}
}

func TestSwitchToggle(t *testing.T) {
	on := signal.New(false)
	inst := Switch(gxscene.Props{
		"checked":  func() any { return on() },
		"onChange": BindBool(func(v bool) { on(v) }),
		"label":    func() any { return "通知" },
	})
	root := inst.Root()
	sw := findByRole(root, "switch")
	if sw == nil {
		t.Fatal("未生成 switch 节点")
	}
	fireClick(t, sw)
	if !on() {
		t.Fatal("点击后开关应打开")
	}
	fireClick(t, sw)
	if on() {
		t.Fatal("再次点击开关应关闭")
	}
}

func TestRadioGroupSelect(t *testing.T) {
	sel := signal.New("a")
	inst := RadioGroup(gxscene.Props{
		"value": func() any { return sel() },
		"options": []Option{
			{Label: "选项 A", Value: "a"},
			{Label: "选项 B", Value: "b"},
			{Label: "禁用 C", Value: "c", Disabled: true},
		},
		"onChange": BindValue(func(v string) { sel(v) }),
	})
	root := inst.Root()
	radios := findAllByRole(root, "radio")
	if len(radios) != 3 {
		t.Fatalf("应生成 3 个 radio, got %d", len(radios))
	}

	// 点击第二项 → 选中 b。
	fireClick(t, radios[1])
	if got := sel(); got != "b" {
		t.Fatalf("点击选项 B 后 = %q, want b", got)
	}

	// 点击禁用项 → 不改变。
	fireClick(t, radios[2])
	if got := sel(); got != "b" {
		t.Fatalf("禁用项不应改变选中, got %q", got)
	}
}

func TestSliderEmitAndClamp(t *testing.T) {
	val := signal.New(50.0)
	inst := Slider(gxscene.Props{
		"value":    func() any { return val() },
		"min":      0,
		"max":      100,
		"step":     25,
		"onChange": BindNumber(func(v float64) { val(v) }),
		"label":    func() any { return "音量" },
	})
	root := inst.Root()
	hits := findAllByRole(root, "slider-hit")
	if len(hits) != 4 { // (100-0)/25 = 4 段
		t.Fatalf("应有 4 个命中段, got %d", len(hits))
	}
	// 点第 4 段 → 值 75（0-based: 0,25,50,75）。
	fireClick(t, hits[3])
	if got := val(); got != 75 {
		t.Fatalf("点击第 4 段后 = %v, want 75", got)
	}
}

func TestOptionsFromShapes(t *testing.T) {
	// []string
	o1 := optionsFrom(gxscene.Props{"options": []string{"a", "b"}}, "options")()
	if len(o1) != 2 || o1[0].Value != "a" || o1[0].Label != "a" {
		t.Fatalf("[]string 解析错误: %+v", o1)
	}
	// func() any → []Option（响应式）
	o2 := optionsFrom(gxscene.Props{"options": func() any {
		return []Option{{Label: "X", Value: "x"}}
	}}, "options")()
	if len(o2) != 1 || o2[0].Label != "X" {
		t.Fatalf("func() any 解析错误: %+v", o2)
	}
	// []any + map
	o3 := optionsFrom(gxscene.Props{"options": []any{
		map[string]any{"label": "甲", "value": "jia"},
		"简单项",
	}}, "options")()
	if len(o3) != 2 || o3[0].Value != "jia" || o3[1].Value != "简单项" {
		t.Fatalf("[]any 解析错误: %+v", o3)
	}
}

func TestBindHelpersDefault(t *testing.T) {
	gotB := true
	BindBool(func(v bool) { gotB = v })(gxscene.EventContext{})
	if gotB {
		t.Error("空事件下 BindBool 应回传 false 零值")
	}
	gotS := "x"
	BindValue(func(v string) { gotS = v })(gxscene.EventContext{})
	if gotS != "" {
		t.Error("空事件下 BindValue 应回传空串")
	}
	gotN := 9.0
	BindNumber(func(v float64) { gotN = v })(gxscene.EventContext{})
	if gotN != 0 {
		t.Error("空事件下 BindNumber 应回传 0")
	}
}

// findByRole / findAllByRole 按 Props["gx-role"] 查找节点（Kind 是元素类型如 div，
// 角色标记放在 Props 里）。
func findByRole(n *gxscene.Node, role string) *gxscene.Node {
	if n == nil {
		return nil
	}
	if r, ok := n.Props["gx-role"]; ok && r == role {
		return n
	}
	for _, c := range n.Children {
		if hit := findByRole(c, role); hit != nil {
			return hit
		}
	}
	return nil
}

func findAllByRole(n *gxscene.Node, role string) []*gxscene.Node {
	var out []*gxscene.Node
	var walk func(*gxscene.Node)
	walk = func(x *gxscene.Node) {
		if x == nil {
			return
		}
		if r, ok := x.Props["gx-role"]; ok && r == role {
			out = append(out, x)
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(n)
	return out
}
