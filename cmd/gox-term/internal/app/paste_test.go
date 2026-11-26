package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/robertpanvip/gox-term/internal/session"
)

func TestWrapBracketedPaste(t *testing.T) {
	got := wrapBracketedPaste("git status\nls")
	if !strings.HasPrefix(got, pasteBegin) || !strings.HasSuffix(got, pasteEnd) {
		t.Errorf("粘贴序列未成对包裹: %q", got)
	}
	if !strings.Contains(got, "git status\nls") {
		t.Error("粘贴内容丢失")
	}
	// 内嵌的结束标记应被剔除，避免提前关闭粘贴模式。
	got2 := wrapBracketedPaste("a" + pasteEnd + "b")
	if strings.Count(got2, pasteEnd) != 1 {
		t.Errorf("应只有一个结束标记: %q", got2)
	}
}

func TestHandlePasteMsg(t *testing.T) {
	m := New(true, "/bin/sh")
	m.sessions = []*session.Session{session.NewStub(0, "sh")}
	m.active = 0
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("echo hi\n"), Paste: true}
	m.handleKey(msg)
	if m.compOpen {
		t.Error("粘贴时应关闭补全")
	}
	if !strings.Contains(m.statusMsg, "已粘贴") {
		t.Errorf("状态栏应显示已粘贴，实际 %q", m.statusMsg)
	}
}

func TestMouseDragSelection(t *testing.T) {
	m := New(true, "/bin/sh")
	s := session.NewStub(0, "sh")
	m.sessions = []*session.Session{s}
	m.active = 0
	m.cols, m.rows, m.width, m.height = 80, 24, 82, 31
	sc := m.screens[0] // 目前为 nil，需要手动建
	if sc == nil {
		// 直接注入一块屏幕模拟器
		m.screens[0] = nil
	}
	// 用 model 的选区状态直接验证坐标转换与提交路径，不依赖屏幕内容。
	// Press（终端区内容第 3 行 → 屏幕 y=4）
	m.handleMouse(tea.MouseMsg{Type: tea.MouseLeft, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 5, Y: 4})
	if !m.selecting {
		t.Fatal("按下后应进入选择态")
	}
	if m.selAnchor.x != 4 || m.selAnchor.y != 0 {
		t.Errorf("锚点坐标错误: %+v", m.selAnchor)
	}
	// Motion
	m.handleMouse(tea.MouseMsg{Type: tea.MouseLeft, Action: tea.MouseActionMotion, X: 12, Y: 5})
	if m.selHead.x != 11 || m.selHead.y != 1 {
		t.Errorf("拖动 head 错误: %+v", m.selHead)
	}
	// Release
	m.handleMouse(tea.MouseMsg{Type: tea.MouseRelease, Action: tea.MouseActionRelease, X: 12, Y: 5})
	if m.selecting {
		t.Error("松开后应退出选择态")
	}
}
