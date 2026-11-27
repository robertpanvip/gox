// Package editor 是 gox-tui 的文本缓冲层，直接复用 gox 的 Rope 作为文本权威。
// 大文件插入/删除走 O(log N)，行列换算也是 O(log N)，与 playground 编辑器同源。
package editor

import (
	"fmt"
	"strings"
	"unicode/utf8"

	Rope "github.com/robertpanvip/gox-core/runtime/rope"
)

// Buffer 是一份可编辑文档。Rope 是唯一文本权威；磁盘文件与内存解耦。
type Buffer struct {
	rope      *Rope.Rope
	Path      string
	Name      string
	Dirty     bool
	Encoding  string // 磁盘文件编码（UTF-8 / GBK），保存时按此写回
	cursorLn  int    // 0-based 行
	cursorCol int    // 0-based 列（rune 计）
}

// New 从路径与内容建缓冲。
func New(path, content string) *Buffer {
	name := path
	if i := strings.LastIndexAny(path, "/\\"); i >= 0 {
		name = path[i+1:]
	}
	return &Buffer{rope: Rope.Of(content), Path: path, Name: name, Encoding: EncUTF8}
}

// Text 返回全文。
func (b *Buffer) Text() string { return b.rope.String() }

// LineCount 返回行数（至少 1）。
func (b *Buffer) LineCount() int { return b.rope.LineCount() }

// Line 返回第 idx 行内容（0-based，越界返回空串）。
func (b *Buffer) Line(idx int) string {
	if idx < 0 || idx >= b.LineCount() {
		return ""
	}
	return b.rope.Line(idx)
}

// Lines 返回所有行。
func (b *Buffer) Lines() []string { return b.rope.Lines() }

// Cursor 返回当前行列（0-based）。
func (b *Buffer) Cursor() (int, int) { return b.cursorLn, b.cursorCol }

// SetCursor 夹取到文档合法范围后设置光标。
func (b *Buffer) SetCursor(ln, col int) {
	ln = clamp(ln, 0, b.LineCount()-1)
	line := b.Line(ln)
	maxCol := utf8.RuneCountInString(line)
	b.cursorLn = ln
	b.cursorCol = clamp(col, 0, maxCol)
}

// moveCursor 按相对位移移动（列会被夹取到目标行行尾）。
func (b *Buffer) moveCursor(dLn, dCol int) {
	b.SetCursor(b.cursorLn+dLn, b.cursorCol+dCol)
}

// offset 返回当前光标在全文中的 rune 偏移。
func (b *Buffer) offset() int {
	return b.rope.LineColToOffset(b.cursorLn, b.cursorCol)
}

// InsertRune 在当前光标插入一个 rune。
func (b *Buffer) InsertRune(r rune) {
	b.rope = b.rope.Insert(b.offset(), string(r))
	b.cursorCol++
	b.Dirty = true
}

// InsertString 在当前光标插入字符串（用于粘贴/补全）。
func (b *Buffer) InsertString(s string) {
	b.rope = b.rope.Insert(b.offset(), s)
	// 重算光标：按插入串里的换行切分。
	lines := strings.Split(s, "\n")
	if len(lines) == 1 {
		b.cursorCol += utf8.RuneCountInString(s)
	} else {
		b.cursorLn += len(lines) - 1
		b.cursorCol = utf8.RuneCountInString(lines[len(lines)-1])
	}
	b.Dirty = true
}

// Backspace 删除光标前一个 rune；行首则并到上一行。
func (b *Buffer) Backspace() {
	if b.cursorCol > 0 {
		off := b.offset()
		b.rope = b.rope.Delete(off-1, 1)
		b.cursorCol--
		b.Dirty = true
		return
	}
	if b.cursorLn > 0 {
		prevLen := utf8.RuneCountInString(b.Line(b.cursorLn - 1))
		off := b.offset()
		b.rope = b.rope.Delete(off-1, 1) // 删掉换行符
		b.cursorLn--
		b.cursorCol = prevLen
		b.Dirty = true
	}
}

// DeleteForward 删除光标处的 rune；行尾则并掉下一行。
func (b *Buffer) DeleteForward() {
	line := b.Line(b.cursorLn)
	if b.cursorCol < utf8.RuneCountInString(line) {
		b.rope = b.rope.Delete(b.offset(), 1)
		b.Dirty = true
		return
	}
	if b.cursorLn < b.LineCount()-1 {
		b.rope = b.rope.Delete(b.offset(), 1)
		b.Dirty = true
	}
}

// Newline 在当前光标断行。
func (b *Buffer) Newline() {
	b.rope = b.rope.Insert(b.offset(), "\n")
	b.cursorLn++
	b.cursorCol = 0
	b.Dirty = true
}

// MoveUp/MoveDown 上下移动一行，列尽量保持（夹到目标行尾）。
func (b *Buffer) MoveUp()   { b.moveCursor(-1, 0) }
func (b *Buffer) MoveDown() { b.moveCursor(1, 0) }

// MoveLeft/MoveRight 左右移动一个 rune，跨行时携带列位置。
func (b *Buffer) MoveLeft() {
	if b.cursorCol > 0 {
		b.cursorCol--
	} else if b.cursorLn > 0 {
		b.cursorLn--
		b.cursorCol = utf8.RuneCountInString(b.Line(b.cursorLn))
	}
}

func (b *Buffer) MoveRight() {
	line := b.Line(b.cursorLn)
	if b.cursorCol < utf8.RuneCountInString(line) {
		b.cursorCol++
	} else if b.cursorLn < b.LineCount()-1 {
		b.cursorLn++
		b.cursorCol = 0
	}
}

// LineStart/LineEnd 跳到行首/行尾。
func (b *Buffer) LineStart() { b.cursorCol = 0 }
func (b *Buffer) LineEnd()   { b.cursorCol = utf8.RuneCountInString(b.Line(b.cursorLn)) }

// WordLeft/WordRight 按词移动（用于 Ctrl+←/→）。
func (b *Buffer) WordLeft() {
	line := b.Line(b.cursorLn)
	runes := []rune(line)
	i := b.cursorCol
	for i > 0 && !isWordByte(runes[i-1]) {
		i--
	}
	for i > 0 && isWordByte(runes[i-1]) {
		i--
	}
	b.cursorCol = i
}

func (b *Buffer) WordRight() {
	line := b.Line(b.cursorLn)
	runes := []rune(line)
	i := b.cursorCol
	for i < len(runes) && !isWordByte(runes[i]) {
		i++
	}
	for i < len(runes) && isWordByte(runes[i]) {
		i++
	}
	b.cursorCol = i
}

func isWordByte(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') || r >= 0x80
}

// MarkSaved 清除脏标记。
func (b *Buffer) MarkSaved() { b.Dirty = false }

// CursorInfo 返回人类可读的行列（1-based），用于状态栏。
func (b *Buffer) CursorInfo() string {
	return fmt.Sprintf("Ln %d, Col %d", b.cursorLn+1, b.cursorCol+1)
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
