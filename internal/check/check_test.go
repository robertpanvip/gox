package check

import (
	"strings"
	"testing"

	"github.com/robertpanvip/gox/pkg/compiler"
)

// checkGx 走完整链路：gx 源码 → Compile(降级 + side table) → go/parser AST → GX 诊断。
func checkGx(t *testing.T, src string) []Diagnostic {
	t.Helper()
	res, err := compiler.Compile("input.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("生成的 Go 非法: %v\n---\n%s", err, res.GoSource)
	}
	c := New(res.SourceMap, res.Analysis)
	diags, err := c.Check(res.GoSource)
	if err != nil {
		t.Fatal(err)
	}
	return diags
}

func hasDiag(t *testing.T, diags []Diagnostic, code, contains string) Diagnostic {
	t.Helper()
	for _, d := range diags {
		if d.Code == code && strings.Contains(d.Message, contains) {
			return d
		}
	}
	t.Fatalf("缺少诊断 code=%q contains=%q, got %v", code, contains, diags)
	return Diagnostic{}
}

// ---------- 联合赋值：should 报错 ----------

func TestUnionAssignBoolToIntString(t *testing.T) {
	src := `package main
func f() {
    var x int | string = true
    _ = x
}
`
	diags := checkGx(t, src)
	hasDiag(t, diags, "union-assign", "bool")
	// 诊断应落在 GX 源码第 3 行（var x ... = true 那行）
	d := hasDiag(t, diags, "union-assign", "bool")
	if d.Line != 3 {
		t.Errorf("诊断行 = %d, want 3（赋值所在 GX 行）: %v", d.Line, d)
	}
}

func TestUnionAssignFloatToIntString(t *testing.T) {
	src := `package main
func f() {
    var x int | string = 3.5
    _ = x
}
`
	hasDiag(t, checkGx(t, src), "union-assign", "float64")
}

// ---------- 联合赋值：不应报错 ----------

func TestUnionAssignCompatible(t *testing.T) {
	src := `package main
func f() {
    var x int | string = 3
    var y int | string = "hi"
    _ = x; _ = y
}
`
	diags := checkGx(t, src)
	for _, d := range diags {
		if d.Code == "union-assign" {
			t.Errorf("兼容赋值不应报错: %v", diags)
		}
	}
}

func TestUnionAssignReassignment(t *testing.T) {
	src := `package main
func f() {
    var x int | string = 1
    x = 2
    x = "ok"
    x = true
    _ = x
}
`
	diags := checkGx(t, src)
	if len(diags) != 1 {
		t.Fatalf("应有且仅有 1 条赋值诊断, got %v", diags)
	}
	hasDiag(t, diags, "union-assign", "bool")
}

func TestUnionAssignUnknownRhsSkipped(t *testing.T) {
	// 右侧是函数调用等无法判定类型时放过，避免误报
	src := `package main
func make() int { return 1 }
func f() {
    var x int | string = make()
    _ = x
}
`
	for _, d := range checkGx(t, src) {
		if d.Code == "union-assign" {
			t.Errorf("未知类型的 RHS 不应误报: %v", d)
		}
	}
}

func TestNonUnionVarSkipped(t *testing.T) {
	src := `package main
func f() {
    var x int = true
    _ = x
}
`
	// x 非联合类型，交给 go/types 兜底；GX 检查器不应插手
	for _, d := range checkGx(t, src) {
		if d.Code == "union-assign" {
			t.Errorf("非联合变量不应由 GX 规则检查: %v", d)
		}
	}
}

// ---------- 端到端：诊断坐标与解析 ----------

func TestParseErrorDiagnostic(t *testing.T) {
	c := New(nil, nil)
	diags, err := c.Check("package main\nfunc {\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) == 0 || diags[0].Code != "parse" {
		t.Errorf("语法错误应产出 parse 诊断, got %v", diags)
	}
}

// ---------- 词法作用域：块内 var 不得泄漏到块外 ----------

// TestBlockScopedVarNoLeak：内层块用同名 var 遮蔽外层联合变量后，
// 块外的赋值必须重新看到外层联合变量，而非块内的局部类型。
// 回归：scopeOf 曾只算函数级范围，内层 var 的 scope 覆盖整个函数体，
// declaredTypeAt 取 DeclIdx 最大者 → 块外引用命中块内非联合类型 → 漏报。
func TestBlockScopedVarNoLeak(t *testing.T) {
	src := `package main
func f() {
    var x int | string = 1
    {
        var x bool = true
        _ = x
    }
    x = true
}
`
	diags := checkGx(t, src)
	// 块外 x = true 必须报：x 是外层 int|string，bool 不兼容。
	found := false
	for _, d := range diags {
		if d.Code == "union-assign" && d.Line == 8 {
			found = true
		}
	}
	if !found {
		t.Errorf("块外赋值应重新看到外层联合变量并报 union-assign（第 8 行）, got %v", diags)
	}
}

// TestBlockScopedVarInnerShadow：块内的同名变量遮蔽应正常生效——
// 块内对非联合 x 的赋值不应误报（x 在块内是 bool）。
func TestBlockScopedVarInnerShadow(t *testing.T) {
	src := `package main
func f() {
    var x int | string = 1
    {
        var x bool = true
        x = false
        _ = x
    }
    _ = x
}
`
	diags := checkGx(t, src)
	for _, d := range diags {
		if d.Code == "union-assign" {
			t.Errorf("块内 x 是 bool，不应报联合赋值: %v", d)
		}
	}
}
