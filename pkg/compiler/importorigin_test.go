package compiler

import (
	"strings"
	"testing"
)

// 验证 import 来源分支：Go 来源维持 upperFirst，GX 来源严格查导出表。
// 这些测试直接驱动 CompileWithProject，避免依赖尚未实现的 pkg/module 层。

func compileWithImports(t *testing.T, file, src string, imports map[string]ImportInfo) string {
	t.Helper()
	res, err := CompileWithProject(file, src, defaultModule, imports)
	if err != nil {
		t.Fatalf("compile %s: %v", file, err)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("generated Go invalid for %s: %v\n---\n%s\n---", file, err, res.GoSource)
	}
	return res.GoSource
}

func TestScanExports(t *testing.T) {
	src := `package util
pub func add(a int, b int) int { return a + b }
func hidden() {}
pub type box struct { v int }
pub enum color { red, green }
`
	got := ScanExports(src)
	// pub func add → Add；pub type box → Box；私有 hidden 不在表
	for k, want := range map[string]string{
		"add":   "Add",
		"box":   "Box",
		"color": "Color",
	} {
		if got[k] != want {
			t.Errorf("ScanExports[%q] = %q, want %q (full: %v)", k, got[k], want, got)
		}
	}
	if _, ok := got["hidden"]; ok {
		t.Errorf("私有 hidden 不应进入导出表: %v", got)
	}
}

func TestImportOriginGoKeepsUpperFirst(t *testing.T) {
	src := `package main
import "example.com/foo"
func main() { foo.newRequest(); foo.uRL() }
`
	imports := map[string]ImportInfo{
		"example.com/foo": {Path: "example.com/foo", Origin: ImportGo},
	}
	got := compileWithImports(t, "main.gx", src, imports)
	containsAll(t, got, "foo.NewRequest()", "foo.URL()")
}

func TestImportOriginGXStrictLookup(t *testing.T) {
	// 被导入方 util 有 pub func add → Add，pub func doWork → DoWork。
	src := `package main
import "example.com/proj/util"
func main() { util.add(); util.doWork() }
`
	imports := map[string]ImportInfo{
		"example.com/proj/util": {
			Path:   "example.com/proj/util",
			Origin: ImportGX,
			Exports: map[string]string{
				"add":    "Add",
				"doWork": "DoWork",
			},
		},
	}
	got := compileWithImports(t, "main.gx", src, imports)
	containsAll(t, got, "util.Add()", "util.DoWork()")
}

func TestImportOriginGXAlreadyExportNamePreserved(t *testing.T) {
	// 书写方直接用导出名 Add：命中「已用导出名」分支，保持原样。
	src := `package main
import "example.com/proj/util"
func main() { util.Add() }
`
	imports := map[string]ImportInfo{
		"example.com/proj/util": {
			Path:    "example.com/proj/util",
			Origin:  ImportGX,
			Exports: map[string]string{"add": "Add"},
		},
	}
	got := compileWithImports(t, "main.gx", src, imports)
	if !strings.Contains(got, "util.Add()") {
		t.Errorf("已用导出名应原样保留:\n%s", got)
	}
}

func TestImportOriginGXWrongCaseKept(t *testing.T) {
	// 写错大小写 ADD：不在导出表，保持原样（交由 Go 编译报错），不做 upperFirst 猜测。
	src := `package main
import "example.com/proj/util"
func main() { util.ADD() }
`
	imports := map[string]ImportInfo{
		"example.com/proj/util": {
			Path:    "example.com/proj/util",
			Origin:  ImportGX,
			Exports: map[string]string{"add": "Add"},
		},
	}
	got := compileWithImports(t, "main.gx", src, imports)
	if !strings.Contains(got, "util.ADD()") {
		t.Errorf("错误大小写应原样保留、不做猜测:\n%s", got)
	}
	if strings.Contains(got, "util.Add()") {
		t.Errorf("错误大小写不应被改写成 Add:\n%s", got)
	}
}

func TestImportOriginNilImportsUnchanged(t *testing.T) {
	// imports 为 nil 时，行为与 Compile 完全一致（upperFirst）。
	src := `package main
import "example.com/foo"
func main() { foo.newRequest() }
`
	got := compileWithImports(t, "main.gx", src, nil)
	containsAll(t, got, "foo.NewRequest()")
}

func TestCollectScopeWithImportsOrigin(t *testing.T) {
	src := `package main
import "example.com/proj/util"
func main() { util.add() }
`
	toks := New(src).AllTokens()
	imports := map[string]ImportInfo{
		"example.com/proj/util": {Path: "example.com/proj/util", Origin: ImportGX, Exports: map[string]string{"add": "Add"}},
	}
	s := collectScopeWithImports(toks, imports)
	info, ok := s.ImportOf("util")
	if !ok {
		t.Fatal("util import 未被识别")
	}
	if info.Origin != ImportGX {
		t.Errorf("util 来源 = %v, want ImportGX", info.Origin)
	}
	if info.Exports["add"] != "Add" {
		t.Errorf("util 导出表 add = %q, want Add", info.Exports["add"])
	}
}

func TestScanExportsFields(t *testing.T) {
	src := `package util
pub type box struct {
    pub value int
    pub name string
    hidden bool
}
type plain struct { pub count int }
`
	got := ScanExports(src)
	// box 的 pub 字段：限定名（原名 box 与导出名 Box 两种 key）
	checks := map[string]string{
		"box.value":   "Value",
		"Box.value":   "Value",
		"box.name":    "Name",
		"Box.name":    "Name",
		"plain.count": "Count",
		"Plain.count": "Count",
	}
	for k, want := range checks {
		if got[k] != want {
			t.Errorf("ScanExports[%q] = %q, want %q (full: %v)", k, got[k], want, got)
		}
	}
	// hidden 未 pub，不应有字段导出
	if _, ok := got["box.hidden"]; ok {
		t.Errorf("私有字段 box.hidden 不应进入导出表: %v", got)
	}
}

func TestImportOriginGXCompositeFieldsStrictLookup(t *testing.T) {
	// 被导入方 box 有 pub value → Value；书写方用小写 box{value:9}。
	// 期望：类型名 box→Box（顶层查表），字段 value→Value（限定名查表）。
	src := `package main
import "example.com/proj/util"
func main() { _ = util.box{value: 9} }
`
	imports := map[string]ImportInfo{
		"example.com/proj/util": {
			Path:   "example.com/proj/util",
			Origin: ImportGX,
			Exports: map[string]string{
				"box":       "Box",
				"box.value": "Value",
				"Box.value": "Value",
			},
		},
	}
	got := compileWithImports(t, "main.gx", src, imports)
	containsAll(t, got, "util.Box{Value: 9}")
}

func TestImportOriginGXCompositeFieldsWrongCaseKept(t *testing.T) {
	// 字段写错大小写 VALUE：不在导出表，保持原样，不做 upperFirst 猜测。
	src := `package main
import "example.com/proj/util"
func main() { _ = util.box{VALUE: 9} }
`
	imports := map[string]ImportInfo{
		"example.com/proj/util": {
			Path:    "example.com/proj/util",
			Origin:  ImportGX,
			Exports: map[string]string{"box": "Box", "box.value": "Value", "Box.value": "Value"},
		},
	}
	got := compileWithImports(t, "main.gx", src, imports)
	if !strings.Contains(got, "util.Box{VALUE: 9}") {
		t.Errorf("错误大小写字段应原样保留:\n%s", got)
	}
}

func TestImportOriginGXCompositeFieldsNoPubKept(t *testing.T) {
	// 被导入方字段未 pub（导出表无该字段）：保持书写方原样，交 Go 检查。
	src := `package main
import "example.com/proj/util"
func main() { _ = util.box{hidden: 9} }
`
	imports := map[string]ImportInfo{
		"example.com/proj/util": {
			Path:    "example.com/proj/util",
			Origin:  ImportGX,
			Exports: map[string]string{"box": "Box", "box.value": "Value"},
		},
	}
	got := compileWithImports(t, "main.gx", src, imports)
	if !strings.Contains(got, "util.Box{hidden: 9}") {
		t.Errorf("未 pub 字段应原样保留:\n%s", got)
	}
}

// compileDiagWithImports 复用来源表转译，返回 GX 语义层诊断与产物。
func compileDiagWithImports(t *testing.T, file, src string, imports map[string]ImportInfo) ([]GXDiagnostic, string) {
	t.Helper()
	res, err := CompileWithProject(file, src, defaultModule, imports)
	if err != nil {
		t.Fatalf("compile %s: %v", file, err)
	}
	return res.Analysis.Diagnostics(), res.GoSource
}

func diagCodes(diags []GXDiagnostic) []string {
	out := make([]string, 0, len(diags))
	for _, d := range diags {
		out = append(out, d.Code)
	}
	return out
}

func hasDiagCode(diags []GXDiagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestImportCaseDiagSelector(t *testing.T) {
	// 导出表 add→Add；书写 util.ADD（大小写错误）→ 应报 import-case，产物保持原样。
	src := `package main
import "example.com/proj/util"
func main() { util.ADD() }
`
	imports := map[string]ImportInfo{
		"example.com/proj/util": {
			Path: "example.com/proj/util", Origin: ImportGX,
			Exports: map[string]string{"add": "Add", "run": "Run"},
		},
	}
	diags, got := compileDiagWithImports(t, "main.gx", src, imports)
	if !hasDiagCode(diags, "import-case") {
		t.Errorf("util.ADD 应报 import-case，得到: %v", diagCodes(diags))
	}
	// 诊断消息应给出正确拼写与位置。
	var found bool
	for _, d := range diags {
		if d.Code == "import-case" && strings.Contains(d.Message, "Add") && d.Pos.Line == 3 {
			found = true
		}
	}
	if !found {
		t.Errorf("诊断应含正确拼写 Add 且定位到第 3 行: %v", diags)
	}
	// 产物保持大写原样（不改赌，交 Go 兜底）。
	if !strings.Contains(got, "util.ADD()") {
		t.Errorf("写错大小写产物应原样保留: %s", got)
	}
}

func TestImportCaseDiagField(t *testing.T) {
	src := `package main
import "example.com/proj/model"
func main() { _ = model.box{VALUE: 9} }
`
	imports := map[string]ImportInfo{
		"example.com/proj/model": {
			Path: "example.com/proj/model", Origin: ImportGX,
			Exports: map[string]string{
				"box": "Box", "box.value": "Value", "Box.value": "Value",
			},
		},
	}
	diags, got := compileDiagWithImports(t, "main.gx", src, imports)
	if !hasDiagCode(diags, "import-field-case") {
		t.Errorf("字段 VALUE 应报 import-field-case，得到: %v", diagCodes(diags))
	}
	if !strings.Contains(got, "model.Box{VALUE: 9}") {
		t.Errorf("类型名应还原为 Box、字段名原样保留: %s", got)
	}
}

func TestImportCaseDiagCorrectAndMisspell(t *testing.T) {
	// 正确写法（小写原名 / 已大写导出名）与真正拼写错误都不应报大小写诊断。
	imports := map[string]ImportInfo{
		"example.com/proj/util": {
			Path: "example.com/proj/util", Origin: ImportGX,
			Exports: map[string]string{"add": "Add"},
		},
	}
	cases := []struct {
		name, src string
	}{
		{"lower-original", `package main
import "example.com/proj/util"
func main() { util.add() }
`},
		{"export-name", `package main
import "example.com/proj/util"
func main() { util.Add() }
`},
		{"true-misspell", `package main
import "example.com/proj/util"
func main() { util.subtract() }
`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			diags, _ := compileDiagWithImports(t, "main.gx", tc.src, imports)
			if hasDiagCode(diags, "import-case") || hasDiagCode(diags, "import-field-case") {
				t.Errorf("不应报大小写诊断，得到: %v", diags)
			}
		})
	}
}

func TestImportCaseDiagNotForGoOrigin(t *testing.T) {
	// Go 来源维持 upperFirst，即便写大写也不产生 GX 大小写诊断。
	src := `package main
import "example.com/foo"
func main() { foo.NewRequest(); foo.URL() }
`
	imports := map[string]ImportInfo{
		"example.com/foo": {Path: "example.com/foo", Origin: ImportGo},
	}
	diags, _ := compileDiagWithImports(t, "main.gx", src, imports)
	if len(diags) != 0 {
		t.Errorf("Go 来源不应产生大小写诊断，得到: %v", diags)
	}
}

func TestImportCaseDiagAmbiguousNoReport(t *testing.T) {
	// 两个导出名大小写不敏感都匹配书写（add→Add 与 ADD→ADD 之类）→ 不猜测。
	src := `package main
import "example.com/proj/util"
func main() { util.aDd() }
`
	imports := map[string]ImportInfo{
		"example.com/proj/util": {
			Path: "example.com/proj/util", Origin: ImportGX,
			Exports: map[string]string{"add": "Add", "aDd": "ADd"},
		},
	}
	diags, _ := compileDiagWithImports(t, "main.gx", src, imports)
	if hasDiagCode(diags, "import-case") {
		t.Errorf("多候选不应猜测，得到: %v", diags)
	}
}
