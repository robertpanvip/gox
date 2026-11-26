package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func mouse(x, y int, action tea.MouseAction, button tea.MouseButton, ctrl bool) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: action, Button: button, Ctrl: ctrl}
}

func sendMouse(m *Model, msg tea.MouseMsg) *Model {
	next, _ := m.Update(msg)
	return next.(*Model)
}

// ---- 文件树点击矩阵 ----

func TestMouseTreeClickMatrix(t *testing.T) {
	m := treeTestModel(t)
	nodes := m.tree.nodes()

	// 点击目录行（index 0，屏幕 Y = headerY 4）：折叠（当前展开）→ focus 保持编辑区
	m = sendMouse(m, mouse(10, 4, tea.MouseActionPress, tea.MouseButtonLeft, false))
	if len(m.tree.nodes()) >= len(nodes) {
		t.Error("点击目录应折叠（子节点收起）")
	}
	if m.focus != 0 {
		t.Errorf("点击目录后 focus=%d，应为 0", m.focus)
	}
	if m.treeSel != 0 {
		t.Errorf("点击目录应选中该行 treeSel=%d", m.treeSel)
	}
	// 再点：展开
	m = sendMouse(m, mouse(10, 4, tea.MouseActionPress, tea.MouseButtonLeft, false))
	if len(m.tree.nodes()) != len(nodes) {
		t.Error("再次点击目录应展开")
	}

	// 悬停（motion）只更新选中，不改焦点、不开关目录
	m.treeSel = 0
	m = sendMouse(m, mouse(10, 4+1, tea.MouseActionMotion, tea.MouseButtonLeft, false))
	if m.treeSel != 1 {
		t.Errorf("悬停应更新 treeSel=%d", m.treeSel)
	}

	// 点击文件行：打开为标签并切到编辑区
	fileIdx := -1
	fileY := -1
	for i, n := range m.tree.nodes() {
		if !n.Dir {
			fileIdx = i
			fileY = 4 + i
			break
		}
	}
	if fileIdx < 0 {
		t.Fatal("夹具无文件节点")
	}
	m = sendMouse(m, mouse(10, fileY, tea.MouseActionPress, tea.MouseButtonLeft, false))
	if m.focus != 1 {
		t.Errorf("点击文件后 focus=%d，应为 1", m.focus)
	}
	if m.Current().Name == "untitled.gx" {
		t.Error("点击文件应打开它")
	}

	// 浮层打开时点击树：不响应
	m = sendKey(m, "ctrl+f")
	m = sendMouse(m, mouse(10, 4, tea.MouseActionPress, tea.MouseButtonLeft, false))
	if m.ov != ovSearch {
		t.Error("浮层打开时鼠标不应影响浮层状态")
	}
}

// ---- 坐标越界：任何输入不得 panic、不得破坏状态 ----

func TestMouseOutOfBoundsNeverPanics(t *testing.T) {
	m := treeTestModel(t)
	m.focus = 1
	cases := []tea.MouseMsg{
		mouse(-1, -1, tea.MouseActionPress, tea.MouseButtonLeft, false),
		mouse(-100, -100, tea.MouseActionMotion, tea.MouseButtonLeft, true),
		mouse(9999, 9999, tea.MouseActionPress, tea.MouseButtonLeft, false),
		mouse(9999, 9999, tea.MouseActionPress, tea.MouseButtonLeft, true),
		mouse(0, 0, tea.MouseActionPress, tea.MouseButtonLeft, false), // 边框
		mouse(1, 1, tea.MouseActionPress, tea.MouseButtonLeft, false), // 标题栏
		mouse(2, 3, tea.MouseActionPress, tea.MouseButtonLeft, false), // body padding 行
		mouse(2, 999, tea.MouseActionPress, tea.MouseButtonLeft, false),
		mouse(5, 2, tea.MouseActionPress, tea.MouseButtonLeft, false), // 代码区上方
		mouse(m.width, m.height, tea.MouseActionPress, tea.MouseButtonLeft, true),
		mouse(0, 0, tea.MouseActionPress, tea.MouseButtonRight, false),
		mouse(0, 0, tea.MouseActionRelease, tea.MouseButtonLeft, false),
		mouse(0, 0, tea.MouseActionPress, tea.MouseButtonWheelLeft, false),
	}
	for i, msg := range cases {
		m2 := sendMouse(m, msg)
		if m2.active < 0 || m2.active >= len(m2.buffers) {
			t.Fatalf("case %d: active 越界", i)
		}
		if m2.treeSel < 0 {
			t.Fatalf("case %d: treeSel=%d < 0", i, m2.treeSel)
		}
	}
}

// ---- 滚轮：编辑区必须可滚动（回归：此前滚轮完全无处理）----

func TestMouseWheelScrollsEditor(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "long.go")
	var body string
	for i := 0; i < 60; i++ {
		body += "line\n"
	}
	os.WriteFile(src, []byte(body), 0o644)
	m := New(dir, true, []string{src})
	m.width, m.height = 100, 30
	if m.scrollLn != 0 {
		t.Fatalf("初始 scrollLn=%d", m.scrollLn)
	}

	// 向下滚：scrollLn 增加
	m = sendMouse(m, mouse(60, 10, tea.MouseActionPress, tea.MouseButtonWheelDown, false))
	if m.scrollLn == 0 {
		t.Error("滚轮向下应滚动编辑区（此前滚轮无任何效果）")
	}
	for i := 0; i < 20; i++ {
		m = sendMouse(m, mouse(60, 10, tea.MouseActionPress, tea.MouseButtonWheelDown, false))
	}
	// 不得滚过头：最多到底
	maxScroll := m.Current().LineCount() - m.viewportHeight()
	if m.scrollLn > maxScroll {
		t.Errorf("滚轮越界 scrollLn=%d max=%d", m.scrollLn, maxScroll)
	}
	// 向上滚：回退且不为负
	for i := 0; i < 30; i++ {
		m = sendMouse(m, mouse(60, 10, tea.MouseActionPress, tea.MouseButtonWheelUp, false))
	}
	if m.scrollLn < 0 {
		t.Errorf("滚轮向上越界 scrollLn=%d", m.scrollLn)
	}
	if m.scrollLn == maxScroll {
		t.Error("滚轮向上应能回滚")
	}
}

// ---- Ctrl+hover / 点击 导航流（WebStorm 风格）----

func TestMouseNavHoverFlow(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.gx")
	os.WriteFile(src, []byte("let count = 0\ncount = count + 1\n"), 0o644)
	m := New(dir, true, []string{src})
	m.width, m.height = 100, 30
	m.focus = 1

	// Ctrl+hover 到 "count"（行 0 "let count = 0"：rune 4 起是 count；
	// 屏幕布局 y=2 是 body 第 0 行，代码区起点 = 1+treeW+1+行号宽）
	codeStart := 1 + m.treeWidth() + 1 + m.lineNoWidth()
	m = sendMouse(m, mouse(codeStart+4, 2, tea.MouseActionMotion, tea.MouseButtonNone, true))
	if m.navHover == nil {
		t.Fatal("Ctrl+hover 应建立导航下划线")
	}
	if m.navHover.Text != "count" {
		t.Errorf("hover 单词 = %q，应为 count", m.navHover.Text)
	}

	// 无 Ctrl 移动：清除下划线
	m = sendMouse(m, mouse(codeStart+3, 2, tea.MouseActionMotion, tea.MouseButtonNone, false))
	if m.navHover != nil {
		t.Error("松开 Ctrl 后移动应清除下划线")
	}

	// 悬停后普通点击（xterm 兼容路径）：触发跳转且不 panic
	m = sendMouse(m, mouse(codeStart+3, 2, tea.MouseActionMotion, tea.MouseButtonNone, true))
	if m.navHover == nil {
		t.Fatal("Ctrl+hover 应再次建立下划线")
	}
	m = sendMouse(m, mouse(codeStart+3, 2, tea.MouseActionPress, tea.MouseButtonLeft, false))

	// 点击非标识符（行 0 的 "=" 附近，col 9）：不应跳转、下划线清除
	m = sendMouse(m, mouse(codeStart+9, 2, tea.MouseActionMotion, tea.MouseButtonNone, true))
	m = sendMouse(m, mouse(codeStart+9, 2, tea.MouseActionPress, tea.MouseButtonLeft, false))

	// 键盘输入清除悬停
	m = sendMouse(m, mouse(codeStart+3, 2, tea.MouseActionMotion, tea.MouseButtonNone, true))
	m = sendRune(m, 'x')
	if m.navHover != nil {
		t.Error("键盘输入应清除导航下划线")
	}
}

// ---- 只读标签下的鼠标导航禁用 ----

func TestMouseNavDisabledOnReadOnly(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.gx")
	os.WriteFile(src, []byte("let count = 0\n"), 0o644)
	m := New(dir, true, []string{src})
	m.width, m.height = 100, 30
	m.focus = 1
	m.readOnly[0] = true

	codeStart := 1 + m.treeWidth() + 1 + m.lineNoWidth()
	m = sendMouse(m, mouse(codeStart+3, 2, tea.MouseActionMotion, tea.MouseButtonNone, true))
	if m.navHover != nil {
		t.Error("只读标签不应建立导航下划线")
	}
}

// ---- 窗口缩放事件 ----

func TestWindowSizeMsg(t *testing.T) {
	m := treeTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	m2 := next.(*Model)
	if m2.width != 60 || m2.height != 20 {
		t.Errorf("缩放后尺寸 = %dx%d，应为 60x20", m2.width, m2.height)
	}
	// 极小尺寸不得 panic，且 View 可渲染
	next, _ = m2.Update(tea.WindowSizeMsg{Width: 10, Height: 3})
	m3 := next.(*Model)
	if v := m3.View(); v == "" {
		t.Error("极小尺寸下 View 不应为空")
	}
}
