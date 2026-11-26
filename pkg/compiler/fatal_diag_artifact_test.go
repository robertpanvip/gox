package compiler

import "testing"

// Bug 05 回归：发出「产出非法 Go」的致命诊断后，CompileScene 仍把坏 GoSource
// 返回给下游（cabi 落盘 / LSP），隐患极大。
//
// 修法：管线末尾无条件用 Go 解析器做闸门，语法非法时标记 InvalidGo=true，
// 诊断仍保留在 Analysis。维持 err==nil 的诊断化契约（LSP 靠诊断工作）。
// GoSource 保留不清空——goxlsp 的语法诊断链路靠非法产物产出 parse 诊断
// （check.CheckAll），清空会让 LSP 用户看不到语法错误；所有落盘方
// （CLI 单文件/项目模式、module.Build、cabi）均按 InvalidGo 拦截写盘。
// 闸门亦覆盖「无诊断但产物非法」（如把包名 signal 当声明关键字）——
// 此前只在已有诊断时校验，该场景漏报。
func TestFatalDiagSuppressesInvalidGo(t *testing.T) {
	cases := []struct {
		name        string
		src         string
		diagCode    string
		wantInvalid bool
	}{
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
				// 产物保留（LSP parse 诊断依赖），但标志必须置位供落盘方拦截。
				if res.GoSource == "" {
					t.Error("非法产物应保留 GoSource 供 LSP 产出 parse 诊断")
				}
				if err := res.Validate(); err == nil {
					t.Error("非法产物 Validate 应报错（InvalidGo 的依据）")
				}
				if c.diagCode != "" && !hasDiag(res, c.diagCode) {
					t.Errorf("诊断仍应保留 %s, got %v", c.diagCode, res.Analysis.Diagnostics())
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

// 回归：无 GX 诊断但产物非法（把包名 signal 当声明关键字 `signal x = 1`）
// 也必须被闸门拦下置位 InvalidGo——此前只在已有诊断时校验，该场景漏报，
// Compile 调用方拿到非法产物却以为产物合法。
func TestInvalidGoFlagWithoutDiagnostics(t *testing.T) {
	res, err := Compile("invg.gx", "package main\nimport \"github.com/robertpanvip/gox/runtime/signal\"\nfunc main() {\n\tsignal count = 42\n\t_ = count\n}\n")
	if err != nil {
		t.Fatalf("诊断化契约应保持 err==nil: %v", err)
	}
	if !res.InvalidGo {
		t.Fatal("产物非法时 InvalidGo 应置位")
	}
	if res.GoSource == "" {
		t.Fatal("产物应保留供 LSP 产出 parse 诊断")
	}
	if err := res.Validate(); err == nil {
		t.Fatal("Validate 应确认产物非法")
	}
}
