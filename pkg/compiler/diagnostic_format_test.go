package compiler

import (
	"strings"
	"testing"
)

// T3.4：所有 GXDiagnostic 走统一格式——1 基坐标、非空 Code/Message、String() 含位置。
// 用一组能触发各诊断码的输入，逐条校验产出符合统一格式。
func TestDiagnosticFormatConsistency(t *testing.T) {
	sources := []string{
		"package main\nfunc main(){ _ = <div class={{a:true}}>x</div> }\n",
		"package main\nfunc main(){ n:=\"x\"; _ = <div class=\"a ${n}\">x</div> }\n",
		"package main\nfunc main(){ rows:=[]int{1}; _ = <ul> for _, r := range rows { <li>{r}</li> } </ul> }\n",
		"package main\nfunc main(){ sig n = 0; _ = <button onClick={() => n = n + 1}>a</button> }\n",
		"package main\nfunc main(){ n:=\"x\"; _ = <input bind:value={n}/> }\n",
		"package main\ntype Row struct{ name string }\nfunc main(){ sig rows = []Row{{name:\"a\"}}; rows[0].name = \"b\"; _ = rows() }\n",
	}
	total := 0
	for i, src := range sources {
		res, err := CompileScene("fmt.gx", src)
		if err != nil {
			t.Fatalf("case %d: 诊断不应硬失败: %v", i, err)
		}
		for _, d := range res.Analysis.Diagnostics() {
			total++
			if d.Pos.Line < 1 || d.Pos.Col < 1 {
				t.Errorf("case %d 诊断坐标非 1 基: %+v", i, d.Pos)
			}
			if strings.TrimSpace(d.Code) == "" {
				t.Errorf("case %d 诊断 Code 空: %+v", i, d)
			}
			if strings.TrimSpace(d.Message) == "" {
				t.Errorf("case %d 诊断 Message 空: %+v", i, d)
			}
			s := d.String()
			if !strings.Contains(s, d.Code) || !strings.Contains(s, d.Message) {
				t.Errorf("case %d String() 应含 Code 与 Message: %q", i, s)
			}
		}
	}
	if total == 0 {
		t.Fatal("测试输入未产出任何诊断，无法校验格式")
	}
}
