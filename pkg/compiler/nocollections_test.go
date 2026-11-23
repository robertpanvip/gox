package compiler

import (
	"strings"
	"testing"
)

// gx:nocollections 是文件级开关：声明后跳过全局切片提升，保留原生 []T 语义。
// 组件库（原生 Go + 手写 opcode）用它避免 list.List[T] 污染函数签名/类型断言。
func TestNoCollectionsPragmaKeepsNativeSlices(t *testing.T) {
	src := `package widgets

// gx:nocollections
func Render() {
	ops := []Op{{Code: 1, Arg: "div"}, {Code: 2}}
	_ = el
	var vals []any
	f := func() []any { return vals }
	switch x := any(ops).(type) {
	case []Op:
		_ = x
	}
	_ = f
}
`
	res, err := CompileSource(src)
	if err != nil {
		t.Fatal(err)
	}
	got := res.GoSource
	if err := res.Validate(); err != nil {
		t.Fatalf("生成非法 Go: %v\n---\n%s\n---", err, got)
	}
	// 原生切片形态原样保留
	for _, want := range []string{"[]Op{{Code: 1, Arg: \"div\"}, {Code: 2}}", "var vals []any", "func() []any", "case []Op:"} {
		if !strings.Contains(got, want) {
			t.Errorf("应保留原生切片 %q，实际输出:\n---\n%s\n---", want, got)
		}
	}
	// 不得注入 list 依赖或改写成 list.List
	assertNotContains(t, got, "runtime/list", "不应引入 runtime/list 依赖")
	assertNotContains(t, got, "list.List[", "不应改写为 list.List[T]")
	// 手写 opcode 里 []Op{{...},...} 的省略类型元素不得被补成匿名 struct
	assertNotContains(t, got, "struct{ Code", "不应补全省略类型元素为匿名 struct")
}

// 无 pragma 时行为零变化：仍然全局提升。
func TestNoCollectionsPragmaDefaultUnchanged(t *testing.T) {
	src := `package main
func f() {
	xs := []int{1, 2, 3}
	_ = xs
}
`
	res, err := CompileSource(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.GoSource, "list.List[int]") {
		t.Errorf("默认仍应提升为 list.List[int]，实际:\n%s", res.GoSource)
	}
}

func TestHasNoCollectionsPragma(t *testing.T) {
	if !hasNoCollectionsPragma("// gx:nocollections\npackage x") {
		t.Error("应识别行注释里的 pragma")
	}
	if !hasNoCollectionsPragma("package x // gx:nocollections 组件库") {
		t.Error("应识别行尾注释里的 pragma")
	}
	if hasNoCollectionsPragma("package x\nfunc f() []int { return nil }") {
		t.Error("普通源码不应误判为 pragma")
	}
}
