package compiler

import (
	"strings"
	"testing"
)

func TestDestructureShortDecl(t *testing.T) {
	// { name, age } := p → name := p.name; age := p.age
	src := `package main
type student struct {
    name string
    age  int
}
pub func f() {
    var p student = { name: "x", age: 1 }
    { name, age } := p
    _, _ = name, age
}
`
	got := compileOK(t, "destruct.gx", src)
	containsAll(t, got, "name := p.name", "age := p.age")
}

func TestDestructureVarDecl(t *testing.T) {
	// var { name, age } = p → var name = p.name; var age = p.age
	src := `package main
type student struct {
    name string
    age  int
}
pub func f() {
    var p student = { name: "x", age: 1 }
    var { name, age } = p
    _, _ = name, age
}
`
	got := compileOK(t, "destructvar.gx", src)
	containsAll(t, got, "var name = p.name", "var age = p.age")
}

func TestDestructureAnonymousStruct(t *testing.T) {
	// 右值是匿名 struct 字面量（p := {...}），字段来自展开后的 struct 声明
	src := `package main
pub func f() {
    p := { name: "x", age: 1 }
    { name, age } := p
    _, _ = name, age
}
`
	got := compileOK(t, "destructanon.gx", src)
	containsAll(t, got, "name := p.name", "age := p.age")
}

func TestDestructureUnknownFieldError(t *testing.T) {
	// 解构了类型不存在的字段 → 报错，位置为字段名 GX 坐标
	src := `package main
type student struct {
    name string
    age  int
}
pub func f() {
    var p student = { name: "x", age: 1 }
    { name, nope } := p
    _ = name
}
`
	_, err := Compile("destructerr.gx", src)
	if err == nil {
		t.Fatal("解构不存在的字段应报错")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("错误应指出字段名 nope, got: %v", err)
	}
	// 坐标 8:13 是 `nope` 所在行列
	if !strings.Contains(err.Error(), "8:13") {
		t.Errorf("错误应携带字段名的 GX 坐标 8:13, got: %v", err)
	}
}

func TestDestructureUnknownTypeLenient(t *testing.T) {
	// 右值类型无法本地推断（函数调用返回外部 struct）→ 宽松生成，不校验字段
	src := `package main
type student struct {
    name string
    age  int
}
func getP() student { return student{"x", 1} }
pub func f() {
    { name, age } := getP()
    _, _ = name, age
}
`
	got := compileOK(t, "destructlenient.gx", src)
	containsAll(t, got, "name := getP().name", "age := getP().age")
}

func TestDestructureNotConfusedWithObjectLit(t *testing.T) {
	// 对象字面量 { name: "x" } 不应被误判为解构（含冒号）
	src := `package main
pub func f() {
    p := { name: "x", age: 1 }
    var q struct{ name string; age int } = p
    _ = q
}
`
	got := compileOK(t, "notdestruct.gx", src)
	// 仍是 struct 字面量形态，没有生成 p.name 解构
	if strings.Contains(got, "name := p.name") {
		t.Errorf("对象字面量被误判为解构:\n%s", got)
	}
}
