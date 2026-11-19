// Package integration 提供 GX 端到端测试：GX 源码 → Compile → SourceMap →
// go build → go run → 校验真实输出。与 golden 测试（仅比对生成文本）互补，
// 这里验证组合特性能真正编译并产出正确运行结果。
package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robertpanvip/gox/internal/buildcheck"
	"github.com/robertpanvip/gox/pkg/compiler"
)

const modulePath = "github.com/robertpanvip/gox"

// runGX 完整链路：转译 → 校验 sourcemap → 真实 go run → 返回 stdout。
func runGX(t *testing.T, gxSrc string) string {
	t.Helper()
	res, err := compiler.CompileWithModule("input.gx", gxSrc, modulePath)
	if err != nil {
		t.Fatalf("转译失败: %v", err)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("生成 Go 语法无效: %v\n---\n%s", err, res.GoSource)
	}
	if res.SourceMap == nil || res.SourceMap.Len() == 0 {
		t.Fatal("sourcemap 为空")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(res.GoSource), 0644); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	gomod := "module integration\n\ngo 1.27\n\nrequire " + modulePath + " v0.0.0\nreplace " + modulePath + " => " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=go1.27.3")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run 失败: %v\n--- output ---\n%s\n--- generated ---\n%s", err, out, res.GoSource)
	}
	return string(out)
}

func TestIntegrationPubEnumUnionTypeguardCollections(t *testing.T) {
	src := `package main

import "fmt"

pub enum status {
	active = 1,
	inactive = 2,
}

pub func describe(v int | string) string {
	if v is int {
		return fmt.Sprintf("int:%d", v)
	}
	return fmt.Sprintf("str:%s", v)
}

func main() {
	fmt.Println(describe(42))
	fmt.Println(describe("hi"))
	fmt.Println(statusActive.String())
	xs := []int{1, 2, 3}
	total := xs.Map(func(x int) int { return x * 2 }).Reduce(0, func(a int, b int) int { return a + b })
	fmt.Println(total)
}
`
	out := runGX(t, src)
	for _, want := range []string{"int:42", "str:hi", "active", "12"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestIntegrationJSXRender(t *testing.T) {
	src := `package main

import gxvdom "github.com/robertpanvip/gox/runtime/vdom"

func main() {
	node := <div class="box">hello</div>
	println(gxvdom.Render(node))
}
`
	out := runGX(t, src)
	if !strings.Contains(out, `<div class="box">hello</div>`) {
		t.Errorf("JSX 渲染输出不符, got:\n%s", out)
	}
}

func TestIntegrationBuildErrorMapsBackToGx(t *testing.T) {
	// 中文出现在使用错误符号之前；诊断须回投到 GX 第 4 行（字节列修复验证）。
	src := `package main
func main() {
	println("中文测试😀")
	undefinedFn()
}
`
	res, err := compiler.CompileWithModule("input.gx", src, modulePath)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	checker := buildcheck.New(8)
	checker.SetToolchain("go", "GOTOOLCHAIN=go1.27.3")
	checker.AddReplace(modulePath, root)
	diags, err := checker.Check(context.Background(), res.GoSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) == 0 {
		t.Fatal("错误代码应产生构建诊断")
	}
	mapped := false
	for _, d := range diags {
		gx, ok := res.SourceMap.GoToGx(d.Line, d.Col)
		if !ok {
			continue
		}
		if gx.Line == 4 && strings.Contains(d.Message, "undefined") {
			mapped = true
		}
	}
	if !mapped {
		t.Fatalf("未找到回投到 GX 第 4 行的 undefined 诊断: %v", diags)
	}
}

func TestIntegrationStdlibSliceChaining(t *testing.T) {
	// 标准库返回原生 []T，链式调用应自动 list.Of 包装（架构问题 #13）。
	src := `package main

import (
	"fmt"
	"strings"
)

func main() {
	xs := strings.Fields("hello brave world")
	upper := xs.map(func(s string) string { return strings.ToUpper(s) })
	fmt.Println(upper[0], upper[1], upper[2])
	total := xs.map(func(s string) int { return len(s) }).reduce(0, func(a int, b int) int { return a + b })
	fmt.Println(total)
}
`
	out := runGX(t, src)
	for _, want := range []string{"HELLO", "BRAVE", "WORLD", "15"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestIntegrationObjectLiteral(t *testing.T) {
	// 对象字面量：匿名结构体（字段类型反推）+ 期望类型补全（var/arg/assign/return）。
	src := `package main
import "fmt"
type point struct {
	name string
	age  int
}
func describe(p point) string { return fmt.Sprint(p.name, ":", p.age) }
func makePoint() point { return { name: "ret", age: 4 } }
func main() {
	anon := { name: "anon", age: 1 }
	fmt.Println(anon.name, anon.age)
	var p point = { name: "var", age: 2 }
	fmt.Println(describe(p))
	fmt.Println(describe({ name: "arg", age: 3 }))
	p = { name: "assign", age: 5 }
	fmt.Println(describe(p))
	fmt.Println(describe(makePoint()))
	nested := { name: "n", inner: { a: 10, ok: true } }
	fmt.Println(nested.inner.a, nested.inner.ok)
}
`
	out := runGX(t, src)
	for _, want := range []string{
		"anon 1", "var:2", "arg:3", "assign:5", "ret:4", "10 true",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("对象字面量输出应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestIntegrationObjectLiteralAnyExpect(t *testing.T) {
	// 期望类型为 any / interface{} 时对象字面量按匿名结构体编译，
	// 真实 go run 验证能编译运行且字段值正确。
	src := `package main
import "fmt"
func make() any { return { name: "x", age: 7 } }
func pass(v any) { fmt.Println(v) }
func main() {
	fmt.Println(make())
	var i interface{} = { name: "y", age: 8 }
	fmt.Println(i)
	pass({ name: "z", age: 9 })
}
`
	out := runGX(t, src)
	for _, want := range []string{"{x 7}", "{y 8}", "{z 9}"} {
		if !strings.Contains(out, want) {
			t.Errorf("any 期望类型下匿名结构体输出应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestIntegrationObjectDestructure(t *testing.T) {
	// 构造后立刻解构：S₁={name,age} 构造、S₂={name,age} 解构，均 ⊆ fields(student)。
	// 覆盖短声明 / var 形式 / 匿名 struct / 部分字段解构。
	src := `package main
import "fmt"
type student struct {
	name string
	age  int
	city string
}
func main() {
	{
		var p student = { name: "x", age: 1, city: "SZ" }
		{ name, age } := p
		fmt.Println(name, age)
	}
	{
		var q student = { name: "y", age: 2, city: "BJ" }
		var { name, city } = q
		fmt.Println(name, city)
	}
	{
		anon := { name: "z", age: 3 }
		{ name, age } := anon
		fmt.Println(name, age)
	}
}
`
	out := runGX(t, src)
	for _, want := range []string{"x 1", "y BJ", "z 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("对象解构输出应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestIntegrationLambda(t *testing.T) {
	// Lambda 最简箭头：表达式反推（int/bool）、显式参数、集合回调、函数实参。
	src := `package main
import "fmt"
func apply(f func(int) int, v int) int { return f(v) }
func main() {
	double := x => x * 2
	isBig := x => x > 10
	add := (a int, b int) => a + b
	fmt.Println(double(7), isBig(20), add(3, 4))
	fmt.Println(apply(x => x + 1, 41))
	xs := []int{1, 2, 3, 4}
	ys := xs.map(x => x * 2).filter(x => x > 4)
	fmt.Println(len(ys), ys[0], ys[1])
}
`
	out := runGX(t, src)
	for _, want := range []string{"14 true 7", "42", "2 6 8"} {
		if !strings.Contains(out, want) {
			t.Errorf("Lambda 输出应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestIntegrationDefaultParams(t *testing.T) {
	// TS 语义核心：未传用默认值，显式传 0/"" 用零值而非默认值。
	src := `package main
import "fmt"
func greet(name string = "world", times int = 2) string {
	out := ""
	for i := 0; i < times; i++ { out += name + " " }
	return out
}
func main() {
	fmt.Println(greet())
	fmt.Println(greet("gx"))
	fmt.Println(greet("gx", 1))
	fmt.Println(greet("", 1))
	fmt.Println(greet("z", 0))
}
`
	out := runGX(t, src)
	// 每行以 \n 结尾（含最后的空串行），去掉末尾一个 \n 再切分。
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("应输出 5 行, got %d: %q", len(lines), out)
	}
	checks := [][2]string{
		{lines[0], "world world "},
		{lines[1], "gx gx "},
		{lines[2], "gx "},
		{lines[3], " "},
		{lines[4], ""},
	}
	for _, c := range checks {
		if c[0] != c[1] {
			t.Errorf("期望 %q, got %q (完整输出: %q)", c[1], c[0], out)
		}
	}
}

func TestIntegrationTemplateString(t *testing.T) {
	// 模板字符串：静态段 + 多插值 + 嵌套表达式，真实运行校验拼接结果。
	src := `package main
import "strconv"
func main() {
	name := "gx"
	score := 95
	msg := ` + "`hello ${name}, score=${score}, double=${strconv.itoa(score * 2)}`" + `
	println(msg)
}
`
	out := runGX(t, src)
	if !strings.Contains(out, "hello gx, score=95, double=190") {
		t.Errorf("模板字符串输出不符, got:\n%s", out)
	}
}

func TestIntegrationLocalSliceNoDoubleWrap(t *testing.T) {
	// 本地 []T 字面量已提升为 List，链式调用不应再 list.Of（golden 语义）。
	src := `package main
import "fmt"
func main() {
	xs := []int{1, 2, 3, 4}
	total := xs.map(func(x int) int { return x * 2 }).reduce(0, func(a int, b int) int { return a + b })
	fmt.Println(total)
}
`
	out := runGX(t, src)
	if !strings.Contains(out, "20") {
		t.Errorf("本地切片链式结果应为 20, got:\n%s", out)
	}
}
