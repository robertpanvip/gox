package app

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/robertpanvip/gox-term/internal/complete"
)

// completionTickMsg 是"按键后延迟一小段时间"的定时器消息：等 PTY 把输入回显
// 到屏幕网格后，再从网格读取当前行做补全查询，避免读到回显前的旧内容。
type completionTickMsg struct {
	id  int
	seq int
}

// scheduleCompletion 返回一个延迟 tea.Cmd。seq 用于丢弃过期定时器。
func scheduleCompletion(id, seq int, delay time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(delay)
		return completionTickMsg{id: id, seq: seq}
	}
}

// bumpCompletion 在每次可能改变当前行的按键后调用：递增序号、安排延迟查询。
func (m *Model) bumpCompletion() tea.Cmd {
	m.compSeq++
	id := -1
	if s := m.current(); s != nil {
		id = s.ID
	}
	return scheduleCompletion(id, m.compSeq, 70*time.Millisecond)
}

// refreshCompletion 从屏幕网格的当前行提取查询前缀，运行引擎更新候选。
func (m *Model) refreshCompletion(id int, seq int) {
	if seq != m.compSeq {
		return // 过期定时器
	}
	s := m.current()
	if s == nil || s.ID != id || s.Exited() {
		m.closeCompletion()
		return
	}
	sc := m.screens[id]
	if sc == nil || sc.InAlt() || m.scrollBack > 0 {
		m.closeCompletion() // vim/htop 全屏或回滚时不弹，绝不糊到 TUI 上
		return
	}
	left := sc.LineLeftOfCursor()
	prev, token := complete.BeforeTokens(left)
	if token == "" || utf8.RuneCountInString(token) > 40 {
		m.closeCompletion()
		return
	}
	// ./main → 用 main 做查询，但保留原 token 供采纳时退格。
	queryToken := strings.TrimPrefix(token, "./")
	if queryToken == "" {
		m.closeCompletion()
		return
	}
	cands := m.compEngine.Complete(complete.Query{Token: queryToken, Prev: prev, Line: left, CWD: m.cwd()})
	if len(cands) == 0 {
		m.closeCompletion()
		return
	}
	m.compOpen = true
	m.compCands = cands
	if m.compSel >= len(cands) {
		m.compSel = 0
	}
}

func (m *Model) closeCompletion() {
	m.compOpen = false
	m.compCands = nil
	m.compSel = 0
}

// acceptCompletion 采纳第 idx 条候选：读当前行的 token，回发等长退格 + 候选文本，
// 让 shell 的 readline 与我们的插入保持同步。
func (m *Model) acceptCompletion(idx int) tea.Cmd {
	if !m.compOpen || idx < 0 || idx >= len(m.compCands) {
		return nil
	}
	cand := m.compCands[idx]
	s := m.current()
	if s == nil {
		return nil
	}
	n := 0
	if sc := m.screens[s.ID]; sc != nil {
		tok := complete.TokenBefore(sc.LineLeftOfCursor())
		n = utf8.RuneCountInString(tok)
	}
	var b strings.Builder
	for i := 0; i < n; i++ {
		b.WriteByte(0x7f) // backspace
	}
	b.WriteString(cand.Text)
	_, _ = s.Write([]byte(b.String()))
	m.statusMsg = "已补全 " + cand.Label
	m.closeCompletion()
	return nil
}

// moveCompletionSel 在候选间移动选中项（环绕）。
func (m *Model) moveCompletionSel(delta int) {
	if !m.compOpen || len(m.compCands) == 0 {
		return
	}
	m.compSel = (m.compSel + delta + len(m.compCands)) % len(m.compCands)
}

func (m *Model) cwd() string {
	if s := m.current(); s != nil {
		if c := s.CWD(); c != "" {
			return c
		}
	}
	return m.homeDir
}

// ---- 鼠标交互 ----

func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// 滚轮：回滚 / 回到实时。
	switch msg.Type {
	case tea.MouseWheelUp:
		m.scrollBack += 3
		m.autoScroll = false
		m.closeCompletion()
		return m, nil
	case tea.MouseWheelDown:
		m.scrollBack = maxInt(0, m.scrollBack-3)
		if m.scrollBack == 0 {
			m.autoScroll = true
		}
		m.closeCompletion()
		return m, nil
	}
	if msg.Type != tea.MouseLeft {
		return m, nil
	}

	// 窗口边框(1) + 标题(1)：内容区从屏幕第 1 行第 1 列开始。
	ix, iy := msg.X-1, msg.Y-1
	if ix < 0 || iy < 0 {
		return m, nil
	}

	// 标签栏（内容第 1 行）。
	if iy == 1 {
		for _, r := range m.tabRanges() {
			if ix >= r.lo && ix <= r.hi {
				if r.idx == len(m.sessions) {
					m.newSession() // 点 "+"
				} else if r.idx >= 0 && r.idx < len(m.sessions) {
					m.active = r.idx
					m.scrollBack = 0
					m.autoScroll = true
					m.closeCompletion()
				}
				return m, nil
			}
		}
	}

	// 补全浮层：内容第 3 行起是终端区，浮层首行记录在 compStart。
	if m.compOpen {
		bodyTop := 3
		row := iy - bodyTop
		if row >= m.compStart && row < m.compStart+1+len(m.compCands) {
			idx := row - m.compStart - 1 // 第 0 行是提示头
			if idx >= 0 && idx < len(m.compCands) {
				return m, m.acceptCompletion(idx)
			}
			return m, nil
		}
	}

	// 终端区其它位置：关闭浮层、回到实时视图。鼠标无法移动 shell 光标，
	// 但点击聚焦/滚动复位是自然的交互。
	if iy >= 3 {
		m.scrollBack = 0
		m.autoScroll = true
		m.closeCompletion()
	}
	return m, nil
}

type tabRange struct {
	lo, hi int
	idx    int // len(sessions) 表示 "+"
}

// tabRanges 复刻 viewTabBar 的横向布局，供鼠标命中测试。坐标是内容区列（0 基）。
func (m *Model) tabRanges() []tabRange {
	var out []tabRange
	col := 1
	for i, s := range m.sessions {
		if i == m.active {
			col++ // "▸"
		}
		label := " " + s.Name + " "
		if s.Exited() {
			label = " " + s.Name + " × "
		}
		w := utf8.RuneCountInString(label)
		out = append(out, tabRange{col, col + w - 1, i})
		col += w
		out[len(out)-1].hi = col // 含分隔线
		col++                    // "│"
	}
	w := utf8.RuneCountInString("  +")
	out = append(out, tabRange{col, col + w - 1, len(m.sessions)})
	return out
}

// ---- 浮层渲染 ----

// overlayCompletion 把补全浮层覆盖到终端可见行 lines 上，返回处理后的行。
// 浮层放在光标行下方；下方空间不足时整体上移，保证完整显示。
func (m *Model) overlayCompletion(lines []string, w, h int) []string {
	if !m.compOpen || len(m.compCands) == 0 || h <= 0 || w <= 0 {
		m.compStart = -1
		return lines
	}
	sc := m.current()
	if sc == nil {
		return lines
	}
	_ = sc
	_, cy := m.screens[m.current().ID].Cursor()
	maxShow := minInt(len(m.compCands), 8)
	total := maxShow + 1 // 1 行提示头

	start := cy + 1
	if start+total > h {
		start = h - total
	}
	if start < 0 {
		start = 0
		maxShow = maxInt(0, h-1)
		total = maxShow + 1
	}
	if total <= 1 || start >= h {
		m.compStart = -1
		return lines
	}
	m.compStart = start

	p := m.theme.Palette
	bgSurface, fgText := parseHex(string(p.Surface)), parseHex(string(p.Text))
	bgAccent, fgAccent := parseHex(string(p.Accent)), parseHex(string(p.AccentFG))
	fgMuted := parseHex(string(p.Muted))

	header := fmt.Sprintf(" 补全  %d/%d   ↑↓ 选择 · Tab/Enter 采纳 · Esc 关闭", m.compSel+1, len(m.compCands))
	lines[start] = paintFullLine(w, bgAccent, fgAccent, header, true)
	for i := 0; i < maxShow; i++ {
		c := m.compCands[i]
		label := truncatePlain(c.Label, maxInt(6, w-16))
		rowText := " " + c.Kind.Mark() + " " + label + "   " + c.Detail
		if i == m.compSel {
			lines[start+1+i] = paintFullLine(w, bgAccent, fgAccent, rowText, true)
		} else {
			_ = fgMuted
			lines[start+1+i] = paintFullLine(w, bgSurface, fgText, rowText, false)
		}
	}
	return lines
}

// paintFullLine 生成一条可见宽度恰为 width 的单行：整行统一背景/前景，末尾重置。
// 手写 24-bit ANSI，绕开 lipgloss 在 alt-screen 下嵌套背景的错误染色。
func paintFullLine(width int, bg, fg rgb, text string, bold bool) string {
	if width <= 0 {
		return ""
	}
	var sb strings.Builder
	if bold {
		sb.WriteString("\x1b[1m")
	}
	fmt.Fprintf(&sb, "\x1b[48;2;%d;%d;%dm\x1b[38;2;%d;%d;%dm", bg.r, bg.g, bg.b, fg.r, fg.g, fg.b)
	runes := []rune(text)
	col := 0
	for _, r := range runes {
		rw := runeWidth(r)
		if col+rw > width {
			break
		}
		sb.WriteRune(r)
		col += rw
		if rw == 2 {
			sb.WriteString("\x1b[48;2;")
			fmt.Fprintf(&sb, "%d;%d;%dm", bg.r, bg.g, bg.b)
		}
	}
	for ; col < width; col++ {
		sb.WriteByte(' ')
	}
	sb.WriteString("\x1b[0m")
	return sb.String()
}

// truncatePlain 按可见宽度截断无 ANSI 的文本。
func truncatePlain(s string, max int) string {
	if max <= 0 {
		return ""
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		rw := runeWidth(r)
		if col+rw > max {
			break
		}
		b.WriteRune(r)
		col += rw
	}
	return b.String()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ = lipgloss.Width
