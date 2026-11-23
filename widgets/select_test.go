package widgets

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

func TestSelectStructureAndSelection(t *testing.T) {
	value := signal.New("")
	st := NewPopoverState(true) // 初始展开，便于检查菜单
	inst := Select(gxscene.Props{
		"value": func() any { return value() },
		"options": []Option{
			{Label: "苹果", Value: "apple"},
			{Label: "香蕉", Value: "banana"},
			{Label: "不可用", Value: "x", Disabled: true},
		},
		"onChange":  BindValue(func(v string) { value(v) }),
		"state":     func() any { return st },
		"placement": "bottom-end",
		"width":     "220",
	})
	inst.BindReactive(signal.Effect)
	root := inst.Root()

	// 触发器存在，显示占位文字。
	trig := findByRole(root, "select-trigger")
	if trig == nil {
		t.Fatal("缺少 select-trigger")
	}
	txt := gxscene.TextBackend{}.Paint(root)
	if !containsStr(txt, "请选择") {
		t.Errorf("未选中应显示占位: %q", txt)
	}

	// 菜单与 3 个选项存在（浮层展开）。
	menu := findByRole(root, "select-menu")
	if menu == nil {
		t.Fatal("展开后缺少 select-menu")
	}
	pop := findByRole(root, "popover")
	if pop == nil || pop.Props["gx-placement"] != "bottom-end" {
		t.Errorf("浮层 placement 错误: %+v", pop)
	}
	opts := findAllByRole(root, "select-option")
	if len(opts) != 3 {
		t.Fatalf("应有 3 个选项, got %d", len(opts))
	}

	// 点击第二项 → 回传 banana + 关闭浮层。
	fireClick(t, opts[1])
	if got := value(); got != "banana" {
		t.Fatalf("选中后 value=%q, want banana", got)
	}
	if st.IsOpen() {
		t.Error("选中后浮层应关闭")
	}

	// 点击禁用项不改变值。
	value("apple")
	inst.BindReactive(signal.Effect) // 重新展开前先确保状态
	st.Open()
	inst.Update()
	opts2 := findAllByRole(inst.Root(), "select-option")
	if len(opts2) == 3 {
		fireClick(t, opts2[2])
		if got := value(); got != "apple" {
			t.Errorf("禁用项不应改变选中, got %q", got)
		}
	}
}

func TestSelectClosedNoMenu(t *testing.T) {
	inst := Select(gxscene.Props{
		"value":    func() any { return "" },
		"options":  []string{"a", "b"},
		"onChange": BindValue(func(string) {}),
		"state":    func() any { return NewPopoverState(false) },
	})
	inst.BindReactive(signal.Effect)
	root := inst.Root()
	if findByRole(root, "select-menu") != nil {
		t.Error("关闭时不应有菜单子树")
	}
	if findByRole(root, "select-trigger") == nil {
		t.Error("关闭时也应有触发器")
	}
}
