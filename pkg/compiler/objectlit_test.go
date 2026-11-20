package compiler

import (
	"strings"
	"testing"
)

func TestObjectLitAnonymous(t *testing.T) {
	src := `package main
func main() {
	p := { name: "x", age: 1, score: 3.5, ok: true }
	_ = p
}
`
	got := compileOK(t, "obj.gx", src)
	containsAll(t, got, `struct{ name string; age int; score float64; ok bool }{ name: "x", age: 1, score: 3.5, ok: true }`)
}

func TestObjectLitVarExpected(t *testing.T) {
	src := `package main
type Point struct {
	name string
	age int
}
func main() {
	var p Point = { name: "x", age: 1 }
	_ = p
}
`
	got := compileOK(t, "obj2.gx", src)
	containsAll(t, got, `var p Point = Point{ name: "x", age: 1 }`)
	if strings.Contains(got, `var p Point = struct`) {
		t.Errorf("有期望类型时不应生成匿名 struct 类型名:\n%s", got)
	}
}

func TestObjectLitArgExpected(t *testing.T) {
	src := `package main
type Point struct {
	name string
	age int
}
func f(p Point) {}
func main() {
	f({ name: "x", age: 1 })
}
`
	got := compileOK(t, "obj3.gx", src)
	containsAll(t, got, `f(Point{ name: "x", age: 1 })`)
}

func TestObjectLitAssignExpected(t *testing.T) {
	src := `package main
type Point struct {
	name string
	age int
}
func main() {
	var p Point
	p = { name: "x", age: 1 }
	_ = p
}
`
	got := compileOK(t, "obj4.gx", src)
	containsAll(t, got, `p = Point{ name: "x", age: 1 }`)
}

func TestObjectLitReturnExpected(t *testing.T) {
	src := `package main
type Point struct {
	name string
	age int
}
func make() Point {
	return { name: "x", age: 1 }
}
func main() { _ = make() }
`
	got := compileOK(t, "obj5.gx", src)
	containsAll(t, got, `return Point{ name: "x", age: 1 }`)
}

func TestObjectLitNested(t *testing.T) {
	src := `package main
func main() {
	p := { name: "x", inner: { a: 1, ok: true } }
	_ = p
}
`
	got := compileOK(t, "obj6.gx", src)
	containsAll(t, got, `struct{ name string; inner struct{ a int; ok bool } }{ name: "x", inner: struct{ a int; ok bool }{ a: 1, ok: true } }`)
}

func TestObjectLitUnknownFieldError(t *testing.T) {
	src := `package main
type Point struct {
	name string
	age int
}
func main() {
	var p Point = { name: "x", bad: 1 }
	_ = p
}
`
	_, err := Compile("obj7.gx", src)
	if err == nil {
		t.Fatal("未知字段应报错")
	}
	if !strings.Contains(err.Error(), "未知字段") || !strings.Contains(err.Error(), "bad") {
		t.Errorf("错误信息应含未知字段 bad, got: %v", err)
	}
	if !strings.Contains(err.Error(), "7:") {
		t.Errorf("错误应带 GX 行坐标, got: %v", err)
	}
}

func TestObjectLitMissingFieldError(t *testing.T) {
	src := `package main
type Point struct {
	name string
	age int
}
func main() {
	var p Point = { name: "x" }
	_ = p
}
`
	_, err := Compile("obj8.gx", src)
	if err == nil {
		t.Fatal("缺少字段应报错")
	}
	if !strings.Contains(err.Error(), "缺少字段") || !strings.Contains(err.Error(), "age") {
		t.Errorf("错误信息应含缺少字段 age, got: %v", err)
	}
}

func TestObjectLitTypeMismatchError(t *testing.T) {
	src := `package main
type Point struct {
	name string
	age int
}
func main() {
	var p Point = { name: "x", age: "old" }
	_ = p
}
`
	_, err := Compile("obj9.gx", src)
	if err == nil {
		t.Fatal("类型不匹配应报错")
	}
	if !strings.Contains(err.Error(), "类型不匹配") {
		t.Errorf("错误信息应含类型不匹配, got: %v", err)
	}
}

func TestObjectLitNoFalsePositiveOnBlocks(t *testing.T) {
	// if/for/struct 声明里的 '{' 不应被误判为对象字面量。
	src := `package main
type Point struct {
	name string
}
func main() {
	if true {
		println("x")
	}
	p := Point{ name: "x" }
	_ = p
}
`
	got := compileOK(t, "obj10.gx", src)
	containsAll(t, got, `if true {`, `Point{ name: "x" }`)
	if strings.Contains(got, "struct{ name string; }") && strings.Count(got, "struct{") > 1 {
		t.Errorf("不应误改写 struct 声明:\n%s", got)
	}
}

func TestObjectLitExpectAnyUsesAnonStruct(t *testing.T) {
	// 期望类型为 any / interface{} 时不携带字段结构，对象字面量按匿名结构体编译，
	// 而不是把 any 当类型名补全（any{...} 是非法 Go）。
	cases := []struct {
		name string
		src  string
	}{
		{"returnAny", `package main
func make() any {
	return { name: "x", age: 1 }
}
func main() { _ = make() }
`},
		{"returnIface", `package main
func make() interface{} {
	return { name: "x", age: 1 }
}
func main() { _ = make() }
`},
		{"argAny", `package main
func f(v any) {}
func main() { f({ name: "x", age: 1 }) }
`},
		{"varAny", `package main
func main() {
	var v any = { name: "x", age: 1 }
	_ = v
}
`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := compileOK(t, "objany_"+c.name+".gx", c.src)
			containsAll(t, got, `struct{ name string; age int }{ name: "x", age: 1 }`)
			if strings.Contains(got, "any{") || strings.Contains(got, "interface{}{") {
				t.Errorf("any/interface{} 不应被当作结构体类型名补全:\n%s", got)
			}
		})
	}
}

func TestIsAnyLikeType(t *testing.T) {
	for _, s := range []string{"any", " any ", "interface{}", "interface {}", "interface { }"} {
		if !isAnyLikeType(s) {
			t.Errorf("%q 应判为 any-like", s)
		}
	}
	for _, s := range []string{"Point", "struct{}", "int", ""} {
		if isAnyLikeType(s) {
			t.Errorf("%q 不应判为 any-like", s)
		}
	}
}

func TestObjectLitIdentFieldTypes(t *testing.T) {
	// 标识符字段值应从声明推断类型，而非降级为 any。
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"loopvar", `package main
func main() {
	s := 0
	for i := 0; i < 3; i++ { p := { a: i, b: i * 2 }; s += p.a + p.b }
	_ = s
}
`, `struct{ a int; b int }{ a: i, b: i * 2 }`},
		{"param", `package main
func mk(n int) any { return { val: n, name: "x" } }
`, `struct{ val int; name string }{ val: n, name: "x" }`},
		{"var", `package main
func main() {
	var age int = 20
	p := { a: age }
	_ = p
}
`, `struct{ a int }{ a: age }`},
		{"shortstr", `package main
func main() {
	s := "hi"
	p := { v: s }
	_ = p
}
`, `struct{ v string }{ v: s }`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := compileOK(t, "objident_"+c.name+".gx", c.src)
			containsAll(t, got, c.want)
			if strings.Contains(got, " a: any") || strings.Contains(got, " v: any") || strings.Contains(got, " val: any") {
				t.Errorf("标识符字段不应推断为 any:\n%s", got)
			}
		})
	}
}
