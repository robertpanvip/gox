package widgets

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

func TestTabsSwitchAndPanels(t *testing.T) {
	active := signal.New("a")
	panelA := func() any { return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		el("span"), gxscene.Op{Code: gxscene.OpChildText, Arg: "PANEL-A"}, closeEl(),
	}}) }
	panelB := func() any { return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		el("span"), gxscene.Op{Code: gxscene.OpChildText, Arg: "PANEL-B"}, closeEl(),
	}}) }
	inst := Tabs(gxscene.Props{
		"value":    func() any { return active() },
		"tabs":     []Option{{Label: "甲", Value: "a"}, {Label: "乙", Value: "b"}},
		"onChange": BindValue(func(v string) { active(v) }),
		"panels":   []func() any{panelA, panelB},
	})
	inst.BindReactive(signal.Effect)
	root := inst.Root()

	tabs := findAllByRole(root, "tab")
	if len(tabs) != 2 {
		t.Fatalf("应有 2 个 tab, got %d", len(tabs))
	}
	txt := gxscene.TextBackend{}.Paint(root)
	if !containsStr(txt, "PANEL-A") || containsStr(txt, "PANEL-B") {
		t.Errorf("初始应显示 PANEL-A: %q", txt)
	}

	// 点击第二个 tab → 切到 b → panel B。
	fireClick(t, tabs[1])
	if got := active(); got != "b" {
		t.Fatalf("切换后 active=%q, want b", got)
	}
	inst.Update()
	txt = gxscene.TextBackend{}.Paint(inst.Root())
	if !containsStr(txt, "PANEL-B") || containsStr(txt, "PANEL-A") {
		t.Errorf("切换后应显示 PANEL-B: %q", txt)
	}
}

func TestSegmented(t *testing.T) {
	sel := signal.New("day")
	inst := Segmented(gxscene.Props{
		"value": func() any { return sel() },
		"segments": []Option{
			{Label: "日", Value: "day"},
			{Label: "周", Value: "week"},
		},
		"onChange": BindValue(func(v string) { sel(v) }),
	})
	root := inst.Root()
	seg := findByRole(root, "segmented")
	if seg == nil {
		t.Fatal("缺少 segmented 节点")
	}
	items := findAllByRole(root, "tab")
	if len(items) != 2 {
		t.Fatalf("应有 2 个分段, got %d", len(items))
	}
	fireClick(t, items[1])
	if got := sel(); got != "week" {
		t.Fatalf("点击周后 = %q, want week", got)
	}
}

func TestPanelsFrom(t *testing.T) {
	f := func() any { return nil }
	p := panelsFrom(gxscene.Props{"panels": []func() any{f, f}}, "panels")
	if len(p) != 2 {
		t.Fatalf("[]func() any 应有 2 项, got %d", len(p))
	}
	p2 := panelsFrom(gxscene.Props{"panels": func() []func() any { return []func() any{f} }}, "panels")
	if len(p2) != 1 {
		t.Fatalf("func() []func() any 应有 1 项, got %d", len(p2))
	}
}
