// Package theme 提供 gox-ide 的 macOS 风格视觉系统。
//
// 设计目标：让终端里跑的 IDE 看起来像一台 Mac 上的原生应用——
//   - 深色/浅色双主题，配色取自 Apple Developer 文档与 Xcode 的柔和色调；
//   - 圆角边框（lipgloss.RoundedBorder）、充足留白、低对比分隔线；
//   - 标题栏红黄绿三个窗口点缀，悬停/选中态用 accent 色；
//   - 语法 token 色复用编辑器的语义名（keyword/str/comment/number/...），
//     由当前主题解析为具体色值。
//
// 所有 Style 在 New 时一次性构建并缓存，渲染热路径只做 .Render，零重复分配。
package theme

import (
	"github.com/charmbracelet/lipgloss"
)

// Mode 是明暗模式。
type Mode int

const (
	Dark Mode = iota
	Light
)

// Palette 是一套完整的色板。语法色也在这里，保证明暗主题各有适配对比度。
type Palette struct {
	// 结构色
	BG       lipgloss.Color // 全局背景
	Panel    lipgloss.Color // 编辑区/面板背景
	Sidebar  lipgloss.Color // 文件树背景
	TitleBar lipgloss.Color // 标题栏背景
	Surface  lipgloss.Color // 悬浮层（标签选中、搜索框）
	Border   lipgloss.Color // 分隔线（低对比）
	Text     lipgloss.Color // 主文字
	Muted    lipgloss.Color // 次要文字（行号、状态栏）
	Accent   lipgloss.Color // 强调色（选中、光标、品牌蓝）
	AccentFG lipgloss.Color // accent 背景上的前景色

	// macOS 窗口点缀
	Close    lipgloss.Color
	Minimize lipgloss.Color
	Zoom     lipgloss.Color

	// 语法 token 色
	SynKeyword lipgloss.Color
	SynFn      lipgloss.Color
	SynType    lipgloss.Color
	SynParam   lipgloss.Color
	SynBracket lipgloss.Color
	SynStr     lipgloss.Color
	SynComment lipgloss.Color
	SynNumber  lipgloss.Color
	SynTag     lipgloss.Color
	SynAttr    lipgloss.Color
	SynPunct   lipgloss.Color
	SynText    lipgloss.Color

	// 文件树图标色
	IconFolder lipgloss.Color
	IconGo     lipgloss.Color
	IconTest   lipgloss.Color
	IconGX     lipgloss.Color
	IconMD     lipgloss.Color
	IconConf   lipgloss.Color
	IconScript lipgloss.Color
	IconFile   lipgloss.Color
}

// Apple 官方系统色（深色 / 浅色）。
func paletteFor(m Mode) Palette {
	if m == Dark {
		return Palette{
			BG: "#1c1c1e", Panel: "#1e1e20", Sidebar: "#232325",
			TitleBar: "#2b2b2e", Surface: "#323235", Border: "#3a3a3d",
			Text: "#e6e6e8", Muted: "#8e8e93", Accent: "#0a84ff", AccentFG: "#ffffff",
			Close: "#ff5f57", Minimize: "#febc2e", Zoom: "#28c840",
			SynKeyword: "#ff7ab2", SynFn: "#82aaff", SynType: "#c3e88d",
			SynParam: "#ffcb6b", SynBracket: "#89ddff", SynStr: "#ffd580",
			SynComment: "#7f848e", SynNumber: "#f78c6c", SynTag: "#ff7ab2",
			SynAttr: "#a5d6ff", SynPunct: "#9aa0a6", SynText: "#e6e6e8",
			IconFolder: "#ffcb6b", IconGo: "#79c0ff", IconTest: "#c3e88d",
			IconGX: "#82aaff", IconMD: "#89ddff", IconConf: "#ff9e64",
			IconScript: "#f07178", IconFile: "#9aa0a6",
		}
	}
	return Palette{
		BG: "#f2f2f7", Panel: "#ffffff", Sidebar: "#ececf1",
		TitleBar: "#e8e8ed", Surface: "#ffffff", Border: "#d1d1d6",
		Text: "#1d1d1f", Muted: "#86868b", Accent: "#007aff", AccentFG: "#ffffff",
		Close: "#ff5f57", Minimize: "#febc2e", Zoom: "#28c840",
		SynKeyword: "#ad3da4", SynFn: "#1b6fc4", SynType: "#2f8f46",
		SynParam: "#b26a00", SynBracket: "#0a7b86", SynStr: "#a67f00",
		SynComment: "#8e8e93", SynNumber: "#c0392b", SynTag: "#ad3da4",
		SynAttr: "#2b6cb0", SynPunct: "#6e6e73", SynText: "#1d1d1f",
		IconFolder: "#b26a00", IconGo: "#1b6fc4", IconTest: "#2f8f46",
		IconGX: "#3b5bdb", IconMD: "#0a7b86", IconConf: "#b3591f",
		IconScript: "#c0392b", IconFile: "#6e6e73",
	}
}

// Theme 持有一整套预构建的 lipgloss 样式。渲染时只用这些样式对象。
type Theme struct {
	Mode    Mode
	Palette Palette

	// 容器
	App      lipgloss.Style // 整个窗口：圆角边框 + 背景
	TitleBar lipgloss.Style
	Sidebar  lipgloss.Style
	Editor   lipgloss.Style
	Status   lipgloss.Style
	Divider  lipgloss.Style

	// 标题栏
	Dots      [3]lipgloss.Style
	TitleText lipgloss.Style
	Tab       lipgloss.Style
	TabActive lipgloss.Style

	// 文件树
	TreeItem   lipgloss.Style
	TreeActive lipgloss.Style
	TreeFolder lipgloss.Style
	TreeIcon   map[string]lipgloss.Style // folder/go/test/gx/md/conf/script/file

	// 编辑区
	LineNo     lipgloss.Style
	CursorLine lipgloss.Style
	Code       lipgloss.Style

	// 状态栏
	StatusKey lipgloss.Style
	StatusVal lipgloss.Style

	// 语法
	Syn map[string]lipgloss.Style

	// 浮层（搜索/快速打开）列表项：无 padding，宽度可控。
	OverlayItem   lipgloss.Style
	OverlayActive lipgloss.Style
	OverlayBox    lipgloss.Style
	OverlayInput  lipgloss.Style

	// 按键提示
	HelpKey lipgloss.Style
	HelpTxt lipgloss.Style
}

// New 构建指定明暗模式的主题；所有样式一次成型。
func New(m Mode) *Theme {
	p := paletteFor(m)
	t := &Theme{Mode: m, Palette: p}

	t.App = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.Border).
		Background(p.BG).
		Padding(0)

	t.TitleBar = lipgloss.NewStyle().
		Background(p.TitleBar).
		Foreground(p.Text).
		Padding(0, 1)

	t.Sidebar = lipgloss.NewStyle().
		Background(p.Sidebar).
		Foreground(p.Text).
		Padding(1, 1)

	t.Editor = lipgloss.NewStyle().
		Background(p.Panel).
		Foreground(p.Text)

	t.Status = lipgloss.NewStyle().
		Background(p.TitleBar).
		Foreground(p.Muted).
		Padding(0, 1)

	t.Divider = lipgloss.NewStyle().Foreground(p.Border)

	t.Dots = [3]lipgloss.Style{
		lipgloss.NewStyle().Foreground(p.Close).SetString("●"),
		lipgloss.NewStyle().Foreground(p.Minimize).SetString("●"),
		lipgloss.NewStyle().Foreground(p.Zoom).SetString("●"),
	}
	t.TitleText = lipgloss.NewStyle().Foreground(p.Muted).Bold(true)

	t.Tab = lipgloss.NewStyle().
		Foreground(p.Muted).
		Background(p.TitleBar).
		Padding(0, 2)
	t.TabActive = lipgloss.NewStyle().
		Foreground(p.Text).
		Background(p.Surface).
		Bold(true).
		Padding(0, 2)

	t.TreeItem = lipgloss.NewStyle().Foreground(p.Text).Padding(0, 1)
	t.TreeActive = lipgloss.NewStyle().
		Foreground(p.AccentFG).Background(p.Accent).Padding(0, 1)
	t.TreeFolder = lipgloss.NewStyle().Foreground(p.Muted).Bold(true).Padding(0, 1)
	t.TreeIcon = map[string]lipgloss.Style{
		"folder": lipgloss.NewStyle().Foreground(p.IconFolder).Bold(true),
		"go":     lipgloss.NewStyle().Foreground(p.IconGo).Bold(true),
		"test":   lipgloss.NewStyle().Foreground(p.IconTest),
		"gx":     lipgloss.NewStyle().Foreground(p.IconGX).Bold(true),
		"md":     lipgloss.NewStyle().Foreground(p.IconMD),
		"conf":   lipgloss.NewStyle().Foreground(p.IconConf),
		"script": lipgloss.NewStyle().Foreground(p.IconScript),
		"file":   lipgloss.NewStyle().Foreground(p.IconFile),
	}

	t.LineNo = lipgloss.NewStyle().Foreground(p.Muted)
	t.CursorLine = lipgloss.NewStyle().Background(p.Surface)
	t.Code = lipgloss.NewStyle().Foreground(p.Text)

	t.StatusKey = lipgloss.NewStyle().Foreground(p.Muted).Bold(true)
	t.StatusVal = lipgloss.NewStyle().Foreground(p.Text)

	t.Syn = map[string]lipgloss.Style{
		"keyword": lipgloss.NewStyle().Foreground(p.SynKeyword).Bold(true),
		"fn":      lipgloss.NewStyle().Foreground(p.SynFn),
		"type":    lipgloss.NewStyle().Foreground(p.SynType),
		"param":   lipgloss.NewStyle().Foreground(p.SynParam),
		"bracket": lipgloss.NewStyle().Foreground(p.SynBracket),
		"str":     lipgloss.NewStyle().Foreground(p.SynStr),
		"comment": lipgloss.NewStyle().Foreground(p.SynComment).Italic(true),
		"number":  lipgloss.NewStyle().Foreground(p.SynNumber),
		"tag":     lipgloss.NewStyle().Foreground(p.SynTag).Bold(true),
		"attr":    lipgloss.NewStyle().Foreground(p.SynAttr),
		"punct":   lipgloss.NewStyle().Foreground(p.SynPunct),
		"text":    lipgloss.NewStyle().Foreground(p.SynText),
	}

	t.OverlayItem = lipgloss.NewStyle().Foreground(p.Text)
	t.OverlayActive = lipgloss.NewStyle().Foreground(p.AccentFG).Background(p.Accent)
	t.OverlayBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.Accent).
		Background(p.Surface)
	t.OverlayInput = lipgloss.NewStyle().
		Background(p.Surface).
		Foreground(p.Text)

	t.HelpKey = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	t.HelpTxt = lipgloss.NewStyle().Foreground(p.Muted)

	return t
}

// Toggle 返回相反模式的主题。
func (t *Theme) Toggle() *Theme {
	if t.Mode == Dark {
		return New(Light)
	}
	return New(Dark)
}

// SynStyle 取语义 token 对应的样式；未知 token 回落正文色。
func (t *Theme) SynStyle(name string) lipgloss.Style {
	if s, ok := t.Syn[name]; ok {
		return s
	}
	return t.Syn["text"]
}
