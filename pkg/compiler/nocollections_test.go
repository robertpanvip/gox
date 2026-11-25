package compiler

import (
	"strings"
	"testing"
)

// 全局切片提升现在是按需的：源码真正用到 .map/.filter/.reduce 链式调用才提升。
// 没有链式调用的源码（尤其原生 Go + 手写 opcode 的组件库）保留 []T 原生语义，
// 不污染函数签名/类型断言、不引入 runtime/list 依赖。
func TestSlicePromotionIsOnDemand(t *testing.T) {
	t.Run("无链式调用保留原生切片", func(t *testing.T) {
		// 组件库式源码带 gx:nocollections（强制跳过对象字面量补全），且无链式调用。
		src := `package widgets
// gx:nocollections
func Render() {
	ops := []Op{{Code: 1, Arg: "div"}, {Code: 2}}
	var vals []any
	f := func() []any { return vals }
	switch any(ops).(type) {
	case []Op:
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
		for _, want := range []string{"[]Op{{Code: 1, Arg: \"div\"}, {Code: 2}}", "var vals []any", "func() []any", "case []Op:"} {
			if !strings.Contains(got, want) {
				t.Errorf("应保留原生切片 %q，实际输出:\n---\n%s\n---", want, got)
			}
		}
		assertNotContains(t, got, "runtime/list", "无链式调用不应引入 runtime/list 依赖")
		assertNotContains(t, got, "list.List[", "无链式调用不应改写为 list.List[T]")
		assertNotContains(t, got, "struct{ Code", "不应补全省略类型元素为匿名 struct")
	})

	t.Run("有链式调用仍提升", func(t *testing.T) {
		src := `package main
func f() {
	xs := []int{1, 2, 3}
	ys := xs.map(func(x int) int { return x + 1 })
	_ = ys
}
`
		res, err := CompileSource(src)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(res.GoSource, "list.List[int]") {
			t.Errorf("用到链式方法时仍应提升为 list.List[int]，实际:\n%s", res.GoSource)
		}
		if err := res.Validate(); err != nil {
			t.Fatalf("生成非法 Go: %v\n%s", err, res.GoSource)
		}
	})
}

// gx:nocollections 是显式强制开关：跳过切片提升与对象字面量补全。
// 组件库源码用它表达"这段是原生 Go，任何情况下都不要 desugar"。
func TestNoCollectionsPragmaForcesSkip(t *testing.T) {
	src := `package widgets
// gx:nocollections
func values() []any {
	var vals []any
	return vals
}
func Render() {
	xs := []int{1, 2, 3}
	_ = xs
	_ = values()
}
`
	res, err := CompileSource(src)
	if err != nil {
		t.Fatal(err)
	}
	got := res.GoSource
	if err := res.Validate(); err != nil {
		t.Fatalf("生成非法 Go: %v\n%s", err, got)
	}
	assertNotContains(t, got, "runtime/list", "pragma 强制跳过，不应引入 runtime/list")
	assertNotContains(t, got, "list.List[", "pragma 强制跳过，不应提升")
	// 函数签名保持原生 []any，不被 list.List 污染
	if !strings.Contains(got, "func values() []any") {
		t.Errorf("函数签名应保持原生 []any:\n%s", got)
	}
}

// 无 gx:nocollections 时，[]pkg.Type{{...}} 的省略类型元素应沿用容器元素类型
// （含包限定名），不能降级成匿名 struct 而无法赋回 []pkg.Type。
// 这是组件库移除原生 Go 豁免标记后能纯 GX 书写的关键。
func TestContainerElemKeepsQualifierForObjectLit(t *testing.T) {
	src := `package widgets
import gxscene "github.com/robertpanvip/gox/runtime/scene"
func ops() []gxscene.Op {
	return []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpClose},
	}
}
`
	res, err := CompileSource(src)
	if err != nil {
		t.Fatal(err)
	}
	got := res.GoSource
	if err := res.Validate(); err != nil {
		t.Fatalf("生成非法 Go: %v\n%s", err, got)
	}
	if !strings.Contains(got, "gxscene.Op{Code: gxscene.OpElem, Arg: \"div\"}") {
		t.Errorf("裸元素字面量应补全为带限定名的 gxscene.Op{...}，实际:\n%s", got)
	}
	assertNotContains(t, got, "struct{ Code", "不应降级为匿名 struct")
}

func TestHasChainMethodCalls(t *testing.T) {
	yes := []string{
		`xs.map(f)`,
		`xs . filter (f)`,
		`a.reduce(fn)`,
	}
	for _, s := range yes {
		if !hasChainMethodCalls(New(s).AllTokens()) {
			t.Errorf("应识别链式调用 %q", s)
		}
	}
	no := []string{
		`xs := []int{1,2}`,               // 只有切片字面量
		`s := "xs.map(f)"`,               // 字符串里的调用
		`m := map[string]int{}; _ = m`,   // map 类型
	}
	for _, s := range no {
		if hasChainMethodCalls(New(s).AllTokens()) {
			t.Errorf("不应误判为链式调用 %q", s)
		}
	}
}
