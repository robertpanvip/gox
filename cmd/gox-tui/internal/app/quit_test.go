package app

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// 退出数据安全红测试：有未保存修改时 esc 不得直接 Quit。
// （既有行为：esc/ctrl+c 无条件 tea.Quit，quitConfirm 置位后从未被读——死标志。）

func quitModel(t *testing.T, dirty bool) *Model {
	root := t.TempDir()
	path := filepath.Join(root, "m.gx")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(root, true, []string{path})
	m.width, m.height = 100, 30
	if dirty {
		m.Current().InsertString("x") // 制造未保存修改
	}
	return m
}

func TestEscQuitBlockedByDirty(t *testing.T) {
	m := quitModel(t, true)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd != nil {
		t.Fatal("脏 buffer 下 esc 不应直接产生 Quit 命令")
	}
	// 再按 esc：确认退出。
	_, cmd2 := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd2 == nil {
		t.Fatal("二次 esc 应放行退出")
	}
	if _, err := os.ReadFile(m.Current().Path); err != nil {
		t.Fatal(err)
	}
}

func TestEscQuitClean(t *testing.T) {
	m := quitModel(t, false)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("无修改时 esc 应直接退出")
	}
}

func TestCtrlCAlwaysQuits(t *testing.T) {
	m := quitModel(t, true)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c 保留终端惯例：无条件退出")
	}
}
