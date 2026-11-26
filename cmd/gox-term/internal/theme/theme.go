// Package theme 提供 gox-term 的 macOS 风格视觉（精简版，独立于 gox-tui）。
// 配色与 gox-tui 同源：Apple 深浅双主题 + 圆角边框 + 红黄绿窗口点缀。
package theme

import "github.com/charmbracelet/lipgloss"

type Mode int

const (
	Dark Mode = iota
	Light
)

type Palette struct {
	BG, Panel, TitleBar, Surface, Border lipgloss.Color
	Text, Muted, Accent, AccentFG        lipgloss.Color
	Close, Minimize, Zoom                lipgloss.Color

	// 标题栏渐变两端（左 → 右），模拟毛玻璃光影。
	TitleGradFrom, TitleGradTo string
}

func paletteFor(m Mode) Palette {
	if m == Dark {
		return Palette{
			BG: "#151517", Panel: "#0f0f11", TitleBar: "#242427", Surface: "#2e2e32",
			Border: "#3a3a3f", Text: "#e8e8ea", Muted: "#86868e", Accent: "#0a84ff", AccentFG: "#ffffff",
			Close: "#ff5f57", Minimize: "#febc2e", Zoom: "#28c840",
			TitleGradFrom: "#303035", TitleGradTo: "#242427",
		}
	}
	return Palette{
		BG: "#ececf1", Panel: "#ffffff", TitleBar: "#e6e6ec", Surface: "#f6f6f9",
		Border: "#c9c9d1", Text: "#1d1d1f", Muted: "#86868b", Accent: "#007aff", AccentFG: "#ffffff",
		Close: "#ff5f57", Minimize: "#febc2e", Zoom: "#28c840",
		TitleGradFrom: "#f4f4f8", TitleGradTo: "#e6e6ec",
	}
}

type Theme struct {
	Mode    Mode
	Palette Palette

	Window    lipgloss.Style
	TitleBar  lipgloss.Style
	Status    lipgloss.Style
	Terminal  lipgloss.Style
	Dots      [3]lipgloss.Style
	TitleText lipgloss.Style
	// 标签栏用手写 ANSI 渲染（见 app/gradient.go），不用 lipgloss 样式，
	// 以规避 alt-screen 下多层 Background 嵌套被错误染成 accent 色的问题。
	StatusKey lipgloss.Style
	StatusVal lipgloss.Style
	HelpKey   lipgloss.Style
	HelpTxt   lipgloss.Style
	Divider   lipgloss.Style // 区隔细线
	Badge     lipgloss.Style // accent 小徽标
}

func New(m Mode) *Theme {
	p := paletteFor(m)
	t := &Theme{Mode: m, Palette: p}
	t.Window = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.Border).Background(p.BG)
	t.TitleBar = lipgloss.NewStyle().Background(p.TitleBar).Foreground(p.Text)
	t.Terminal = lipgloss.NewStyle().Background(p.Panel).Foreground(p.Text)
	t.Status = lipgloss.NewStyle().Background(p.TitleBar).Foreground(p.Muted)
	t.Dots = [3]lipgloss.Style{
		lipgloss.NewStyle().Foreground(p.Close).SetString("●"),
		lipgloss.NewStyle().Foreground(p.Minimize).SetString("●"),
		lipgloss.NewStyle().Foreground(p.Zoom).SetString("●"),
	}
	t.TitleText = lipgloss.NewStyle().Foreground(p.Muted).Bold(true)
	t.StatusKey = lipgloss.NewStyle().Foreground(p.Muted).Bold(true)
	t.StatusVal = lipgloss.NewStyle().Foreground(p.Text)
	t.HelpKey = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	t.HelpTxt = lipgloss.NewStyle().Foreground(p.Muted)
	t.Divider = lipgloss.NewStyle().Foreground(p.Border)
	t.Badge = lipgloss.NewStyle().Background(p.Accent).Foreground(p.AccentFG).Padding(0, 1).Bold(true)
	return t
}

func (t *Theme) Toggle() *Theme {
	if t.Mode == Dark {
		return New(Light)
	}
	return New(Dark)
}
