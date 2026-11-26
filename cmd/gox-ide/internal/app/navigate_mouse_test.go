package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/robertpanvip/gox-ide/internal/editor"
)

func mouseModel(src string) *Model {
	m := New("", true, nil)
	m.focus = 1
	m.width = 100
	m.height = 30
	m.tree = newFileTree(".")
	m.buffers[0] = editor.New("t.gx", src)
	return m
}

func TestScreenToCodeColASCII(t *testing.T) {
	m := mouseModel("package main\n    local := 1\n")
	ln := 1
	// 代码区起点 = 1 + treeW + 1 + noWidth
	codeStart := 1 + m.treeWidth() + 1 + m.lineNoWidth()
	// "local" 的首个 rune 在代码列 4
	col, ok := m.screenToCodeCol(codeStart+4, ln)
	if !ok || col != 4 {
		t.Fatalf("屏幕列→rune列 = %d,%v; want 4,true (codeStart=%d)", col, ok, codeStart)
	}
	// 文件树区域应拒绝
	if _, ok := m.screenToCodeCol(2, ln); ok {
		t.Fatal("文件树区域不应映射到代码列")
	}
}

func TestScreenToCodeColCJK(t *testing.T) {
	m := mouseModel("中文 abc\n")
	// 行 "中文 abc"：rune 0,1 各占 2 列(0-3)，空格 rune2 占1列(4)，abc 从列5
	codeStart := 1 + m.treeWidth() + 1 + m.lineNoWidth()
	cases := []struct{ dx, wantCol int }{
		{0, 0}, {1, 0}, // 中（宽字符首/中）
		{2, 1}, {3, 1}, // 文
		{4, 2}, // 空格
		{5, 3}, // a
		{7, 5}, // 行尾 c 之后
	}
	for _, c := range cases {
		got, ok := m.screenToCodeCol(codeStart+c.dx, 0)
		if !ok || got != c.wantCol {
			t.Errorf("dx=%d → col=%d,%v; want %d", c.dx, got, ok, c.wantCol)
		}
	}
}

func TestWordSpanAt(t *testing.T) {
	m := mouseModel("    local := local\n")
	// "    local" 的 local 在 rune 列 4..9
	span := m.wordSpanAt(0, 6)
	if span == nil || span.Text != "local" || span.Start != 4 || span.End != 9 {
		t.Fatalf("wordSpan(6) = %+v; want local[4,9)", span)
	}
	// 点末尾也应命中
	span2 := m.wordSpanAt(0, 9)
	if span2 == nil || span2.Text != "local" {
		t.Fatalf("wordSpan(9) = %+v; want local", span2)
	}
	// 点在 ':=' 上不命中
	if got := m.wordSpanAt(0, 10); got != nil {
		t.Fatalf("标点上不应命中, got %+v", got)
	}
	// CJK 单词
	m2 := mouseModel("变量 := 1\n")
	sp := m2.wordSpanAt(0, 0)
	if sp == nil || sp.Text != "变量" || sp.End != 2 {
		t.Fatalf("CJK wordSpan = %+v; want 变量[0,2)", sp)
	}
}

func TestCtrlHoverSetsUnderline(t *testing.T) {
	// lipgloss 在无 TTY 的测试进程里默认关闭 ANSI；强制真彩 profile 以验证下划线序列。
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)

	src := "package main\n    local := 1\n    x := local\n"
	m := mouseModel(src)
	codeStart := 1 + m.treeWidth() + 1 + m.lineNoWidth()
	// hover 到第 2 行 "    x := local" 里的 local（rune 列 10..15）
	msg := tea.MouseMsg{Button: tea.MouseButtonNone, Action: tea.MouseActionMotion,
		Ctrl: true, X: codeStart + 12, Y: 2 + 2}
	m.handleMouse(msg)
	if m.navHover == nil || m.navHover.Text != "local" || m.navHover.Line != 2 {
		t.Fatalf("hover 未命中 local: %+v", m.navHover)
	}
	// 渲染应包含下划线 SGR（4;...m 或 4m）
	view := m.View()
	if !strings.Contains(view, "4;") && !strings.Contains(view, "4m") {
		t.Fatalf("hover 渲染应含下划线序列, view 片段:\n%s", view)
	}
}

func TestInUnderline(t *testing.T) {
	if !inUnderline(10, true, 10, 15) || !inUnderline(14, true, 10, 15) {
		t.Fatal("[10,15) 内应命中")
	}
	if inUnderline(9, true, 10, 15) || inUnderline(15, true, 10, 15) {
		t.Fatal("区间外不应命中")
	}
	if inUnderline(12, false, 10, 15) {
		t.Fatal("未开启下划线时不应命中")
	}
}

func TestCtrlHoverClearedWithoutCtrl(t *testing.T) {
	m := mouseModel("    local := 1\n")
	codeStart := 1 + m.treeWidth() + 1 + m.lineNoWidth()
	m.handleMouse(tea.MouseMsg{Action: tea.MouseActionMotion, Ctrl: true, X: codeStart + 5, Y: 2})
	if m.navHover == nil {
		t.Fatal("Ctrl+hover 应建立下划线")
	}
	m.handleMouse(tea.MouseMsg{Action: tea.MouseActionMotion, Ctrl: false, X: codeStart + 5, Y: 2})
	if m.navHover != nil {
		t.Fatal("无 Ctrl 的移动应清除下划线")
	}
}

func TestCtrlClickJumpsToDefinition(t *testing.T) {
	src := "package main\n\nfunc f() {\n    local := 1\n    x := local\n    _ = x\n}\n"
	m := mouseModel(src)
	codeStart := 1 + m.treeWidth() + 1 + m.lineNoWidth()
	// Ctrl+点击第 4 行 "    x := local" 里的 local（rune 列 10..15，点列 12）
	msg := tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
		Ctrl: true, X: codeStart + 12, Y: 2 + 4}
	m.handleMouse(msg)
	if m.ov != ovNone {
		t.Fatalf("本地跳转不应弹 peek, ov=%v", m.ov)
	}
	ln, _ := m.buffers[0].Cursor()
	if ln != 3 {
		t.Errorf("Ctrl+点击应跳到 local 声明行(0基 3), got %d", ln)
	}
	if m.navHover != nil {
		t.Error("点击后应清除下划线")
	}
}

// xterm 会吞掉 Ctrl+click；验证"Ctrl+悬停出下划线后，普通左键点击同一单词"也能跳转。
func TestHoverThenPlainClickJumps(t *testing.T) {
	src := "package main\n\nfunc f() {\n    local := 1\n    x := local\n    _ = x\n}\n"
	m := mouseModel(src)
	codeStart := 1 + m.treeWidth() + 1 + m.lineNoWidth()
	clickX := codeStart + 12
	clickY := 2 + 4
	// 1) Ctrl+悬停建立下划线
	m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonNone, Action: tea.MouseActionMotion,
		Ctrl: true, X: clickX, Y: clickY})
	if m.navHover == nil || m.navHover.Text != "local" {
		t.Fatalf("悬停未命中 local: %+v", m.navHover)
	}
	// 2) 普通左键点击同一位置（xterm 兼容路径）
	m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
		Ctrl: false, X: clickX, Y: clickY})
	ln, _ := m.buffers[0].Cursor()
	if ln != 3 {
		t.Errorf("悬停后普通点击应跳到声明行 3, got %d", ln)
	}
}

// 无悬停时的普通点击不应误跳转。
func TestPlainClickWithoutHoverIgnored(t *testing.T) {
	src := "package main\n    local := 1\n"
	m := mouseModel(src)
	codeStart := 1 + m.treeWidth() + 1 + m.lineNoWidth()
	m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress,
		Ctrl: false, X: codeStart + 5, Y: 2 + 1})
	ln, _ := m.buffers[0].Cursor()
	if ln != 0 {
		t.Errorf("无悬停的普通点击不应跳光标, got ln=%d", ln)
	}
}
