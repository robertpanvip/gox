package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View 实现 tea.Model。布局（自上而下）：
//
//	标题栏（渐变 + 红黄绿点 + 居中标题）
//	标签栏（独立一行，激活标签带 accent 下划线）
//	区隔细线
//	终端区
//	区隔细线
//	状态栏（徽标 + 会话信息 + 快捷键）
func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return "正在初始化…"
	}
	t := m.theme
	innerW := m.width - 2
	innerH := m.height - 2
	if innerW < 20 || innerH < 9 {
		return "终端窗口太小，请放大后重试"
	}

	title := m.viewTitle(innerW)
	tabbar := m.viewTabBar(innerW)
	rule := m.viewRule(innerW)
	bodyH := bodyHeight(m.height)
	body := m.viewBody(innerW, bodyH)
	status := m.viewStatus(innerW)

	content := lipgloss.JoinVertical(lipgloss.Left,
		title, tabbar, rule, body, rule, status,
	)
	return t.Window.Width(m.width - 2).Height(m.height - 2).Render(content)
}

// viewTitle 渲染渐变标题栏：红黄绿点在最左，标题居中。
func (m *Model) viewTitle(w int) string {
	p := m.theme.Palette
	cells := map[int]barCell{}

	// 红黄绿点，间距 1 列。
	dotColors := []lipgloss.Color{p.Close, p.Minimize, p.Zoom}
	for i, c := range dotColors {
		cells[2+i*2] = barCell{text: "●", fg: c}
	}

	// 居中标题。
	title := "  gox-term  "
	start := (w - lipgloss.Width(title)) / 2
	if start < 8 {
		start = 8
	}
	putText(cells, start, title, p.Muted)

	return gradientBar(w, p.TitleGradFrom, p.TitleGradTo, cells)
}

// viewTabBar 渲染独立标签栏。用手写 ANSI 铺纯色背景，绕开 lipgloss 在
// alt-screen 下对多层嵌套 Background 的渲染异常（会错误显示 accent 蓝）。
func (m *Model) viewTabBar(w int) string {
	p := m.theme.Palette
	surface := string(p.Surface)
	cells := map[int]barCell{}

	col := 1
	activeFG := lipgloss.Color(p.Text)
	idleFG := lipgloss.Color(p.Muted)
	for i, s := range m.sessions {
		fg := idleFG
		if i == m.active {
			fg = activeFG
			putText(cells, col, "▸", lipgloss.Color(p.Accent))
			col++
		}
		label := " " + s.Name + " "
		if s.Exited() {
			label = " " + s.Name + " × "
		}
		for _, r := range label {
			if col >= w {
				break
			}
			cells[col] = barCell{text: string(r), fg: fg}
			col++
		}
		if col < w {
			cells[col] = barCell{text: "│", fg: lipgloss.Color(p.Border)}
			col++
		}
	}
	if col < w {
		putText(cells, col, "  +", lipgloss.Color(p.Muted))
	}
	return gradientBar(w, surface, surface, cells)
}

func (m *Model) viewRule(w int) string {
	return m.theme.Divider.Render(strings.Repeat("─", w))
}

func (m *Model) viewBody(w, h int) string {
	s := m.current()
	var lines []string
	if s != nil {
		if sc := m.screens[s.ID]; sc != nil {
			back := m.scrollBack
			// 交替屏幕（vim/htop 全屏）没有回滚历史，强制回到实时画面。
			if sc.InAlt() {
				back = 0
			}
			lines = sc.RenderWindow(back, h)
		}
	}
	if len(lines) == 0 {
		lines = make([]string, h)
	}
	// 旁路补全浮层覆盖到光标行附近（alt-screen / 回滚时由 overlay 内部跳过）。
	lines = m.overlayCompletion(lines, w, h)
	var out []string
	for _, ln := range lines[:h] {
		// 屏幕模拟器输出的行宽恰为 cols（== w），此处只补齐主题背景。
		out = append(out, m.theme.Terminal.Width(w).Render(padANSI(ln, w)))
	}
	return strings.Join(out, "\n")
}

func (m *Model) viewStatus(w int) string {
	t := m.theme

	// 分段：徽标 + 会话信息 | 尺寸 | 快捷键。
	badge := t.Badge.Render("❯")
	info := " " + t.StatusKey.Render("会话") + " " + t.StatusVal.Render(itoa(len(m.sessions))) +
		"   " + t.StatusKey.Render("尺寸") + " " + t.StatusVal.Render(itoa(m.cols)+"×"+itoa(m.rows))
	if m.statusMsg != "" {
		info += "   " + t.StatusVal.Render(m.statusMsg)
	}
	left := badge + info

	right := t.HelpKey.Render("^T") + t.HelpTxt.Render(" 新会话  ") +
		t.HelpKey.Render("^N") + t.HelpTxt.Render(" 切换  ") +
		t.HelpKey.Render("^W") + t.HelpTxt.Render(" 关闭")

	// 状态栏无 padding，内容宽度按 w 算，留 2 列安全余量防中文宽度误差换行。
	avail := w - 2
	used := lipgloss.Width(left) + lipgloss.Width(right) + 2
	spacer := avail - used
	if spacer < 1 {
		spacer = 1
	}
	mid := t.Divider.Render(strings.Repeat("·", spacer))
	return t.Status.Width(w).Render(truncateToWidth(left+" "+mid+" "+right, avail))
}

// ---- ANSI 安全的宽度处理 ----

func truncateANSI(s string, max int) string {
	vis := 0
	i := 0
	var b strings.Builder
	for i < len(s) && vis < max {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			if j < len(s) {
				b.WriteString(s[i : j+1])
				i = j + 1
				continue
			}
		}
		r, size := decodeRune(s[i:])
		rw := runeWidth(r)
		if vis+rw > max {
			break
		}
		b.WriteString(string(r))
		vis += rw
		i += size
	}
	out := b.String()
	out += "\x1b[0m"
	return out
}

func padANSI(s string, width int) string {
	v := visibleLen(s)
	if v >= width {
		return s
	}
	return s + strings.Repeat(" ", width-v)
}

func visibleLen(s string) int {
	n := 0
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		r, size := decodeRune(s[i:])
		n += runeWidth(r)
		i += size
	}
	return n
}

func decodeRune(s string) (rune, int) {
	if len(s) == 0 {
		return 0, 0
	}
	b := s[0]
	if b < 0x80 {
		return rune(b), 1
	}
	var n int
	switch {
	case b&0xe0 == 0xc0:
		n = 2
	case b&0xf0 == 0xe0:
		n = 3
	case b&0xf8 == 0xf0:
		n = 4
	default:
		return rune(b), 1
	}
	if len(s) < n {
		return rune(b), 1
	}
	r := []rune(s[:n])
	if len(r) != 1 {
		return rune(b), 1
	}
	return r[0], n
}

func runeWidth(r rune) int {
	if r < 0x20 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if r < 0x2ff || (r >= 0x370 && r < 0x1100) {
		return 1
	}
	return 2
}

func truncateToWidth(s string, max int) string {
	if lipgloss.Width(s) <= max {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(max).Render(s)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
