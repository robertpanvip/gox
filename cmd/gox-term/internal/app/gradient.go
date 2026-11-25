package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// 本文件提供"水平渐变色条"渲染，用来模拟 macOS 标题栏的毛玻璃光影。
// 直接手写 24-bit ANSI（ESC[48;2;r;g;bm），一整行只用一次前缀、末尾一次重置，
// 避免逐字符 lipgloss.Render 带来的重置序列导致背景断续/露底。

type rgb struct{ r, g, b int }

func parseHex(s string) rgb {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 {
		return rgb{}
	}
	var r, g, b int
	if _, err := fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b); err != nil {
		return rgb{}
	}
	return rgb{r, g, b}
}

func lerpRGB(a, b rgb, t float64) rgb {
	return rgb{
		int(float64(a.r) + (float64(b.r)-float64(a.r))*t),
		int(float64(a.g) + (float64(b.g)-float64(a.g))*t),
		int(float64(a.b) + (float64(b.b)-float64(a.b))*t),
	}
}

// barCell 是渐变条上某一列要显示的内容；text 为空表示空格。
type barCell struct {
	text string
	fg   lipgloss.Color // 仅用于取色值；实际写 ANSI 时用 r,g,b
}

// gradientBar 渲染一条 width 列的水平渐变色条，from→to。
// cells 指定若干列上要叠加的文字（含前景色），其余列为渐变空格。
func gradientBar(width int, from, to string, cells map[int]barCell) string {
	if width <= 0 {
		return ""
	}
	a, b := parseHex(from), parseHex(to)
	var sb strings.Builder
	for x := 0; x < width; x++ {
		t := 0.0
		if width > 1 {
			t = float64(x) / float64(width-1)
		}
		c := lerpRGB(a, b, t)

		text := " "
		fg := rgb{232, 232, 234} // 默认前景：浅灰
		if cell, ok := cells[x]; ok {
			if cell.text != "" {
				text = cell.text
			}
			if cell.fg != "" {
				fg = parseHex(string(cell.fg))
			}
		}

		// 每列：设置背景 + 前景 + 字符。背景每列重设，保证过渡连续。
		fmt.Fprintf(&sb, "\x1b[48;2;%d;%d;%dm\x1b[38;2;%d;%d;%dm%s",
			c.r, c.g, c.b, fg.r, fg.g, fg.b, text)
	}
	sb.WriteString("\x1b[0m")
	return sb.String()
}

// putText 把一段文字（多列）写入 cells，从 start 列开始，每列一个 cell。
func putText(cells map[int]barCell, start int, text string, fg lipgloss.Color) {
	for i, r := range text {
		cells[start+i] = barCell{text: string(r), fg: fg}
	}
}
