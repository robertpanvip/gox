package widgets

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

func menuContent() *gxscene.Instance {
	return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpAttr, Key: "bg", Arg: ThemePanel},
		{Code: gxscene.OpChildText, Arg: "Menu item 1"},
		{Code: gxscene.OpClose},
	}})
}

func triggerButton() *gxscene.Instance {
	return Button(gxscene.Props{"label": "Open", "variant": "primary"})
}

func findByGxRole(n *gxscene.Node, role string) *gxscene.Node {
	if n == nil {
		return nil
	}
	if v, ok := n.Props["gx-role"].(string); ok && v == role {
		return n
	}
	for _, c := range n.Children {
		if h := findByGxRole(c, role); h != nil {
			return h
		}
	}
	return nil
}

func TestPopoverStructure(t *testing.T) {
	state := NewPopoverState(false)
	p := Popover(gxscene.Props{
		"state":      state,
		"placement":  "bottom-end",
		"offset":     "10",
		"width":      "180",
		"trigger":    func() any { return triggerButton() },
		"content":    func() any { return menuContent() },
	})
	root := p.Root()

	anchor := findByGxRole(root, "anchor")
	if anchor == nil {
		t.Fatal("缺少 gx-role=anchor 锚点包装")
	}
	aid, _ := anchor.Props["gx-anchor-id"].(string)
	if aid == "" {
		t.Error("锚点缺少 gx-anchor-id")
	}
	// trigger 按钮应挂在锚点下
	if findKind(anchor, "button") == nil {
		t.Error("trigger 按钮未挂到锚点下")
	}
	// 初始关闭：不应有 popover/scrim
	if findByGxRole(root, "popover") != nil || findByGxRole(root, "scrim") != nil {
		t.Error("初始关闭时不应有浮层/遮罩子树")
	}
}

func TestPopoverOpensAndContent(t *testing.T) {
	state := NewPopoverState(true) // 初始打开
	p := Popover(gxscene.Props{
		"state":     state,
		"placement": "top-start",
		"trigger":   func() any { return triggerButton() },
		"content":   func() any { return menuContent() },
	})
	// Branch 子树在响应式接线时解析（App.Render 会自动做，单测手动触发）。
	p.BindReactive(signal.Effect)
	root := p.Root()

	pop := findByGxRole(root, "popover")
	if pop == nil {
		t.Fatal("打开时应有 gx-role=popover 浮层")
	}
	if v := pop.Props["gx-placement"]; v != "top-start" {
		t.Errorf("placement = %v, want top-start", v)
	}
	if v := pop.Props["gx-offset"]; v != "6" {
		t.Errorf("offset = %v, want 6 (默认)", v)
	}
	aid, _ := findByGxRole(root, "anchor").Props["gx-anchor-id"].(string)
	if ref, _ := pop.Props["gx-anchor"].(string); ref != aid {
		t.Errorf("gx-anchor=%q 应等于锚点 id %q", ref, aid)
	}
	// 遮罩存在且带 onClick
	scrim := findByGxRole(root, "scrim")
	if scrim == nil || scrim.Handler("onClick") == nil {
		t.Error("缺少带 onClick 的 scrim 遮罩")
	}
	// 内容子树挂到了浮层下
	if findKind(pop, "div") == nil {
		t.Error("浮层缺少内容子树")
	}
}

func TestPopoverStateToggle(t *testing.T) {
	s := NewPopoverState(false)
	if s.IsOpen() {
		t.Fatal("初始应为关闭")
	}
	s.Toggle()
	if !s.IsOpen() {
		t.Error("Toggle 后应打开")
	}
	s.Close()
	if s.IsOpen() {
		t.Error("Close 后应关闭")
	}
	s.Set(true)
	if !s.Getter()() {
		t.Error("Set(true) 后 Getter 应为 true")
	}
}
