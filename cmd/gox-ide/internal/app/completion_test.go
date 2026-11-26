package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/robertpanvip/gox-ide/internal/complete"
	"github.com/robertpanvip/gox-ide/internal/editor"
)

func TestAcceptCompReplacesToken(t *testing.T) {
	m := New("", true, nil)
	m.focus = 1
	m.compOpen = true
	m.compToken = "cou"
	m.compSel = 0
	m.compCands = []complete.Candidate{{Text: "count", Label: "count", Kind: complete.KindBuffer}}
	m.buffers[0] = editor.New("t.gx", "sig cou = 0")
	// 光标放到行尾（"sig cou " 之后的位置在 "cou" 末尾）。
	m.buffers[0].SetCursor(0, 7) // "sig cou" → 列 7（rune 计：s i g _ c o u = 7）
	m.acceptComp()
	got := m.buffers[0].Text()
	if !strings.Contains(got, "count") {
		t.Errorf("采纳后应含 count，实际 %q", got)
	}
	if strings.Contains(got, "cou =") {
		t.Errorf("旧 token 未被替换: %q", got)
	}
}

func TestMoveCompSelWrap(t *testing.T) {
	m := New("", true, nil)
	m.compOpen = true
	m.compCands = []complete.Candidate{{}, {}, {}}
	m.compSel = 0
	m.moveCompSel(-1)
	if m.compSel != 2 {
		t.Errorf("环绕应为 2，实际 %d", m.compSel)
	}
	m.moveCompSel(1)
	if m.compSel != 0 {
		t.Errorf("环绕应为 0，实际 %d", m.compSel)
	}
}

func TestCloseComp(t *testing.T) {
	m := New("", true, nil)
	m.compOpen = true
	m.compCands = []complete.Candidate{{}}
	m.compToken = "x"
	m.closeComp()
	if m.compOpen || len(m.compCands) != 0 || m.compToken != "" {
		t.Error("closeComp 未清空状态")
	}
}

func TestAcceptCompMember(t *testing.T) {
	// 有前缀：b.Re → 采纳 Replace 应只回退 "Re"，保留 "b."。
	m := New("", true, nil)
	m.focus = 1
	m.compOpen = true
	m.compToken = "Re"
	m.compSel = 0
	m.compCands = []complete.Candidate{{Text: "Replace", Label: "Replace", Kind: complete.KindMember}}
	m.buffers[0] = editor.New("t.gx", "b.Re")
	m.buffers[0].SetCursor(0, 4) // "b.Re" 末尾
	m.acceptComp()
	if got := m.buffers[0].Text(); got != "b.Replace" {
		t.Errorf("有前缀成员采纳 = %q, want b.Replace", got)
	}

	// 空前缀：b. → 采纳 Replace 应直接追加，不吞掉点号。
	m2 := New("", true, nil)
	m2.focus = 1
	m2.compOpen = true
	m2.compToken = ""
	m2.compSel = 0
	m2.compCands = []complete.Candidate{{Text: "Replace", Label: "Replace", Kind: complete.KindMember}}
	m2.buffers[0] = editor.New("t.gx", "b.")
	m2.buffers[0].SetCursor(0, 2) // "b." 末尾
	m2.acceptComp()
	if got := m2.buffers[0].Text(); got != "b.Replace" {
		t.Errorf("空前缀成员采纳 = %q, want b.Replace", got)
	}
}

func TestTabAcceptsWhenCompletionOpen(t *testing.T) {
	// 补全打开 + 焦点在编辑区时，全局 Tab 应转交采纳而非切换焦点。
	m := New("", true, nil)
	m.focus = 1
	m.compOpen = true
	m.compToken = "cou"
	m.compSel = 0
	m.compCands = []complete.Candidate{{Text: "count", Label: "count", Kind: complete.KindBuffer}}
	m.buffers[0] = editor.New("t.gx", "sig cou = 0")
	m.buffers[0].SetCursor(0, 7)
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != 1 {
		t.Errorf("Tab 采纳不应切换焦点，实际 focus=%d", m.focus)
	}
	if !strings.Contains(m.buffers[0].Text(), "count") {
		t.Errorf("Tab 应采纳候选，实际 %q", m.buffers[0].Text())
	}
	if m.compOpen {
		t.Error("采纳后应关闭浮层")
	}
}

func TestTabSwitchesFocusWhenNoCompletion(t *testing.T) {
	m := New("", true, nil)
	m.focus = 1
	m.compOpen = false
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.focus != 0 {
		t.Errorf("无补全时 Tab 应切换焦点到文件树，实际 focus=%d", m.focus)
	}
}
