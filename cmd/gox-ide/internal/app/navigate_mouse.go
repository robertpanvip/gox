package app

// Ctrl+鼠标导航（WebStorm 风格）：按住 Ctrl 移入标识符 → 该单词显示下划线；
// Ctrl+左键点击 → 跳转到定义。坐标换算全部基于 View() 的布局常量，纯函数便于单测。

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// wordSpan 是屏幕上的一个可导航单词：所在 buffer 行、rune 列区间 [Start, End)、文本。
type wordSpan struct {
	Line       int
	Start, End int // rune 列，End 为开区间
	Text       string
}

// treeWidth 与 view.go 的 viewBody / compOverlayView 保持一致。
func (m *Model) treeWidth() int {
	treeW := (m.width - 2) * 25 / 100
	if treeW < 16 {
		treeW = 16
	}
	if treeW > 42 {
		treeW = 42
	}
	return treeW
}

// screenToCodeCol 把鼠标屏幕 x 换算成编辑区当前行的 rune 列。
// 屏幕布局（0 基，见 View/viewBody/viewEditor）：
//
//	x=0            窗口左边框
//	x=1..treeW     文件树
//	x=treeW+1      竖分隔线
//	x=treeW+2      +noWidth 之前是行号
//	x=treeW+2+noWidth  代码区起点
//
// CJK 在屏幕占 2 列，按显示宽度逐 rune 累加，返回覆盖该屏幕列的 rune 下标。
func (m *Model) screenToCodeCol(screenX, line int) (col int, ok bool) {
	codeStart := 1 + m.treeWidth() + 1 + m.lineNoWidth()
	if screenX < codeStart {
		return 0, false
	}
	dx := screenX - codeStart // 距代码区起点的屏幕列偏移
	runes := []rune(m.Current().Line(line))
	w := 0
	for i, r := range runes {
		rw := runeCellWidth(r)
		if dx < w+rw {
			return i, true // 落在第 i 个 rune（含宽字符首列）
		}
		w += rw
	}
	// 落在行尾空白区：吸附到行末，便于点击行尾的符号后空白处。
	if dx == w {
		return len(runes), true
	}
	return 0, false
}

// screenToLine 把鼠标屏幕 y 换算成 buffer 行（0 基）。
// 屏幕布局：y=0 上边框，y=1 标题栏，y=2 起是 body 第 0 行。
func (m *Model) screenToLine(screenY int) (int, bool) {
	const bodyTop = 2
	rel := screenY - bodyTop
	if rel < 0 {
		return 0, false
	}
	vpH := m.viewportHeight()
	b := m.Current()
	ln := m.scrollLn + rel
	if ln < 0 || ln >= b.LineCount() || rel >= vpH {
		return 0, false
	}
	return ln, true
}

// lineNoWidth 与 viewEditor 一致。
func (m *Model) lineNoWidth() int {
	return len(itoa(m.Current().LineCount())) + 2
}

// runeCellWidth 返回 rune 在等宽终端里的显示列宽。
func runeCellWidth(r rune) int {
	if r < 0x20 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if r < 0x2ff || (r >= 0x370 && r < 0x1100) {
		return 1
	}
	return 2
}

// isNavIdent 判断 rune 是否属于"标识符"。与 gxcomplete 的 isIdentRune 同规则：
// 下划线、ASCII 字母数字、以及所有 r>=0x80（含 CJK）。
func isNavIdent(r rune) bool {
	return r == '_' || (r >= '0' && r <= '9') ||
		(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r >= 0x80
}

// wordSpanAt 求 buffer 第 line 行中覆盖 rune 列 col 的单词范围。
// col 落在标识符内部或紧贴其末尾/开头时都能命中；落在标点/空白上返回 nil。
func (m *Model) wordSpanAt(line, col int) *wordSpan {
	text := m.Current().Line(line)
	runes := []rune(text)
	if col < 0 || col > len(runes) {
		return nil
	}
	// 确定一个"锚点"rune：优先 col 本身，其次左侧（贴合"点末尾也命中"的直觉）。
	anchor := -1
	if col < len(runes) && isNavIdent(runes[col]) {
		anchor = col
	} else if col > 0 && isNavIdent(runes[col-1]) {
		anchor = col - 1
	}
	if anchor < 0 {
		return nil
	}
	start, end := anchor, anchor+1
	for start > 0 && isNavIdent(runes[start-1]) {
		start--
	}
	for end < len(runes) && isNavIdent(runes[end]) {
		end++
	}
	return &wordSpan{Line: line, Start: start, End: end, Text: string(runes[start:end])}
}

// handleMouse 处理 Ctrl+hover / Ctrl+点击（WebStorm 风格）。
//
// 终端兼容性说明（重要）：
//   - 现代终端（iTerm2/Kitty/Alacritty/Windows Terminal）会把 Ctrl 修饰如实编码进
//     鼠标事件，Ctrl+hover 显示下划线、Ctrl+左键点击跳转都能原生工作。
//   - xterm 会"吞掉" Ctrl+鼠标点击（连 press 都不上报），只保留 Ctrl+hover 的 motion。
//     因此在 xterm 下提供等价兼容路径：先 Ctrl+悬停使单词出现下划线，再普通左键
//     点击该单词即可跳转。下划线本身就是"此处可导航"的可视提示，语义自然。
func (m *Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// 文件树交互：普通左键点击即可（xterm 对无修饰点击正常上报）。
	// 浮层打开时不响应，避免与浮层键盘交互冲突。
	if m.ov == ovNone && m.inTreeX(msg.X) {
		idx, ok := m.nodeAt(msg.Y)
		if ok {
			if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
				nodes := m.tree.nodes()
				n := nodes[idx]
				m.treeSel = idx
				if n.Dir {
					m.tree.toggle(n.Path) // 目录：展开/折叠
					m.focus = 0
				} else {
					m.openFile(n.Path) // 文件：打开为标签并切到编辑区
					m.closeComp()
					m.focus = 1
				}
				m.clearNavHover()
				return m, nil
			}
			if msg.Action == tea.MouseActionMotion {
				// 悬停高亮：仅更新选中行，不改变焦点、不打开文件。
				m.treeSel = idx
				return m, nil
			}
		}
	}

	// 只在"编辑区可导航"状态下激活：无浮层、焦点在编辑区、非只读标签。
	usable := m.ov == ovNone && m.focus == 1 && !m.readOnly[m.active]
	if !usable {
		m.navHover = nil
		return m, nil
	}

	ln, ok := m.screenToLine(msg.Y)
	if !ok {
		return m, nil
	}
	col, ok := m.screenToCodeCol(msg.X, ln)
	if !ok {
		return m, nil
	}
	span := m.wordSpanAt(ln, col)

	// 左键点击：Ctrl+点击（现代终端），或"已悬停单词上的普通点击"（xterm 兼容）。
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		target := span
		if target == nil && msg.Ctrl {
			return m, nil
		}
		// 兼容路径：无 Ctrl 时，只有点击正落在已下划线单词上才跳转。
		if !msg.Ctrl {
			h := m.navHover
			if h == nil || h.Line != ln || (col < h.Start || col > h.End) {
				m.navHover = nil
				return m, nil
			}
			target = h
		}
		m.navHover = nil
		m.Current().SetCursor(target.Line, target.End)
		m.gotoDefinition()
		return m, nil
	}

	// 鼠标移动：仅带 Ctrl 时建立/更新下划线。
	if msg.Action != tea.MouseActionMotion {
		return m, nil
	}
	if !msg.Ctrl {
		m.navHover = nil // 松开 Ctrl 后的普通移动：清除下划线
		return m, nil
	}
	m.navHover = span
	return m, nil
}

// clearNavHover 在编辑内容变化或切换焦点时清除下划线。
func (m *Model) clearNavHover() { m.navHover = nil }

// underlineSpan 返回应用在 code ANSI 字符串上的下划线区间（rune 列）。
// 供 view 层渲染时查询，避免 view 直接依赖 wordSpan 内部字段语义。
func (m *Model) underlineSpan(line int) (start, end int, ok bool) {
	h := m.navHover
	if h == nil || h.Line != line {
		return 0, 0, false
	}
	return h.Start, h.End, true
}

// spanContains 供测试与渲染使用：判断 rune 列 i 是否在 [Start,End)。
func (s *wordSpan) contains(i int) bool { return s != nil && i >= s.Start && i < s.End }

var _ = strings.TrimSpace
