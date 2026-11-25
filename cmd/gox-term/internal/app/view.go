package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// View 实现 tea.Model。
func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return "正在初始化…"
	}
	t := m.theme
	innerW := m.width - 2
	innerH := m.height - 2
	if innerW < 20 || innerH < 8 {
		return "终端窗口太小，请放大后重试"
	}

	title := m.viewTitle(innerW)
	body := m.viewBody(innerW, innerH-2)
	status := m.viewStatus(innerW)

	content := lipgloss.JoinVertical(lipgloss.Left, title, body, status)
	return t.Window.Width(m.width - 2).Height(m.height - 2).Render(content)
}

func (m *Model) viewTitle(w int) string {
	t := m.theme
	dots := t.Dots[0].Render() + " " + t.Dots[1].Render() + " " + t.Dots[2].Render()
	left := dots + "  " + t.TitleText.Render("gox-term")

	var tabs []string
	for i, s := range m.sessions {
		label := s.Name
		if s.Exited() {
			label += " ×"
		}
		style := t.Tab
		if i == m.active {
			style = t.TabActive
		}
		tabs = append(tabs, style.Render(" "+label+" "))
	}
	tabStr := strings.Join(tabs, "")

	used := lipgloss.Width(left) + lipgloss.Width(tabStr) + 1
	spacer := w - used
	if spacer < 0 {
		spacer = 0
	}
	return t.TitleBar.Width(w).Render(truncateToWidth(left+strings.Repeat(" ", spacer)+tabStr, w))
}

func (m *Model) viewBody(w, h int) string {
	s := m.current()
	var lines []string
	if s != nil {
		all := ansiToLines(s.Snapshot())
		// 取末尾 h 行（考虑回滚）。
		end := len(all)
		back := m.scrollBack
		if back > 0 && end-back > 0 {
			end = end - back
		}
		start := end - h
		if start < 0 {
			start = 0
		}
		visible := all[start:end]
		for _, ln := range visible {
			lines = append(lines, truncateANSI(ln.text, w))
		}
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	// 用 Terminal 样式给每行铺背景；保留行内 SGR 颜色。
	var out []string
	for _, ln := range lines[:h] {
		out = append(out, m.theme.Terminal.Width(w).Render(padANSI(ln, w)))
	}
	return strings.Join(out, "\n")
}

func (m *Model) viewStatus(w int) string {
	t := m.theme
	n := len(m.sessions)
	left := "  " + t.StatusKey.Render("终端") + " " +
		t.StatusVal.Render("会话 "+itoa(n)) + "  " +
		t.StatusKey.Render(itoa(m.cols)+"x"+itoa(m.rows))
	if m.statusMsg != "" {
		left += "  " + t.StatusVal.Render(m.statusMsg)
	}
	right := t.HelpKey.Render("^T") + t.HelpTxt.Render(" 新会话 ") +
		t.HelpKey.Render("^W") + t.HelpTxt.Render(" 关闭 ") +
		t.HelpKey.Render("^N") + t.HelpTxt.Render(" 切换 ") +
		t.HelpKey.Render("^S↑↓") + t.HelpTxt.Render(" 滚动 ")

	used := lipgloss.Width(left) + lipgloss.Width(right) + 1
	spacer := w - used
	if spacer < 0 {
		spacer = 0
	}
	return t.Status.Width(w).Render(truncateToWidth(left+strings.Repeat(" ", spacer)+right, w))
}

// ---- ANSI 安全的宽度处理 ----

// truncateANSI 按可见宽度截断，保留 SGR 颜色序列并在末尾补全 ANSI 状态，
// 避免截断后颜色泄漏到后续行。
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
		w := runeWidth(r)
		if vis+w > max {
			break
		}
		b.WriteString(string(r))
		vis += w
		i += size
	}
	out := b.String()
	// 重置 SGR，防止颜色跨越到下一行。
	out += "\x1b[0m"
	return out
}

// padANSI 按可见宽度右侧补空格到 width。
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

// decodeRune 解析首 rune（UTF-8），非法字节当作宽度 1 的单字节。
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

// runeWidth 返回显示列宽：ASCII 控制/单字节宽字符算 1，其余（CJK/emoji）算 2。
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
