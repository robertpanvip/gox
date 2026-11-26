package app

import (
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// refreshMatches 根据浮层模式与 query 重算匹配列表。
func (m *Model) refreshMatches() {
	q := strings.ToLower(strings.TrimSpace(m.query))
	switch m.ov {
	case ovOpen:
		m.matches = m.findFiles(q)
	case ovSearch:
		m.matches = m.findInBuffer(q)
	}
	m.matchSel = 0
}

// findFiles 在项目根目录内按子串匹配文件路径（限制 200 条）。
func (m *Model) findFiles(q string) []string {
	if m.root == "" {
		return nil
	}
	var out []string
	_ = filepath.Walk(m.root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".") || base == "go.sum" {
			return nil
		}
		rel, _ := filepath.Rel(m.root, path)
		if q == "" || strings.Contains(strings.ToLower(rel), q) {
			out = append(out, rel)
		}
		if len(out) >= 200 {
			return filepath.SkipAll
		}
		return nil
	})
	return out
}

// findInBuffer 在当前 buffer 里找包含 q 的行，返回 "行号:内容"。
func (m *Model) findInBuffer(q string) []string {
	if q == "" {
		return nil
	}
	b := m.Current()
	var out []string
	for i, line := range b.Lines() {
		if strings.Contains(strings.ToLower(line), q) {
			trimmed := line
			if len(trimmed) > 120 {
				trimmed = trimmed[:120] + "…"
			}
			out = append(out, fmtLine(i+1, trimmed))
		}
	}
	return out
}

func fmtLine(n int, s string) string {
	return padNum(n) + "  " + s
}

func padNum(n int) string {
	const digits = "0123456789"
	s := ""
	if n == 0 {
		return "0"
	}
	for n > 0 {
		s = string(digits[n%10]) + s
		n /= 10
	}
	if len(s) < 3 {
		s = strings.Repeat(" ", 3-len(s)) + s
	}
	return s
}

func (m *Model) handleOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	switch key {
	case "esc", "ctrl+c":
		m.ov = ovNone
		m.query = ""
		return m, nil
	case "enter":
		m.applyMatch()
		return m, nil
	case "up":
		if m.matchSel > 0 {
			m.matchSel--
		}
		return m, nil
	case "down":
		if m.matchSel < len(m.matches)-1 {
			m.matchSel++
		}
		return m, nil
	case "backspace":
		if len(m.query) > 0 {
			runes := []rune(m.query)
			m.query = string(runes[:len(runes)-1])
			m.refreshMatches()
		}
		return m, nil
	default:
		// 追加输入字符：按 KeyRunes 事件取 rune（含多字节 CJK——此前按
		// len(key)==1 判断，中文被整体漏掉导致搜索框无法输入中文）。
		if msg.Type == tea.KeyRunes && len(msg.Runes) > 0 {
			m.query += string(msg.Runes)
			m.refreshMatches()
			return m, nil
		}
		if key == " " { // KeySpace
			m.query += " "
			m.refreshMatches()
		}
	}
	return m, nil
}

func (m *Model) applyMatch() {
	if m.matchSel >= len(m.matches) {
		return
	}
	sel := m.matches[m.matchSel]
	switch m.ov {
	case ovOpen:
		m.openFile(filepath.Join(m.root, sel))
		m.focus = 1
	case ovSearch:
		// 解析行号并跳转。
		n := parseLeadingInt(sel)
		if n > 0 {
			m.Current().SetCursor(n-1, 0)
			m.ensureCursorVisible()
		}
	}
	m.ov = ovNone
	m.query = ""
}

func parseLeadingInt(s string) int {
	n := 0
	s = strings.TrimSpace(s)
	for i := 0; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		n = n*10 + int(s[i]-'0')
	}
	return n
}
