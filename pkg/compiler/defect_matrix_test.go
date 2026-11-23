package compiler

import (
	"strings"
	"testing"
)

// T3.3 语法特性交互测试矩阵（总闸门）。
//
// 缺陷清单 A/B/C 每条都在专门测试文件里有详细断言；本文件用一张表把它们串成
// 「一条目一用例」的矩阵，任一回归都能按条目名快速定位。诊断类条目统一校验：
//   - CompileScene 不返回硬错误（诊断走 Analysis，不中断编译）；
//   - 目标诊断码出现，且 Pos 为 1 基合法坐标、Message 非空。
//
// 产物类条目（A1/B1/B2/B3/B7/B8/B11 等）由各自测试断言 GoSource 片段，本表在
// matrixArtifactCases 中列出最小输入与期望片段，作为交叉守护。

type matrixDiagCase struct {
	item string // 缺陷清单条目
	src  string
	code string
}

func TestDefectMatrixDiagnostics(t *testing.T) {
	cases := []matrixDiagCase{
		{"A2-class对象", "package main\nfunc main(){ _ = <div class={{a:true}}>x</div> }\n", "attr-unsupported"},
		{"A3-style对象", "package main\nfunc main(){ _ = <div style={{color:\"red\"}}>x</div> }\n", "attr-unsupported"},
		{"B10-双引号模板", "package main\nfunc main(){ n:=\"x\"; _ = <div class=\"a ${n}\">x</div> }\n", "attr-unsupported"},
		{"A4-For-empty", "package main\nfunc main(){ rows:=[]int{1}; _ = <ul><For each={rows} empty={<li>x</li>}>{(r)=><li>{r}</li>}</For></ul> }\n", "attr-unsupported"},
		{"A5-bind前缀", "package main\nfunc main(){ n:=\"x\"; _ = <input bind:value={n}/> }\n", "attr-unknown"},
		{"A6-JSX控制语句", "package main\nfunc main(){ rows:=[]int{1}; _ = <ul> for _, r := range rows { <li>{r}</li> } </ul> }\n", "jsx-control-statement"},
		{"B6-事件箭头赋值", "package main\nfunc main(){ signal n = 0; _ = <button onClick={() => n = n + 1}>a</button> }\n", "event-arrow-statement"},
		{"B9-多根", "package main\nfunc C(){}\nfunc main(){ _ = C() }\n", ""}, // 见专门测试：多根需组件上下文
		{"B4-signal深写", "package main\nfunc main(){ signal s = P{a:1}; s().a = 2; _ = s() }\n", "signal-deep-write"},
		{"C2-切片元素字段深写", "package main\ntype Row struct{ name string }\nfunc main(){ signal rows = []Row{{name:\"a\"}}; rows[0].name = \"b\"; _ = rows() }\n", "signal-deep-write"},
	}
	for _, c := range cases {
		if c.code == "" {
			continue // 该条目输入需额外上下文，由专门测试覆盖
		}
		t.Run(c.item, func(t *testing.T) {
			res, err := CompileScene("matrix.gx", c.src)
			if err != nil {
				t.Fatalf("诊断应由 Analysis 消费而非硬错误: %v", err)
			}
			found := false
			for _, d := range res.Analysis.Diagnostics() {
				if d.Code == c.code {
					found = true
					if d.Pos.Line < 1 || d.Pos.Col < 1 {
						t.Errorf("诊断坐标须 1 基: %+v", d.Pos)
					}
					if strings.TrimSpace(d.Message) == "" {
						t.Error("诊断 Message 非空")
					}
				}
			}
			if !found {
				t.Errorf("条目 %s 应产出 %s, got %v", c.item, c.code, res.Analysis.Diagnostics())
			}
		})
	}
}

type matrixArtifactCase struct {
	item, src, want string
}

// 产物类条目的交叉守护：GoSource（或诊断缺失）符合期望片段。
func TestDefectMatrixArtifacts(t *testing.T) {
	cases := []matrixArtifactCase{
		{"B1-Get泛型", "package main\nfunc main(){ _ = <ul><For each={gxscene.Get[[]int](nil,\"rows\")}>{(r)=><li>{r}</li>}</For></ul> }\n", "gxscene.Get[[]int]"},
		{"B11-零props组件", "package main\nfunc Counter(){}\nfunc main(){ _ = <Counter/> }\n", "Counter()"},
	}
	for _, c := range cases {
		t.Run(c.item, func(t *testing.T) {
			res, err := CompileScene("matrix.gx", c.src)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(res.GoSource, c.want) {
				t.Errorf("条目 %s 产物应含 %q:\n%s", c.item, c.want, res.GoSource)
			}
		})
	}
}
