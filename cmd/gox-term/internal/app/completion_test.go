package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/robertpanvip/gox-term/internal/complete"
	"github.com/robertpanvip/gox-term/internal/session"
)

func TestTabRangesHit(t *testing.T) {
	m := New(true, "/bin/sh")
	m.sessions = []*session.Session{
		{ID: 0, Name: "bash"},
		{ID: 1, Name: "zsh"},
		{ID: 2, Name: "sh"},
	}
	m.active = 1
	ranges := m.tabRanges()
	if len(ranges) != len(m.sessions)+1 {
		t.Fatalf("命中区间数 = %d，期望 %d", len(ranges), len(m.sessions)+1)
	}
	prevHi := -2
	for i, r := range ranges {
		if r.lo > r.hi {
			t.Errorf("区间 %d lo>hi: %+v", i, r)
		}
		if r.lo <= prevHi {
			t.Errorf("区间 %d 与前一个重叠: %+v prev=%d", i, r, prevHi)
		}
		prevHi = r.hi
	}
	if ranges[len(ranges)-1].idx != len(m.sessions) {
		t.Error("最后一段应为 '+'")
	}
}

func TestPaintFullLineWidth(t *testing.T) {
	cases := []struct {
		text  string
		width int
	}{
		{"hello", 10},
		{"中文测试", 10},
		{"", 5},
		{"abcdefghij", 5},
	}
	for _, c := range cases {
		got := paintFullLine(c.width, rgb{20, 20, 20}, rgb{200, 200, 200}, c.text, false)
		if w := lipgloss.Width(got); w != c.width {
			t.Errorf("paintFullLine(%q,%d) 宽度 = %d", c.text, c.width, w)
		}
		if strings.Contains(got, "\n") {
			t.Errorf("paintFullLine 不应含换行")
		}
	}
}

func TestTruncatePlain(t *testing.T) {
	if got := truncatePlain("abcdef", 3); got != "abc" {
		t.Errorf("truncatePlain = %q", got)
	}
	if got := truncatePlain("中文abc", 4); got != "中文" {
		t.Errorf("CJK 截断 = %q", got)
	}
}

func TestMoveCompletionSelWrap(t *testing.T) {
	m := New(true, "/bin/sh")
	m.compOpen = true
	m.compCands = []complete.Candidate{
		{Label: "a"}, {Label: "b"}, {Label: "c"},
	}
	m.compSel = 0
	m.moveCompletionSel(-1)
	if m.compSel != 2 {
		t.Errorf("向前环绕应为 2，实际 %d", m.compSel)
	}
	m.moveCompletionSel(1)
	if m.compSel != 0 {
		t.Errorf("向后环绕应为 0，实际 %d", m.compSel)
	}
	m.moveCompletionSel(2)
	if m.compSel != 2 {
		t.Errorf("应为 2，实际 %d", m.compSel)
	}
}

func TestCloseCompletion(t *testing.T) {
	m := New(true, "/bin/sh")
	m.compOpen = true
	m.compCands = []complete.Candidate{{Label: "x"}}
	m.compSel = 0
	m.closeCompletion()
	if m.compOpen || len(m.compCands) != 0 || m.compSel != 0 {
		t.Error("closeCompletion 未清空状态")
	}
}

// 构造一个补全打开的测试 Model：3 个会话，浮层从可见行 0 开始，4 条候选。
func newCompletionModel() *Model {
	m := New(true, "/bin/sh")
	m.sessions = []*session.Session{
		session.NewStub(0, "bash"), session.NewStub(1, "zsh"),
	}
	m.active = 0
	m.width, m.height = 100, 30
	m.compOpen = true
	m.compStart = 0
	m.compCands = []complete.Candidate{
		{Label: "status", Text: "status ", Detail: "git"},
		{Label: "stash", Text: "stash ", Detail: "git"},
		{Label: "reset", Text: "reset ", Detail: "git"},
	}
	return m
}

func TestHandleMouseClickCandidate(t *testing.T) {
	m := newCompletionModel()
	// 浮层 header 在屏幕 y=4，候选 0 在 y=5。点候选 1（stash）→ y=6。
	msg := tea.MouseMsg{Type: tea.MouseLeft, X: 40, Y: 6}
	m.handleMouse(msg)
	if m.compOpen {
		t.Error("点击候选后浮层应关闭")
	}
	if !strings.Contains(m.statusMsg, "stash") {
		t.Errorf("状态栏应显示已补全 stash，实际 %q", m.statusMsg)
	}
}

func TestHandleMouseClickTab(t *testing.T) {
	m := newCompletionModel()
	r := m.tabRanges()[1]                                       // zsh 标签
	msg := tea.MouseMsg{Type: tea.MouseLeft, X: r.lo + 1, Y: 2} // 标签栏在内容第1行→屏幕 y=2
	m.handleMouse(msg)
	if m.active != 1 {
		t.Errorf("点击 zsh 标签应切换 active=1，实际 %d", m.active)
	}
}

func TestHandleMouseWheel(t *testing.T) {
	m := newCompletionModel()
	m.handleMouse(tea.MouseMsg{Type: tea.MouseWheelUp, X: 40, Y: 10})
	if m.scrollBack != 3 {
		t.Errorf("滚轮上滚应 scrollBack=3，实际 %d", m.scrollBack)
	}
	m.handleMouse(tea.MouseMsg{Type: tea.MouseWheelDown, X: 40, Y: 10})
	if m.scrollBack != 0 {
		t.Errorf("滚轮下滚应 scrollBack=0，实际 %d", m.scrollBack)
	}
}
