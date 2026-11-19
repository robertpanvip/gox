package check

import (
	"strings"
	"testing"

	"github.com/robertpanvip/gox/pkg/compiler"
)

// ---------- 规则 2：union-call-arg ----------

func TestUnionCallArgInvalid(t *testing.T) {
	src := `package main
func f(v int | string) {}
func g() {
    f(true)
}
`
	diags := checkGx(t, src)
	d := hasDiag(t, diags, "union-call-arg", "bool")
	if d.Line != 4 {
		t.Errorf("诊断应在 GX 第 4 行（f(true)），got line=%d: %v", d.Line, d)
	}
}

func TestUnionCallArgCompatible(t *testing.T) {
	src := `package main
func f(v int | string) {}
func g() {
    f(1)
    f("hi")
}
`
	for _, d := range checkGx(t, src) {
		if d.Code == "union-call-arg" {
			t.Errorf("兼容实参不应报错: %v", d)
		}
	}
}

func TestUnionCallArgUnknownSkipped(t *testing.T) {
	src := `package main
func f(v int | string) {}
func make() bool { return true }
func g() {
    f(make())
}
`
	// make() 返回 bool，但经 exprType 推断出 bool，应报错——改为验证能推断函数返回类型
	// 若推断不到则放过；这里能推断到，故应报
	hasDiag(t, checkGx(t, src), "union-call-arg", "bool")
}

// ---------- 规则 3：union-return ----------

func TestUnionReturnInvalid(t *testing.T) {
	src := `package main
func f() int | string {
    return true
}
`
	hasDiag(t, checkGx(t, src), "union-return", "bool")
}

func TestUnionReturnCompatible(t *testing.T) {
	src := `package main
func f() int | string {
    return 1
}
func g() int | string {
    return "hi"
}
`
	for _, d := range checkGx(t, src) {
		if d.Code == "union-return" {
			t.Errorf("兼容返回不应报错: %v", d)
		}
	}
}

// ---------- 规则 4：union-operand ----------

func TestUnionOperandUnnarrowed(t *testing.T) {
	src := `package main
func f() {
    var x int | string = 1
    _ = x + 2
}
`
	hasDiag(t, checkGx(t, src), "union-operand", "x")
}

func TestUnionOperandNarrowedOK(t *testing.T) {
	src := `package main
func f() {
    var x int | string = 1
    if x is int {
        _ = x + 2
    }
}
`
	for _, d := range checkGx(t, src) {
		if d.Code == "union-operand" {
			t.Errorf("is 收窄后不应报未收窄诊断: %v", d)
		}
	}
}

func TestUnionOperandCompareSkipped(t *testing.T) {
	src := `package main
func f() {
    var x int | string = 1
    _ = x == nil
}
`
	for _, d := range checkGx(t, src) {
		if d.Code == "union-operand" {
			t.Errorf("比较运算不应对 any 误报: %v", d)
		}
	}
}

// ---------- 规则 5：non-exhaustive（type switch）----------

func TestTypeSwitchNonExhaustive(t *testing.T) {
	src := `package main
func f(v int | string) {
    switch v.(type) {
    case int:
        _ = 1
    }
}
`
	hasDiag(t, checkGx(t, src), "non-exhaustive", "string")
}

func TestTypeSwitchExhaustive(t *testing.T) {
	src := `package main
func f(v int | string) {
    switch v.(type) {
    case int:
        _ = 1
    case string:
        _ = 2
    }
}
`
	for _, d := range checkGx(t, src) {
		if d.Code == "non-exhaustive" {
			t.Errorf("覆盖全部成员不应报错: %v", d)
		}
	}
}

func TestTypeSwitchWithDefaultSkipped(t *testing.T) {
	src := `package main
func f(v int | string) {
    switch v.(type) {
    case int:
        _ = 1
    default:
        _ = 2
    }
}
`
	for _, d := range checkGx(t, src) {
		if d.Code == "non-exhaustive" {
			t.Errorf("有 default 不应报错: %v", d)
		}
	}
}

// ---------- 统一出口：CheckAll 合并 Analysis 层诊断 ----------

func TestCheckAllMergesAnalysisDiagnostics(t *testing.T) {
	src := `package main
func f() {
    var x int | string = 1
    if x is bool {
        _ = x
    }
}
`
	res, err := compiler.Compile("input.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	c := New(res.SourceMap, res.Analysis)
	diags, err := c.CheckAll(res.GoSource)
	if err != nil {
		t.Fatal(err)
	}
	// Analysis 层的 union-member 诊断必须出现在统一出口
	hasDiag(t, diags, "union-member", "bool")
}

func TestCheckAllSortedAndDedup(t *testing.T) {
	src := `package main
func f() {
    var x int | string = true
    _ = x
}
`
	res, err := compiler.Compile("input.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	c := New(res.SourceMap, res.Analysis)
	diags, err := c.CheckAll(res.GoSource)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(diags); i++ {
		prev, cur := diags[i-1], diags[i]
		if prev.Line > cur.Line || (prev.Line == cur.Line && prev.Col > cur.Col) {
			t.Errorf("诊断未按坐标排序: %v 在 %v 之前", prev, cur)
		}
	}
}

// 验证 exprType 对括号表达式的推断（通过 union-call-arg 间接验证）
func TestExprTypeParen(t *testing.T) {
	src := `package main
func f(v int | string) {}
func g() {
    f((true))
}
`
	hasDiag(t, checkGx(t, src), "union-call-arg", "bool")
}

// 验证诊断消息含联合成员
func TestDiagnosticMessageContainsUnion(t *testing.T) {
	src := `package main
func f(v int | string) {}
func g() { f(true) }
`
	diags := checkGx(t, src)
	for _, d := range diags {
		if d.Code == "union-call-arg" && strings.Contains(d.Message, "int|string") {
			return
		}
	}
	t.Fatalf("诊断消息应含联合成员 int|string: %v", diags)
}

// ---------- 严格化回归测试 ----------

func TestStrictNamedTypeAliasAssign(t *testing.T) {
	src := `package main
type MyU = int | string
func f() {
    var x MyU = true
    _ = x
}
`
	hasDiag(t, checkGx(t, src), "union-assign", "bool")
}

func TestStrictUntypedIntToInt64OK(t *testing.T) {
	// Go 无类型常量规则：1 可赋给 int64，不应误报
	src := `package main
func f() {
    var x int64 | string = 1
    _ = x
}
`
	for _, d := range checkGx(t, src) {
		if d.Code == "union-assign" {
			t.Errorf("无类型常量 1 应可赋给 int64: %v", d)
		}
	}
}

func TestStrictUntypedFloatToIntRejected(t *testing.T) {
	// 3.5 是无类型浮点，不能赋给 int|string（int 成员容不下浮点）
	src := `package main
func f() {
    var x int | string = 3.5
    _ = x
}
`
	hasDiag(t, checkGx(t, src), "union-assign", "float")
}

func TestStrictMultiAssignBothChecked(t *testing.T) {
	src := `package main
func f() {
    var x int | string = 1
    var y int | string = 2
    x, y = true, false
    _, _ = x, y
}
`
	diags := checkGx(t, src)
	if n := countCode(diags, "union-assign"); n != 2 {
		t.Errorf("多值赋值应报 2 条, got %d: %v", n, diags)
	}
}

func TestStrictNamedReturnAssign(t *testing.T) {
	src := `package main
func f() (v int | string) {
    v = true
    return
}
`
	hasDiag(t, checkGx(t, src), "union-assign", "bool")
}

func TestStrictConstPropagation(t *testing.T) {
	src := `package main
const c = true
func f() {
    var x int | string = c
    _ = x
}
`
	hasDiag(t, checkGx(t, src), "union-assign", "bool")
}

func TestStrictUnaryOperand(t *testing.T) {
	src := `package main
func f() {
    var v int | string = 1
    _ = -v
}
`
	hasDiag(t, checkGx(t, src), "union-operand", "-")
}

func TestStrictElseBranchExclusionNarrowed(t *testing.T) {
	// 排除法收窄：int|string 守卫 int 后，else 内 v 必为 string，可直接拼接。
	// desugar 注入 `v := v.(string)`，check 与 go build 一致，均不报错。
	src := `package main
func f() {
    var v int | string = 1
    if v is int {
        _ = v + 1
    } else {
        _ = v + "x"
    }
}
`
	for _, d := range checkGx(t, src) {
		if d.Code == "union-operand" {
			t.Errorf("else 内 v 已排除法收窄为 string，不应误报: %v", d)
		}
	}
}

func TestStrictElseBranchUnionRemainderReported(t *testing.T) {
	// 剩余仍是联合（排除一个后还有 string|bool），无法单断言注入 shadow，
	// else 内 v 仍是 any，做运算应报（不生成非法 v.(string|bool)）。
	src := `package main
func f() {
    var v int | string | bool = 1
    if v is int {
        _ = v + 1
    } else {
        _ = v + "x"
    }
}
`
	hasDiag(t, checkGx(t, src), "union-operand", "int|string|bool")
}

func TestStrictIfBlockShadowNoFalsePositive(t *testing.T) {
	src := `package main
func f() {
    var v int | string = 1
    if v is int {
        _ = v + 1
    }
}
`
	for _, d := range checkGx(t, src) {
		if d.Code == "union-operand" {
			t.Errorf("if 块内有 shadow, 不应误报: %v", d)
		}
	}
}

func countCode(ds []Diagnostic, code string) int {
	n := 0
	for _, d := range ds {
		if d.Code == code {
			n++
		}
	}
	return n
}

func TestStrictNegIntToUintRejected(t *testing.T) {
	src := `package main
func f() {
    var x uint | string = -1
    _ = x
}
`
	hasDiag(t, checkGx(t, src), "union-assign", "负整数")
}

func TestStrictCrossPackageSelectorNoFalsePositive(t *testing.T) {
	// 跨包同名函数不应被本包联合签名错误检查
	src := `package main
import "strconv"
func Itoa(v int | string) {}
func f() {
    _ = strconv.Itoa(true)
}
`
	for _, d := range checkGx(t, src) {
		if d.Code == "union-call-arg" {
			t.Errorf("跨包 selector 不应误报: %v", d)
		}
	}
}

func TestStrictAliasCaseInTypeSwitch(t *testing.T) {
	// case 是联合别名（擦除为 any），应视为覆盖全部
	src := `package main
type MyU = int | string
func f(v MyU) {
    switch v.(type) {
    case MyU:
    }
}
`
	for _, d := range checkGx(t, src) {
		if d.Code == "non-exhaustive" {
			t.Errorf("case MyU(any) 已覆盖全部，不应报穷尽性: %v", d)
		}
	}
}

// ---------- 作用域与收窄一致性回归（审查 P2 #8）----------

func TestScopeNestedShadowNoFalsePositive(t *testing.T) {
	// 内层 value := true 遮蔽外层联合变量，内层 println(value) 不应误报联合。
	src := `package main
func main() {
    var value int | string
    {
        value := true
        println(value)
    }
    println(value)
}
`
	diags := checkGx(t, src)
	if len(diags) != 0 {
		t.Fatalf("嵌套遮蔽不应产生误报: %v", diags)
	}
}

func TestScopeNarrowDoesNotLeakOutOfBlock(t *testing.T) {
	// 内层 if 收窄 v，块结束后外层 v 仍是联合，参与 + 应报错。
	src := `package main
func f(v int | string) {
    {
        if v is int {
            println(v)
        }
    }
    println(v + 1)
}
func main() { f(1) }
`
	diags := checkGx(t, src)
	hasDiag(t, diags, "union-operand", "v")
}

func TestScopeClosureCaptureKeepsUnion(t *testing.T) {
	// 闭包捕获外部联合变量，未收窄就参与运算应报错。
	src := `package main
func f(v int | string) {
    g := func() { println(v + 1) }
    g()
}
func main() { f(1) }
`
	diags := checkGx(t, src)
	hasDiag(t, diags, "union-operand", "v")
}

func TestScopeShortDeclShadowsParamNoFalsePositive(t *testing.T) {
	// 局部 v := "hello" 遮蔽联合参数，后续 println(v) 不应误报联合。
	src := `package main
func f(v int | string) {
    v := "hello"
    println(v)
}
func main() { f(1) }
`
	diags := checkGx(t, src)
	if len(diags) != 0 {
		t.Fatalf("短声明遮蔽参数不应误报: %v", diags)
	}
}

func TestScopeNarrowRestoredAfterIfElse(t *testing.T) {
	// if/else 各自收窄，分支结束后 v 恢复为联合，参与运算应报错。
	src := `package main
func f(v int | string) {
    if v is int {
        println(v)
    } else {
        println(v)
    }
    println(v + 1)
}
func main() { f(1) }
`
	diags := checkGx(t, src)
	hasDiag(t, diags, "union-operand", "v")
}

func TestScopeLoopReassignKeepsUnion(t *testing.T) {
	// 循环内重新赋兼容值，变量声明类型仍是联合，未收窄参与运算应报错。
	src := `package main
func f() {
    var v int | string = 1
    for i := 0; i < 3; i++ {
        v = i
    }
    println(v + 1)
}
func main() { f() }
`
	diags := checkGx(t, src)
	hasDiag(t, diags, "union-operand", "v")
}

func TestNestedUnionAliasCallNoFalsePositive(t *testing.T) {
	// 嵌套联合别名扁平化后，兼容的实参不应被误报。
	src := `package main
type Num = int | int64
type Val = Num | string
func f(v Val) { println(v) }
func main() { f(1) }
`
	diags := checkGx(t, src)
	if len(diags) != 0 {
		t.Fatalf("嵌套联合兼容实参不应误报: %v", diags)
	}
}

func TestNestedUnionStillNarrowedInElse(t *testing.T) {
	// Val = Num|string，is string 的 else 分支仍是 int|int64（联合），参与 + 应报错。
	src := `package main
type Num = int | int64
type Val = Num | string
func f(v Val) {
    if v is string {
        println(v)
    } else {
        println(v + 1)
    }
}
func main() { f(1) }
`
	diags := checkGx(t, src)
	hasDiag(t, diags, "union-operand", "v")
}
