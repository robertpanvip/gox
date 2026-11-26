package typecheck

import (
	"strings"
	"testing"
)

// std 跳转声明与类型化 hover 的红测试：fake fmt（带 doc 注释）验证
// Pkg/File/Line/Snippet/Doc 五件套；local 回跳；内建函数与字面量必须降级。

func fmtLookup() Lookup {
	return func(path string) ([]File, bool) {
		if path == "fmt" {
			return []File{{Name: "print.go", Src: "package fmt\n\n// Sprintf formats according to a format specifier.\nfunc Sprintf(format string, a ...any) string { return format }\n"}}, true
		}
		return nil, false
	}
}

const defSrc = "package main\n\nimport \"fmt\"\n\nfunc main() {\n\ts := fmt.Sprintf(\"%d\", 1)\n\t_ = s\n}\n"

func curSprintf(src string) int { return strings.Index(src, "Sprintf") + 3 }

func TestDefinitionStd(t *testing.T) {
	d, ok := Definition(defSrc, curSprintf(defSrc), fmtLookup())
	if !ok {
		t.Fatal("std 符号跳转应命中")
	}
	if d.Pkg != "fmt" || d.File != "print.go" {
		t.Fatalf("Pkg/File=%q/%q want fmt/print.go", d.Pkg, d.File)
	}
	if d.Line != 4 {
		t.Fatalf("Line=%d want 4", d.Line)
	}
	if !strings.Contains(d.Snippet, "func Sprintf") {
		t.Fatalf("snippet 应含声明行: %q", d.Snippet)
	}
	if !strings.Contains(d.Doc, "formats according") {
		t.Fatalf("doc 应含注释: %q", d.Doc)
	}
}

func TestDefinitionLocal(t *testing.T) {
	d, ok := Definition(defSrc, strings.Index(defSrc, "main()")+2, fmtLookup())
	if !ok {
		t.Fatal("本地符号跳转应命中")
	}
	if d.Pkg != "" || d.Line != 5 {
		t.Fatalf("Pkg=%q Line=%d want \"\"/5", d.Pkg, d.Line)
	}
}

func TestDefinitionBuiltinAndMiss(t *testing.T) {
	src := "package main\n\nfunc main() {\n\tx := append([]int{1}, 2)\n\t_ = x\n}\n"
	if _, ok := Definition(src, strings.Index(src, "append")+3, fmtLookup()); ok {
		t.Fatal("内建函数没有源码，应优雅返回 false")
	}
	if _, ok := Definition(src, strings.Index(src, "2"), fmtLookup()); ok {
		t.Fatal("字面量上应返回 false")
	}
}

func TestTypeInfoTyped(t *testing.T) {
	sig, _, ok := TypeInfo(defSrc, curSprintf(defSrc), fmtLookup())
	if !ok || !strings.Contains(sig, "func(format string, a ...any) string") {
		t.Fatalf("ok=%v sig=%q", ok, sig)
	}
	sig2, _, ok := TypeInfo(defSrc, strings.Index(defSrc, "_ = s")+5, fmtLookup())
	if !ok || sig2 != "string" {
		t.Fatalf("局部变量类型 ok=%v sig=%q want string", ok, sig2)
	}
}

// ImportContext 判定：单行式与括号块式都要认，非 import 字符串不触发。
func TestImportContext(t *testing.T) {
	src := "package main\n\nimport \"str\n"
	if p, ok := ImportContext(src, 2, 11); !ok || p != "str" {
		t.Fatalf("单行式 %q %v", p, ok)
	}
	block := "package main\n\nimport (\n\t\"str\n)\n"
	if p, ok := ImportContext(block, 3, 5); !ok || p != "str" {
		t.Fatalf("括号块 %q %v", p, ok)
	}
	if _, ok := ImportContext("package main\n\nvar s = \"st", 2, 12); ok {
		t.Fatal("普通字符串不得触发")
	}
}

// TestStdSourceFull 只读视图数据源：按包+文件取回内嵌源码全文。
func TestStdSourceFull(t *testing.T) {
	src, ok := NewImporter(fmtLookup()).StdSource("fmt", "print.go")
	if !ok || !strings.Contains(src, "func Sprintf") {
		t.Fatalf("StdSource ok=%v len=%d", ok, len(src))
	}
	if _, ok := NewImporter(fmtLookup()).StdSource("fmt", "nope.go"); ok {
		t.Fatal("不存在文件必须 false")
	}
}
