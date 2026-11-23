package lsp

import (
	"strings"
	"testing"
)

// T4.3：sig 声明 hover 显示 Signal[T]。
func TestSignalDetailShowsType(t *testing.T) {
	src := `package main
func main() {
    sig count = 0
    sig name = "gx"
    sig rows = []int{1,2}
}
`
	sm := BuildSymbolMap(src)
	cases := map[string]string{
		"count": "Signal[int]",
		"name":  "Signal[string]",
		"rows":  "Signal[[]int]",
	}
	for name, want := range cases {
		syms := sm.Lookup(name)
		if len(syms) == 0 {
			t.Fatalf("未找到 sig 符号 %s", name)
		}
		if syms[0].Detail != want {
			t.Errorf("sig %s Detail = %q, want %q", name, syms[0].Detail, want)
		}
	}
}

// T4.3：显式类型注解优先。
func TestSignalExplicitType(t *testing.T) {
	sm := BuildSymbolMap("package main\nfunc main() {\n    sig n float64 = 1.5\n}\n")
	syms := sm.Lookup("n")
	if len(syms) == 0 || syms[0].Detail != "Signal[float64]" {
		t.Fatalf("显式类型应优先: %+v", syms)
	}
}

// T4.1：:= 局部变量 hover 显示推导类型。
func TestAssignDetailShowsInferredType(t *testing.T) {
	sm := BuildSymbolMap("package main\nfunc main() {\n    n := 42\n    s := \"x\"\n}\n")
	for name, want := range map[string]string{"n": "int", "s": "string"} {
		syms := sm.Lookup(name)
		if len(syms) == 0 || !strings.Contains(syms[0].Detail, want) {
			t.Errorf("变量 %s Detail 应含推导类型 %q: %+v", name, want, syms)
		}
	}
}

// T4.2：JSX 标签内的补全触发判定。
func TestInJSXTagOpen(t *testing.T) {
	cases := []struct {
		line string
		col  int
		want bool
	}{
		{"  <Card", len("  <Card"), true},
		{"  <Card>", len("  <Card>"), false},
		{"  x := 1", len("  x := 1"), false},
	}
	for _, c := range cases {
		got := inJSXTagOpen("package main\n"+c.line+"\n", 1, c.col)
		if got != c.want {
			t.Errorf("inJSXTagOpen(%q,%d) = %v, want %v", c.line, c.col, got, c.want)
		}
	}
}
