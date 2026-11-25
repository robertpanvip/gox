package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// TestLayoutSingleLine 防止回归：标题栏/标签栏/状态栏必须各占一行，
// 否则 View 里按固定行数计算的 bodyH 会失配，导致顶部内容被裁掉。
func TestLayoutSingleLine(t *testing.T) {
	m := New(true, "/bin/bash")
	m.width, m.height = 110, 34
	m.cols, m.rows = 108, 30

	w := m.width - 2
	checks := map[string]string{
		"title":  m.viewTitle(w),
		"tabbar": m.viewTabBar(w),
		"rule":   m.viewRule(w),
		"status": m.viewStatus(w),
	}
	for name, seg := range checks {
		if n := len(strings.Split(seg, "\n")); n != 1 {
			t.Errorf("%s 占 %d 行，应为 1 行: %q", name, n, seg)
		}
		if got := lipgloss.Width(seg); got != w {
			t.Errorf("%s 宽度 %d，应为 %d", name, got, w)
		}
	}
}

func TestGradientBar(t *testing.T) {
	cells := map[int]barCell{2: {text: "●"}}
	bar := gradientBar(10, "#303035", "#242427", cells)
	if n := len(strings.Split(bar, "\n")); n != 1 {
		t.Errorf("渐变条占 %d 行，应为 1", n)
	}
	if got := lipgloss.Width(bar); got != 10 {
		t.Errorf("渐变条宽度 %d，应为 10", got)
	}
}
