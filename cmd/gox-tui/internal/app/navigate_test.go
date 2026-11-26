package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/robertpanvip/gox-tui/internal/editor"
)

func TestGotoDefinitionLocal(t *testing.T) {
	m := New("", true, nil)
	m.focus = 1
	m.buffers[0] = editor.New("t.gx", "package main\n\nfunc f() {\n    local := 1\n    x := local\n    _ = x\n}\n")
	// 光标在使用处 "local" 末尾：第 4 行(0基)，列 = "    x := local" 的 rune 数 14
	m.buffers[0].SetCursor(4, 14)
	m.gotoDefinition()
	if m.ov != ovNone {
		t.Fatalf("本地跳转不应弹 peek, ov=%v", m.ov)
	}
	ln, _ := m.buffers[0].Cursor()
	if ln != 3 {
		t.Errorf("应跳到 local 声明行(0基 3), got %d", ln)
	}
	if !strings.Contains(m.statusMsg, "跳转") {
		t.Errorf("状态栏应提示跳转, got %q", m.statusMsg)
	}
}

func TestGotoDefinitionStdPeek(t *testing.T) {
	m := New("", true, nil)
	m.focus = 1
	m.buffers[0] = editor.New("t.gx", "package main\n\nimport \"strings\"\n\nfunc f() {\n    b := strings.NewReplacer(\"a\",\"x\")\n    _ = b\n}\n")
	// 光标在 NewReplacer 末尾：第 5 行，列 = "    b := strings.NewReplacer(...)" 中 NewReplacer 末尾
	line := "    b := strings.NewReplacer(\"a\",\"x\")"
	col := strings.Index(line, "NewReplacer") + len("NewReplacer")
	m.buffers[0].SetCursor(5, col)
	m.gotoDefinition()
	if m.ov != ovPeek {
		t.Fatalf("std 符号应弹 peek, ov=%v", m.ov)
	}
	if m.peekDef.Pkg != "strings" || m.peekDef.File == "" {
		t.Errorf("peek 应是 strings 符号, got %+v", m.peekDef)
	}
	// Esc 关闭
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.ov != ovNone {
		t.Error("Esc 应关闭 peek")
	}
}

func TestHoverSignatureOverlay(t *testing.T) {
	m := New("", true, nil)
	m.focus = 1
	m.buffers[0] = editor.New("t.gx", "package main\n\nimport \"strings\"\n\nfunc f() {\n    b := strings.NewReplacer(\"a\",\"x\")\n    r := b.Replace(\"x\",\"y\")\n    _ = r\n}\n")
	line := "    r := b.Replace(\"x\",\"y\")"
	col := strings.Index(line, "Replace") + len("Replace")
	m.buffers[0].SetCursor(6, col)
	m.openHover()
	if m.ov != ovHover {
		t.Fatalf("应弹 hover 浮层, ov=%v", m.ov)
	}
	if m.hoverInfo.Expr != "Replace" || !strings.Contains(m.hoverInfo.Sig, "func(") {
		t.Errorf("hover 信息不对: %+v", m.hoverInfo)
	}
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.ov != ovNone {
		t.Error("Esc 应关闭 hover")
	}
}

func TestReadOnlyBufferRejectsEdit(t *testing.T) {
	m := New("", true, nil)
	m.focus = 1
	m.buffers[0] = editor.New("std://strings/strings.go", "package strings\nfunc foo() {}\n")
	m.readOnly[0] = true
	m.buffers[0].SetCursor(0, 0)
	before := m.buffers[0].Text()
	// 输入字符应被拒绝。
	m.handleEditorKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'X'}})
	if got := m.buffers[0].Text(); got != before {
		t.Errorf("只读标签不应被编辑: %q", got)
	}
	// 移动允许。
	m.handleEditorKey(tea.KeyMsg{Type: tea.KeyDown})
}
