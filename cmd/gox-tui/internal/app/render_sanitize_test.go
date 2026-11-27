package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 重现"打开字符混乱"：CRLF 行尾残留 \r 会把终端光标拉回行首（整行重叠），
// tab 在终端展开为 8 列而计宽按 1 列（缩进错位）。两者都会让打开文件后
// 编辑区字符显示混乱。
func TestRenderStripsCRAndExpandsTabs(t *testing.T) {
	old := setTrueColor(t)
	defer old()

	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	content := "package main\r\n\r\nfunc f() {\r\n\tif true {\r\n\t\treturn 1 // 中文注释\r\n\t}\r\n}\r\n"
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New(dir, true, []string{src})
	m.width, m.height = 80, 24
	m.focus = 1

	v := m.View()
	if strings.Contains(v, "\r") {
		t.Errorf("帧输出含 \\r：终端会把光标拉回行首，导致整行字符重叠混乱")
	}
	if strings.Contains(v, "\t") {
		t.Errorf("帧输出含原始 \\t：终端按制表位展开，与 lipgloss 计宽（1 列）不一致，缩进错位")
	}
}
