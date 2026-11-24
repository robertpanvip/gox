package gxscene

import (
	"testing"
)

// 端到端：Style 值经 OpDynAttr getter → applyAll → Node.Props 应被展平为
// 普通属性 + &:hover 伪状态串，且 style 本身不残留在 Props。
func TestStyleFlattenIntoProps(t *testing.T) {
	s := Style{
		Bg:     "#fff",
		Color:  "#000",
		Hover:  &Style{Bg: "#eee"},
		Active: &Style{Bg: "#ddd", Opacity: "0.9"},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpDynAttr, Key: "style", Slot: 0},
		{Code: OpClose},
	}, N: 1}
	inst := Render(tpl, func() any { return s })
	defer inst.Dispose()
	div := inst.Root().Children[0]
	p := div.Props
	if p["bg"] != "#fff" || p["color"] != "#000" {
		t.Errorf("普通属性展平失败: %v", p)
	}
	if _, ok := p["style"]; ok {
		t.Error("style 不应残留在 Props")
	}
	if hov, ok := p["&:hover"]; !ok || hov == "" {
		t.Errorf("伪状态子表未展平: %v", p)
	}
	sub, ok := PseudoStateFromProps(map[string]any(p), "hover")
	if !ok || sub.Bg != "#eee" {
		t.Errorf("回读 hover 子表失败: %+v", sub)
	}
}

// 增量更新：style 从含 hover 的集合变成只含 color，消失的 bg/&:hover 应变空串。
func TestStyleIncrementalCleanup(t *testing.T) {
	var cur any = Style{Bg: "#fff", Hover: &Style{Bg: "#eee"}}
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpDynAttr, Key: "style", Slot: 0},
		{Code: OpClose},
	}, N: 1}
	inst := Render(tpl, func() any { return cur })
	defer inst.Dispose()
	inst.BindReactive(nil)
	div := inst.Root().Children[0]
	p := div.Props
	if p["bg"] != "#fff" {
		t.Fatalf("初始 bg 失败: %v", p)
	}

	cur = Style{Color: "#000"}
	inst.Update()
	p = div.Props
	if p["color"] != "#000" {
		t.Errorf("新 color 失败: %v", p)
	}
	if bg, ok := p["bg"]; !ok || bg != "" {
		t.Errorf("消失的 bg 应变空串: %v", p)
	}
	if hov, ok := p["&:hover"]; !ok || hov != "" {
		t.Errorf("消失的 &:hover 应变空串: %v", p)
	}
}
