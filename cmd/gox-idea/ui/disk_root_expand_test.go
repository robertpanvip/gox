package ui

import (
	"os"
	"testing"
)

// TestDiskRootFilesVisibleAfterSetRoot 磁盘模式打开工程后，根级文件应立即可见
// （根路径必须进 projectExpanded，否则 containsStr(parent) 永假）。
func TestDiskRootFilesVisibleAfterSetRoot(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/a.go", []byte("package a\n"), 0o644)
	os.MkdirAll(dir+"/sub", 0o755)
	os.WriteFile(dir+"/sub/b.go", []byte("package b\n"), 0o644)
	SetProjectRoot(dir, func(p string) string { return "package x" })
	vis, total := 0, 0
	for _, n := range projectAllNodes() {
		if n.IsDir { continue }
		total++
		if projectNodeVisible(n) { vis++ }
	}
	if total < 2 {
		t.Fatalf("磁盘节点扫描异常 total=%d", total)
	}
	if vis != total {
		t.Errorf("根级文件不可见: vis=%d/%d（缺 root 展开键）", vis, total)
	}
}

