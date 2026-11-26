package app

import (
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/robertpanvip/gox-ide/internal/complete"
)

// completionTickMsg 是输入后延迟查询的定时器消息（等 buffer 更新后再取 token）。
type completionTickMsg struct{ seq int }

func scheduleComp(seq int) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(40 * time.Millisecond)
		return completionTickMsg{seq: seq}
	}
}

// bumpComp 在每次可能影响当前 token 的输入后调用，安排延迟查询。
func (m *Model) bumpComp() tea.Cmd {
	m.compSeq++
	return scheduleComp(m.compSeq)
}

// refreshCompletion 从当前 buffer 光标左侧提取 token，运行引擎更新候选。
func (m *Model) refreshCompletion(seq int) {
	if seq != m.compSeq {
		return // 过期定时器
	}
	if m.ov != ovNone || m.focus != 1 {
		m.closeComp()
		return
	}
	b := m.Current()
	ln, col := b.Cursor()
	line := b.Line(ln)
	left := line
	if col < len([]rune(line)) {
		left = string([]rune(line)[:col])
	}
	token := complete.TokenBefore(left)
	if token == "" || utf8.RuneCountInString(token) < 1 || utf8.RuneCountInString(token) > 40 {
		m.closeComp()
		return
	}
	cands := m.compEngine.Complete(complete.Query{
		Token:      token,
		Line:       left,
		BufferText: b.Text(),
		CWD:        m.root,
	})
	if len(cands) == 0 {
		m.closeComp()
		return
	}
	m.compOpen = true
	m.compCands = cands
	m.compToken = token
	if m.compSel >= len(cands) {
		m.compSel = 0
	}
}

func (m *Model) closeComp() {
	m.compOpen = false
	m.compCands = nil
	m.compSel = 0
	m.compToken = ""
}

func (m *Model) moveCompSel(delta int) {
	if !m.compOpen || len(m.compCands) == 0 {
		return
	}
	m.compSel = (m.compSel + delta + len(m.compCands)) % len(m.compCands)
}

// acceptComp 采纳候选：回退当前 token（逐 rune 退格），再插入候选文本。
func (m *Model) acceptComp() tea.Cmd {
	if !m.compOpen || m.compSel < 0 || m.compSel >= len(m.compCands) {
		return nil
	}
	cand := m.compCands[m.compSel]
	b := m.Current()
	for i := 0; i < utf8.RuneCountInString(m.compToken); i++ {
		b.Backspace()
	}
	b.InsertString(cand.Text)
	m.statusMsg = "已补全 " + cand.Label
	m.closeComp()
	m.ensureCursorVisible()
	return nil
}
