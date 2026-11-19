package compiler

import "testing"

func TestIsValidGoTypeExpr(t *testing.T) {
	valid := []string{
		"int", "string", "*pkg.Type", "[]int", "map[string]int",
		"func(int) bool", "func(int, string) (bool, error)",
		"struct{ Name string }", "interface{ String() string }",
		"chan int", "<-chan int", "chan<- int", "Foo[int]", "(func(int) bool)",
	}
	for _, s := range valid {
		if !isValidGoTypeExpr(s) {
			t.Errorf("合法类型被拒绝: %q", s)
		}
	}
	invalid := []string{
		"func", "func(int", "map[", "int|string", // union 不是 Go 类型
		"struct{", "",
	}
	for _, s := range invalid {
		if isValidGoTypeExpr(s) {
			t.Errorf("非法类型被接受: %q", s)
		}
	}
}

func TestIsSliceGoTypeExpr(t *testing.T) {
	for _, s := range []string{"[]int", "[]func() bool", "[][]string", "[]struct{ A int }"} {
		if !isSliceGoTypeExpr(s) {
			t.Errorf("应为切片类型: %q", s)
		}
	}
	// 数组 [N]T 不是切片（Len 非 nil）
	for _, s := range []string{"[3]int", "int", "map[string]int"} {
		if isSliceGoTypeExpr(s) {
			t.Errorf("不应判为切片类型: %q", s)
		}
	}
}

func TestTypeEndBoundaryVerifiedByGoAST(t *testing.T) {
	// 交叉验证：对一批 Go 类型，typeEnd 识别出的边界片段必须能被 go/parser
	// 解析为合法类型表达式——即 token 扫描器与 Go 官方解析结果一致。
	cases := []string{
		"func(int) bool", "func()", "func(int, string) (bool, error)",
		"struct{ A int; B string }", "interface{ M(int) error }",
		"map[string][]int", "chan<- int", "<-chan chan int",
		"[]func() int", "[3]map[string]bool", "pkg.Type[int, string]",
	}
	for _, src := range cases {
		toks := New(src).AllTokens()
		end := typeEnd(toks, 0)
		fragment := exprText(toks, 0, end)
		if !isValidGoTypeExpr(fragment) {
			t.Errorf("typeEnd(%q) 识别为 %q，go/parser 判定非法", src, fragment)
		}
	}
}
