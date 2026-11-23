package compiler

import (
	"strings"
	"testing"
)

// Bug 02 回归：sig 字段持有 struct 时，h.s.n = 2（s 是 sig 字段、n 是普通子字段）
// 会被改写成 h.s().n = 2——对函数结果取字段赋值，是非法 Go。
// checkSignalValueDeepWrite 只认 sig 变量（isSignalRefAt），这类「中间 sig 字段段」
// 命不中，此前静默产出非法 Go 且无诊断。现由 rewriteSignalFieldRefs 在链改写时补
// signal-deep-write 诊断（带 1 基坐标 + 整体替换修法）。
func TestSigFieldCompositeDeepWriteDiagnostic(t *testing.T) {
	src := `package main
type S struct { n int }
type H struct { sig s S }
func main() { h := H{s: S{n:1}}; h.s.n = 2; _ = h }
`
	res, err := CompileScene("c2-sigfield.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range res.Analysis.Diagnostics() {
		if d.Code == "signal-deep-write" && strings.Contains(d.Message, "h.s.n") {
			found = true
			if d.Pos.Line < 1 || d.Pos.Col < 1 {
				t.Errorf("诊断应带 1 基 GX 坐标, got %+v", d.Pos)
			}
			if !strings.Contains(d.Message, "h.s(新值)") {
				t.Errorf("诊断应含整体替换修法, got %s", d.Message)
			}
		}
	}
	if !found {
		t.Errorf("应报 signal-deep-write 诊断含 h.s.n 与修法, got %v", res.Analysis.Diagnostics())
	}
}

// 守护：只读、sig 字段自身写、普通字段深写都不应误报；已有 B4/C2 仍报。
func TestSigFieldDeepWriteDiagnosticGuards(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantDiag bool
	}{
		{"只读不报", "package main\ntype S struct { n int }; type H struct { sig s S }\nfunc main() { h := H{s: S{n:1}}; _ = h.s.n }\n", false},
		{"sig字段自身写不报", "package main\ntype H struct { sig n int }\nfunc main() { h := H{n: 1}; h.n = 2; _ = h }\n", false},
		{"普通字段深写不报", "package main\ntype S struct { n int }; type H struct { s S }\nfunc main() { h := H{s: S{n:1}}; h.s.n = 2; _ = h }\n", false},
		{"B4仍报", "package main\ntype S struct { n int }\nfunc main() { sig s = S{n:1}; s.n = 2; _ = s() }\n", true},
		{"C2仍报", "package main\ntype Row struct { name string }\nfunc main() { sig rows = []Row{{name:\"a\"}}; rows[0].name = \"b\"; _ = rows() }\n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := CompileScene("guard.gx", c.src)
			if err != nil {
				t.Fatal(err)
			}
			got := false
			for _, d := range res.Analysis.Diagnostics() {
				if d.Code == "signal-deep-write" {
					got = true
				}
			}
			if got != c.wantDiag {
				t.Errorf("wantDiag=%v got=%v, diags=%+v", c.wantDiag, got, res.Analysis.Diagnostics())
			}
		})
	}
}
