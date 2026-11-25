package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/robertpanvip/gox-ide/internal/syntax"
)

// View 实现 tea.Model：渲染一整帧。
func (m *Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return "正在初始化…"
	}
	t := m.theme

	// 各分区宽度与高度。
	innerW := m.width - 2 // 去掉 App 圆角边框左右各 1
	innerH := m.height - 2
	if innerW < 20 || innerH < 8 {
		return "终端窗口太小，请放大后重试"
	}

	titleBar := m.viewTitleBar(innerW)
	body := m.viewBody(innerW, innerH-2) // 减标题栏、状态栏
	statusBar := m.viewStatusBar(innerW)

	content := lipgloss.JoinVertical(lipgloss.Left, titleBar, body, statusBar)
	view := t.App.Width(m.width - 2).Height(m.height - 2).Render(content)

	// 浮层覆盖在最上层。
	if m.ov != ovNone {
		view = m.overlayView(view)
	}
	return view
}

// viewTitleBar 渲染 macOS 风格标题栏：红黄绿点 + 居中标题 + 多标签。
func (m *Model) viewTitleBar(w int) string {
	t := m.theme
	dots := t.Dots[0].Render() + " " + t.Dots[1].Render() + " " + t.Dots[2].Render()

	title := "gox-ide"
	left := dots + "   " + t.TitleText.Render(title)

	// 标签页。
	var tabs []string
	for i, b := range m.buffers {
		label := b.Name
		if b.Dirty {
			label += " •"
		}
		style := t.Tab
		if i == m.active {
			style = t.TabActive
		}
		tabW := runningWidth(label) + 4
		if tabW > 22 {
			label = truncate(label, 18)
			tabW = 22
		}
		tabs = append(tabs, style.Width(tabW).Render(label))
	}
	tabStr := strings.Join(tabs, "")

	used := runningWidth(left) + runningWidth(tabStr) + 2
	spacer := w - used
	if spacer < 1 {
		spacer = 1
	}
	line := left + strings.Repeat(" ", spacer) + tabStr
	return t.TitleBar.Width(w).MaxWidth(w).Render(truncateWidth(line, w))
}

// viewBody 渲染主体：左侧文件树 + 竖线 + 右侧编辑区。
func (m *Model) viewBody(w, h int) string {
	treeW := w * 25 / 100
	if treeW < 16 {
		treeW = 16
	}
	if treeW > 42 {
		treeW = 42
	}
	editorW := w - treeW - 1 // 竖线占 1

	tree := m.viewTree(treeW, h)
	divider := strings.Repeat(m.theme.Divider.Render("│")+"\n", h)
	editor := m.viewEditor(editorW, h)

	return joinColumns(tree, divider, editor, h)
}

func (m *Model) viewTree(w, h int) string {
	t := m.theme
	nodes := m.tree.nodes()
	var lines []string
	header := t.TreeFolder.Render("▾ PROJECT")
	lines = append(lines, header)

	start := m.scrollLn // 文件树跟随编辑区滚动意义不大，这里从 0 开始
	_ = start
	for i, n := range nodes {
		if i >= h-1 {
			break
		}
		indent := strings.Repeat("  ", n.Depth)
		icon := "  "
		if n.Dir {
			if n.Open {
				icon = "▾ "
			} else {
				icon = "▸ "
			}
		} else {
			icon = "  "
		}
		label := indent + icon + n.Name
		style := t.TreeItem
		if i == m.treeSel {
			style = t.TreeActive
		}
		lines = append(lines, style.Width(w).Render(truncateWidth(label, w-1)))
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	block := strings.Join(lines, "\n")
	return t.Sidebar.Width(w).Height(h).Render(block)
}

// viewEditor 渲染代码区：行号 + 语法高亮 + 当前行高亮 + 虚拟滚动。
func (m *Model) viewEditor(w, h int) string {
	t := m.theme
	b := m.Current()
	ln, col := b.Cursor()
	total := b.LineCount()

	noWidth := len(itoa(total)) + 2
	codeW := w - noWidth - 1
	if codeW < 1 {
		codeW = 1
	}

	// 虚拟滚动：只渲染视口内可见行。
	start := m.scrollLn
	if start > total-1 {
		start = 0
	}
	end := minInt(start+h, total)

	var lines []string
	for i := start; i < end; i++ {
		no := padLeft(itoa(i+1), noWidth-1) + " "
		code := m.renderHighlighted(b.Line(i), codeW)
		row := t.LineNo.Render(no) + code
		if i == ln {
			// 当前行高亮：整行铺底色。
			row = t.CursorLine.Width(w).Render(row)
		}
		lines = append(lines, truncateWidth(row, w))
	}
	for len(lines) < h {
		lines = append(lines, strings.Repeat(" ", w))
	}
	_ = col // 光标列用状态栏展示；终端光标可用但这里保持静态高亮行
	return t.Editor.Width(w).Height(h).Render(strings.Join(lines, "\n"))
}

// renderHighlighted 把一行源码按语义 token 着色，拼成带 ANSI 的字符串。
func (m *Model) renderHighlighted(line string, width int) string {
	var b strings.Builder
	for _, tk := range syntax.Highlight(line) {
		b.WriteString(m.theme.SynStyle(tk.Color).Render(tk.Text))
	}
	return truncateAnsi(b.String(), width)
}

// viewStatusBar 渲染底部状态栏。
func (m *Model) viewStatusBar(w int) string {
	t := m.theme
	left := t.StatusKey.Render("  ") + " " +
		t.StatusKey.Render(m.Current().Name) + "  " +
		t.StatusVal.Render(m.Current().CursorInfo()) + "  " +
		t.StatusKey.Render("UTF-8") + "  " +
		t.StatusKey.Render(itoa(m.Current().LineCount())+" lines")
	if m.Current().Dirty {
		left += "  " + t.StatusKey.Render("● 未保存")
	}
	if m.statusMsg != "" {
		left += "   " + t.StatusVal.Render(m.statusMsg)
	}
	right := t.HelpKey.Render("^P") + t.HelpTxt.Render(" 打开 ") +
		t.HelpKey.Render("^F") + t.HelpTxt.Render(" 搜索 ") +
		t.HelpKey.Render("^T") + t.HelpTxt.Render(" 主题 ") +
		t.HelpKey.Render("^S") + t.HelpTxt.Render(" 保存 ")

	used := runningWidth(left) + runningWidth(right) + 1
	spacer := w - used
	if spacer < 1 {
		spacer = 1
	}
	return t.Status.Width(w).Render(truncateWidth(left+strings.Repeat(" ", spacer)+right, w))
}

// overlayView 在主体中间叠一个浮层面板。
func (m *Model) overlayView(base string) string {
	t := m.theme
	title := "搜索"
	placeholder := "输入关键字…"
	if m.ov == ovOpen {
		title = "快速打开文件"
		placeholder = "输入文件名…"
	}

	panelW := minInt(72, m.width-8)
	panelH := minInt(16, m.height-6)
	inner := panelW - 2 // 去掉浮层圆角边框左右各 1

	var lines []string
	lines = append(lines, t.TitleText.Render("  "+title))
	query := m.query
	if query == "" {
		query = placeholder
	}
	lines = append(lines, t.OverlayInput.Width(inner).Render("  > "+truncate(query, inner-3)))
	lines = append(lines, t.OverlayItem.Render(strings.Repeat("─", inner)))

	visible := minInt(len(m.matches), panelH-4)
	for i := 0; i < visible; i++ {
		item := truncate(m.matches[i], inner-2)
		style := t.OverlayItem
		if i == m.matchSel {
			style = t.OverlayActive
		}
		lines = append(lines, style.Width(inner).Render("  "+item))
	}
	for len(lines) < panelH-2 {
		lines = append(lines, strings.Repeat(" ", inner))
	}
	panel := t.OverlayBox.Width(panelW).Height(panelH).Render(strings.Join(lines, "\n"))

	return placeOverlay(base, panel)
}

// ---- 渲染辅助 ----

func joinColumns(a, sep, b string, h int) string {
	al := strings.Split(a, "\n")
	bl := strings.Split(b, "\n")
	sl := strings.Split(sep, "\n")
	var out []string
	for i := 0; i < h; i++ {
		left := ""
		if i < len(al) {
			left = al[i]
		}
		mid := ""
		if i < len(sl) {
			mid = sl[i]
		}
		right := ""
		if i < len(bl) {
			right = bl[i]
		}
		out = append(out, left+mid+right)
	}
	return strings.Join(out, "\n")
}

func placeOverlay(base, panel string) string {
	lines := strings.Split(base, "\n")
	pl := strings.Split(panel, "\n")
	ph := len(pl)
	pw := lipgloss.Width(panel)
	y := (len(lines) - ph) / 2
	if y < 0 {
		y = 0
	}
	for i, row := range pl {
		yi := y + i
		if yi >= len(lines) {
			break
		}
		prefix := truncateWidth(lines[yi], maxInt(0, lipgloss.Width(lines[yi])-pw))
		suffix := ""
		remain := lipgloss.Width(lines[yi]) - lipgloss.Width(prefix)
		if remain > 0 {
			suffix = strings.Repeat(" ", remain)
		}
		lines[yi] = prefix + row + suffix
	}
	return strings.Join(lines, "\n")
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

func padLeft(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}

func truncate(s string, max int) string {
	if lipgloss.Width(s) <= max {
		return s
	}
	// 按显示宽度逐 rune 累加，CJK 等宽字符算 2 列。
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > max-1 {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	b.WriteRune('…')
	return b.String()
}

func truncateWidth(s string, max int) string {
	if lipgloss.Width(s) <= max {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(max).Render(s)
}

func truncateAnsi(s string, max int) string {
	if lipgloss.Width(s) <= max {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(max).Render(s)
}

func runningWidth(s string) int { return lipgloss.Width(s) }
