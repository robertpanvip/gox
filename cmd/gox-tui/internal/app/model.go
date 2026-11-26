// Package app 是 gox-tui 的 bubbletea 应用：一个终端里全屏运行的 IDE。
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/robertpanvip/gox-tui/internal/complete"
	"github.com/robertpanvip/gox-tui/internal/editor"
	"github.com/robertpanvip/gox-tui/internal/theme"
	"github.com/robertpanvip/gox/pkg/gxcomplete"
)

// overlay 是当前的浮层模式。
type overlay int

const (
	ovNone   overlay = iota
	ovSearch         // Ctrl+F 搜索
	ovOpen           // Ctrl+P 快速打开
	ovPeek           // F12 定义速览（std 符号）
	ovHover          // Ctrl+K 签名速览
)

// Model 是整个 IDE 的状态。
type Model struct {
	theme   *theme.Theme
	root    string
	tree    *fileTree
	buffers []*editor.Buffer
	active  int
	treeSel int
	focus   int // 0=文件树, 1=编辑区

	width  int
	height int

	// 编辑区滚动（0-based 起始行）
	scrollLn int

	// 浮层
	ov       overlay
	query    string
	matches  []string
	matchSel int

	// 状态提示（短暂显示）
	statusMsg string

	// 退出确认
	quitConfirm bool

	// 代码补全状态：输入标识符时弹候选，Tab/Enter 采纳替换当前 token。
	compEngine *complete.Engine
	compOpen   bool
	compCands  []complete.Candidate
	compSel    int
	compSeq    int    // 延迟查询序号，丢弃过期定时器
	compToken  string // 当前正在补全的 token（采纳时据此回退）

	// 类型感知导航：F12 跳转定义、Ctrl+K hover 签名。
	nav       *gxcomplete.Session
	peekDef   gxcomplete.Def       // peek 中的定义（std 符号）
	hoverInfo gxcomplete.HoverInfo // hover 签名信息
	readOnly  map[int]bool         // 只读标签（std 全文视图），按 buffer 索引

	// Ctrl+鼠标导航（WebStorm 风格）：按住 Ctrl 移入标识符时，在该单词下画下划线；
	// 再按住 Ctrl+左键点击即跳转到定义（本地跳行 / std 弹 peek）。
	navHover *wordSpan // 当前正在展示下划线的单词（nil 表示无）
}

// New 构建 Model。root 为空时退化为内存草稿。
func New(root string, dark bool, files []string) *Model {
	m := &Model{
		theme:    theme.New(toMode(dark)),
		root:     root,
		focus:    1,
		readOnly: map[int]bool{},
	}
	m.compEngine = complete.NewEngine(root)
	m.nav = gxcomplete.New()
	m.nav.Warm() // 与补全共用类型图，异步预热
	if root != "" {
		m.tree = newFileTree(root)
	}
	for _, f := range files {
		m.openFile(f)
	}
	if len(m.buffers) == 0 {
		m.buffers = []*editor.Buffer{editor.New("untitled.gx", welcomeSource)}
	}
	m.refreshMatches()
	return m
}

func toMode(dark bool) theme.Mode {
	if dark {
		return theme.Dark
	}
	return theme.Light
}

// 默认欢迎内容：展示语法高亮的例子。
const welcomeSource = `// 欢迎使用 gox-tui —— 终端里的 macOS 风格 IDE
// 快捷键：Ctrl+P 打开文件 · Ctrl+F 搜索 · Ctrl+T 切换主题 · Ctrl+S 保存
sig count = 0

func main() {
    // 这是一行注释
    name = "GX IDE"
    for i in range(3) {
        count = count + 1
    }
    return name
}`

// Current 返回当前 buffer。
func (m *Model) Current() *editor.Buffer { return m.buffers[m.active] }

// Init 实现 tea.Model。
func (m *Model) Init() tea.Cmd { return nil }

// openFile 从磁盘打开一个文件为标签（已打开则激活）。
func (m *Model) openFile(path string) {
	for i, b := range m.buffers {
		if b.Path != "" && b.Path == path {
			m.active = i
			return
		}
	}
	content, err := os.ReadFile(path)
	if err != nil {
		content = []byte("")
	}
	m.buffers = append(m.buffers, editor.New(path, string(content)))
	m.active = len(m.buffers) - 1
}

// closeActive 关闭当前标签（至少保留一个）。
func (m *Model) closeActive() {
	if len(m.buffers) <= 1 {
		return
	}
	m.buffers = append(m.buffers[:m.active], m.buffers[m.active+1:]...)
	if m.active >= len(m.buffers) {
		m.active = len(m.buffers) - 1
	}
}

// saveActive 保存当前 buffer 到磁盘（同步 IO，直接执行）。
func (m *Model) saveActive() {
	b := m.Current()
	if b.Path == "" || b.Path == "untitled.gx" {
		m.statusMsg = "未命名文件，请用终端指定路径"
		return
	}
	if err := os.WriteFile(b.Path, []byte(b.Text()), 0o644); err != nil {
		m.statusMsg = "保存失败: " + err.Error()
		return
	}
	b.MarkSaved()
	m.statusMsg = "已保存 " + b.Name
}

// Update 实现 tea.Model，处理所有按键与窗口事件。
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case completionTickMsg:
		m.refreshCompletion(msg.seq)
		return m, nil
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// 浮层优先处理。
	if m.ov == ovPeek {
		return m.handlePeekKey(key)
	}
	if m.ov == ovHover {
		return m.handleHoverKey(key)
	}
	if m.ov != ovNone {
		return m.handleOverlayKey(key)
	}

	// 全局快捷键（Ctrl 组合）。
	switch key {
	case "ctrl+c", "esc":
		if key == "esc" && m.focus == 0 {
			m.focus = 1
			return m, nil
		}
		m.quitConfirm = true
		return m, tea.Quit
	case "ctrl+s":
		m.saveActive()
		return m, nil
	case "ctrl+t":
		m.theme = m.theme.Toggle()
		m.statusMsg = "主题已切换"
		return m, nil
	case "ctrl+f":
		m.ov = ovSearch
		m.query = ""
		m.refreshMatches()
		return m, nil
	case "ctrl+p":
		m.ov = ovOpen
		m.query = ""
		m.refreshMatches()
		return m, nil
	case "ctrl+w":
		m.closeActive()
		return m, nil
	case "f12":
		m.gotoDefinition()
		return m, nil
	case "ctrl+k":
		m.openHover()
		return m, nil
	case "tab":
		// 补全浮层打开且焦点在编辑区时，Tab 用于采纳候选（优先级高于切换焦点）。
		if m.compOpen && m.focus == 1 {
			return m.handleEditorKey(msg)
		}
		if m.focus == 0 {
			m.focus = 1
		} else {
			m.focus = 0
		}
		return m, nil
	case "ctrl+tab":
		m.active = (m.active + 1) % len(m.buffers)
		return m, nil
	}

	if m.focus == 0 {
		return m.handleTreeKey(key)
	}
	return m.handleEditorKey(msg)
}

func (m *Model) handleTreeKey(key string) (tea.Model, tea.Cmd) {
	nodes := m.tree.nodes()
	switch key {
	case "up", "k":
		if m.treeSel > 0 {
			m.treeSel--
		}
	case "down", "j":
		if m.treeSel < len(nodes)-1 {
			m.treeSel++
		}
	case "right", "l":
		if m.treeSel < len(nodes) && nodes[m.treeSel].Dir && !nodes[m.treeSel].Open {
			m.tree.toggle(nodes[m.treeSel].Path)
		}
	case "left", "h":
		if m.treeSel < len(nodes) && nodes[m.treeSel].Dir && nodes[m.treeSel].Open {
			m.tree.toggle(nodes[m.treeSel].Path)
		}
	case "enter":
		if m.treeSel < len(nodes) {
			n := nodes[m.treeSel]
			if n.Dir {
				m.tree.toggle(n.Path)
			} else {
				m.openFile(n.Path)
				m.focus = 1
			}
		}
	}
	return m, nil
}

func (m *Model) handleEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// 任何键盘编辑/移动都意味着离开鼠标悬停态，清除导航下划线。
	m.clearNavHover()
	b := m.Current()
	key := msg.String()

	// 只读标签（std 源码视图）：只允许光标移动与滚动，禁止一切编辑。
	if m.readOnly[m.active] {
		switch key {
		case "up":
			b.MoveUp()
		case "down":
			b.MoveDown()
		case "left":
			b.MoveLeft()
		case "right":
			b.MoveRight()
		case "home", "ctrl+a":
			b.LineStart()
		case "end", "ctrl+e":
			b.LineEnd()
		case "pgup":
			ln, col := b.Cursor()
			b.SetCursor(ln-10, col)
		case "pgdown":
			ln, col := b.Cursor()
			b.SetCursor(ln+10, col)
		}
		m.ensureCursorVisible()
		return m, nil
	}

	// 补全浮层打开时，导航/采纳键优先归补全使用。
	if m.compOpen {
		switch key {
		case "up":
			m.moveCompSel(-1)
			return m, nil
		case "down":
			m.moveCompSel(1)
			return m, nil
		case "tab", "enter":
			return m, m.acceptComp()
		case "esc":
			m.closeComp()
			return m, nil
		}
	}

	// 文本输入（单 rune）。
	if len(key) == 1 || (len(key) > 0 && strings.HasPrefix(key, "runes:")) {
		for _, r := range msg.Runes {
			b.InsertRune(r)
		}
		m.ensureCursorVisible()
		return m, m.bumpComp()
	}

	switch key {
	case "up":
		b.MoveUp()
	case "down":
		b.MoveDown()
	case "left":
		b.MoveLeft()
	case "right":
		b.MoveRight()
	case "home", "ctrl+a":
		b.LineStart()
	case "end", "ctrl+e":
		b.LineEnd()
	case "backspace":
		b.Backspace()
	case "delete", "ctrl+d":
		b.DeleteForward()
	case "enter":
		b.Newline()
	case "tab":
		b.InsertString("    ")
	case "pgup":
		ln, col := b.Cursor()
		b.SetCursor(ln-10, col)
	case "pgdown":
		ln, col := b.Cursor()
		b.SetCursor(ln+10, col)
	case "ctrl+left":
		b.WordLeft()
	case "ctrl+right":
		b.WordRight()
	case "ctrl+shift+up":
		_, col := b.Cursor()
		b.SetCursor(0, col)
	case "ctrl+shift+down":
		_, col := b.Cursor()
		b.SetCursor(b.LineCount()-1, col)
	}
	m.ensureCursorVisible()
	if key == "backspace" || key == "delete" || key == "ctrl+d" {
		return m, m.bumpComp()
	}
	m.closeComp() // 光标移动/换行等：关闭补全
	return m, nil
}

// CursorLn 便捷访问（避免 editor 包暴露字段名）。
func (m *Model) CursorLn() int { ln, _ := m.Current().Cursor(); return ln }

// ensureCursorVisible 调整滚动让光标行在视口内。
func (m *Model) ensureCursorVisible() {
	ln, _ := m.Current().Cursor()
	vh := m.viewportHeight()
	if vh <= 0 {
		return
	}
	if ln < m.scrollLn {
		m.scrollLn = ln
	} else if ln >= m.scrollLn+vh {
		m.scrollLn = ln - vh + 1
	}
}

func (m *Model) viewportHeight() int {
	// 窗口高度 - 标题栏(1) - 标签栏(1) - 状态栏(1) - 边框(2)
	return maxInt(1, m.height-5)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 便捷错误格式化（供浮层与状态栏用）。
func errText(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprint(err)
}

var _ = filepath.Join
var _ = lipgloss.NewStyle
