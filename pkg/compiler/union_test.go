package compiler

import (
	"strings"
	"testing"
)

// unionOut 只跑 union 阶段，便于精确断言消歧与替换范围。
func unionOut(t *testing.T, src string) string {
	t.Helper()
	out, err := CompileStage(src, "union")
	if err != nil {
		t.Fatalf("union stage: %v", err)
	}
	return out
}

// ---------- 各类型位置的联合擦除 ----------

func TestUnionVarDecl(t *testing.T) {
	got := unionOut(t, "package main\nfunc f() {\n var x int | string\n _ = x\n}\n")
	containsAll(t, got, "var x any")
	if strings.Contains(got, "|") {
		t.Errorf("var 声明的联合未擦除:\n%s", got)
	}
}

func TestUnionFuncParamAndReturn(t *testing.T) {
	got := unionOut(t, "package main\nfunc f(v int | string) bool | int { return true }\n")
	containsAll(t, got, "func f(v any) any")
}

func TestUnionTypeAlias(t *testing.T) {
	got := unionOut(t, "package main\ntype Num = int | float64\n")
	containsAll(t, got, "type Num = any")
}

func TestUnionStructField(t *testing.T) {
	got := unionOut(t, "package main\ntype T struct { v int | string; n int }\n")
	containsAll(t, got, "type T struct { v any; n int }")
}

func TestUnionMultiMember(t *testing.T) {
	got := unionOut(t, "package main\nfunc f(v int | string | bool | float64) {}\n")
	containsAll(t, got, "func f(v any) {", "case int, string, bool, float64:")
}

func TestUnionQualifiedAndCompositeMembers(t *testing.T) {
	// 成员为包限定类型 / 切片 / 指针 / map / 泛型实例，均能被 typeEnd 解析
	src := `package main
import "fmt"
func a(v fmt.Stringer | int) {}
func b(v []int | string) {}
func c(v *int | *string) {}
func d(v map[string]int | int) {}
func e(v Box[int] | Box[string]) {}
`
	got := unionOut(t, src)
	for _, want := range []string{
		"func a(v any) {",
		"func b(v any) {",
		"func c(v any) {",
		"func d(v any) {",
		"func e(v any) {",
		"case fmt.Stringer, int:",
		"case []int, string:",
		"case *int, *string:",
		"case map[string]int, int:",
		"case Box[int], Box[string]:",
	} {
		containsAll(t, got, want)
	}
}

func TestUnionGroupedDecl(t *testing.T) {
	src := "package main\nvar (\n a int | string\n b bool\n c float64 | int\n)\n"
	got := unionOut(t, src)
	containsAll(t, got, "a any", "c any")
	containsAll(t, got, "b bool") // 非联合声明保持原样
}

// ---------- 消歧：位运算与初始化不得误伤 ----------

func TestUnionDoesNotTouchBitwise(t *testing.T) {
	src := `package main
func f(a int, b int) int {
    return a | b
}
func g(x int, y int) int { x |= y; return x & y }
`
	got := unionOut(t, src)
	containsAll(t, got, "return a | b", "x |= y", "x & y")
}

func TestUnionDoesNotTouchInitWithoutType(t *testing.T) {
	// var x = a|b 无显式类型，'=' 后的位运算不应被当作联合
	src := "package main\nfunc f() { a := 1; b := 2; var x = a | b; _ = x }\n"
	got := unionOut(t, src)
	containsAll(t, got, "var x = a | b")
}

func TestUnionDoesNotTouchInitWithType(t *testing.T) {
	// var x int = a|b：类型 int 后是 '='，其后的位运算不碰
	src := "package main\nfunc f() { a := 1; b := 2; var x int = a | b; _ = x }\n"
	got := unionOut(t, src)
	containsAll(t, got, "var x int = a | b")
}

func TestUnionNoUnionNoChange(t *testing.T) {
	src := "package main\nfunc f(v int) string { return \"x\" }\n"
	got := unionOut(t, src)
	if got != src {
		t.Errorf("无联合时输出应与输入一致:\n--- got ---\n%s\n--- want ---\n%s", got, src)
	}
}

// ---------- 端到端（完整管线 + Go 语法校验）----------

func TestUnionEndToEndWithTypeGuard(t *testing.T) {
	src := `package main

import "fmt"

func describe(v int | string | bool) string {
    if (v is int) {
        return fmt.sprint("int:", v+100)
    } else if (v is string) {
        return fmt.sprint("str:", v)
    }
    return "other"
}
`
	got := compileOK(t, "union_e2e.gx", src)
	containsAll(t, got,
		"func describe(v any) string",
		"v.(int)",
		"v.(string)",
		"v := __gx_v",
	)
}

func TestUnionEndToEndValidateAliasAndStruct(t *testing.T) {
	src := `package main

type Num = int | float64

type Box struct {
    value int | string
}

func main() {
    var n Num = 3.5
    _ = n
}
`
	got := compileOK(t, "union_misc.gx", src)
	containsAll(t, got, "type Num = any", "value any")
}

// ---------- sourcemap：any token 位置取联合起点 ----------

func TestUnionSourceMapStartPos(t *testing.T) {
	src := "package main\nfunc f(v int | string) {}\n"
	res, err := Compile("union_pos.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	// any 应映射到第 2 行联合起始处（int 所在列 12），而非 '|' 或后续成员
	pos, ok := res.SourceMap.GoToGx(2, 12)
	if !ok || pos.Line != 2 || pos.Column != 12 {
		t.Errorf("any 起点映射 = %+v ok=%v, want 2:12", pos, ok)
	}
}

// ---------- 联合参数运行时守卫 ----------

func TestUnionGuardSingleParam(t *testing.T) {
	got := unionOut(t, "package main\nfunc f(v int | string) {}\n")
	containsAll(t, got,
		"switch v.(type) {",
		"case int, string:",
		"default:",
		`panic("gx: f: v expects int|string, got a non-matching type")`,
	)
}

func TestUnionGuardMultipleParams(t *testing.T) {
	got := unionOut(t, "package main\nfunc f(v int | string, n bool | float64) {}\n")
	containsAll(t, got,
		"switch v.(type) {", "case int, string:",
		"switch n.(type) {", "case bool, float64:",
	)
}

func TestUnionGuardMethod(t *testing.T) {
	src := "package main\ntype T struct{}\nfunc (r T) f(v int | string) {}\n"
	got := unionOut(t, src)
	containsAll(t, got, `panic("gx: f: v expects int|string, got a non-matching type")`)
}

func TestUnionGuardNoUnionNoGuard(t *testing.T) {
	// 无联合参数时不得插入任何守卫
	src := "package main\nfunc f(v int) {}\nfunc g(v any) {}\n"
	got := unionOut(t, src)
	if strings.Contains(got, ".(type)") || strings.Contains(got, "panic(") {
		t.Errorf("无联合参数却插入了守卫:\n%s", got)
	}
}

func TestUnionGuardOnlyParamsNotLocals(t *testing.T) {
	// 局部变量/type 别名的联合只擦除，不插守卫（没有函数入口可检查）
	src := "package main\ntype Num = int | string\nfunc f() { var x int | string; _ = x }\n"
	got := unionOut(t, src)
	containsAll(t, got, "type Num = any", "var x any")
	if strings.Contains(got, ".(type)") || strings.Contains(got, "panic(") {
		t.Errorf("局部变量/别名不该有守卫:\n%s", got)
	}
}

func TestUnionGuardReturnOnlyNoGuard(t *testing.T) {
	// 仅返回值是联合：没有参数入口，不插守卫
	src := "package main\nfunc f() int | string { return 1 }\n"
	got := unionOut(t, src)
	containsAll(t, got, "func f() any")
	if strings.Contains(got, ".(type)") {
		t.Errorf("仅返回值联合不该有守卫:\n%s", got)
	}
}

func TestUnionGuardEndToEndValidInputs(t *testing.T) {
	src := `package main
import "fmt"
func describe(v int | string) string {
    if (v is int) { return fmt.sprint("int:", v+1) }
    return fmt.sprint("str:", v)
}
`
	got := compileOK(t, "ug_valid.gx", src)
	containsAll(t, got,
		"func describe(v any) string",
		"switch v.(type) {",
		"case int, string:",
		"v.(int)",
	)
}

func TestUnionGuardGeneratedCodeValid(t *testing.T) {
	// 守卫代码必须通过 go/parser（尤其 panic 单参数）
	src := `package main
func f(a int | string, b bool | int) {}
func g(v []int | map[string]int) {}
`
	res, err := Compile("ug_valid2.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("带守卫的生成代码语法非法: %v\n---\n%s", err, res.GoSource)
	}
}
