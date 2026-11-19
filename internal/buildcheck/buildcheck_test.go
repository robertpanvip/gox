package buildcheck

import (
	"context"
	"testing"
)

func TestCheckCleanCode(t *testing.T) {
	c := New(8)
	src := "package main\n\nfunc main() { println(\"ok\") }\n"
	diags, err := c.Check(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("干净代码不应有诊断: %v", diags)
	}
}

func TestCheckBadCode(t *testing.T) {
	c := New(8)
	src := "package main\n\nfunc main() { undefinedFunc() }\n"
	diags, err := c.Check(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) == 0 {
		t.Fatal("错误代码应产生诊断")
	}
	found := false
	for _, d := range diags {
		if d.Line > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("诊断应带行号: %v", diags)
	}
}

func TestLRUCache(t *testing.T) {
	c := New(2)
	src := "package main\n\nfunc main() {}\n"
	if _, err := c.Check(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	n, cap := c.Stats()
	if n != 1 || cap != 2 {
		t.Fatalf("stats = %d/%d, want 1/2", n, cap)
	}
	// 相同源码应命中缓存（再查一次仍只有 1 条）
	if _, err := c.Check(context.Background(), src); err != nil {
		t.Fatal(err)
	}
	n, _ = c.Stats()
	if n != 1 {
		t.Fatalf("缓存命中不应新增条目: %d", n)
	}
}

func TestCancel(t *testing.T) {
	c := New(8)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消
	_, err := c.Check(ctx, "package main\nfunc main(){}\n")
	if err != context.Canceled {
		t.Fatalf("取消应返回 context.Canceled, got %v", err)
	}
}

func TestLRUEviction(t *testing.T) {
	c := New(2)
	srcs := []string{
		"package main\nfunc a() {}\n",
		"package main\nfunc b() {}\n",
		"package main\nfunc c() {}\n",
	}
	for _, src := range srcs {
		if _, err := c.Check(context.Background(), src); err != nil {
			t.Fatal(err)
		}
	}
	n, cap := c.Stats()
	if n != 2 || cap != 2 {
		t.Fatalf("淘汰后应保留 2 条, got %d/%d", n, cap)
	}
}

func TestClear(t *testing.T) {
	c := New(8)
	if _, err := c.Check(context.Background(), "package main\nfunc main(){}\n"); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.Stats(); n != 1 {
		t.Fatalf("清理前应有 1 条, got %d", n)
	}
	c.Clear()
	if n, _ := c.Stats(); n != 0 {
		t.Fatalf("Clear 后应为 0 条, got %d", n)
	}
}

func TestDiagnosticParsing(t *testing.T) {
	c := New(8)
	// 语法错误会产生带 file:line:col 的结构化诊断
	src := "package main\n\nfunc main() { var x int = \"str\" }\n"
	diags, err := c.Check(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, d := range diags {
		if d.File == "main.go" && d.Line == 3 && d.Col > 0 && d.Message != "" {
			found = true
		}
	}
	if !found {
		t.Fatalf("应解析出 main.go:3:col 的诊断, got %+v", diags)
	}
}

func TestAddReplaceClearsCache(t *testing.T) {
	c := New(8)
	if _, err := c.Check(context.Background(), "package main\nfunc main(){}\n"); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.Stats(); n != 1 {
		t.Fatalf("替换前应缓存 1 条, got %d", n)
	}
	c.AddReplace("example.com/foo", "/tmp/foo")
	if n, _ := c.Stats(); n != 0 {
		t.Fatalf("AddReplace 应清空缓存, got %d", n)
	}
}
