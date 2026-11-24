package compiler

import (
	"strings"
	"testing"
)

func hasDiag(res *Result, code string) bool {
	for _, d := range res.Analysis.Diagnostics() {
		if d.Code == code {
			return true
		}
	}
	return false
}

// A6：JSX 里的 Go 控制语句（for range / switch）会被当文本节点并产出坏 Go，
// 应报 jsx-control-statement 并提示用 <For> / if 块，且不生成文本节点。
func TestJSXControlStatementDiagnostic(t *testing.T) {
	src := "package main\nfunc main(){ rows := []int{1,2}; _ = <ul> for _, r := range rows { <li>{r}</li> } </ul> }\n"
	res, err := CompileScene("a6.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	if !hasDiag(res, "jsx-control-statement") {
		t.Fatalf("应报 jsx-control-statement, got %v", res.Analysis.Diagnostics())
	}
	if strings.Contains(res.GoSource, "for _, r := range rows") {
		t.Errorf("控制语句不应作为文本节点进产物:\n%s", res.GoSource)
	}
}

// 事件 handler 简化写法（原 B6）：箭头函数体含赋值（() => n = n + 1）曾因
// JSX 属性路径不展开箭头而残留 => 产非法 Go。现统一规范化成 gxscene.Handler，
// 箭头赋值、具名函数、func 字面量都应产合法 Go 且不再报诊断。
func TestEventHandlerSimplifiedForms(t *testing.T) {
	cases := []struct {
		name, decl, handler, jsx string
	}{
		{"箭头赋值", "sig n = 0\n", "() => n = n + 1", `<button onClick={%s}>a</button>`},
		{"带参箭头", "sig n = 0\n", "(e) => n = n + 1", `<input onInput={%s} />`},
		{"箭头调函数", "sig n = 0\nfunc inc(){ n = n + 1 }\n", "() => inc()", `<button onClick={%s}>a</button>`},
		{"具名函数", "sig n = 0\nfunc inc(){ n = n + 1 }\n", "inc", `<button onClick={%s}>a</button>`},
		{"func字面量", "sig n = 0\n", "func() { n = n + 1 }", `<button onClick={%s}>a</button>`},
		{"已Handler", "sig n = 0\n", "gxscene.Handler(func(ctx gxscene.EventContext) { n = n + 1 })", `<button onClick={%s}>a</button>`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			jsx := strings.Replace(c.jsx, "%s", c.handler, 1)
			src := "package main\nimport gxscene \"github.com/robertpanvip/gox/runtime/scene\"\n" + c.decl + "func main(){ _ = " + jsx + " }\n"
			res, err := CompileScene("b6.gx", src)
			if err != nil {
				t.Fatal(err)
			}
			if verr := res.Validate(); verr != nil {
				t.Fatalf("事件简化写法应产合法 Go: %v\n%s", verr, res.GoSource)
			}
			if hasDiag(res, "event-arrow-statement") {
				t.Errorf("事件写法不应再报 event-arrow-statement: %v", res.Analysis.Diagnostics())
			}
			if !strings.Contains(res.GoSource, "gxscene.Handler(func(") {
				t.Errorf("事件应包装成 gxscene.Handler:\n%s", res.GoSource)
			}
		})
	}
}

// B9：组件多根、return if 都产出非法 Go，应报带修法的 GX 诊断。
func TestJSXMultiRootAndReturnIfDiagnostics(t *testing.T) {
	cases := []struct {
		name, src, code string
	}{
		{"多根", "package main\nfunc Two() any { return <div>a</div><div>b</div> }\nfunc main(){ _ = Two() }\n", "jsx-multi-root"},
		{"return-if", "package main\nfunc Choose(v bool) any { return if v { <b>y</b> } else { <i>n</i> } }\nfunc main(){ _ = Choose(true) }\n", "jsx-return-if"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := CompileScene("b9.gx", c.src)
			if err != nil {
				t.Fatal(err)
			}
			if !hasDiag(res, c.code) {
				t.Errorf("应报 %s, got %v", c.code, res.Analysis.Diagnostics())
			}
		})
	}
}

// 守护：合法写法不产出这三类诊断——JSX 内 if 块、func(){} 事件、单根 return。
func TestB6B9A6NoFalsePositive(t *testing.T) {
	src := `package main
func main() {
 sig n = 0
 show := true
 _ = <div>{if show { <b>yes</b> } else { <i>no</i> }}</div>
 _ = <button onClick={func() { n = n + 1 }}>a</button>
}
`
	res, err := CompileScene("guard.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"jsx-control-statement", "event-arrow-statement", "jsx-multi-root", "jsx-return-if"} {
		if hasDiag(res, code) {
			t.Errorf("合法写法不应报 %s: %v", code, res.Analysis.Diagnostics())
		}
	}
}
