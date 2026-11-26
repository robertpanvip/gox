package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func treeTestModel() *Model {
	m := New("/workspace/gox/cmd/gox-ide", true, nil)
	m.width = 100
	m.height = 30
	m.focus = 1
	return m
}

func TestGlyphFor(t *testing.T) {
	cases := []struct {
		n        treeNode
		glyph    string
		styleKey string
	}{
		{treeNode{Name: "internal", Dir: true, Open: true}, "▾", "folder"},
		{treeNode{Name: "internal", Dir: true, Open: false}, "▸", "folder"},
		{treeNode{Name: "main.go"}, "◆", "go"},
		{treeNode{Name: "model_test.go"}, "◈", "test"},
		{treeNode{Name: "app.gx"}, "◇", "gx"},
		{treeNode{Name: "README.md"}, "▤", "md"},
		{treeNode{Name: "go.mod"}, "◇", "conf"},
		{treeNode{Name: "build.sh"}, "▪", "script"},
		{treeNode{Name: "LICENSE"}, "·", "file"},
	}
	for _, c := range cases {
		g, k := glyphFor(c.n)
		if g != c.glyph || k != c.styleKey {
			t.Errorf("glyphFor(%s): got (%s,%s), want (%s,%s)", c.n.Name, g, k, c.glyph, c.styleKey)
		}
	}
}

func TestNodeAtAndInTreeX(t *testing.T) {
	m := treeTestModel()
	// header 在 y=3，node[0] 在 y=4。
	if idx, ok := m.nodeAt(4); !ok || idx != 0 {
		t.Errorf("nodeAt(4) = %d,%v; want 0,true", idx, ok)
	}
	if idx, ok := m.nodeAt(6); !ok || idx != 2 {
		t.Errorf("nodeAt(6) = %d,%v; want 2,true", idx, ok)
	}
	if _, ok := m.nodeAt(3); ok {
		t.Error("nodeAt(3) 落在 header，应 false")
	}
	// x：左边框1 + padding1 → x=2 起，到 treeW 结束。
	if !m.inTreeX(2) || !m.inTreeX(m.treeWidth()) {
		t.Error("文件树内容区应可点击")
	}
	if m.inTreeX(m.treeWidth() + 3) {
		t.Error("编辑区不应被判为文件树")
	}
}

func TestTreeClickTogglesDir(t *testing.T) {
	m := treeTestModel()
	nodes := m.tree.nodes()
	// node[0] 是根目录 gox-ide（dir，展开）。点它应折叠。
	clickY := 4
	wasOpen := nodes[0].Open
	m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 5, Y: clickY})
	after := m.tree.nodes()
	if after[0].Open == wasOpen {
		t.Error("点击目录应切换展开/折叠")
	}
	if m.treeSel != 0 {
		t.Errorf("点击应设置 treeSel=0, got %d", m.treeSel)
	}
	if m.focus != 0 {
		t.Errorf("点击目录后焦点应在文件树, got %d", m.focus)
	}
}

func TestTreeClickOpensFile(t *testing.T) {
	m := treeTestModel()
	nodes := m.tree.nodes()
	// 找第一个文件节点的可见下标。
	fileIdx := -1
	for i, n := range nodes {
		if !n.Dir {
			fileIdx = i
			break
		}
	}
	if fileIdx < 0 {
		t.Fatal("没有文件节点")
	}
	path := nodes[fileIdx].Path
	clickY := 4 + fileIdx
	m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 6, Y: clickY})
	if m.focus != 1 {
		t.Errorf("点击文件后焦点应在编辑区, got %d", m.focus)
	}
	// 文件应被打开为标签。
	found := false
	for _, b := range m.buffers {
		if b.Path == path {
			found = true
		}
	}
	if !found {
		t.Errorf("点击的文件未被打开: %s", path)
	}
}

func TestTreeHoverHighlights(t *testing.T) {
	m := treeTestModel()
	m.handleMouse(tea.MouseMsg{Action: tea.MouseActionMotion, X: 5, Y: 6})
	if m.treeSel != 2 {
		t.Errorf("悬停应设置 treeSel=2, got %d", m.treeSel)
	}
	// hover 不改变焦点。
	if m.focus != 1 {
		t.Errorf("hover 不应改变焦点, got %d", m.focus)
	}
}

func TestTreeIconsRendered(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)
	m := treeTestModel()
	v := m.View()
	// 强制彩色后，图标应带 SGR 前景色；且目录展开箭头与 go 文件图标应出现。
	for _, g := range []string{"▾", "◆"} {
		if !strings.Contains(v, g) {
			t.Errorf("渲染结果缺少图标 %q", g)
		}
	}
	if !strings.Contains(v, "\x1b[38;2;") && !strings.Contains(v, "\x1b[38;5;") {
		t.Error("图标应带彩色 SGR，渲染结果无前景色序列")
	}
}
