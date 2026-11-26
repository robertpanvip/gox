package app

import (
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/robertpanvip/gox-tui/internal/syntax"
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
	switch m.ov {
	case ovPeek:
		view = m.peekOverlayView(view)
	case ovHover:
		view = m.hoverOverlayView(view)
	case ovNone:
		if m.compOpen {
			view = m.compOverlayView(view)
		}
	default:
		view = m.overlayView(view)
	}
	return view
}

// compOverlayView 把补全候选框锚定到光标行下方，叠在编辑区上。
func (m *Model) compOverlayView(base string) string {
	t := m.theme
	panelW := minInt(56, m.width-8)
	if panelW < 20 {
		return base // 太窄不画
	}
	inner := panelW - 2
	var lines []string
	show := minInt(len(m.compCands), 8)
	for i := 0; i < show; i++ {
		c := m.compCands[i]
		label := truncate(c.Label, inner-8)
		row := " " + c.Kind.Mark() + " " + label
		detail := truncate(c.Detail, inner-lipgloss.Width(row)-1)
		if detail != "" {
			row += strings.Repeat(" ", maxInt(1, inner-lipgloss.Width(row)-lipgloss.Width(detail))) + detail
		}
		style := t.OverlayItem
		if i == m.compSel {
			style = t.OverlayActive
		}
		lines = append(lines, style.Width(inner).Render(row))
	}
	panel := t.OverlayBox.Width(panelW).Height(show + 2).Render(strings.Join(lines, "\n"))

	// 锚定行：光标行在 body 中的相对位置 + 标题栏(1) + 边框(1) + 1。
	ln, _ := m.Current().Cursor()
	relY := ln - m.scrollLn
	startY := relY + 3
	if startY < 1 {
		startY = 1
	}
	// 面板放不下时上移。
	baseLines := strings.Split(base, "\n")
	if startY+show+2 > len(baseLines) {
		startY = maxInt(1, len(baseLines)-show-3)
	}
	// 水平位置：编辑区在右侧，留出左侧文件树宽度。
	treeW := (m.width - 2) * 25 / 100
	if treeW < 16 {
		treeW = 16
	}
	if treeW > 42 {
		treeW = 42
	}
	startX := 1 + treeW + 1 // 边框 + 文件树 + 分隔线
	return placeOverlayAt(base, panel, startX, startY)
}

// viewTitleBar 渲染 macOS 风格标题栏：红黄绿点 + 居中标题 + 多标签。
func (m *Model) viewTitleBar(w int) string {
	t := m.theme
	dots := t.Dots[0].Render() + " " + t.Dots[1].Render() + " " + t.Dots[2].Render()

	title := "gox-tui"
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
	// TitleBar 自带 Padding(0,1)：内容区是 w-2，按 w 截断会让贴边的标签页
	// 挤超内容区被 lipgloss 折行（与文件树同源的问题）。
	return t.TitleBar.Width(w).MaxWidth(w).Render(truncateWidth(line, w-2))
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
	// 宽度口径：Sidebar 带 Padding(1,1)，TreeItem/TreeActive 带 Padding(0,1)。
	// lipgloss 的 Width() 把 padding 算进块宽，所以 Sidebar 内容区只剩 w-2，
	// 行样式的内容区再减 2。行宽若不按这个口径截断，长文件名一挤超内容区
	// 就会被 lipgloss 折行，整棵树从此往下错位（右侧分隔线/编辑区对不上）。
	avail := w - 2
	if avail < 8 {
		avail = 8
	}
	rowW := avail - 2
	nodes := m.tree.nodes()
	var lines []string
	header := t.TreeFolder.Render("📂 PROJECT")
	lines = append(lines, header)

	start := m.scrollLn // 文件树跟随编辑区滚动意义不大，这里从 0 开始
	_ = start
	// Sidebar 的 Padding(1,1) 纵向也占 2 行：Height(h) 的内容区是 h-2，
	// 内容行数按 h-2 供给，避免底部行被裁掉。
	for i, n := range nodes {
		if i >= h-3 {
			break
		}
		indent := strings.Repeat("  ", n.Depth)
		glyph, styleKey := glyphFor(n)
		// WebStorm 风格展开/折叠箭头：目录 ▾(展开)/▸(折叠)，文件用等宽占位对齐。
		if n.Dir {
			chev := "▾"
			if !n.Open {
				chev = "▸"
			}
			if i == m.treeSel {
				label := indent + t.TreeChevron.Render(chev) + " " + glyph + " " + n.Name
				lines = append(lines, t.TreeActive.Width(avail).Render(truncate(label, rowW)))
			} else {
				label := indent + t.TreeChevron.Render(chev) + " " +
					t.TreeIcon[styleKey].Render(glyph) + " " + t.TreeName[styleKey].Render(n.Name)
				lines = append(lines, t.TreeItem.Width(avail).Render(truncate(label, rowW)))
			}
			continue
		}
		if i == m.treeSel {
			// 选中行整行反显：图标与文件名统一用强调前景色，保证在强调背景上可读。
			label := indent + "  " + glyph + " " + n.Name
			lines = append(lines, t.TreeActive.Width(avail).Render(truncate(label, rowW)))
			continue
		}
		// 未选中行按文件类型着色：emoji 自带颜色，文件名用同语义色区分高亮。
		icon := t.TreeIcon[styleKey].Render(glyph) + " "
		name := t.TreeName[styleKey].Render(n.Name)
		label := indent + "  " + icon + name
		lines = append(lines, t.TreeItem.Width(avail).Render(truncate(label, rowW)))
	}
	for len(lines) < h-2 {
		lines = append(lines, strings.Repeat(" ", avail))
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
		us, ue, hasHover := m.underlineSpan(i)
		code := m.renderHighlightedHover(b.Line(i), codeW, us, ue, hasHover)
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
	return m.renderHighlightedHover(line, width, 0, 0, false)
}

// renderHighlightedHover 与 renderHighlighted 相同，但把 rune 列区间
// [uStart, uEnd) 内的字符加下划线（Ctrl+hover 导航的 WebStorm 风格提示）。
// token 内按 rune 再切一段，保证下划线边界精确落在单词上，不破坏语法配色。
func (m *Model) renderHighlightedHover(line string, width, uStart, uEnd int, underline bool) string {
	var b strings.Builder
	col := 0
	for _, tk := range syntax.Highlight(line) {
		style := m.theme.SynStyle(tk.Color)
		if !underline {
			b.WriteString(style.Render(tk.Text))
			col += utf8.RuneCountInString(tk.Text)
			continue
		}
		// 逐 rune 走，遇到区间边界就切换样式。
		runes := []rune(tk.Text)
		segStart := 0
		segUnderline := inUnderline(col, underline, uStart, uEnd)
		for i := 0; i <= len(runes); i++ {
			curUnderline := i < len(runes) && inUnderline(col+i, underline, uStart, uEnd)
			if i == len(runes) || curUnderline != segUnderline {
				seg := string(runes[segStart:i])
				if seg != "" {
					s := style
					if segUnderline {
						s = s.Underline(true)
					}
					b.WriteString(s.Render(seg))
				}
				segStart = i
				segUnderline = curUnderline
			}
		}
		col += len(runes)
	}
	return truncateAnsi(b.String(), width)
}

// inUnderline 判断第 i 个 rune 列是否落在下划线区间。
func inUnderline(i int, underline bool, uStart, uEnd int) bool {
	return underline && i >= uStart && i < uEnd
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
	// Status 自带 Padding(0,1)：内容区是 w-2，按 w 截断同理有折行隐患。
	return t.Status.Width(w).Render(truncateWidth(left+strings.Repeat(" ", spacer)+right, w-2))
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

// placeOverlayAt 把 panel 叠到 base 的 (startX, startY) 处（字符列/行，0 基）。
// 用 ANSI 安全的宽度处理，避免 CJK 造成错位。
func placeOverlayAt(base, panel string, startX, startY int) string {
	lines := strings.Split(base, "\n")
	pl := strings.Split(panel, "\n")
	for i, row := range pl {
		yi := startY + i
		if yi < 0 || yi >= len(lines) {
			continue
		}
		baseLine := lines[yi]
		left := truncateWidth(baseLine, maxInt(0, startX))
		lines[yi] = left + row
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
