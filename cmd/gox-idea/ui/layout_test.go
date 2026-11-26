package ui

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// collectProp 递归收集场景树中某个 prop 的所有值（用于验证响应式样式）。
func collectProp(n *gxscene.Node, key string, out *[]any) {
	if n == nil {
		return
	}
	if v, ok := n.Props[key]; ok {
		*out = append(*out, v)
	}
	for _, c := range n.Children {
		collectProp(c, key, out)
	}
}

func TestLayoutProjectWidthClamp(t *testing.T) {
	cases := []struct{ in, want int }{
		{-50, 160},   // 下界
		{100, 160},   // 低于下界
		{260, 260},   // 正常
		{400, 400},   // 正常
		{999, 520},   // 上界
		{10000, 520}, // 超上界
	}
	for _, c := range cases {
		SetLayoutProjectW(c.in)
		if got := LayoutProjectW(); got != c.want {
			t.Errorf("SetLayoutProjectW(%d): got %d, want %d", c.in, got, c.want)
		}
	}
	SetLayoutProjectW(260) // 复位
}

func TestLayoutTerminalHeightClamp(t *testing.T) {
	cases := []struct{ in, want int }{
		{-10, 90},
		{50, 90},
		{180, 180},
		{300, 300},
		{999, 420},
	}
	for _, c := range cases {
		SetLayoutTerminalH(c.in)
		if got := LayoutTerminalH(); got != c.want {
			t.Errorf("SetLayoutTerminalH(%d): got %d, want %d", c.in, got, c.want)
		}
	}
	SetLayoutTerminalH(180) // 复位
}

// TestTerminalViewHeightFollowsSignal 验证终端面板内容区高度随 layoutTerminalH
// 信号变化——这是水平分隔条拖拽能生效的关键（自绘 JSX 让 height 走响应式 style）。
func TestTerminalViewHeightFollowsSignal(t *testing.T) {
	SetLayoutTerminalH(180)
	v1 := TerminalView()
	var h1 []any
	collectProp(v1.Root(), "height", &h1)

	SetLayoutTerminalH(300)
	v2 := TerminalView()
	var h2 []any
	collectProp(v2.Root(), "height", &h2)

	if len(h1) == 0 || len(h2) == 0 {
		t.Fatal("终端视图未找到 height 属性")
	}
	// 内容区高度应从 "180" 变为 "300"。
	found180, found300 := false, false
	for _, v := range h1 {
		if s, _ := v.(string); s == "180" {
			found180 = true
		}
	}
	for _, v := range h2 {
		if s, _ := v.(string); s == "300" {
			found300 = true
		}
	}
	if !found180 {
		t.Errorf("layoutTerminalH=180 时场景树无 height=180，实际: %v", h1)
	}
	if !found300 {
		t.Errorf("layoutTerminalH=300 时场景树无 height=300，实际: %v", h2)
	}
	SetLayoutTerminalH(180) // 复位
}

func TestSplitDraggingToggle(t *testing.T) {
	SetSplitDragging(true)
	if !LayoutSplitActive() {
		t.Error("SetSplitDragging(true) 未置位")
	}
	SetSplitDragging(false)
	if LayoutSplitActive() {
		t.Error("SetSplitDragging(false) 未清除")
	}
}
