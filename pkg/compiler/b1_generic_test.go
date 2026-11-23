package compiler

import (
	"strings"
	"testing"
)

// B1：gxscene.Get[T] 返回 getter（func() T），其局部变量传给 <For> each 时，
// 元素类型必须从显式类型实参 T 推断（Get[[]Row] → Row），不能因参数里的字符串
// 字面量被误推成 string、进而 sliceElemType 得到 rune；也不该产出误导性的
// "each 的类型 string 不是列表" 诊断。
func TestForEachGetGenericTypeArg(t *testing.T) {
	for _, tc := range []struct {
		name string
		each string
	}{
		{"getter变量", "each={rows}"},
		{"getter调用", "each={rows()}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type Row struct { id int; name string }
func List(props gxscene.Props) *gxscene.Instance {
    rows := gxscene.Get[[]Row](props, "rows")
    return <ul><For ` + tc.each + ` key={(r) => r.id}>{(r) => <li>{r.name}</li>}</For></ul>
}
`
			res, err := CompileScene("b1.gx", src)
			if err != nil {
				t.Fatal(err)
			}
			goSrc := res.GoSource
			// 元素类型必须是 Row，不是 rune；key/render 参数都用 Row。
			for _, want := range []string{
				"ForGetterOfSignal[Row]",
				"func(r Row) any { return r.id }",
				"func(r func() Row)",
				"func() any{ return r().name }",
			} {
				if !strings.Contains(goSrc, want) {
					t.Errorf("产物应包含 %q, got:\n%s", want, goSrc)
				}
			}
			if strings.Contains(goSrc, "ForGetterOfSignal[rune]") || strings.Contains(goSrc, "[]rune") {
				t.Errorf("元素类型不应退化为 rune, got:\n%s", goSrc)
			}
			// 不得产生 for-not-list 误报——rows 本就是切片 getter。
			for _, d := range res.Analysis.Diagnostics() {
				if d.Code == "for-not-list" {
					t.Errorf("不应报 for-not-list, got: %s", d.Message)
				}
			}
		})
	}
}

// B1 守护：普通函数调用、索引表达式不被泛型实参提取误伤。
func TestGenericCallTypeArgNoFalsePositive(t *testing.T) {
	src := `package main
type Row struct { name string }
func getRows() []Row { return nil }
func main() {
    rows := []Row{{"a"}}
    first := rows[0]
    got := getRows()
    _ = <div>{first.name} {got[0].name}</div>
}
`
	res, err := CompileScene("b1-guard.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	// first 应为 Row（rows[0] 索引），got 应为 []Row（getRows 返回值），都能取 name。
	if !strings.Contains(res.GoSource, "func() any{ return first.name }") &&
		!strings.Contains(res.GoSource, "first.name") {
		t.Errorf("索引取值 first.name 应保留, got:\n%s", res.GoSource)
	}
}
