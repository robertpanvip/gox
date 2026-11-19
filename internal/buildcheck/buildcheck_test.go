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
