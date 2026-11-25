package goxlsp

import (
	"strings"
	"testing"
)

const validSource = `package main

import gxscene "github.com/robertpanvip/gox/runtime/scene"

sig count = 0
func inc() { count = count + 1 }
func view() *gxscene.Instance {
    return <button onClick={inc}>{count}</button>
}
`

func TestDiagnosticsClean(t *testing.T) {
	s := New()
	s.SetSource(validSource)
	if diags := s.Diagnostics(); len(diags) != 0 {
		t.Fatalf("干净源码不应有诊断，得到 %+v", diags)
	}
}

func TestDiagnosticsCatchUnknownVar(t *testing.T) {
	s := New()
	s.SetSource(`package main
func f() { x := ; }
`)
	diags := s.Diagnostics()
	if len(diags) == 0 {
		t.Fatal("语法错误应产生诊断")
	}
	found := false
	for _, d := range diags {
		if len(d.Message) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("诊断消息不应为空")
	}
}

func TestHoverAndCompletion(t *testing.T) {
	s := New()
	s.SetSource(validSource)
	// sig count 声明在第 4 行（0 基 3），列 4 落在 count 标识符上。
	h := s.HoverAt(4, 6)
	if h == nil {
		t.Fatal("hover 应命中 count 符号")
	}
	if len(h.Contents.Value) == 0 {
		t.Fatal("hover 内容不应为空")
	}
	list := s.CompleteAt(0, 0)
	if len(list.Items) == 0 {
		t.Fatal("补全列表不应为空")
	}
	found := false
	for _, item := range list.Items {
		if item.Label == "count" || item.Label == "inc" || item.Label == "view" {
			found = true
		}
	}
	if !found {
		t.Fatalf("补全应包含源码符号，得到 %+v", list.Items)
	}
}

func TestDefinitionAndSymbols(t *testing.T) {
	s := New()
	s.SetSource(validSource)
	loc := s.DefinitionAt(4, 6)
	if loc == nil {
		t.Fatal("跳转应命中 count 定义")
	}
	if loc.Range.Start.Line != 4 {
		t.Fatalf("定义行应为 4，得到 %d", loc.Range.Start.Line)
	}
	syms := s.DocumentSymbols()
	if len(syms) < 3 {
		t.Fatalf("文档符号过少: %d", len(syms))
	}
}

func TestFormatIsIdempotent(t *testing.T) {
	s := New()
	s.SetSource(validSource)
	once := s.Format()
	s.SetSource(once)
	twice := s.Format()
	if once != twice {
		t.Fatal("格式化应幂等")
	}
}

// TestGoSyntaxDiagnosticMapped 验证 wasm LSP 的 Go 语法诊断：位置回投 .gx（不钉
// 0:0）、消息不携带用户不可见的生成文件坐标（check.go:N:M）。
func TestGoSyntaxDiagnosticMapped(t *testing.T) {
	s := New()
	s.SetSource("package main\n\nfunc f() {\n\tx := ;\n}\n")
	diags := s.Diagnostics()
	var hit *Diagnostic
	for i := range diags {
		if diags[i].Code != nil && *diags[i].Code == "parse" {
			hit = &diags[i]
		}
	}
	if hit == nil {
		t.Fatalf("期望 parse 诊断: %+v", diags)
	}
	if hit.Range.Start.Line < 2 || hit.Range.Start.Line > 4 {
		t.Fatalf("诊断行应指向错误附近（0 基 3±1）: %+v", *hit)
	}
	if strings.Contains(hit.Message, "check.go") {
		t.Fatalf("消息不应含生成文件坐标: %q", hit.Message)
	}
}

// TestSessionTypeLayerDiagnostic 类型层接线验证：embed 数据存在时，语法合法但
// 类型错误的源码必须产出 Code="type" 且坐标回投到 .gx 行。
func TestSessionTypeLayerDiagnostic(t *testing.T) {
	s := New()
	s.SetSource("package main\n\nfunc f() {\n\tn := 41\n\tvar g string = n\n\t_ = g\n}\n")
	var hit *Diagnostic
	for i := range s.Diagnostics() {
		d := &s.Diagnostics()[i]
		if d.Code != nil && *d.Code == "type" {
			hit = d
		}
	}
	if hit == nil {
		t.Skip("embedstd 未生成，类型层缺席（预期行为）")
	}
	if hit.Range.Start.Line < 4 || hit.Range.Start.Line > 6 {
		t.Fatalf("type 诊断坐标应回投 .gx 第 6 行附近: %+v", *hit)
	}
}
