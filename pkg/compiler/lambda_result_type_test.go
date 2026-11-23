package compiler

import (
	"strings"
	"testing"
)

// 这些场景 body 本可静态推断出具体类型，此前 inferExprType 不认识
// 「本文件函数调用」「struct 字段访问」「已声明变量」而降级 any，导致
// lambda 结果类型失真。回归：确保推断出精确类型而不是 any。

func TestLambdaResultCallsLocalFunc(t *testing.T) {
	src := `package main
func double(x int) int { return x * 2 }
func main() {
	f := (x int) => double(x)
	_ = f
}
`
	got := compileOK(t, "lamlocalfunc.gx", src)
	containsAll(t, got, `func(x int) int {return double(x)}`)
	assertNotContains(t, got, `func(x int) any {return double(x)}`, "本文件函数调用不应降级 any")
}

func TestLambdaResultStructField(t *testing.T) {
	src := `package main
type Person struct {
	Age int
	Name string
}
func main() {
	age := (p Person) => p.Age
	name := (p Person) => p.Name
	_, _ = age, name
}
`
	got := compileOK(t, "lamfield.gx", src)
	containsAll(t, got,
		`func(p Person) int {return p.Age}`,
		`func(p Person) string {return p.Name}`,
	)
	assertNotContains(t, got, `func(p Person) any`, "struct 字段访问不应降级 any")
}

func TestLambdaResultDeclaredVar(t *testing.T) {
	src := `package main
func main() {
	sig base = 10
	f := () => base
	_ = f
}
`
	got := compileOK(t, "lamvar.gx", src)
	containsAll(t, got, `func() int {return base()}`)
	if strings.Contains(got, `func() any`) {
		t.Fatalf("已声明变量读取不应降级 any:\n%s", got)
	}
}
