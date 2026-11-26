package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestFrameNeverExceedsTerminal 固化「整帧不超宽、不超高」：
// 任一行显示宽度超过终端宽度、或总行数超过终端高度，都会让终端折行/滚动，
// bubbletea 每次事件触发全屏重绘——表现为点击长文件名后整个编辑区闪动。
// 根因家族：lipgloss Width() 含 padding（Sidebar/TreeItem/TitleBar/Status），
// 行宽按 w 截断而非内容区口径（w-2），超宽行被折行。
func TestFrameNeverExceedsTerminal(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(old)

	for _, tc := range []struct{ width, height int }{
		{60, 20}, {80, 24}, {120, 40}, {40, 14},
	} {
		m := treeTestModel(t)
		m.width, m.height = tc.width, tc.height

		// 夹具里放超长 ASCII / CJK 文件名，并模拟点击打开它们。
		longASCII := "a-really-long-file-name-that-should-not-break-the-panel-or-status-layout.go"
		longCJK := "中文超长文件名用来验证状态栏与标签页宽度截断而不折行.go"
		for _, name := range []string{longASCII, longCJK} {
			if err := os.WriteFile(filepath.Join(m.tree.root, name), []byte("package main\nfunc f() {}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			m.openFile(filepath.Join(m.tree.root, name))
		}
		// 长状态消息（点击跳转后的提示）也要过这关。
		m.statusMsg = "跳转到定义 · 第 1 行 · a-really-long-status-message-for-the-bar.go"

		view := m.View()
		lines := strings.Split(view, "\n")
		if len(lines) != tc.height {
			t.Errorf("终端 %dx%d：整帧 %d 行（应为 %d），存在折行/溢出", tc.width, tc.height, len(lines), tc.height)
		}
		for i, line := range lines {
			if lw := lipgloss.Width(line); lw > tc.width {
				t.Errorf("终端 %dx%d：第 %d 行显示宽度 %d > %d（会被终端折行导致闪动）",
					tc.width, tc.height, i, lw, tc.width)
			}
		}
	}
}

// TestOpenFileResetsScroll 固化：点开新文件必须从顶部显示，
// 不能沿用上一个文件的滚动位置（scrollLn 是全局态）。
func TestOpenFileResetsScroll(t *testing.T) {
	m := treeTestModel(t)
	// 打开一个足够长的文件并滚到深处。
	longPath := filepath.Join(m.tree.root, "long.go")
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		sb.WriteString("// filler line\n")
	}
	if err := os.WriteFile(longPath, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	m.openFile(longPath)
	m.scrollLn = 150

	// 再打开一个新文件：应回到顶部。
	other := filepath.Join(m.tree.root, "other.go")
	if err := os.WriteFile(other, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.openFile(other)
	if m.scrollLn != 0 {
		t.Errorf("打开新文件后 scrollLn=%d，应为 0", m.scrollLn)
	}
}
