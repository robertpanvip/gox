package compiler

import (
	"strings"
	"testing"
)

// A2/A3/B10：属性位的三种静默错值写法必须产出带坐标、带修法的 GX 诊断，
// 把"编译通过 + 错值"变成明确报错（CLI 消费诊断后阻断构建）。
func TestUnsupportedAttrDiagnostics(t *testing.T) {
	cases := []struct {
		name, jsx, extra, want string
	}{
		{"class对象字面量", `<div class={{ active: true, big: true }}>x</div>`, "", "class 不支持对象字面量"},
		{"双引号模板串", `<div class="a ${n}">x</div>`, "n := \"x\"\n    ", "需用反引号模板串"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := CompileScene("attr.gx", "package main\nfunc main() {\n    "+c.extra+"_ = "+c.jsx+"\n}\n")
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, d := range res.Analysis.Diagnostics() {
				if d.Code == "attr-unsupported" && strings.Contains(d.Message, c.want) {
					found = true
					if d.Pos.Line < 1 || d.Pos.Col < 1 {
						t.Errorf("诊断应带 1 基 GX 坐标, got %+v", d.Pos)
					}
				}
			}
			if !found {
				t.Errorf("应产出 attr-unsupported 诊断含 %q, got %v", c.want, res.Analysis.Diagnostics())
			}
		})
	}
}

// 守护：合法属性写法（静态字符串、表达式、反引号模板串）不产出 attr-unsupported。
func TestSupportedAttrsNoDiagnostic(t *testing.T) {
	src := "package main\nfunc main() {\n n := \"x\"\n _ = <div class=\"a b\" id={n} title={`a ${n}`}>x</div>\n}\n"
	res, err := CompileScene("attr-ok.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range res.Analysis.Diagnostics() {
		if d.Code == "attr-unsupported" {
			t.Errorf("合法属性不应报诊断: %s", d.Message)
		}
	}
}
