package app

import (
	"strings"
	"testing"

	"github.com/robertpanvip/gox-ide/internal/complete"
	"github.com/robertpanvip/gox-ide/internal/editor"
)

func TestAcceptCompReplacesToken(t *testing.T) {
	m := New("", true, nil)
	m.focus = 1
	m.compOpen = true
	m.compToken = "cou"
	m.compSel = 0
	m.compCands = []complete.Candidate{{Text: "count", Label: "count", Kind: complete.KindBuffer}}
	m.buffers[0] = editor.New("t.gx", "sig cou = 0")
	// 光标放到行尾（"sig cou " 之后的位置在 "cou" 末尾）。
	m.buffers[0].SetCursor(0, 7) // "sig cou" → 列 7（rune 计：s i g _ c o u = 7）
	m.acceptComp()
	got := m.buffers[0].Text()
	if !strings.Contains(got, "count") {
		t.Errorf("采纳后应含 count，实际 %q", got)
	}
	if strings.Contains(got, "cou =") {
		t.Errorf("旧 token 未被替换: %q", got)
	}
}

func TestMoveCompSelWrap(t *testing.T) {
	m := New("", true, nil)
	m.compOpen = true
	m.compCands = []complete.Candidate{{}, {}, {}}
	m.compSel = 0
	m.moveCompSel(-1)
	if m.compSel != 2 {
		t.Errorf("环绕应为 2，实际 %d", m.compSel)
	}
	m.moveCompSel(1)
	if m.compSel != 0 {
		t.Errorf("环绕应为 0，实际 %d", m.compSel)
	}
}

func TestCloseComp(t *testing.T) {
	m := New("", true, nil)
	m.compOpen = true
	m.compCands = []complete.Candidate{{}}
	m.compToken = "x"
	m.closeComp()
	if m.compOpen || len(m.compCands) != 0 || m.compToken != "" {
		t.Error("closeComp 未清空状态")
	}
}
