// Package theme 提供 gox-term 的 macOS 风格视觉（精简版，独立于 gox-ide）。
// 配色与 gox-ide 同源：Apple 深浅双主题 + 圆角边框 + 红黄绿窗口点缀。
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
}

func paletteFor(m Mode) Palette {
	if m == Dark {
		return Palette{
			BG: "#1c1c1e", Panel: "#141416", TitleBar: "#2b2b2e", Surface: "#323235",
			Border: "#3a3a3d", Text: "#e6e6e8", Muted: "#8e8e93", Accent: "#0a84ff", AccentFG: "#ffffff",
			Close: "#ff5f57", Minimize: "#febc2e", Zoom: "#28c840",
		}
	}
	return Palette{
		BG: "#f2f2f7", Panel: "#ffffff", TitleBar: "#e8e8ed", Surface: "#ffffff",
		Border: "#d1d1d6", Text: "#1d1d1f", Muted: "#86868b", Accent: "#007aff", AccentFG: "#ffffff",
		Close: "#ff5f57", Minimize: "#febc2e", Zoom: "#28c840",
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
	Tab       lipgloss.Style
	TabActive lipgloss.Style
	StatusKey lipgloss.Style
	StatusVal lipgloss.Style
	HelpKey   lipgloss.Style
	HelpTxt   lipgloss.Style
}

func New(m Mode) *Theme {
	p := paletteFor(m)
	t := &Theme{Mode: m, Palette: p}
	t.Window = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(p.Border).Background(p.BG)
	t.TitleBar = lipgloss.NewStyle().Background(p.TitleBar).Foreground(p.Text).Padding(0, 1)
	t.Terminal = lipgloss.NewStyle().Background(p.Panel).Foreground(p.Text)
	t.Status = lipgloss.NewStyle().Background(p.TitleBar).Foreground(p.Muted).Padding(0, 1)
	t.Dots = [3]lipgloss.Style{
		lipgloss.NewStyle().Foreground(p.Close).SetString("●"),
		lipgloss.NewStyle().Foreground(p.Minimize).SetString("●"),
		lipgloss.NewStyle().Foreground(p.Zoom).SetString("●"),
	}
	t.TitleText = lipgloss.NewStyle().Foreground(p.Muted).Bold(true)
	t.Tab = lipgloss.NewStyle().Foreground(p.Muted).Background(p.TitleBar).Padding(0, 2)
	t.TabActive = lipgloss.NewStyle().Foreground(p.Text).Background(p.Surface).Bold(true).Padding(0, 2)
	t.StatusKey = lipgloss.NewStyle().Foreground(p.Muted).Bold(true)
	t.StatusVal = lipgloss.NewStyle().Foreground(p.Text)
	t.HelpKey = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)
	t.HelpTxt = lipgloss.NewStyle().Foreground(p.Muted)
	return t
}

func (t *Theme) Toggle() *Theme {
	if t.Mode == Dark {
		return New(Light)
	}
	return New(Dark)
}
