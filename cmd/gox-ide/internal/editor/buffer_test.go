package editor

import (
	"strings"
	"testing"
)

func TestInsertAndCursor(t *testing.T) {
	b := New("t.gx", "abc\ndef")
	b.SetCursor(0, 3) // 行尾
	b.InsertRune('!')
	if got := b.Line(0); got != "abc!" {
		t.Fatalf("line0 = %q", got)
	}
	if _, col := b.Cursor(); col != 4 {
		t.Fatalf("col = %d, want 4", col)
	}
}

func TestBackspaceAcrossLines(t *testing.T) {
	b := New("t.gx", "abc\ndef")
	b.SetCursor(1, 0)
	b.Backspace()
	if got := b.Text(); got != "abcdef" {
		t.Fatalf("text = %q, want abcdef", got)
	}
	if ln, col := b.Cursor(); ln != 0 || col != 3 {
		t.Fatalf("cursor = (%d,%d), want (0,3)", ln, col)
	}
}

func TestNewlineAndDelete(t *testing.T) {
	b := New("t.gx", "abcdef")
	b.SetCursor(0, 3)
	b.Newline()
	if got := b.Text(); got != "abc\ndef" {
		t.Fatalf("after newline = %q", got)
	}
	b.DeleteForward() // 删掉 'd'
	if got := b.Text(); got != "abc\nef" {
		t.Fatalf("after delete = %q", got)
	}
}

func TestUnicodeCursor(t *testing.T) {
	b := New("t.gx", "你好世界")
	b.SetCursor(0, 2)
	b.InsertRune('!')
	if got := b.Text(); got != "你好!世界" {
		t.Fatalf("text = %q", got)
	}
	if _, col := b.Cursor(); col != 3 {
		t.Fatalf("col = %d, want 3", col)
	}
}

func TestBigFileLineLookup(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 20000; i++ {
		sb.WriteString("line ")
		sb.WriteString(itoa(i))
		sb.WriteString("\n")
	}
	b := New("big.gx", sb.String())
	if b.LineCount() < 20000 {
		t.Fatalf("line count = %d", b.LineCount())
	}
	b.SetCursor(15000, 2)
	ln, col := b.Cursor()
	if ln != 15000 || col != 2 {
		t.Fatalf("cursor = (%d,%d)", ln, col)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
