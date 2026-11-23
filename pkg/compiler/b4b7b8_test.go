package compiler

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// B8：Signal.computed(() => { x: a, y: b }) 的箭头 body 是对象字面量而非语句块，
// 应生成返回匿名 struct 的 lambda；旧实现误判为 blockBody，产出非法 Go。
func TestComputedArrowObjectLiteral(t *testing.T) {
	src := "package main\nfunc main() { a:=1; b:=2; let s = Signal.computed(() => { x: a, y: b }); _ = s }\n"
	res, err := CompileScene("b8.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.GoSource, "Signal.Computed(func() any {return struct{") &&
		!strings.Contains(res.GoSource, "Signal.Computed(func() any { return struct{") {
		t.Errorf("对象字面量 body 应生成为匿名 struct 返回, got:\n%s", res.GoSource)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "", res.GoSource, 0); err != nil {
		t.Errorf("生成代码应为合法 Go: %v\n%s", err, res.GoSource)
	}
}

// 守护：真正的花括号语句块 body（含 ; / := / 控制语句）仍按 void 回调生成。
func TestLambdaBlockBodyStillWorks(t *testing.T) {
	src := "package main\nfunc main() { Signal.effect(() => { x := 1; println(x) }) }\n"
	res, err := CompileScene("b8-block.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.GoSource, "Signal.Effect(func()") {
		t.Errorf("语句块 body 应生成 void lambda, got:\n%s", res.GoSource)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "", res.GoSource, 0); err != nil {
		t.Errorf("生成代码应为合法 Go: %v\n%s", err, res.GoSource)
	}
}

// B7：位置简写 P{v}（P 唯一字段是 signal 字段）时，signal 变量 v 应传 getter
// 本身（P{v}），不是读出的值（P{v()}）。多字段布局简写保持原样（显式键可用）。
func TestStructSignalFieldShorthand(t *testing.T) {
	src := "package main\ntype P struct { signal a string }\nfunc main() { signal v = \"x\"; p := P{v}; _ = p }\n"
	res, err := CompileScene("b7.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.GoSource, "p := P{v}") {
		t.Errorf("单字段 signal 简写应传 getter 本身 P{v}, got:\n%s", res.GoSource)
	}
	if strings.Contains(res.GoSource, "P{v()}") {
		t.Errorf("不应把 signal getter 读成值 P{v()}, got:\n%s", res.GoSource)
	}
}

// B4：signal 持有复合值的字段深写（s.n = 2）产出带坐标、带修法的 GX 诊断，
// 把 Go 的 "cannot assign to s().n" 变成明确的 GX 提示。
func TestSignalDeepWriteDiagnostic(t *testing.T) {
	src := "package main\ntype S struct { n int }\nfunc main() { signal s = S{n:1}; s.n = 2 }\n"
	res, err := CompileScene("b4.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range res.Analysis.Diagnostics() {
		if d.Code == "signal-deep-write" && strings.Contains(d.Message, "s.n") {
			found = true
			if d.Pos.Line < 1 || d.Pos.Col < 1 {
				t.Errorf("诊断应带 1 基 GX 坐标, got %+v", d.Pos)
			}
		}
	}
	if !found {
		t.Errorf("应报 signal-deep-write 诊断含修法, got %v", res.Analysis.Diagnostics())
	}
}

// 守护：signal 字段自身的写（p.name = v，p 非 signal 变量）不应误报 B4。
func TestSignalFieldWriteNoDeepWriteDiag(t *testing.T) {
	src := "package main\ntype P struct { signal name string }\nfunc main() { p := P{name: \"a\"}; p.name = \"b\"; _ = p }\n"
	res, err := CompileScene("b4-ok.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range res.Analysis.Diagnostics() {
		if d.Code == "signal-deep-write" {
			t.Errorf("signal 字段写不应误报 signal-deep-write: %s", d.Message)
		}
	}
}
