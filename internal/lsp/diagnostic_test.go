package lsp

import (
	"testing"

	"github.com/robertpanvip/gox/internal/buildcheck"
	"github.com/robertpanvip/gox/internal/check"
)

func TestToGxDiagnostics(t *testing.T) {
	// GX 语义诊断（1 基）转 LSP（0 基），带 code 与 code 前缀消息。
	in := []check.Diagnostic{{Line: 3, Col: 5, Code: "union-operand", Message: "未收窄"}}
	out := toGxDiagnostics(in)
	if len(out) != 1 {
		t.Fatalf("应有 1 条诊断, got %d", len(out))
	}
	d := out[0]
	if d.Range.Start.Line != 2 || d.Range.Start.Character != 4 {
		t.Errorf("1 基应转 0 基: got %+v", d.Range.Start)
	}
	if d.Code == nil || *d.Code != "union-operand" {
		t.Errorf("应带 code: %+v", d)
	}
	if d.Message != "union-operand: 未收窄" {
		t.Errorf("消息应带 code 前缀: %q", d.Message)
	}
}

func TestCompileAndCheckMergesSemanticDiagnostics(t *testing.T) {
	// 未收窄的联合变量参与运算：GX 语义层应报 union-operand。
	src := `package main
func f(v int | string) {
	println(v + 1)
}
func main() { f(1) }
`
	checker := buildcheck.New(8)
	checker.SetToolchain("go", "GOTOOLCHAIN=go1.27.4")
	diags, _, err := compileAndCheck(src, checker)
	if err != nil && len(diags) == 0 {
		t.Fatalf("compileAndCheck 应返回诊断: %v", err)
	}
	found := false
	for _, d := range diags {
		if d.Code != nil && *d.Code == "union-operand" {
			found = true
		}
	}
	if !found {
		t.Fatalf("应包含 union-operand 语义诊断, got %+v", diags)
	}
}

func TestCompileAndCheckCleanCode(t *testing.T) {
	src := `package main
func main() { println("ok") }
`
	checker := buildcheck.New(8)
	checker.SetToolchain("go", "GOTOOLCHAIN=go1.27.4")
	diags, _, _ := compileAndCheck(src, checker)
	if len(diags) != 0 {
		t.Fatalf("干净代码应无诊断: %+v", diags)
	}
}

func TestCompileErrorDiagnostic(t *testing.T) {
	// 转译失败也应产出一条 0:0 诊断供 LSP 展示。
	// 守卫位于条件首位的 && 复合条件现已支持收窄（不再报错）；
	// 改用守卫不在条件首位的形态触发 Compile 错误（comma-ok 无法前置，保持报错）。
	src := `package main
func f() {
    var v int | string = 1
    if v == 1 && v is int {
    }
}
`
	checker := buildcheck.New(8)
	diags, res, _ := compileAndCheck(src, checker)
	if len(diags) == 0 || res != nil {
		t.Fatalf("编译失败应产出诊断且无产物: diags=%+v res=%v", diags, res)
	}
}
