package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robertpanvip/gox-core/runtime/signal"
)

// withDiskProject 在真实磁盘项目模式下运行 fn，结束后恢复示例库模式，
// 避免全局 projectRoot 在用例间串味。
func withDiskProject(t *testing.T, root string, fn func()) {
	t.Helper()
	reader := func(p string) string {
		b, _ := os.ReadFile(p)
		return string(b)
	}
	SetProjectRoot(root, reader)
	defer func() {
		SetProjectRoot("", nil)
		LoadSample(0)
	}()
	fn()
}

func TestScanDiskNodes(t *testing.T) {
	dir := t.TempDir()
	// 构造：根目录 main.gx、README.md、src/ 下有 a.gx 与隐藏文件、node_modules 噪声。
	os.WriteFile(filepath.Join(dir, "main.gx"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Demo"), 0644)
	os.MkdirAll(filepath.Join(dir, "src"), 0755)
	os.WriteFile(filepath.Join(dir, "src", "a.gx"), []byte("sig x = 1"), 0644)
	os.WriteFile(filepath.Join(dir, "src", ".hidden"), []byte("x"), 0644)
	os.MkdirAll(filepath.Join(dir, "node_modules"), 0755)
	os.WriteFile(filepath.Join(dir, "node_modules", "junk.js"), []byte("x"), 0644)

	nodes := scanDiskNodes(dir)
	var names []string
	for _, n := range nodes {
		names = append(names, n.Label)
		if strings.HasPrefix(n.Label, ".") || n.Label == "node_modules" {
			t.Errorf("噪声节点未过滤: %s", n.Label)
		}
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{"src", "main.gx", "README.md", "a.gx"} {
		if !strings.Contains(joined, want) {
			t.Errorf("扫描结果缺少 %q: %v", want, names)
		}
	}
	// 目录优先：src 应排在 main.gx 之前。
	if strings.Index(joined, "src") > strings.Index(joined, "main.gx") {
		t.Errorf("目录未优先排序: %v", names)
	}
	// src 下的文件 depth 应比 src 深一层。
	for _, n := range nodes {
		if n.Label == "a.gx" {
			if n.Depth < 2 {
				t.Errorf("a.gx 深度=%d, want >=2", n.Depth)
			}
			if n.Parent != filepath.Join(dir, "src") {
				t.Errorf("a.gx parent=%q", n.Parent)
			}
		}
	}
}

func TestDiskProjectVisibility(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0755)
	os.WriteFile(filepath.Join(dir, "src", "a.gx"), []byte("x"), 0644)
	withDiskProject(t, dir, func() {
		// SetProjectRoot 默认全展开，a.gx 应可见。
		rows := projectRows()
		found := false
		for _, r := range rows {
			if r.Label == "a.gx" {
				found = true
			}
		}
		if !found {
			t.Fatalf("全展开时 a.gx 不可见: %+v", rows)
		}
		// 折叠 src 后，a.gx 应隐藏。
		toggleGroup(filepath.Join(dir, "src"))
		rows = projectRows()
		for _, r := range rows {
			if r.Label == "a.gx" {
				t.Fatal("折叠 src 后 a.gx 仍可见")
			}
		}
	})
}

func TestDiskProjectOpenFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "hello.gx")
	want := "package main\n// hello from disk"
	os.WriteFile(target, []byte(want), 0644)
	withDiskProject(t, dir, func() {
		onProjectRowClick(ProjectNode{Label: "hello.gx", Path: target, Sample: -1, Depth: 1, Parent: dir})
		if got := EditorText(); got != want {
			t.Fatalf("点击真实文件后编辑器内容=%q, want %q", got, want)
		}
		if got := ProjectFileSelected(); got != target {
			t.Fatalf("选中文件=%q, want %q", got, target)
		}
	})
}

func TestDiskProjectExpandCollapseAll(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0755)
	os.MkdirAll(filepath.Join(dir, "docs"), 0755)
	os.WriteFile(filepath.Join(dir, "src", "a.gx"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(dir, "docs", "r.md"), []byte("x"), 0644)
	withDiskProject(t, dir, func() {
		collapseAllGroups()
		// 全部折叠后只剩 2 个顶层目录。
		rows := projectRows()
		if len(rows) != 2 {
			t.Fatalf("全部折叠后行数=%d, want 2（顶层目录）", len(rows))
		}
		expandAllGroups()
		rows = projectRows()
		wantNames := map[string]bool{"src": false, "docs": false, "a.gx": false, "r.md": false}
		for _, r := range rows {
			if _, ok := wantNames[r.Label]; ok {
				wantNames[r.Label] = true
			}
		}
		for name, seen := range wantNames {
			if !seen {
				t.Errorf("全部展开后缺少节点 %q", name)
			}
		}
	})
}

func TestDiskProjectDoesNotTouchSamples(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "x.gx"), []byte("x"), 0644)
	withDiskProject(t, dir, func() {
		// 真实模式下 projectAllNodes 来自磁盘，不应含示例分组名。
		nodes := projectAllNodes()
		for _, n := range nodes {
			if n.Label == "基础" || n.Label == "响应式" {
				t.Fatalf("真实模式混入示例分组: %s", n.Label)
			}
		}
		if len(nodes) == 0 {
			t.Fatal("真实模式扫描结果为空")
		}
	})
	// 恢复后回到示例模式，应再次看到示例分组。
	nodes := projectAllNodes()
	if len(nodes) == 0 || nodes[0].Label != "基础" {
		t.Fatalf("恢复示例模式失败: %+v", nodes[:minInt(2, len(nodes))])
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 保证 signal 包在真实模式切换时被正确使用（编译期接线检查）。
var _ = signal.Batch
