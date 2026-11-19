package compiler

import (
	"regexp"
	"strings"
	"testing"
)

func tgStage(t *testing.T, src string) string {
	t.Helper()
	got, err := CompileStage(src, "typeguard")
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func assertNo(t *testing.T, got, bad, why string) {
	t.Helper()
	if strings.Contains(got, bad) {
		t.Errorf("不应包含 %q（%s）\n---\n%s\n---", bad, why, got)
	}
}

func TestTypeGuardBasic(t *testing.T) {
	src := `package main
func main() {
    var x any = float64(3.5)
    if (x is float64) {
        println(x)
    }
}
`
	got := tgStage(t, src)
	containsAll(t, got,
		"x.(float64)",
		"x := __gx_v_",
		"; __gx_ok_",
	)
	assertNo(t, got, " is ", "is 守卫应被消除")
}

func TestTypeGuardNoParens(t *testing.T) {
	src := `package main
func main() {
    var x any = 1
    if x is int {
        println(x)
    }
}
`
	got := tgStage(t, src)
	containsAll(t, got, "x.(int)", "x := __gx_v_")
	assertNo(t, got, " is ", "is 守卫应被消除")
}

func TestTypeGuardFullPipelineValid(t *testing.T) {
	src := `package main

import "fmt"

func main() {
    var v any = "hi"
    if (v is string) {
        fmt.println(len(v))
    }
}
`
	got := compileOK(t, "typeguard.gx", src)
	containsAll(t, got, "v.(string)", "v := __gx_v_")
	assertNo(t, got, " is ", "is 守卫应被消除")
}

func TestTypeGuardTypes(t *testing.T) {
	cases := []struct {
		typ  string
		want string
	}{
		{"float64", "x.(float64)"},
		{"int", "x.(int)"},
		{"string", "x.(string)"},
		{"[]int", "x.([]int)"},
		{"*point", "x.(*point)"},
		{"http.Request", "x.(http.Request)"},
		{"map[string]int", "x.(map[string]int)"},
	}
	for _, c := range cases {
		t.Run(c.typ, func(t *testing.T) {
			src := "package main\nfunc main() {\n    var x any\n    if (x is " + c.typ + ") { println(x) }\n}\n"
			got := tgStage(t, src)
			containsAll(t, got, c.want)
			assertNo(t, got, " is ", "is 守卫应被消除")
		})
	}
}

func TestTypeGuardIsAsVariable(t *testing.T) {
	// is 作普通变量名时不应被误伤
	src := `package main
func main() {
    is := 3
    var x any = 4
    if x > is { println("big") }
    if is == 3 { println("ok") }
}
`
	got := tgStage(t, src)
	containsAll(t, got, "is := 3", "x > is", "if is == 3")
	assertNo(t, got, "__gx_", "不应生成任何守卫变量")
}

func TestTypeGuardCompoundConditionSkipped(t *testing.T) {
	// 复合条件不是单一守卫，保留原样（GX 不做类型检查，交给 Go 解析器报错）
	src := `package main
func main() {
    var x any = 1
    y := 2
    if (x is int && y > 0) { println(x) }
}
`
	got := tgStage(t, src)
	containsAll(t, got, "x is int")
	assertNo(t, got, "__gx_", "复合条件不应转换")
}

func TestTypeGuardSelectorRecvSkipped(t *testing.T) {
	// 复合左值 obj.field 暂不支持，保留原样
	src := `package main
type wrap struct{ f any }
func main() {
    obj := wrap{f: 1}
    if (obj.f is int) { println(obj.f) }
}
`
	got := tgStage(t, src)
	containsAll(t, got, "obj.f is int")
	assertNo(t, got, "__gx_", "selector 左值不应转换")
}

func TestTypeGuardNested(t *testing.T) {
	src := `package main
func main() {
    var a any = 1
    var b any = "s"
    if (a is int) {
        if (b is string) {
            println(a, b)
        }
    }
}
`
	got := tgStage(t, src)
	containsAll(t, got, "a := __gx_v_", "b := __gx_v_", "a.(int)", "b.(string)")
}

func TestTypeGuardElseBranch(t *testing.T) {
	src := `package main
func main() {
    var x any = 1
    if (x is int) {
        println(x)
    } else {
        println("other")
    }
}
`
	got := tgStage(t, src)
	containsAll(t, got, "x.(int)", "x := __gx_v_", "} else {")
	// shadow 只出现在 if 块，else 块不注入
	if n := strings.Count(got, "x := __gx_v_"); n != 1 {
		t.Errorf("shadow 应只注入一次，got %d\n---\n%s\n---", n, got)
	}
}

func TestTypeGuardUniqueTempNames(t *testing.T) {
	src := `package main
func main() {
    var x any = 1
    if (x is int) { println(x) }
    if (x is string) { println(x) }
    if (x is float64) { println(x) }
}
`
	got := tgStage(t, src)
	re := regexp.MustCompile(`__gx_v_\d+_\d+`)
	seen := map[string]bool{}
	for _, n := range re.FindAllString(got, -1) {
		seen[n] = true
	}
	if len(seen) != 3 {
		t.Errorf("3 个 if 应有 3 个不同的临时变量名，got %d: %v\n---\n%s\n---", len(seen), seen, got)
	}
}
