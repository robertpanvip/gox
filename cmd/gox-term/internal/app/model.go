// Package app 是 gox-term 的 bubbletea 应用：终端里运行的 macOS 风格终端面板。
package app

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/robertpanvip/gox-term/internal/complete"
	"github.com/robertpanvip/gox-term/internal/screen"
	"github.com/robertpanvip/gox-term/internal/session"
	"github.com/robertpanvip/gox-term/internal/theme"
)

// Model 是终端面板状态。
type Model struct {
	theme    *theme.Theme
	prog     *tea.Program
	sessions []*session.Session
	screens  map[int]*screen.Screen // 每个会话一块屏幕（VT 模拟器）
	active   int
	nextID   int
	shell    string

	width, height int
	// 终端显示区尺寸（去掉标题栏/状态栏/边框）
	cols, rows int

	// 滚动偏移（从缓冲末尾往前数多少行）
	scrollBack int
	autoScroll bool

	statusMsg string

	// 旁路补全状态：shell 行编辑保持权威，这里只做"读取屏幕→弹候选→回发采纳"。
	compEngine *complete.Engine
	compOpen   bool
	compCands  []complete.Candidate
	compSel    int
	compSeq    int    // 延迟查询序号，丢弃过期定时器
	compStart  int    // 浮层在终端可见行的起始行（-1 表示未显示）
	homeDir    string // 补全取不到 shell cwd 时的兜底目录

	// 鼠标文本选区（网格坐标，0 基）。selecting 表示正在拖拽。
	selecting bool
	selAnchor struct{ x, y int }
	selHead   struct{ x, y int }
}

// New 构建 Model（不立即建会话，等 Init 拿到 prog 后建）。
func New(dark bool, shell string) *Model {
	wd, _ := os.Getwd()
	m := &Model{
		theme: theme.New(toMode(dark)), shell: shell,
		autoScroll: true, screens: map[int]*screen.Screen{},
		homeDir: wd, compStart: -1,
	}
	m.compEngine = complete.NewEngine(wd)
	return m
}

func toMode(dark bool) theme.Mode {
	if dark {
		return theme.Dark
	}
	return theme.Light
}

func (m *Model) Init() tea.Cmd { return nil }

// BindProgram 由 main 在创建 tea.Program 后回灌，供 session 投递消息。
func (m *Model) BindProgram(p *tea.Program) {
	m.prog = p
	m.newSession()
}

// newSession 建一个新 shell 标签。
func (m *Model) newSession() {
	if m.prog == nil {
		return
	}
	cols, rows := uint16(80), uint16(24)
	if m.cols > 0 {
		cols = uint16(m.cols)
	}
	if m.rows > 0 {
		rows = uint16(m.rows)
	}
	s, err := session.New(m.nextID, m.prog, m.shell, cols, rows)
	if err != nil {
		m.statusMsg = "启动 shell 失败: " + err.Error()
		return
	}
	m.sessions = append(m.sessions, s)
	// 建屏幕模拟器，并让默认前景/背景跟随主题面板色。
	sc := screen.New(int(cols), int(rows))
	p := m.theme.Palette
	sc.SetDefaultColors(string(p.Text), string(p.Panel))
	// 重放建立前已到达的 PTY 输出：readLoop 在 Run() 前就可能产生提示符，
	// 那时 tea.Program.Send 尚未消费，OutputMsg 可能丢失；从缓冲补齐。
	if early := s.Snapshot(); len(early) > 0 {
		sc.Write(early)
	}
	m.screens[s.ID] = sc
	m.active = len(m.sessions) - 1
	m.nextID++
	m.scrollBack = 0
	m.autoScroll = true
}

func (m *Model) current() *session.Session {
	if len(m.sessions) == 0 {
		return nil
	}
	return m.sessions[m.active]
}

func (m *Model) closeActive() {
	if len(m.sessions) == 0 {
		return
	}
	closed := m.sessions[m.active]
	closed.Close()
	delete(m.screens, closed.ID)
	m.sessions = append(m.sessions[:m.active], m.sessions[m.active+1:]...)
	if len(m.sessions) == 0 {
		m.newSession()
		return
	}
	if m.active >= len(m.sessions) {
		m.active = len(m.sessions) - 1
	}
}

// Update 实现 tea.Model。
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		oldCols, oldRows := m.cols, m.rows
		m.width, m.height = msg.Width, msg.Height
		m.cols = maxInt(20, m.width-2)
		m.rows = maxInt(4, bodyHeight(m.height))
		if oldCols != m.cols || oldRows != m.rows {
			for _, s := range m.sessions {
				s.Resize(uint16(m.cols), uint16(m.rows))
			}
			for _, sc := range m.screens {
				sc.Resize(m.cols, m.rows)
			}
			m.scrollBack = 0
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case completionTickMsg:
		m.refreshCompletion(msg.id, msg.seq)
		return m, nil

	case session.OutputMsg:
		// 把 PTY 字节喂给对应会话的 VT 模拟器——这是让 vim/htop 正常显示的关键。
		// 解析必须在 UI 线程完成，避免与渲染并发读写网格。
		if sc := m.screens[msg.ID]; sc != nil {
			sc.Write(msg.Data)
		}
		if s := m.current(); s != nil && s.ID == msg.ID && m.autoScroll {
			m.scrollBack = 0
		}
		return m, nil

	case session.ExitedMsg:
		for i, s := range m.sessions {
			if s.ID == msg.ID {
				m.statusMsg = fmt.Sprintf("会话 %d 已退出", msg.ID)
				// 不自动删除标签，让用户看到最后的退出画面。
				_ = i
				break
			}
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// bracketed paste：bubbletea 把整段粘贴文本放进 Runes，用 bracketed-paste
	// 序列包裹后整体写入 PTY，让 shell/readline 当作一次粘贴（vim 里也不会逐字符执行）。
	if msg.Paste && len(msg.Runes) > 0 {
		m.pasteToShell(string(msg.Runes))
		return m, nil
	}

	key := msg.String()

	// 补全浮层打开时，导航/采纳键优先归补全使用。
	if m.compOpen {
		switch key {
		case "up":
			m.moveCompletionSel(-1)
			return m, nil
		case "down":
			m.moveCompletionSel(1)
			return m, nil
		case "tab", "enter", "ctrl+y":
			return m, m.acceptCompletion(m.compSel)
		case "esc":
			m.closeCompletion()
			return m, nil
		default:
			// 数字快选：1..9 直接采纳对应候选。
			if len(key) == 1 && key[0] >= '1' && key[0] <= '9' {
				idx := int(key[0] - '1')
				if idx < len(m.compCands) {
					return m, m.acceptCompletion(idx)
				}
			}
		}
	}

	// 全局控制键（在转发给 shell 之前拦截）。
	switch key {
	case "alt+v", "f9":
		// 兜底粘贴：从本地剪贴板读取（系统原生 Cmd/Ctrl+Shift+V 走 Paste 路径）。
		// Alt+V 在部分 xterm 下被编码成 UTF-8 meta 字符，故同时提供 F9。
		m.pasteFromClipboard()
		return m, nil
	case "ctrl+t":
		m.newSession()
		return m, nil
	case "ctrl+w":
		m.closeActive()
		return m, nil
	case "ctrl+n", "ctrl+tab":
		// Ctrl+Tab 在部分终端（如 xterm）会退化成普通 Tab，Ctrl+N 作为可靠替代。
		if len(m.sessions) > 1 {
			m.active = (m.active + 1) % len(m.sessions)
			m.scrollBack = 0
			m.autoScroll = true
		}
		return m, nil
	case "ctrl+shift+up":
		m.scrollBack += 5
		m.autoScroll = false
		return m, nil
	case "ctrl+shift+down":
		m.scrollBack = maxInt(0, m.scrollBack-5)
		if m.scrollBack == 0 {
			m.autoScroll = true
		}
		return m, nil
	case "ctrl+l":
		// 清屏：发 Ctrl+L 给 shell（由 shell/程序自己处理），同时清本地缓冲做不到（在 session 内），
		// 这里把滚动复位即可。
		m.scrollBack = 0
		m.autoScroll = true
	}

	// 其余按键编码成字节写进 PTY，交给 shell 处理。
	b := keyBytes(msg)
	if len(b) > 0 {
		if s := m.current(); s != nil {
			_, _ = s.Write(b)
		}
		m.scrollBack = 0
		m.autoScroll = true
		// 可打印字符 / 退格 / 方向键可能改变当前行，安排一次延迟补全查询。
		if isCompletionRelevant(msg) {
			return m, m.bumpCompletion()
		}
	}
	return m, nil
}

// isCompletionRelevant 判断按键是否可能改变 shell 当前输入行，值得触发补全查询。
func isCompletionRelevant(msg tea.KeyMsg) bool {
	if len(msg.Runes) > 0 {
		return true
	}
	switch msg.String() {
	case "backspace", "delete", "left", "right", "home", "end", "ctrl+a", "ctrl+e", "ctrl+u", "ctrl+w":
		return true
	}
	// 单个可打印 ASCII（keyBytesFromString 已覆盖特殊键，剩下的是普通字符）。
	k := msg.String()
	return len(k) == 1 && k[0] >= 0x20 && k[0] < 0x7f
}

// keyBytes 把 tea.KeyMsg 编码为终端按键序列。
func keyBytes(msg tea.KeyMsg) []byte {
	b := keyBytesFromString(msg.String())
	if len(b) > 0 {
		return b
	}
	// 可打印字符（Runes 优先，兼容输入法）。
	if r := msg.Runes; len(r) > 0 {
		return []byte(string(r))
	}
	return nil
}

// keyBytesFromString 把按键名编码为终端序列；可打印字符返回单字节。
func keyBytesFromString(key string) []byte {
	switch key {
	case "enter":
		return []byte{'\r'}
	case "backspace":
		return []byte{0x7f}
	case "tab":
		return []byte{'\t'}
	case "esc":
		return []byte{0x1b}
	case "up":
		return []byte{0x1b, '[', 'A'}
	case "down":
		return []byte{0x1b, '[', 'B'}
	case "right":
		return []byte{0x1b, '[', 'C'}
	case "left":
		return []byte{0x1b, '[', 'D'}
	case "home":
		return []byte{0x1b, '[', 'H'}
	case "end":
		return []byte{0x1b, '[', 'F'}
	case "delete":
		return []byte{0x1b, '[', '3', '~'}
	case "pgup":
		return []byte{0x1b, '[', '5', '~'}
	case "pgdown":
		return []byte{0x1b, '[', '6', '~'}
	case "ctrl+c":
		return []byte{0x03}
	case "ctrl+d":
		return []byte{0x04}
	case "ctrl+z":
		return []byte{0x1a}
	case "ctrl+a":
		return []byte{0x01}
	case "ctrl+e":
		return []byte{0x05}
	case "ctrl+k":
		return []byte{0x0b}
	case "ctrl+u":
		return []byte{0x15}
	case "ctrl+r":
		return []byte{0x12}
	}
	// 可打印单字节字符。
	if len(key) == 1 {
		return []byte(key)
	}
	return nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// bodyHeight 是终端显示区行数，必须与 view.go 的布局段数严格一致：
// 窗口边框(2) + 标题(1) + 标签(1) + 细线(1) + 状态(1) + 细线(1) = 7。
// PTY 的 winsize 和屏幕网格都用这个值，否则网格行数与可见行错位。
func bodyHeight(totalHeight int) int {
	return maxInt(4, totalHeight-7)
}

var _ = strings.TrimSpace
