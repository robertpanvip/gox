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

func TestNamedTypeAliasUnion(t *testing.T) {
	src := `package main
type MyU = int | string
func f(v MyU) {
    var x MyU = 1
    _ = v; _ = x
}
`
	a := Analyze(New(src).AllTokens())
	// 别名应展开：参数 v 与变量 x 都应是 int|string 联合
	for _, s := range a.Symbols() {
		if (s.Name == "v" || s.Name == "x") && !s.Type.IsUnion() {
			t.Errorf("%s 类型应为展开后的 int|string 联合, got %s", s.Name, s.Type.String())
		}
	}
}

func TestNamedTypeAliasChain(t *testing.T) {
	src := `package main
type A = int | string
type B = A
func f(v B) {}
`
	a := Analyze(New(src).AllTokens())
	sig, ok := a.FuncSig("f")
	if !ok || len(sig.Params) != 1 || !sig.Params[0].IsUnion() || sig.Params[0].String() != "int|string" {
		t.Errorf("链式别名 B=A=int|string 应展开, got %v", sig.Params)
	}
}

func TestNamedTypeAliasCycle(t *testing.T) {
	src := `package main
type A = B
type B = A
func f() {}
`
	// 循环别名不应死循环或 panic
	a := Analyze(New(src).AllTokens())
	if a == nil {
		t.Fatal("Analyze 应正常返回")
	}
}

func TestStrandedIsInCompoundCondition(t *testing.T) {
	// 守卫位于条件首位的 && 复合条件现已支持收窄（遗留缺陷修复）：
	// 尾部条件与 then 块内的 recv 读替换为收窄临时变量。
	src := `package main
func f() {
    var v int | string = 1
    if v is int && v == 1 {
        println(v)
    }
}
`
	res, err := Compile("input.gx", src)
	if err != nil {
		t.Fatalf("守卫在条件首位的复合条件应支持收窄: %v", err)
	}
	got := res.GoSource
	if !strings.Contains(got, "; __gx_ok_") || !strings.Contains(got, "&&") {
		t.Fatalf("复合条件应生成 comma-ok + && 尾部:\n%s", got)
	}
	assertNotContains(t, got, " is ", "守卫已消费，产物不应残留 is")

	// 守卫不在条件首位（&& 之后）仍不支持：comma-ok 无法前置，保持明确报错。
	mid := `package main
func f() {
    var v int | string = 1
    if v == 1 && v is int {
        println(v)
    }
}
`
	if _, err := Compile("mid.gx", mid); err == nil {
		t.Fatal("守卫不在条件首位应报错，而不是静默产出非法 Go")
	} else if !strings.Contains(err.Error(), "is 守卫仅支持") {
		t.Errorf("错误信息应说明 is 限制, got %v", err)
	}
}

func TestStrandedIsDoesNotRejectVariableNamedIs(t *testing.T) {
	src := `package main
func f() {
    is := 1
    _ = is
}
`
	if _, err := Compile("input.gx", src); err != nil {
		t.Errorf("变量名为 is 不应被误判: %v", err)
	}
}

func TestNestedUnionFlattened(t *testing.T) {
	// type Num = int|int64; type Val = Num|string
	// Val 应扁平化为 int|int64|string，否则 Has/Accepts 把 "int|int64" 当单一成员误报。
	src := `package main
type Num = int | int64
type Val = Num | string
func f(v Val) { println(v) }
func main() { f(1) }
`
	res, err := Compile("nested.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	sig, ok := res.Analysis.FuncSig("f")
	if !ok {
		t.Fatal("缺少函数 f 签名")
	}
	got := sig.Params[0].String()
	if got != "int|int64|string" {
		t.Errorf("嵌套联合应扁平化为 int|int64|string, got %q", got)
	}
}

func TestElseIfGuardWithNonGuardHead(t *testing.T) {
	// 回归 H-7：`if c {} else if v is T {}` 链首非守卫时，else-if 守卫仍须处理，
	// 不得误报 "is 守卫仅支持简单形态"。
	src := `package main
func f(v any) {
	if v == nil { println(0) } else if v is int { println(v + 1) }
}
func main() { f(1) }
`
	res, err := CompileSource(src)
	if err != nil {
		t.Fatalf("合法 else-if 守卫被拒: %v", err)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("生成代码语法无效: %v\n%s", err, res.GoSource)
	}
}

// TestAnalyzeSharedTypeParams 验证共享类型参数 `a, b int|string` 的两个名字
// 都进入符号表并各自得到联合类型。collectParams 早先用 parseParamSeg 逐段切分，
// 导致段 `a` 无类型被跳过、只有 b 拿到联合类型（P5 合流修复）。
func TestAnalyzeSharedTypeParams(t *testing.T) {
	src := "package main\nfunc f(a, b int|string) {}\n"
	a := Analyze(New(src).AllTokens())
	got := map[string]string{}
	for _, s := range a.Symbols() {
		got[s.Name] = s.Type.String()
	}
	for _, name := range []string{"a", "b"} {
		if typ, ok := got[name]; !ok {
			t.Errorf("共享类型参数 %q 未进符号表; symbols=%v", name, got)
		} else if typ != "int|string" {
			t.Errorf("参数 %q 类型 = %q, want int|string", name, typ)
		}
	}
}

// TestBlockScopedVarScope：内层块用同名 var 遮蔽时，其 scope 必须被收紧到
// 最内层词法块，不得覆盖块外的引用（否则 check 层块外赋值会命中块内类型、
// 漏报联合兼容性）。回归 E3 的词法作用域 bug。
func TestBlockScopedVarScope(t *testing.T) {
	src := "package main\nfunc f() {\n var x int|string = 1\n {\n  var x bool = true\n  _ = x\n }\n x = true\n}\n"
	a := Analyze(New(src).AllTokens())
	var outer, inner *Symbol
	for i := range a.symbols {
		s := &a.symbols[i]
		if s.Name != "x" {
			continue
		}
		if s.Type.String() == "int|string" {
			outer = s
		} else if s.Type.String() == "bool" {
			inner = s
		}
	}
	if outer == nil || inner == nil {
		t.Fatalf("应收集到外层 x(int|string) 与内层 x(bool); symbols=%v", a.Symbols())
	}
	if !(inner.ScopeFrom >= outer.ScopeFrom && inner.ScopeTo <= outer.ScopeTo &&
		(inner.ScopeFrom > outer.ScopeFrom || inner.ScopeTo < outer.ScopeTo)) {
		t.Errorf("内层 x 的 scope 应收紧到块内: outer=[%d,%d) inner=[%d,%d)",
			outer.ScopeFrom, outer.ScopeTo, inner.ScopeFrom, inner.ScopeTo)
	}
}

// TestCompositeLitNotScopeBoundary：复合字面量 P{...} 的花括号不得被当作
// 词法块——其后的 var 声明 scope 仍应从函数体 '{' 起（覆盖整个函数体）。
func TestCompositeLitNotScopeBoundary(t *testing.T) {
	src := "package main\ntype P struct{ name string }\nfunc f() {\n p := P{name: \"z\"}\n var x int|string = 1\n x = true\n _ = p\n}\n"
	a := Analyze(New(src).AllTokens())
	var xSym *Symbol
	for i := range a.symbols {
		if a.symbols[i].Name == "x" && a.symbols[i].Type.String() == "int|string" {
			xSym = &a.symbols[i]
		}
	}
	if xSym == nil {
		t.Fatal("x 符号缺失")
	}
	// 复合字面量 P{name:"z"} 的花括号应被深度扫描跳过——x 的 scope 起点
	// 必须是函数体 '{'，而不是 P{...} 的 '{'。用 idxNearGx 定位块外 x=true
	// 引用点，确认它仍落在 x 的 scope 内（若误把 P{...} 当块，x 的 scope
	// 会从 P 的 '{' 起，而该引用在其后——仍在内，故改用 ScopeFrom 上界判断：
	// x 声明前、函数体 '{' 之后不应有未配对块边界被误认）。
	toks := New(src).AllTokens()
	// 定位 `x = true` 里 true 的下标（第 6 行）并确认在 scope 内。
	for i, tk := range toks {
		if tk.Text == "true" && tk.Pos.Line == 6 && i >= xSym.ScopeFrom && i < xSym.ScopeTo {
			return // 通过
		}
	}
	t.Errorf("复合字面量后的 x 声明 scope 应覆盖块外 x=true 引用: x=[%d,%d)", xSym.ScopeFrom, xSym.ScopeTo)
}
