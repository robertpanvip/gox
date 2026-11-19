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
