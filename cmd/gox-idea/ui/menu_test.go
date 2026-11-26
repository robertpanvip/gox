package ui

import (
	"strings"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// rendered 构造场景并接线响应式（单测里 Branch/浮层需要手动 BindReactive，
// 与 widgets/popover_test.go 同款处理；app.Render 会自动做这一步）。
func rendered() *gxscene.Node {
	inst := Playground()
	inst.BindReactive(signal.Effect)
	return inst.Root()
}

// treeContainsText 递归查找文本节点是否包含子串（菜单挂载断言用）。
func treeContainsText(n *gxscene.Node, sub string) bool {
	if n == nil {
		return false
	}
	if n.Type == gxscene.NodeText && strings.Contains(n.Text, sub) {
		return true
	}
	for _, c := range n.Children {
		if treeContainsText(c, sub) {
			return true
		}
	}
	return false
}

// TestNewFileResetsEditor File 菜单第一项必须真能建新文件骨架。
func TestNewFileResetsEditor(t *testing.T) {
	NewFile()
	txt := EditorText()
	if !strings.Contains(txt, "package main") || !strings.Contains(txt, "func main") {
		t.Errorf("New File 应插入可运行骨架，实际: %q", txt)
	}
}

// TestMenuPopupMountOnOpen File 菜单开合必须动态挂载/卸载浮层子树。
func TestMenuPopupMountOnOpen(t *testing.T) {
	SetMenu("")
	if treeContainsText(rendered(), "New File") {
		t.Fatal("未开菜单时树里不应有菜单项")
	}
	SetMenu("File")
	if !treeContainsText(rendered(), "New File") {
		t.Error("打开 File 后应出现 New File 菜单项")
	}
	if !treeContainsText(rendered(), "Next Example") {
		t.Error("打开 File 后应出现 Next Example 菜单项")
	}
	SetMenu("")
	if treeContainsText(rendered(), "New File") {
		t.Error("关闭后菜单项应卸载")
	}
}

// TestMenuMutualExclusionAndRunItems 同时只开一个菜单；Run 项绑真实命令。
func TestMenuMutualExclusionAndRunItems(t *testing.T) {
	SetMenu("File")
	SetMenu("Run")
	v := rendered()
	if !treeContainsText(v, "Clear Console") {
		t.Error("Run 菜单应含 Clear Console")
	}
	if treeContainsText(v, "New File") {
		t.Error("切到 Run 后 File 菜单项不应残留（互斥）")
	}
	if MenuName() != "Run" {
		t.Errorf("MenuName 应为 Run, got %q", MenuName())
	}
	SetMenu("")
}
