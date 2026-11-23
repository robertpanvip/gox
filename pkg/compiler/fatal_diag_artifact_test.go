package compiler

import "testing"

// Bug 05 回归：发出「产出非法 Go」的致命诊断后，CompileScene 仍把坏 GoSource
// 返回给下游（cabi 落盘 / LSP），隐患极大。
//
// 修法：管线末尾在「有诊断且产物语法非法」时清空 GoSource/SourceMap、标记
// InvalidGo=true，诊断仍保留在 Analysis。维持 err==nil 的诊断化契约（LSP 靠
// 诊断工作），但下游再也不会拿到坏产物。cabi 据此回传诊断 JSON。
func TestFatalDiagSuppressesInvalidGo(t *testing.T) {
	cases := []struct {
		name        string
		src         string
		diagCode    string
		wantInvalid bool
	}{
		{
			name:        "事件箭头赋值（非法 Go）",
			src:         "package main\nfunc main(){ sig n = 0; _ = <button onClick={() => n = n + 1}>a</button> }\n",
			diagCode:    "event-arrow-statement",
			wantInvalid: true,
		},
		{
			name:        "组件多根（非法 Go）",
			src:         "package main\nfunc Two() any { return <div>a</div><div>b</div> }\nfunc main(){ _ = Two() }\n",
			diagCode:    "jsx-multi-root",
			wantInvalid: true,
		},
		{
			name:        "return if（非法 Go）",
			src:         "package main\nfunc Choose(v bool) any { return if v { <b>y</b> } else { <i>n</i> } }\nfunc main(){ _ = Choose(true) }\n",
			diagCode:    "jsx-return-if",
			wantInvalid: true,
		},
		{
			name:        "合法写法不抑制",
			src:         "package main\nfunc main(){ sig n = 0; _ = <button onClick={func() { n = n + 1 }}>a</button> }\n",
			wantInvalid: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := CompileScene("fatal.gx", c.src)
			if err != nil {
				t.Fatalf("诊断化错误应保持 err==nil: %v", err)
			}
			if res.InvalidGo != c.wantInvalid {
				t.Errorf("InvalidGo=%v want=%v", res.InvalidGo, c.wantInvalid)
			}
			if c.wantInvalid {
				if res.GoSource != "" || res.SourceMap != nil {
					t.Errorf("非法 Go 产物应被清空: goSource=%q", res.GoSource)
				}
				if !hasDiag(res, c.diagCode) {
					t.Errorf("抑制产物后诊断仍应保留 %s, got %v", c.diagCode, res.Analysis.Diagnostics())
				}
			} else {
				if res.GoSource == "" {
					t.Error("合法写法产物不应被清空")
				}
				if err := res.Validate(); err != nil {
					t.Errorf("合法产物应语法正确: %v", err)
				}
			}
		})
	}
}
