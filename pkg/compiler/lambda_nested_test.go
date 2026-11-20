package compiler

import (
	"strings"
	"testing"
)

// TestNestedLambda 回归 H-1：嵌套 lambda 必须全部展开、无残留 '=>'、
// 且生成代码能被 go/parser 校验。此前 tokenizeTypeText 把 AllTokens() 的
// EOF 插进 token 流，导致外层 body 区间被截断、内层片段重复。
func TestNestedLambda(t *testing.T) {
	src := `package main
func main() {
	h := x => (y => y + x)
	println(h(1)(2))
}
`
	got := compileOK(t, "nestedlambda.gx", src)
	if strings.Contains(got, "=>") {
		t.Fatalf("残留 lambda 箭头 =>:\n%s", got)
	}
	// 内层 body 片段只应出现一次（旧 bug 会重复一遍 return y + x）
	if n := strings.Count(got, "return y + x"); n != 1 {
		t.Fatalf("内层 body 片段出现 %d 次（应为 1）:\n%s", n, got)
	}
	containsAll(t, got,
		`func(x any) any {return (func(y any) any {return y + x})}`,
	)
}
