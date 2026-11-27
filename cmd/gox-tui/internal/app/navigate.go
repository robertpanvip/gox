package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/robertpanvip/gox-tui/internal/editor"
	"github.com/robertpanvip/gox-core/pkg/gxcomplete"
)

// ---- 导航动作 ----

// gotoDefinition 在 F12 时调用：本地符号跳到 .gx 对应行；std 符号弹 peek 速览。
func (m *Model) gotoDefinition() {
	if m.ov != ovNone || m.focus != 1 {
		return
	}
	b := m.Current()
	if m.readOnly[m.active] {
		m.statusMsg = "只读视图，无法导航"
		return
	}
	ln, col := b.Cursor()
	d, ok := m.nav.Definition(b.Text(), ln, col)
	if !ok {
		m.statusMsg = "未找到定义"
		return
	}
	if d.Local {
		target := maxInt(0, d.GxLine)
		if target >= b.LineCount() {
			target = b.LineCount() - 1
		}
		b.SetCursor(target, maxInt(0, d.GxCol))
		// 居中显示跳转目标。
		vh := m.viewportHeight()
		if vh > 0 {
			m.scrollLn = maxInt(0, target-vh/2)
		}
		m.ensureCursorVisible()
		m.statusMsg = fmt.Sprintf("跳转到定义 · 第 %d 行", target+1)
		m.closeComp()
		return
	}
	// std 符号：弹 peek。
	m.peekDef = d
	m.ov = ovPeek
	m.closeComp()
}

// openHover 在 Ctrl+K 时调用：弹签名速览浮层。
func (m *Model) openHover() {
	if m.ov != ovNone || m.focus != 1 {
		return
	}
	b := m.Current()
	ln, col := b.Cursor()
	h, ok := m.nav.Hover(b.Text(), ln, col)
	if !ok {
		m.statusMsg = "此处无类型信息"
		return
	}
	m.hoverInfo = h
	m.ov = ovHover
	m.closeComp()
}

// openStdFull 在 peek 里再按 F12/Enter：打开 std 源文件全文的只读标签。
func (m *Model) openStdFull() {
	d := m.peekDef
	if d.Local || d.Pkg == "" || d.File == "" {
		return
	}
	src, ok := m.nav.StdSource(d.Pkg, d.File)
	if !ok {
		m.statusMsg = "源码不可用（GOROOT 未安装？）"
		m.ov = ovNone
		return
	}
	display := d.Pkg + "/" + d.File
	nb := editor.New("std://"+display, src)
	// 定位到声明行。
	if d.Line > 0 {
		nb.SetCursor(minInt(d.Line-1, nb.LineCount()-1), 0)
	}
	m.buffers = append(m.buffers, nb)
	m.active = len(m.buffers) - 1
	m.readOnly[m.active] = true
	m.scrollLn = maxInt(0, d.Line-1-m.viewportHeight()/2)
	m.ensureCursorVisible()
	m.ov = ovNone
	m.statusMsg = "只读源码 · " + display
}

// ---- 键处理 ----

func (m *Model) handlePeekKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "ctrl+c":
		m.ov = ovNone
	case "f12", "enter":
		m.openStdFull()
	}
	return m, nil
}

func (m *Model) handleHoverKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "ctrl+c", "enter", "ctrl+k":
		m.ov = ovNone
	}
	return m, nil
}

// ---- 浮层渲染 ----

// peekOverlayView 渲染定义速览：签名片段 + 文档注释 + 来源路径。
func (m *Model) peekOverlayView(base string) string {
	t := m.theme
	panelW := minInt(78, m.width-8)
	if panelW < 24 {
		return base
	}
	inner := panelW - 2
	d := m.peekDef
	var lines []string
	lines = append(lines, t.TitleText.Render("  定义 · "+d.Pkg+"/"+d.File+":"+itoa(d.Line)))
	lines = append(lines, t.OverlayItem.Render(strings.Repeat("─", inner)))
	if d.Doc != "" {
		for _, dl := range strings.Split(d.Doc, "\n") {
			if dl == "" {
				continue
			}
			lines = append(lines, t.OverlayItem.Render("  "+truncate(dl, inner-2)))
		}
	}
	if d.Snippet != "" {
		for _, sl := range strings.Split(d.Snippet, "\n") {
			lines = append(lines, t.OverlayActive.Render("  "+truncate(sl, inner-2)))
		}
	}
	path := gxcomplete.StdAbsPath(d.Pkg, d.File)
	lines = append(lines, t.OverlayItem.Render(strings.Repeat("─", inner)))
	lines = append(lines, t.TitleText.Render("  F12/Enter 打开源码 · Esc 关闭"))
	if path != "" {
		lines = append(lines, t.OverlayItem.Render("  "+truncate(path, inner-2)))
	}
	panel := t.OverlayBox.Width(panelW).Height(len(lines) + 2).Render(strings.Join(lines, "\n"))
	return placeOverlay(base, panel)
}

// hoverOverlayView 渲染签名速览：expr — sig，附来源位置。
func (m *Model) hoverOverlayView(base string) string {
	t := m.theme
	panelW := minInt(78, m.width-8)
	if panelW < 24 {
		return base
	}
	inner := panelW - 2
	h := m.hoverInfo
	var lines []string
	lines = append(lines, t.TitleText.Render("  签名 · "+h.Expr))
	lines = append(lines, t.OverlayItem.Render(strings.Repeat("─", inner)))
	lines = append(lines, t.OverlayActive.Render("  "+truncate(h.Sig, inner-2)))
	if h.Where != "" {
		lines = append(lines, t.OverlayItem.Render("  "+truncate(h.Where, inner-2)))
	}
	lines = append(lines, t.OverlayItem.Render(strings.Repeat("─", inner)))
	lines = append(lines, t.TitleText.Render("  Esc 关闭"))
	panel := t.OverlayBox.Width(panelW).Height(len(lines) + 2).Render(strings.Join(lines, "\n"))

	// 锚定到光标行下方。
	ln, _ := m.Current().Cursor()
	relY := ln - m.scrollLn
	startY := relY + 3
	if startY < 1 {
		startY = 1
	}
	baseLines := strings.Split(base, "\n")
	if startY+len(lines)+2 > len(baseLines) {
		startY = maxInt(1, len(baseLines)-len(lines)-3)
	}
	treeW := (m.width - 2) * 25 / 100
	if treeW < 16 {
		treeW = 16
	}
	if treeW > 42 {
		treeW = 42
	}
	startX := 1 + treeW + 1
	return placeOverlayAt(base, panel, startX, startY)
}
