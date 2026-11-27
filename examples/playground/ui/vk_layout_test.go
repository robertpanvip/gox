package ui

import (
	"strings"
	"testing"

	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// walkNodes 深度遍历场景树。
func walkNodes(n *gxscene.Node, fn func(*gxscene.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, c := range n.Children {
		walkNodes(c, fn)
	}
}

// TestKeyboardBarRendered 验证紧凑布局在 KeyboardEnabled=true 时渲染虚拟键盘。
func TestKeyboardBarRendered(t *testing.T) {
	SetViewport(393, 730)
	SetKeyboardEnabled(true)
	defer SetKeyboardEnabled(false)

	inst := Playground()
	if inst == nil {
		t.Fatal("Playground() 返回 nil")
	}
	root := inst.Root()
	keys := 0
	rows := 0
	sampleRows := 0
	walkNodes(root, func(n *gxscene.Node) {
		if n.Props == nil {
			return
		}
		cls, _ := n.Props["class"].(string)
		switch {
		case strings.Contains(cls, "gx-vk-key"):
			keys++
			if _, ok := n.Props["data-key"]; !ok {
				t.Errorf("gx-vk-key 缺 data-key: %v", n.Props)
			}
		case strings.Contains(cls, "gx-vk-row"):
			rows++
		case strings.Contains(cls, "gx-sample-row"):
			sampleRows++
		}
	})
	if keys < 20 {
		t.Errorf("虚拟键盘按键数 %d < 20", keys)
	}
	if sampleRows < 15 {
		t.Errorf("示例行数 %d < 15", sampleRows)
	}
}

// TestCompactLayoutByViewport 验证视口断点分派：窄视口走紧凑布局。
func TestCompactLayoutByViewport(t *testing.T) {
	SetViewport(393, 730)
	SetKeyboardEnabled(false)
	root := Playground().Root()
	found := false
	walkNodes(root, func(n *gxscene.Node) {
		if cls, _ := n.Props["class"].(string); strings.Contains(cls, "gx-vk-bar") {
			found = true
		}
	})
	if found {
		t.Error("KeyboardEnabled=false 时不应渲染键盘面板")
	}
	SetViewport(1360, 760)
}
