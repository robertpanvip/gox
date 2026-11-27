package ui

import (
	"strings"
	"testing"
)

// ---- 多标签：打开/切换/关闭 ----

func TestTabOpenAndActivate(t *testing.T) {
	CloseAllTabs()
	OpenTab("a.gx", "/tmp/a.gx", "package a\n")
	if ActiveTabName() != "a.gx" {
		t.Errorf("激活标签应为 a.gx, got %q", ActiveTabName())
	}
	if !strings.Contains(EditorText(), "package a") {
		t.Errorf("编辑器应加载 a.gx 内容, got %q", EditorText())
	}
	OpenTab("b.gx", "/tmp/b.gx", "package b\n")
	if len(TabNames()) != 2 {
		t.Fatalf("应有 2 个标签, got %v", TabNames())
	}
	// 同名 path 重开不追加，只激活。
	OpenTab("a.gx", "/tmp/a.gx", "package a\n")
	if len(TabNames()) != 2 {
		t.Errorf("重复打开不应增加标签, got %v", TabNames())
	}
}

func TestTabSwitchKeepsContent(t *testing.T) {
	CloseAllTabs()
	OpenTab("x.gx", "/tmp/x.gx", "content-x")
	OpenTab("y.gx", "/tmp/y.gx", "content-y")
	// 编辑 x 前切回 y 再切回 x：应保留各自文本。
	SwitchTab("x.gx")
	if !strings.Contains(EditorText(), "content-x") {
		t.Errorf("切回 x 应见 content-x, got %q", EditorText())
	}
	EditorInsert("Z") // x 变成 content-xZ
	SwitchTab("y.gx")
	if !strings.Contains(EditorText(), "content-y") {
		t.Errorf("切到 y 应见 content-y, got %q", EditorText())
	}
	SwitchTab("x.gx")
	// 标签切换后光标归位（与 WebStorm 一致），EditorInsert 在光标处插入。
	if EditorText() != "Zcontent-x" {
		t.Errorf("x 的原内容与编辑都应保留, got %q", EditorText())
	}
}

func TestTabCloseReactivatesNeighbor(t *testing.T) {
	CloseAllTabs()
	OpenTab("p.gx", "/tmp/p.gx", "pp")
	OpenTab("q.gx", "/tmp/q.gx", "qq")
	CloseTab("q.gx")
	if len(TabNames()) != 1 || ActiveTabName() != "p.gx" {
		t.Errorf("关闭激活标签后应回落到邻居, tabs=%v active=%q", TabNames(), ActiveTabName())
	}
	if !strings.Contains(EditorText(), "pp") {
		t.Errorf("关闭 q 后编辑器应显示 p 内容, got %q", EditorText())
	}
	CloseTab("p.gx")
	if len(TabNames()) != 0 {
		t.Errorf("全部关闭后标签应清空, got %v", TabNames())
	}
}

func TestSampleLoadOpensTab(t *testing.T) {
	CloseAllTabs()
	LoadSample(0)
	names := TabNames()
	if len(names) != 1 || !strings.Contains(names[0], "Hello") {
		t.Errorf("loadSample 应打开示例标签, got %v", names)
	}
}

// ---- 底部工具窗标签切换 ----

func TestBottomTabSwitch(t *testing.T) {
	SetBottomTab("Problems")
	if BottomTab() != "Problems" {
		t.Fatalf("底部标签应切到 Problems, got %q", BottomTab())
	}
	root := rendered()
	if !treeContainsText(root, "No problems detected") && !treeContainsText(root, "✗") {
		t.Error("Problems 页应显示问题列表或无问题提示")
	}
	SetBottomTab("Terminal")
	if !treeContainsText(rendered(), "Terminal") {
		t.Error("切回 Terminal 后标签栏应在位")
	}
}

// ---- 项目面板显隐（工具栏 folder 动作）----

func TestProjectPanelToggle(t *testing.T) {
	SetProjectPanelVisible(true)
	if !treeContainsText(rendered(), "Project") {
		t.Fatal("可见时项目面板应在树中")
	}
	SetProjectPanelVisible(false)
	if treeContainsText(rendered(), "Project") {
		t.Error("隐藏后项目面板不应在树中")
	}
	SetProjectPanelVisible(true)
}
