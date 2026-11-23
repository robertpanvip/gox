package compiler

import (
	"strings"
	"testing"
)

// B2：嵌套 <For> 的内层 each 是外层 item 的字段（r.cells）。外层 item r:Row
// 即使因 r.cells 传给内层 For 被判逃逸（值快照 ForGetterOf），其类型信息仍须
// 登记，内层才能从 `type Row struct { cells []Cell }` 推出元素 Cell 而非 any。
// struct 字段绑定在 Binder 阶段不记 Type，需回到原始 token 流按声明文本反查。
func TestB2NestedForElemType(t *testing.T) {
	src := `package main
type Cell struct { v int }
type Row struct { cells []Cell }
func main() {
	rows := []Row{{[]Cell{{1}}}}
	_ = <table><For each={rows}>{(r) => <tr><For each={r.cells}>{(c) => <td>{c.v}</td>}</For></tr>}</For></table>
}
`
	res, err := CompileScene("b2.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	goSrc := res.GoSource
	if !strings.Contains(goSrc, "ForGetterOfSignal[Cell]") {
		t.Errorf("内层 For 元素类型应为 Cell, got:\n%s", goSrc)
	}
	if !strings.Contains(goSrc, "func() []Cell { return r.cells }") {
		t.Errorf("内层 each getter 应为 []Cell, got:\n%s", goSrc)
	}
	if strings.Contains(goSrc, "ForGetterOfSignal[any]") || strings.Contains(goSrc, "ForGetterOf[any]") {
		t.Errorf("内层 For 不应退化为 any, got:\n%s", goSrc)
	}
}

// B3：`rows := func() []Row {...}` 是 getter 变量，绑定表记解包类型 []Row。
// <For each={rows}> 的 each getter 必须调用一次 rows() 才能拿到切片，
// 否则生成 `func() []Row { return rows }`，go build 报函数值不能作返回值。
func TestB3GetterVariableEach(t *testing.T) {
	src := `package main
type Row struct { name string }
func main() {
	rows := func() []Row { return []Row{{"a"}} }
	_ = <ul><For each={rows}>{(r) => <li>{r.name}</li>}</For></ul>
}
`
	res, err := CompileScene("b3.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	goSrc := res.GoSource
	for _, want := range []string{
		"ForGetterOfSignal[Row]",
		"func() []Row { return rows() }",
	} {
		if !strings.Contains(goSrc, want) {
			t.Errorf("产物应包含 %q, got:\n%s", want, goSrc)
		}
	}
	if strings.Contains(goSrc, "func() []Row { return rows }") {
		t.Errorf("getter 变量未调用，应生成 rows(), got:\n%s", goSrc)
	}
}

// B11：零参数组件 <Counter/> 应生成 Counter()，不是 Counter(gxscene.Props{})。
func TestB11ZeroPropComponent(t *testing.T) {
	src := `package main
func Counter() any { return <button>x</button> }
func main() { _ = <Counter/> }
`
	res, err := CompileScene("b11.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	goSrc := res.GoSource
	if !strings.Contains(goSrc, "_ = Counter()") {
		t.Errorf("零参数组件调用点应生成 _ = Counter(), got:\n%s", goSrc)
	}
	if strings.Contains(goSrc, "Counter(gxscene.Props{})") {
		t.Errorf("零参数组件不应生成空 Props 实参, got:\n%s", goSrc)
	}
}
