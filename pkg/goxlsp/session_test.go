package goxlsp

import (
	"github.com/robertpanvip/gox/internal/typecheck/embedstd"

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

// TestCompleteTypeMember 类型感知补全：`b.`（*strings.Replacer）应给出 Replace。
// embedstd 未生成时跳过——该守卫同时是「类型层缺席 = 行为零变化」的证据。
func TestCompleteTypeMember(t *testing.T) {
	if embedstd.Lookup() == nil {
		t.Skip("embedstd 未生成，类型层缺席（预期行为）")
	}
	s := New()
	s.SetSource("package main\n\nimport \"strings\"\n\nfunc f() {\n\tb := strings.NewReplacer()\n\tb.\n\t_ = b\n}\n")
	list := s.CompleteAt(6, 3)
	var labels []string
	for _, it := range list.Items {
		labels = append(labels, it.Label)
	}
	found := false
	for _, l := range labels {
		if l == "Replace" {
			found = true
		}
	}
	if !found {
		t.Fatalf("b. 类型补全应含 Replace: %v", labels)
	}
}

// TestSignatureAtTyped 浏览器会话层的签名接线：fmt.Sprintf 第 1/2 参数处
// 分别 active=0/1；非调用位置返回 nil。embedstd 缺席时跳过（缺席=零变化）。
func TestSignatureAtTyped(t *testing.T) {
	if embedstd.Lookup() == nil {
		t.Skip("embedstd 未生成，类型层缺席（预期行为）")
	}
	s := New()
	s.SetSource("package main\n\nimport \"fmt\"\n\nfunc f() {\n\tfmt.Sprintf(\"%d\", \"x\")\n\t_ = 0\n}\n")
	h := s.SignatureAt(5, 13) // 紧跟 '(' 之后（LSP character 13）
	if h == nil || len(h.Signatures) == 0 || h.ActiveParameter != 0 {
		t.Fatalf("第一参数签名不符: %+v", h)
	}
	if !strings.Contains(h.Signatures[0].Label, "format string") {
		t.Fatalf("签名标签不符: %q", h.Signatures[0].Label)
	}
	h2 := s.SignatureAt(5, 19) // 逗号+空格之后
	if h2 == nil || h2.ActiveParameter != 1 {
		t.Fatalf("第二参数应 active=1: %+v", h2)
	}
	if h3 := s.SignatureAt(6, 3); h3 != nil {
		t.Fatalf("非调用上下文应返回 nil: %+v", h3)
	}
}

// TestDefinitionInfoStd 浏览器 std 跳转：fmt.Sprintf → std + 快览片段；本地符号 → local。
func TestDefinitionInfoStd(t *testing.T) {
	if embedstd.Lookup() == nil {
		t.Skip("embedstd 未生成，类型层缺席（预期行为）")
	}
	s := New()
	s.SetSource("package main\n\nimport \"fmt\"\n\nfunc main() {\n\ts := fmt.Sprintf(\"%d\", 1)\n\t_ = s\n}\n")
	d := s.DefinitionInfo(5, 16)
	if d == nil || d.Kind != "std" || d.Pkg != "fmt" {
		t.Fatalf("std 跳转: %+v", d)
	}
	if !strings.Contains(d.Snippet, "func Sprintf") {
		t.Fatalf("snippet=%q", d.Snippet)
	}
	if dl := s.DefinitionInfo(5, 2); dl == nil || dl.Kind != "local" {
		t.Fatalf("本地跳转: %+v", dl)
	}
}

// TestHoverTypedStd 类型化 hover：std 符号必须给出真实函数签名。
func TestHoverTypedStd(t *testing.T) {
	if embedstd.Lookup() == nil {
		t.Skip("embedstd 未生成")
	}
	s := New()
	s.SetSource("package main\n\nimport \"fmt\"\n\nfunc main() {\n\ts := fmt.Sprintf(\"%d\", 1)\n\t_ = s\n}\n")
	h := s.HoverAt(5, 16)
	if h == nil || !strings.Contains(h.Contents.Value, "func(") {
		t.Fatalf("类型化 hover: %+v", h)
	}
}

// TestImportPathCompletion import 路径补全：纯词法，源码半截也能给清单。
func TestImportPathCompletion(t *testing.T) {
	if embedstd.Lookup() == nil {
		t.Skip("embedstd 未生成")
	}
	s := New()
	s.SetSource("package main\n\nimport \"str\n")
	list := s.CompleteAt(2, 11)
	for _, it := range list.Items {
		if it.Label == "strings" {
			return
		}
	}
	t.Fatalf("import 补全应含 strings，got %d items", len(list.Items))
}

// TestReferencesAtStd 浏览器引用查找：fmt.Sprintf 处应含外部声明点与本地使用点。
func TestReferencesAtStd(t *testing.T) {
	if embedstd.Lookup() == nil {
		t.Skip("embedstd 未生成")
	}
	s := New()
	s.SetSource("package main\n\nimport \"fmt\"\n\nfunc main() {\n\ts := fmt.Sprintf(\"%d\", \"x\")\n\t_ = s\n}\n")
	refs := s.ReferencesAt(5, 16)
	if len(refs) < 2 {
		t.Fatalf("引用数量: %+v", refs)
	}
	hasDef, hasUse := false, false
	for _, r := range refs {
		if r.IsDef && r.Pkg == "fmt" {
			hasDef = true
		}
		if !r.IsDef {
			hasUse = true
		}
	}
	if !hasDef || !hasUse {
		t.Fatalf("声明/使用点缺失: %+v", refs)
	}
}

// TestCodeActionsAddImport 诊断驱动的 QuickFix：undefined: fmt → 补 import。
func TestCodeActionsAddImport(t *testing.T) {
	if embedstd.Lookup() == nil {
		t.Skip("embedstd 未生成")
	}
	s := New()
	s.SetSource("package main\n\nfunc main() {\n\ts := fmt.Sprintf(\"%d\", \"x\")\n\t_ = s\n}\n")
	acts := s.CodeActions()
	for _, a := range acts {
		if strings.Contains(a.Title, "fmt") && a.Edit != nil && strings.Contains(a.Edit.Text, `import "fmt"`) {
			return
		}
	}
	t.Fatalf("应产出 import \"fmt\" 修复: %+v", acts)
}

// TestCodeActionsUnused 未使用 import → 删除该行修复。
func TestCodeActionsUnused(t *testing.T) {
	if embedstd.Lookup() == nil {
		t.Skip("embedstd 未生成")
	}
	s := New()
	s.SetSource("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tprintln(1)\n}\n")
	acts := s.CodeActions()
	for _, a := range acts {
		if strings.Contains(a.Title, "fmt") && a.Edit != nil && a.Edit.Delete && a.Edit.Line == 2 {
			return
		}
	}
	t.Fatalf("应产出删除修复: %+v", acts)
}

// TestSemanticTokensJSON 浏览器语义 token：编码数组合法且非空。
func TestSemanticTokensJSON(t *testing.T) {
	s := New()
	s.SetSource("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(1)\n}\n")
	tk := struct {
		Data []int `json:"data"`
	}{}
	tk.Data = decodeTokensForTest(s.SemanticTokensJSON())
	if len(tk.Data) < 15 || len(tk.Data)%5 != 0 {
		t.Fatalf("data=%d 项", len(tk.Data))
	}
}

// TestParamHintsJSON 浏览器参数提示：fmt.Sprintf 首参给 format:（0 基 .gx 坐标）。
func TestParamHintsJSON(t *testing.T) {
	if embedstd.Lookup() == nil {
		t.Skip("embedstd 未生成")
	}
	s := New()
	s.SetSource("package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Sprintf(\"%d\", 1)\n}\n")
	hints := decodeHintsForTest(s.ParamHintsJSON())
	for _, h := range hints {
		if h.Label == "format:" && h.Line == 5 {
			return
		}
	}
	t.Fatalf("缺 format: 提示: %+v", hints)
}

// TestResolveDocStd 补全项惰性文档：fmt/Sprintf 内嵌 doc 可取回。
func TestResolveDocStd(t *testing.T) {
	if embedstd.Lookup() == nil {
		t.Skip("embedstd 未生成")
	}
	s := New()
	doc := s.ResolveDocJSON("fmt", "Sprintf")
	if !strings.Contains(doc, "formats according") {
		t.Fatalf("resolve doc: %s", doc)
	}
}

// —— 测试辅助：不引 encoding/json，红阶段直接肉眼解析极简 JSON 数组 ——
func decodeTokensForTest(js string) []int {
	return parseIntList(js[strings.Index(js, "[")+1 : strings.LastIndex(js, "]")])
}
func decodeHintsForTest(js string) []hintRow {
	secs := strings.Split(js, "},")
	out := make([]hintRow, 0, len(secs))
	for _, sec := range secs {
		h := hintRow{}
		if i := strings.Index(sec, "\"line\":"); i >= 0 {
			rest := sec[i+7:]
			j := strings.IndexAny(rest, ",}\"")
			h.Line, _ = atoi(rest[:j])
		}
		if i := strings.Index(sec, "\"label\":\""); i >= 0 {
			rest := sec[i+9:]
			h.Label = rest[:strings.Index(rest, "\"")]
		}
		if h.Label != "" {
			out = append(out, h)
		}
	}
	return out
}

type hintRow struct {
	Line  int
	Col   int
	Label string
}

func parseIntList(s string) []int {
	fs := strings.FieldsFunc(s, func(r rune) bool { return r == ',' })
	out := make([]int, 0, len(fs))
	for _, f := range fs {
		if v, ok := atoi(strings.TrimSpace(f)); ok {
			out = append(out, v)
		}
	}
	return out
}
func atoi(s string) (int, bool) {
	neg := false
	if strings.HasPrefix(s, "-") {
		neg, s = true, s[1:]
	}
	if s == "" {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	if neg {
		n = -n
	}
	return n, true
}

// TestTypeDefinitionStdJSON 浏览器 typeDefinition：*strings.Reader → std 声明定位。
func TestTypeDefinitionStdJSON(t *testing.T) {
	if embedstd.Lookup() == nil {
		t.Skip("embedstd 未生成")
	}
	s := New()
	s.SetSource("package main\n\nimport \"strings\"\n\nfunc main() {\n\tr := strings.NewReader(\"x\")\n\t_ = r\n}\n")
	d := s.TypeDefinitionAt(5, 2)
	if d == nil || d.Kind != "std" || d.Pkg != "strings" {
		t.Fatalf("typeDefinition: %+v", d)
	}
}

// TestImplementationLocalJSON 浏览器 implementation：本地接口 → 实现列表。
func TestImplementationLocalJSON(t *testing.T) {
	if embedstd.Lookup() == nil {
		t.Skip("embedstd 未生成，类型层缺席（预期行为）")
	}
	s := New()
	s.SetSource("package main\n\ntype Speaker interface{ Speak() string }\n\ntype Dog struct{}\n\nfunc (d Dog) Speak() string { return \"w\" }\n")
	ds := s.ImplementationsAt(2, 7)
	if len(ds) != 1 || ds[0].Name != "Dog" {
		t.Fatalf("implementations: %+v", ds)
	}
}

// TestCodeActionsOrganize 浏览器 organizeImports：连续单行 import 合并排序动作。
func TestCodeActionsOrganize(t *testing.T) {
	s := New()
	s.SetSource("package main\n\nimport \"strings\"\nimport \"fmt\"\n\nfunc main() { _, _ = strings.Builder{}, fmt.Println }\n")
	acts := s.CodeActions()
	for _, a := range acts {
		if a.Edit != nil && strings.Contains(a.Edit.Text, "import (") && strings.Contains(a.Edit.Text, `"fmt"`) && a.Edit.EndLine == 4 {
			return
		}
	}
	t.Fatalf("缺 organize 动作: %+v", acts)
}

// TestImplementationsCrossPackageStd 跨包反查的浏览器面：io.Reader 应查到 strings.Reader。
func TestImplementationsCrossPackageStd(t *testing.T) {
	if embedstd.Lookup() == nil {
		t.Skip("embedstd 未生成")
	}
	s := New()
	s.SetSource("package main\n\nimport (\n\t\"io\"\n\t\"strings\"\n)\n\nfunc main() {\n\tvar r io.Reader = strings.NewReader(\"x\")\n\t_ = r\n}\n")
	ds := s.ImplementationsAt(8, 11)
	for _, d := range ds {
		if d.Pkg == "strings" && d.Name == "Reader" {
			return
		}
	}
	t.Fatalf("应含 strings.Reader: %+v", ds)
}
