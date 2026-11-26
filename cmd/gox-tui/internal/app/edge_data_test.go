package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// ---- 边界数据：各类"奇怪文件"打开后必须可渲染、可交互、不 panic ----

func TestEdgeDataFilesOpenAndRender(t *testing.T) {
	old := setTrueColor(t)
	defer old()

	cases := []struct {
		name    string
		content string
	}{
		{"empty.go", ""},
		{"only_newline.go", "\n"},
		{"crlf.go", "package main\r\nfunc f() {}\r\n"},
		{"cjk.go", "package main\n// 中文注释：响应式信号\nlet 值 = 0\n"},
		{"emoji.go", "package main\n// 🐹🔥✅ emoji 行\nlet x = 1\n"},
		{"long_line.go", "package main\n// " + strings.Repeat("长行内容", 20000) + "\n"},
		{"binary.bin", "\x00\x01\x02\xff\xfe\x00binary\x00"},
		{"invalid_utf8.go", "package main\n// \xff\xfe broken utf8\n"},
		{"no_trailing_newline.go", "package main\nfunc f() {}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, tc.name)
			os.WriteFile(src, []byte(tc.content), 0o644)
			m := New(dir, true, []string{src})
			m.width, m.height = 80, 24
			m.focus = 1

			// 渲染：帧不得超终端
			assertFrameFits(t, m)

			// 交互：移动 + 编辑 + 保存，全程不 panic、状态合法
			m = sendKey(m, "down")
			m = sendKey(m, "end")
			m = sendRune(m, 'X')
			m = sendKey(m, "backspace")
			m = sendKey(m, "home")
			m = sendKey(m, "ctrl+s")
			assertFrameFits(t, m)
			ln, col := m.Current().Cursor()
			if ln < 0 || ln >= m.Current().LineCount() {
				t.Errorf("光标行 %d 越界（行数 %d）", ln, m.Current().LineCount())
			}
			if col < 0 {
				t.Errorf("光标列 %d < 0", col)
			}
		})
	}
}

// 无权限目录：文件树与浮层不得崩。
func TestUnreadableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root 下 chmod 000 不产生无权限效果")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	os.MkdirAll(locked, 0o755)
	os.WriteFile(filepath.Join(locked, "secret.go"), []byte("x"), 0o644)
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Skip("chmod 不支持")
	}
	defer os.Chmod(locked, 0o755)

	m := New(dir, true, nil)
	m.width, m.height = 80, 24
	m = sendKey(m, "ctrl+p")
	m = sendRune(m, 's')
	m = sendKey(m, "enter")
	if v := m.View(); v == "" {
		t.Error("无权限目录下 View 不应为空")
	}
}

// 混合换行 + CJK + emoji 的 300 行文档：连续缩放渲染帧始终合规。
func TestFrameFitsAcrossResize(t *testing.T) {
	old := setTrueColor(t)
	defer old()

	dir := t.TempDir()
	var sb strings.Builder
	for i := 0; i < 300; i++ {
		sb.WriteString("func f() { // 注释 🐹 CJK宽字符测试行\n")
	}
	src := filepath.Join(dir, "big.gx")
	os.WriteFile(src, []byte(sb.String()), 0o644)
	m := New(dir, true, []string{src})
	m.focus = 1

	for _, size := range [][2]int{{120, 40}, {80, 24}, {40, 14}, {20, 8}, {200, 60}, {80, 24}} {
		next, _ := m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m = next.(*Model)
		// 缩放后滚到底再回顶，光标可见性逻辑在每个尺寸下都要成立
		m = sendKey(m, "ctrl+shift+down")
		assertFrameFits(t, m)
		m = sendKey(m, "ctrl+shift+up")
		assertFrameFits(t, m)
	}
}

func assertFrameFits(t *testing.T, m *Model) {
	t.Helper()
	v := m.View()
	lines := strings.Split(v, "\n")
	if len(lines) > m.height {
		t.Errorf("整帧 %d 行超过终端高度 %d（会滚动→闪动）", len(lines), m.height)
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w > m.width {
			t.Errorf("第 %d 行显示宽度 %d 超过终端宽度 %d（会折行→闪动）", i, w, m.width)
		}
	}
}

// setTrueColor 固定颜色配置（渲染测试需要确定性输出），返回恢复函数。
func setTrueColor(t *testing.T) func() {
	t.Helper()
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	return func() { lipgloss.SetColorProfile(old) }
}
