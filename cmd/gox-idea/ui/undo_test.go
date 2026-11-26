package ui

import "testing"

// resetEditor 把编辑器置于已知初始状态并清空撤销/重做栈，避免各测试之间
// 共享的包级状态互相污染。
func resetEditor(text string) {
	clearHistory()
	EditorSetText(text)
	clearHistory()
	EditorHome()
}

// TestUndoRedoInsert：一次插入成一步，撤销恢复原文与编辑前光标，重做再回到编辑后。
func TestUndoRedoInsert(t *testing.T) {
	resetEditor("abc")
	EditorSetCursor(0, 3)
	EditorInsert("X")
	if got := EditorText(); got != "abcX" {
		t.Fatalf("after insert: %q", got)
	}
	EditorUndo()
	if got := EditorText(); got != "abc" {
		t.Fatalf("after undo: %q, want abc", got)
	}
	if EditorLine() != 0 || EditorCol() != 3 {
		t.Fatalf("after undo cursor = (%d,%d), want (0,3)", EditorLine(), EditorCol())
	}
	EditorRedo()
	if got := EditorText(); got != "abcX" {
		t.Fatalf("after redo: %q, want abcX", got)
	}
	if EditorLine() != 0 || EditorCol() != 4 {
		t.Fatalf("after redo cursor = (%d,%d), want (0,4)", EditorLine(), EditorCol())
	}
}

// TestUndoBackspaceAndNewline：退格（含跨行合并）与断行都进撤销栈。
func TestUndoBackspaceAndNewline(t *testing.T) {
	resetEditor("a\nb")
	EditorSetCursor(1, 0)
	EditorBackspace() // 行首退格与上一行合并
	if got := EditorText(); got != "ab" {
		t.Fatalf("after backspace: %q", got)
	}
	EditorUndo()
	if got := EditorText(); got != "a\nb" {
		t.Fatalf("after undo backspace: %q", got)
	}
	EditorSetCursor(0, 1)
	EditorNewline()
	if got := EditorText(); got != "a\n\nb" {
		t.Fatalf("after newline: %q", got)
	}
	EditorUndo()
	if got := EditorText(); got != "a\nb" {
		t.Fatalf("after undo newline: %q", got)
	}
}

// TestNewEditClearsRedo：撤销后发生新编辑，重做分支必须失效（标准编辑器语义）。
func TestNewEditClearsRedo(t *testing.T) {
	resetEditor("ab")
	EditorSetCursor(0, 2)
	EditorInsert("C")
	EditorUndo()      // 回到 "ab"，redo 栈有 1 项
	EditorInsert("D") // 新编辑清空 redo
	EditorRedo()
	if got := EditorText(); got != "abD" {
		t.Fatalf("new edit should clear redo, got %q", got)
	}
	EditorUndo()
	if got := EditorText(); got != "ab" {
		t.Fatalf("undo after new edit: %q", got)
	}
}

// TestUndoEmptyStackNoop：空栈上撤销/重做不改变源码。
func TestUndoEmptyStackNoop(t *testing.T) {
	resetEditor("hello")
	EditorUndo()
	EditorRedo()
	if got := EditorText(); got != "hello" {
		t.Fatalf("undo/redo on empty stack changed source: %q", got)
	}
}

// TestPickColumnEmptyLine：空行无论点哪个 x，列都落在 0（配合前端整行命中）。
func TestPickColumnEmptyLine(t *testing.T) {
	SetCharWidth(7.83)
	SetWideWidth(13.02)
	resetEditor("a\n\nb")
	for _, x := range []float64{0, 7, 100, 999} {
		if got := PickColumn(1, x); got != 0 {
			t.Errorf("empty line PickColumn(%v) = %d, want 0", x, got)
		}
	}
}
