package compiler

import (
	"strings"
	"testing"
)

// ---------- 符号表：联合类型的声明解析 ----------

func TestAnalyzeParamUnionType(t *testing.T) {
	src := "package main\nfunc f(v int | string | bool) {}\n"
	a := Analyze(New(src).AllTokens())
	var found *Symbol
	for i := range a.symbols {
		if a.symbols[i].Name == "v" {
			found = &a.symbols[i]
		}
	}
	if found == nil {
		t.Fatal("参数 v 未进符号表")
	}
	if !found.Type.IsUnion() || found.Type.String() != "int|string|bool" {
		t.Errorf("v 的类型 = %q, want int|string|bool", found.Type.String())
	}
}

func TestAnalyzeVarUnionType(t *testing.T) {
	src := "package main\nfunc f() { var x int | string; _ = x }\n"
	a := Analyze(New(src).AllTokens())
	for i := range a.symbols {
		if a.symbols[i].Name == "x" && a.symbols[i].Type.IsUnion() {
			if got := a.symbols[i].Type.String(); got != "int|string" {
				t.Errorf("x 的类型 = %q, want int|string", got)
			}
			return
		}
	}
	t.Fatal("局部变量 x 的联合类型未被收集")
}

// ---------- 守卫识别：Analysis 是唯一语义来源 ----------

func TestAnalyzeGuardAt(t *testing.T) {
	src := `package main
func f(v any) {
    if (v is int) { println(v) }
}
`
	toks := New(src).AllTokens()
	a := Analyze(toks)
	// 找到 if token，GuardAt 应命中
	for _, tk := range toks {
		if tk.is("if") {
			g, ok := a.GuardAt(tk.Pos)
			if !ok {
				t.Fatal("if 守卫未被 Analyze 识别")
			}
			if g.Recv != "v" || g.Target.String() != "int" {
				t.Errorf("守卫 = %s is %s, want v is int", g.Recv, g.Target.String())
			}
			return
		}
	}
	t.Fatal("源码中没有 if")
}

func TestAnalyzeGuardAtMissForPlainIf(t *testing.T) {
	src := "package main\nfunc f(v any) { if v == nil { println(v) } }\n"
	toks := New(src).AllTokens()
	a := Analyze(toks)
	for _, tk := range toks {
		if tk.is("if") {
			if _, ok := a.GuardAt(tk.Pos); ok {
				t.Error("普通 if 不应被识别为 is 守卫")
			}
			return
		}
	}
}

// ---------- 收窄数据流：then / else-if / else ----------

// typeAtInBranch 找到包含标记行的语句下标，查询 v 在该处的有效类型。
func typeAtInBranch(t *testing.T, src, marker string) string {
	t.Helper()
	toks := New(src).AllTokens()
	a := Analyze(toks)
	for i, tk := range toks {
		if tk.Kind == Ident && tk.Text == marker {
			return a.TypeAt("v", i).String()
		}
	}
	t.Fatalf("marker %q 未找到", marker)
	return ""
}

func TestNarrowThenBranch(t *testing.T) {
	src := `package main
func f(v int | string) {
    if (v is int) {
        in_then
    }
}
`
	if got := typeAtInBranch(t, src, "in_then"); got != "int" {
		t.Errorf("then 分支类型 = %q, want int", got)
	}
}

func TestNarrowElseIfChain(t *testing.T) {
	src := `package main
func f(v int | string | bool) {
    if (v is int) {
        in_a
    } else if (v is string) {
        in_b
    } else {
        in_c
    }
}
`
	a := typeAtInBranch(t, src, "in_a")
	b := typeAtInBranch(t, src, "in_b")
	c := typeAtInBranch(t, src, "in_c")
	if a != "int" {
		t.Errorf("第一个分支 = %q, want int", a)
	}
	if b != "string" {
		t.Errorf("else-if 分支 = %q, want string", b)
	}
	if c != "bool" {
		t.Errorf("else 分支 = %q, want bool", c)
	}
}

func TestNarrowElseBranchSingle(t *testing.T) {
	src := `package main
func f(v int | string) {
    if (v is int) {
        in_then
    } else {
        in_else
    }
}
`
	if got := typeAtInBranch(t, src, "in_else"); got != "string" {
		t.Errorf("else 分支 = %q, want string", got)
	}
}

func TestNarrowNested(t *testing.T) {
	src := `package main
func f(v int | string | bool) {
    if (v is int) {
        outer
    } else {
        if (v is string) {
            inner
        }
    }
}
`
	if got := typeAtInBranch(t, src, "outer"); got != "int" {
		t.Errorf("外层 then = %q, want int", got)
	}
	if got := typeAtInBranch(t, src, "inner"); got != "string" {
		t.Errorf("嵌套 else 内收窄 = %q, want string", got)
	}
}

// ---------- GX 原生诊断 ----------

func TestDiagnosticUnionMemberMismatch(t *testing.T) {
	src := `package main
func f(v int | string) {
    if (v is float64) { println(v) }
}
`
	diags := Analyze(New(src).AllTokens()).Diagnostics()
	found := false
	for _, d := range diags {
		if d.Code == "union-member" && strings.Contains(d.Message, "float64") {
			found = true
		}
	}
	if !found {
		t.Errorf("应报 float64 不属于联合的诊断, got %v", diags)
	}
}

func TestDiagnosticNoFalsePositiveForAny(t *testing.T) {
	src := `package main
func f(v any) {
    if (v is int) { println(v) }
}
`
	diags := Analyze(New(src).AllTokens()).Diagnostics()
	for _, d := range diags {
		if d.Code == "union-member" || d.Code == "redundant-guard" {
			t.Errorf("any 守卫不应误报, got %v", diags)
		}
	}
}

// ---------- desugar 消费 Analysis 的端到端验证 ----------

func TestAnalysisDrivesTypeGuard(t *testing.T) {
	src := `package main
func f(v any) {
    if (v is int) { println(v) }
}
`
	got := compileOK(t, "analysis_drive.gx", src)
	containsAll(t, got, "v.(int)", "v := __gx_v_")
	assertNo(t, got, " is ", "is 守卫应被消除")
}

func TestResultCarriesAnalysis(t *testing.T) {
	res, err := Compile("a.gx", "package main\nfunc f(v any) { if (v is int) { println(v) } }\n")
	if err != nil {
		t.Fatal(err)
	}
	if res.Analysis == nil {
		t.Fatal("Result 应携带 Analysis")
	}
	toks := New("package main\nfunc f(v any) { if (v is int) { println(v) } }\n").AllTokens()
	hit := false
	for _, tk := range toks {
		if tk.is("if") {
			if _, ok := res.Analysis.GuardAt(tk.Pos); ok {
				hit = true
			}
		}
	}
	if !hit {
		t.Error("Result.Analysis 应含 if 守卫")
	}
}

func TestExhaustiveIfChainMissingMember(t *testing.T) {
	// int|string|bool 只守卫 int、string 两支且无 else → 报缺少 bool
	src := `package main
func f(v int | string | bool) {
    if v is int {
        _ = 1
    } else if v is string {
        _ = 2
    }
}
`
	a := Analyze(New(src).AllTokens())
	found := false
	for _, d := range a.Diagnostics() {
		if d.Code == "non-exhaustive" && strings.Contains(d.Message, "bool") {
			found = true
		}
	}
	if !found {
		t.Errorf("应报缺少 bool，got %v", a.Diagnostics())
	}
}

func TestExhaustiveIfChainSingleBranchSkipped(t *testing.T) {
	// 单分支守卫是常见写法，不报穷尽性
	src := `package main
func f(v int | string) {
    if v is int {
        _ = 1
    }
}
`
	a := Analyze(New(src).AllTokens())
	for _, d := range a.Diagnostics() {
		if d.Code == "non-exhaustive" {
			t.Errorf("单分支不应报穷尽性: %v", d)
		}
	}
}

func TestExhaustiveIfChainWithElseSkipped(t *testing.T) {
	// 有 else 兜底视为穷尽
	src := `package main
func f(v int | string | bool) {
    if v is int {
        _ = 1
    } else if v is string {
        _ = 2
    } else {
        _ = 3
    }
}
`
	a := Analyze(New(src).AllTokens())
	for _, d := range a.Diagnostics() {
		if d.Code == "non-exhaustive" {
			t.Errorf("有 else 兜底不应报穷尽性: %v", d)
		}
	}
}
