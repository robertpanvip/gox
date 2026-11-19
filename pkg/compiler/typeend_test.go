package compiler

import (
	"strings"
	"testing"
)

// typeEnd 必须能完整解析 Go 规范允许的所有类型表达式，
// 使集合提升生成合法的 list.List[T]，而非 list.List[func](int) bool 这类非法代码。
func TestTypeEndComplexTypes(t *testing.T) {
	cases := []struct {
		src      string // 类型表达式（可能带后续分隔符）
		wantType string // 期望被识别为类型的源码片段
	}{
		{"func(int) bool", "func(int) bool"},
		{"func(int, string) (bool, error)", "func(int, string) (bool, error)"},
		{"func()", "func()"},
		{"struct{ Name string }", "struct{ Name string }"},
		{"interface{ String() string }", "interface{ String() string }"},
		{"chan int", "chan int"},
		{"chan<- int", "chan<- int"},
		{"<-chan int", "<-chan int"},
		{"map[string][]int", "map[string][]int"},
		{"[]func(int) bool", "[]func(int) bool"},
		{"[3]func()", "[3]func()"},
		{"pkg.Type[int]", "pkg.Type[int]"},
		{"* pkg.Type", "* pkg.Type"},
		{"(func(int) bool)", "(func(int) bool)"},
	}
	for _, c := range cases {
		toks := New(c.src).AllTokens()
		end := typeEnd(toks, 0)
		// end 是 exclusive token 下标；用最后一个有效 token 的 Offset+长度截取原文。
		last := end - 1
		for last >= 0 && (toks[last].Kind == Space || toks[last].Kind == Newline) {
			last--
		}
		if last < 0 {
			t.Errorf("typeEnd(%q) 返回空区间", c.src)
			continue
		}
		got := c.src[:toks[last].Pos.Offset+len(toks[last].Text)]
		if got != c.wantType {
			t.Errorf("typeEnd(%q) 识别为 %q, want %q", c.src, got, c.wantType)
		}
	}
}

func TestTypeEndStopsAtBoundary(t *testing.T) {
	// func(int) 作为元素类型时，遇到 ',' 应停止，不能吞掉后续参数。
	toks := New("func(int), other").AllTokens()
	end := typeEnd(toks, 0)
	got := joinTypeText(toks, 0, end)
	if got != "func(int)" {
		t.Fatalf("func 类型应在边界停止, got %q", got)
	}
}

func TestCollectionsPromoteComplexSliceTypes(t *testing.T) {
	src := `package main
func a(x []func(int) bool) {}
func b(x []chan int) {}
func c(x []struct{ Name string }) {}
func main() {}
`
	res, err := CompileSource(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"list.List[func(int) bool]",
		"list.List[chan int]",
		"list.List[struct{ Name string }]",
	} {
		if !strings.Contains(res.GoSource, want) {
			t.Errorf("生成代码应包含 %q:\n%s", want, res.GoSource)
		}
	}
	// 绝不能出现非法的 list.List[func](...) 形态
	if strings.Contains(res.GoSource, "list.List[func](") {
		t.Errorf("生成了非法的函数类型提升:\n%s", res.GoSource)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("生成代码语法无效: %v", err)
	}
}
