package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/robertpanvip/gox-tui/internal/theme"
)

// ---- 合成按键工具：字符串 → tea.KeyMsg，与真实终端按键一一对应 ----

var specialKeys = map[string]tea.KeyType{
	"enter":      tea.KeyEnter,
	"esc":        tea.KeyEsc,
	"tab":        tea.KeyTab,
	"backspace":  tea.KeyBackspace,
	"delete":     tea.KeyDelete,
	"up":         tea.KeyUp,
	"down":       tea.KeyDown,
	"left":       tea.KeyLeft,
	"right":      tea.KeyRight,
	"home":       tea.KeyHome,
	"end":        tea.KeyEnd,
	"pgup":       tea.KeyPgUp,
	"pgdown":     tea.KeyPgDown,
	"space":      tea.KeySpace,
	"f12":        tea.KeyF12,
	"ctrl+c":     tea.KeyCtrlC,
	"ctrl+s":     tea.KeyCtrlS,
	"ctrl+t":     tea.KeyCtrlT,
	"ctrl+f":     tea.KeyCtrlF,
	"ctrl+p":     tea.KeyCtrlP,
	"ctrl+w":     tea.KeyCtrlW,
	"ctrl+k":     tea.KeyCtrlK,
	"ctrl+a":     tea.KeyCtrlA,
	"ctrl+d":     tea.KeyCtrlD,
	"ctrl+e":     tea.KeyCtrlE,
	"ctrl+up":    tea.KeyCtrlUp,
	"ctrl+down":  tea.KeyCtrlDown,
	"ctrl+left":  tea.KeyCtrlLeft,
	"ctrl+right": tea.KeyCtrlRight,
}

func keyMsg(s string) tea.KeyMsg {
	if t, ok := specialKeys[s]; ok {
		return tea.KeyMsg{Type: t}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// sendKey 向 Model 注入一个按键并返回更新后的 Model。
func sendKey(m *Model, s string) *Model {
	next, _ := m.Update(keyMsg(s))
	return next.(*Model)
}

func sendRune(m *Model, r rune) *Model {
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	return next.(*Model)
}

// ---- 全局快捷键矩阵 ----

func TestGlobalShortcutsMatrix(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	os.WriteFile(src, []byte("package main\n"), 0o644)
	m := New(dir, true, []string{src})
	m.width, m.height = 100, 30

	// ctrl+t：主题切换
	before := m.theme.Mode
	m = sendKey(m, "ctrl+t")
	if m.theme.Mode == before {
		t.Error("ctrl+t 应切换主题模式")
	}
	if m.statusMsg != "主题已切换" {
		t.Errorf("ctrl+t 状态提示 = %q", m.statusMsg)
	}

	// ctrl+f / ctrl+p：浮层打开
	m = sendKey(m, "ctrl+f")
	if m.ov != ovSearch {
		t.Errorf("ctrl+f 后 overlay = %v，应为 ovSearch", m.ov)
	}
	m = sendKey(m, "esc")
	if m.ov != ovNone {
		t.Error("esc 应关闭浮层")
	}
	m = sendKey(m, "ctrl+p")
	if m.ov != ovOpen {
		t.Errorf("ctrl+p 后 overlay = %v，应为 ovOpen", m.ov)
	}
	m = sendKey(m, "esc")

	// ctrl+w：关闭标签（至少保留一个）
	m = sendKey(m, "ctrl+p") // 先加一个标签
	m = sendKey(m, "esc")
	file2 := filepath.Join(dir, "other.go")
	os.WriteFile(file2, []byte("package main\n"), 0o644)
	m2 := New(dir, true, []string{src, file2})
	m2.width, m2.height = 100, 30
	if len(m2.buffers) != 2 {
		t.Fatalf("初始标签数 = %d", len(m2.buffers))
	}
	m2 = sendKey(m2, "ctrl+w")
	if len(m2.buffers) != 1 || m2.active != 0 {
		t.Errorf("ctrl+w 后 buffers=%d active=%d，应为 1/0", len(m2.buffers), m2.active)
	}
	// 只剩一个标签时再 ctrl+w：不得关闭
	m2 = sendKey(m2, "ctrl+w")
	if len(m2.buffers) != 1 {
		t.Errorf("仅剩一个标签时 ctrl+w 不应关闭，buffers=%d", len(m2.buffers))
	}

	// ctrl+tab：循环切换标签（两种真实编码都必须生效）
	m3 := New(dir, true, []string{src, file2})
	m3.width, m3.height = 100, 30
	if m3.active != 1 {
		t.Fatalf("初始 active=%d，应为 1（最后打开的文件）", m3.active)
	}
	// 编码一：历史写法 "ctrl+tab"（runes 合成路径）。1 → 0
	m3 = sendKey(m3, "ctrl+tab")
	if m3.active != 0 {
		t.Errorf("ctrl+tab（runes 编码）后 active=%d，应为 0", m3.active)
	}
	// 编码二：真实终端路径——多数终端把 Ctrl+Tab 发为 \x1b\t，
	// bubbletea 解析为 alt+tab（bubbletea 没有 KeyCtrlTab，String() 不带 ctrl）。
	next, _ := m3.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("alt+tab")})
	if m3c := next.(*Model); m3c.active != 1 {
		t.Errorf("alt+tab（真实终端 Ctrl+Tab 编码）后 active=%d，应为 1——该编码曾不被处理", m3c.active)
	}
	m3 = sendKey(m3, "ctrl+tab")
	if m3.active != 0 {
		t.Errorf("再次 ctrl+tab 后 active=%d，应循环回 0", m3.active)
	}

	// ctrl+c：无条件请求退出
	if _, cmd := m3.Update(keyMsg("ctrl+c")); cmd == nil {
		t.Error("ctrl+c 应返回退出命令")
	}

	// ctrl+s：保存到磁盘
	m4 := New(dir, true, []string{src})
	m4.width, m4.height = 100, 30
	m4 = sendRune(m4, 'x') // 制造修改
	if !m4.Current().Dirty {
		t.Fatal("输入后应标记 Dirty")
	}
	m4 = sendKey(m4, "ctrl+s")
	if m4.Current().Dirty {
		t.Error("ctrl+s 后不应再是 Dirty")
	}
	got, _ := os.ReadFile(src)
	if string(got) != "xpackage main\n" {
		t.Errorf("保存内容 = %q，应为 %q", got, "xpackage main\n")
	}
	// 只读标签禁止保存（readOnly 由 openStdFull 设置，这里直接注入模拟）
	m4.readOnly[0] = true
	m4 = sendRune(m4, 'y') // 只读下输入应被拒绝
	if m4.Current().Dirty {
		t.Error("只读标签不应接受输入")
	}

	// 未命名文件 ctrl+s：提示而不写盘
	m5 := New("", true, nil)
	m5.width, m5.height = 100, 30
	m5 = sendKey(m5, "ctrl+s")
	if m5.statusMsg != "未命名文件，请用终端指定路径" {
		t.Errorf("未命名保存提示 = %q", m5.statusMsg)
	}
}

func TestEscQuitConfirmFlow(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	os.WriteFile(src, []byte("package main\n"), 0o644)
	m := New(dir, true, []string{src})
	m.width, m.height = 100, 30

	// 无修改：esc 直接退出
	if _, cmd := m.Update(keyMsg("esc")); cmd == nil {
		t.Error("无未保存修改时 esc 应退出")
	}

	// 有修改：第一次 esc 只警告，第二次才退出
	m = sendRune(m, 'z')
	if _, cmd := m.Update(keyMsg("esc")); cmd != nil {
		t.Error("有未保存修改时第一次 esc 不应退出")
	}
	if !m.quitConfirm {
		t.Error("第一次 esc 后应进入退出确认态")
	}
	if _, cmd := m.Update(keyMsg("esc")); cmd == nil {
		t.Error("第二次 esc 应退出")
	}

	// 确认态下按其他键应解除确认
	m = sendRune(m, 'z')
	m.Update(keyMsg("esc"))
	m = sendRune(m, 'z')
	if m.quitConfirm {
		t.Error("确认态下按其他键应解除 quitConfirm")
	}
}

func TestTabFocusToggleAndEscInTree(t *testing.T) {
	dir := t.TempDir()
	m := New(dir, true, nil)
	m.width, m.height = 100, 30

	m = sendKey(m, "tab")
	if m.focus != 0 {
		t.Errorf("tab 后 focus=%d，应为 0（文件树）", m.focus)
	}
	m = sendKey(m, "tab")
	if m.focus != 1 {
		t.Errorf("再 tab 后 focus=%d，应为 1（编辑区）", m.focus)
	}
	m = sendKey(m, "tab")
	m = sendKey(m, "esc")
	if m.focus != 1 {
		t.Errorf("文件树中按 esc 应回编辑区，focus=%d", m.focus)
	}
}

// ---- 文件树按键矩阵（focus=0）----

func TestTreeKeysMatrix(t *testing.T) {
	m := treeTestModel(t)
	m.focus = 0
	nodes := m.tree.nodes()
	if len(nodes) < 2 {
		t.Fatalf("夹具节点不足: %d", len(nodes))
	}

	// down / j 越界钳取
	m = sendKey(m, "down")
	m = sendKey(m, "down")
	m = sendKey(m, "down")
	m = sendKey(m, "down")
	if m.treeSel > len(nodes)-1 {
		t.Errorf("down 越界：treeSel=%d nodes=%d", m.treeSel, len(nodes))
	}
	// up / k 回到 0
	for i := 0; i < len(nodes)+2; i++ {
		m = sendKey(m, "up")
	}
	if m.treeSel != 0 {
		t.Errorf("up 多次后 treeSel=%d，应为 0", m.treeSel)
	}
	// vim 键等价
	m = sendKey(m, "j")
	if m.treeSel != 1 {
		t.Errorf("j 后 treeSel=%d，应为 1", m.treeSel)
	}
	m = sendKey(m, "k")
	if m.treeSel != 0 {
		t.Errorf("k 后 treeSel=%d，应为 0", m.treeSel)
	}

	// right/l：展开折叠的目录（nodes[0] 是根目录）
	m.tree.toggle(nodes[0].Path) // 先折叠
	m = sendKey(m, "right")
	if len(m.tree.nodes()) <= 1 {
		t.Error("right/l 应展开折叠的目录")
	}
	// right 对已展开目录：无操作
	m = sendKey(m, "right")
	// left/h：折叠展开的目录
	m = sendKey(m, "left")
	if len(m.tree.nodes()) > 1 {
		t.Error("left/h 应折叠展开的目录（子节点收起后只剩根行）")
	}
	m = sendKey(m, "right") // 恢复展开

	// enter 在文件上：打开并切到编辑区
	idx := -1
	for i, n := range m.tree.nodes() {
		if !n.Dir {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("夹具无文件节点")
	}
	m.treeSel = idx
	m = sendKey(m, "enter")
	if m.focus != 1 {
		t.Errorf("enter 打开文件后 focus=%d，应为 1", m.focus)
	}
	if m.Current().Name == "untitled.gx" {
		t.Error("enter 应打开所选文件")
	}

	// enter 在目录上：仅切换展开
	m.focus = 0
	m.treeSel = 0
	rootOpen := m.tree.nodes()[0].Open
	m = sendKey(m, "enter")
	if m.tree.nodes()[0].Open == rootOpen {
		t.Error("enter 在目录上应切换展开状态")
	}
	if m.focus != 0 {
		t.Error("enter 在目录上不应切换焦点")
	}
}

// ---- 编辑区按键矩阵（focus=1）----

func editorTestModel(t *testing.T) *Model {
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	// 尾换行在 rope 中是独立空行：共 3 行
	os.WriteFile(src, []byte("hello world\nsecond line\n"), 0o644)
	m := New(dir, true, []string{src})
	m.width, m.height = 100, 30
	m.focus = 1
	return m
}

func TestEditorKeysMatrix(t *testing.T) {
	m := editorTestModel(t) // "hello world\nsecond line\n"

	// end / home
	m = sendKey(m, "end")
	if _, col := m.Current().Cursor(); col != 11 {
		t.Errorf("end 后 col=%d，应为 11", col)
	}
	m = sendKey(m, "home")
	if _, col := m.Current().Cursor(); col != 0 {
		t.Errorf("home 后 col=%d，应为 0", col)
	}

	// down / up / right / left
	m = sendKey(m, "down")
	if ln, _ := m.Current().Cursor(); ln != 1 {
		t.Errorf("down 后行=%d，应为 1", ln)
	}
	m = sendKey(m, "up")
	m = sendKey(m, "right")
	m = sendKey(m, "right")
	if _, col := m.Current().Cursor(); col != 2 {
		t.Errorf("right×2 后 col=%d，应为 2", col)
	}
	m = sendKey(m, "left")
	if _, col := m.Current().Cursor(); col != 1 {
		t.Errorf("left 后 col=%d，应为 1", col)
	}
	m = sendKey(m, "home") // col=0

	// 文本输入与退格
	m = sendRune(m, 'X')
	if got := m.Current().Line(0); got != "Xhello world" {
		t.Errorf("输入 X 后行内容 = %q", got)
	}
	m = sendKey(m, "backspace")
	if got := m.Current().Line(0); got != "hello world" {
		t.Errorf("backspace 后行内容 = %q", got)
	}

	// 行尾 delete：合并下一行（标准编辑器语义），随后在合并点拆回
	m = sendKey(m, "end")
	m = sendKey(m, "delete")
	if got := m.Current().Line(0); got != "hello worldsecond line" {
		t.Errorf("行尾 delete 应合并下一行，行内容 = %q", got)
	}
	m = sendKey(m, "enter")   // 在 (0,11) 拆回两行，光标 (1,0)
	m = sendKey(m, "ctrl+up") // 回 (0,0)

	// enter 换行（光标在 (0,0)）→ 首行空、原内容下移
	m = sendKey(m, "home")
	m = sendKey(m, "enter")
	if got := m.Current().Line(0); got != "" {
		t.Errorf("enter 后首行 = %q，应为空", got)
	}
	if got := m.Current().Line(1); got != "hello world" {
		t.Errorf("enter 后第二行 = %q", got)
	}
	// backspace 在 (1,0)：并回上一行（注意尾换行在 rope 中是独立空行，
	// 文档为 "hello world"+"second line"+"" 共 3 行）
	m = sendKey(m, "backspace")
	if got := m.Current().Line(0); got != "hello world" || m.Current().LineCount() != 3 {
		t.Errorf("backspace 合并后行内容 = %q 行数=%d", got, m.Current().LineCount())
	}

	// tab：全局设计为切换焦点（focus 1→0→1），编辑器收不到 tab
	// （编辑器的"tab 插入空格"分支只在补全打开时可达）。
	m = sendKey(m, "tab")
	if m.focus != 0 {
		t.Errorf("tab 后 focus=%d，应为 0（切到文件树）", m.focus)
	}
	m = sendKey(m, "tab")
	if m.focus != 1 {
		t.Errorf("再 tab 后 focus=%d，应为 1", m.focus)
	}

	// pgup/pgdown 钳取
	m = sendKey(m, "pgdown")
	if ln, _ := m.Current().Cursor(); ln > m.Current().LineCount()-1 {
		t.Errorf("pgdown 越界：行=%d 行数=%d", ln, m.Current().LineCount())
	}
	m = sendKey(m, "pgup")
	if ln, _ := m.Current().Cursor(); ln < 0 {
		t.Errorf("pgup 越界：行=%d", ln)
	}

	// ctrl+a / ctrl+e 行首行尾（行 "hello world" 长 11）
	m = sendKey(m, "ctrl+a")
	if _, col := m.Current().Cursor(); col != 0 {
		t.Errorf("ctrl+a 后 col=%d", col)
	}
	m = sendKey(m, "ctrl+e")
	if _, col := m.Current().Cursor(); col != 11 {
		t.Errorf("ctrl+e 后 col=%d，应为 11", col)
	}

	// ctrl+d 前删：行尾删除换行符=合并下一行（标准语义），拆回后行首删首字符
	m = sendKey(m, "ctrl+d")
	if got := m.Current().Line(0); got != "hello worldsecond line" {
		t.Errorf("行尾 ctrl+d 应合并下一行，行内容 = %q", got)
	}
	m = sendKey(m, "enter")   // 拆回，光标 (1,0)
	m = sendKey(m, "ctrl+up") // 回 (0,0)
	m = sendKey(m, "ctrl+d")
	if got := m.Current().Line(0); got != "ello world" {
		t.Errorf("行首 ctrl+d 后行内容 = %q，应为 %q", got, "ello world")
	}

	// ctrl+up/down：跳首末行（真实终端 Ctrl+Shift+Up/Down 的可达编码）
	m = sendKey(m, "ctrl+down")
	if ln, _ := m.Current().Cursor(); ln != m.Current().LineCount()-1 {
		t.Errorf("ctrl+down 后行=%d，应为末行 %d", ln, m.Current().LineCount()-1)
	}
	m = sendKey(m, "ctrl+up")
	if ln, _ := m.Current().Cursor(); ln != 0 {
		t.Errorf("ctrl+up 后行=%d，应为 0", ln)
	}

	// ctrl+left 词移动：先回首行行尾，词左移后不越界
	m = sendKey(m, "ctrl+up")
	m = sendKey(m, "ctrl+e")
	m = sendKey(m, "ctrl+left")
	ln, col := m.Current().Cursor()
	if ln != 0 || col < 0 || col >= len([]rune(m.Current().Line(0))) {
		t.Errorf("ctrl+left 后 (%d,%d) 越界（行长 %d）", ln, col, len([]rune(m.Current().Line(0))))
	}
}

// ---- 浮层按键矩阵 ----

func TestOverlaySearchKeysMatrix(t *testing.T) {
	m := editorTestModel(t)
	m = sendKey(m, "ctrl+f")
	if m.ov != ovSearch {
		t.Fatalf("搜索浮层未打开")
	}

	// 输入过滤
	m = sendRune(m, 's')
	if m.query != "s" {
		t.Errorf("query = %q，应为 %q", m.query, "s")
	}
	if len(m.matches) == 0 {
		t.Error("输入 s 应匹配到 second line")
	}
	// CJK 查询（bug 回归：多字节 rune 曾无法输入）
	m = sendKey(m, "esc")
	m = sendKey(m, "ctrl+f")
	m = sendRune(m, '中')
	if m.query != "中" {
		t.Errorf("搜索框 CJK 输入失败 query = %q", m.query)
	}
	m = sendKey(m, "backspace")
	if m.query != "" {
		t.Errorf("backspace 后 query = %q", m.query)
	}

	// up/down 选择与钳取
	m = sendKey(m, "esc")
	m = sendKey(m, "ctrl+f")
	m = sendRune(m, 'l') // hello / second line 都含 l
	if len(m.matches) < 2 {
		t.Fatalf("匹配数 %d 不足 2", len(m.matches))
	}
	m = sendKey(m, "down")
	if m.matchSel != 1 {
		t.Errorf("down 后 matchSel=%d，应为 1", m.matchSel)
	}
	m = sendKey(m, "down")
	if m.matchSel != len(m.matches)-1 {
		t.Errorf("down 越界钳取失败 matchSel=%d", m.matchSel)
	}
	m = sendKey(m, "up")
	if m.matchSel != len(m.matches)-2 {
		t.Errorf("up 后 matchSel=%d", m.matchSel)
	}

	// enter 跳转：解析行号并移动光标
	m = sendKey(m, "esc")
	m = sendKey(m, "ctrl+f")
	m = sendRune(m, 's') // 选中 "second line" 行
	m.matchSel = len(m.matches) - 1
	for i, mt := range m.matches {
		if containsLn(mt, 2) {
			m.matchSel = i
			break
		}
	}
	m = sendKey(m, "enter")
	if m.ov != ovNone {
		t.Error("enter 后浮层应关闭")
	}
	// 跳转后光标应落在目标行（second line 是第 2 行，0 基 1）
	if ln, _ := m.Current().Cursor(); ln != 1 {
		t.Errorf("enter 搜索跳转后光标行=%d，应为 1", ln)
	}
	m = sendKey(m, "esc")
}

func containsLn(s string, n int) bool {
	// fmtLine 产出 "NN  内容"，前缀 padNum(n)
	want := padNum(n)
	return len(s) >= len(want) && s[:len(want)] == want
}

func TestOverlayOpenKeysMatrix(t *testing.T) {
	m := treeTestModel(t)
	m = sendKey(m, "ctrl+p")
	if m.ov != ovOpen {
		t.Fatalf("快速打开浮层未打开")
	}
	if len(m.matches) == 0 {
		t.Fatal("空查询应列出项目文件")
	}
	// 过滤
	m = sendRune(m, 'm') // main.go / app_test.go 里 main.go 匹配路径 "m"
	if len(m.matches) == 0 {
		t.Fatal("查询 m 应有匹配")
	}
	for _, mt := range m.matches {
		if filepath.Base(mt) == "app_test.go" {
			t.Errorf("查询 m 不应匹配 app_test.go: %v", m.matches)
		}
	}
	// enter 打开选中文件
	m.matchSel = 0
	target := m.matches[0]
	m = sendKey(m, "enter")
	if m.ov != ovNone {
		t.Error("enter 后浮层应关闭")
	}
	if m.focus != 1 {
		t.Error("enter 打开文件后应切到编辑区")
	}
	if !filepath.IsAbs(m.Current().Path) {
		// 打开的是 fixture 文件
		_ = target
	}
}

// ---- 状态不变量：脚本化事件流后逐一断言 ----

func TestUpdateInvariantsScript(t *testing.T) {
	m := treeTestModel(t)
	keys := []string{
		"down", "down", "enter", "tab", "up", "k", "j", "left", "right",
		"enter", "esc", "ctrl+w", "ctrl+tab", "tab", "down", "ctrl+tab",
		"x", "中", "backspace", "enter", "ctrl+f", "s", "esc", "ctrl+p",
		"down", "enter", "f12", "ctrl+k", "ctrl+t", "home", "end",
		"pgup", "pgdown", "delete", "ctrl+d",
	}
	for i, k := range keys {
		m = sendKey(m, k)
		if m.active < 0 || m.active >= len(m.buffers) {
			t.Fatalf("步 %d（%s）：active=%d 越界 buffers=%d", i, k, m.active, len(m.buffers))
		}
		if m.focus != 0 && m.focus != 1 {
			t.Fatalf("步 %d（%s）：focus=%d 非法", i, k, m.focus)
		}
		nodes := m.tree.nodes()
		if len(nodes) > 0 && (m.treeSel < 0 || m.treeSel >= len(nodes)) {
			t.Fatalf("步 %d（%s）：treeSel=%d 越界 nodes=%d", i, k, m.treeSel, len(nodes))
		}
		b := m.Current()
		ln, col := b.Cursor()
		if ln < 0 || ln >= b.LineCount() {
			t.Fatalf("步 %d（%s）：光标行 %d 越界行数 %d", i, k, ln, b.LineCount())
		}
		if col < 0 || col > len([]rune(b.Line(ln))) {
			t.Fatalf("步 %d（%s）：光标列 %d 越界（行长 %d）", i, k, col, len([]rune(b.Line(ln))))
		}
		if m.scrollLn < 0 {
			t.Fatalf("步 %d（%s）：scrollLn=%d < 0", i, k, m.scrollLn)
		}
	}
	if m.theme == nil || (m.theme.Mode != theme.Dark && m.theme.Mode != theme.Light) {
		t.Error("主题状态非法")
	}
}
