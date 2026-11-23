package widgets

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

func tipContent() *gxscene.Instance {
	return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpChildText, Arg: "提示内容"},
		{Code: gxscene.OpClose},
	}})
}

func TestTooltipClosedByDefault(t *testing.T) {
	st := NewPopoverState(false)
	tp := Tooltip(gxscene.Props{
		"state":   st,
		"trigger": func() any { return triggerButton() },
		"content": func() any { return tipContent() },
	})
	root := tp.Root()
	if findByGxRole(root, "tooltip-anchor") == nil {
		t.Fatal("缺少 tooltip-anchor 锚点")
	}
	if findByGxRole(root, "tooltip") != nil {
		t.Error("关闭态不应有 tooltip 浮层")
	}
	// 无 scrim（与 Popover 的关键差异）
	if findByGxRole(root, "scrim") != nil {
		t.Error("Tooltip 不应有 scrim 遮罩")
	}
}

func TestTooltipHoverOpensAndCloses(t *testing.T) {
	st := NewPopoverState(false)
	tp := Tooltip(gxscene.Props{
		"state":     st,
		"placement": "bottom",
		"trigger":   func() any { return triggerButton() },
		"content":   func() any { return tipContent() },
	})
	tp.BindReactive(signal.Effect)
	root := tp.Root()

	anchor := findByGxRole(root, "tooltip-anchor")
	if anchor == nil {
		t.Fatal("缺少锚点")
	}
	// hover enter → 打开
	var enter *gxscene.Node
	var walk func(*gxscene.Node)
	walk = func(n *gxscene.Node) {
		if n.Handler("onMouseEnter") != nil {
			enter = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	if enter == nil {
		t.Fatal("trigger 包装未绑定 onMouseEnter")
	}
	enter.Dispatch("onMouseEnter", gxscene.EventContext{})
	if !st.IsOpen() {
		t.Error("mouseEnter 后状态应打开")
	}

	// 重新接线使 Branch 解析浮层子树（signal 写触发 effect 自动重跑；单测显式刷新）。
	tp.BindReactive(signal.Effect)
	root = tp.Root()
	tip := findByGxRole(root, "tooltip")
	if tip == nil {
		t.Fatal("打开后应有 tooltip 浮层")
	}
	if tip.Props["gx-placement"] != "bottom" {
		t.Errorf("placement=%v want bottom", tip.Props["gx-placement"])
	}
	aid, _ := findByGxRole(root, "tooltip-anchor").Props["gx-anchor-id"].(string)
	if ref, _ := tip.Props["gx-anchor"].(string); ref != aid {
		t.Errorf("gx-anchor=%q want %q", ref, aid)
	}

	// hover leave → 关闭
	var leave *gxscene.Node
	walk = func(n *gxscene.Node) {
		if n.Handler("onMouseLeave") != nil {
			leave = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	leave = nil
	walk(tp.Root())
	if leave == nil {
		t.Fatal("trigger 包装未绑定 onMouseLeave")
	}
	leave.Dispatch("onMouseLeave", gxscene.EventContext{})
	if st.IsOpen() {
		t.Error("mouseLeave 后状态应关闭")
	}
}
