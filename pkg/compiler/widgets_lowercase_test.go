package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 组件库 .gx 源必须遵循 GX 书写规范：外部 Go 包的「函数/方法/opcode 常量」
// 一律写小写，由编译器 upperFirst 还原；只有类型名与全大写缩写保持大写。
// 机械从 Go 迁移时容易把 gxscene.Get/signal.New 原样带成大写，此测试锁定风格。
//
// 注意：只检查代码 token，Comment/String/Template/Rune/JSX 内的大写一律放过
// （注释描述的是 Go API 真实名字，本就该大写）。
func TestWidgetsGxUsesLowercaseExternalCalls(t *testing.T) {
	dir := filepath.Join("..", "..", "widgets")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取 widgets 目录: %v", err)
	}
	extPkgs := map[string]bool{
		"gxscene": true, "signal": true, "fmt": true, "strconv": true,
		"atomic": true, "time": true, "strings": true,
	}
	// 类型名 + 全大写缩写：按规范保持大写，不算违规。
	keepUpper := map[string]bool{
		"gxscene.Op": true, "gxscene.Instance": true, "gxscene.Props": true,
		"gxscene.Handler": true, "gxscene.Template": true, "gxscene.EventContext": true,
		"gxscene.Node": true, "gxscene.NodeID": true, "gxscene.Branch": true,
		"gxscene.SyntheticEvent": true,
		"signal.Signal": true, "signal.Owner": true,
		"atomic.Uint64": true, "strings.Builder": true,
	}
	codeKinds := map[Kind]bool{Ident: true, Operator: true, Space: true, Newline: true}
	checked := 0
	for _, e := range ents {
		name := e.Name()
		if !strings.HasSuffix(name, ".gx") {
			continue
		}
		srcB, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		toks := New(string(srcB)).AllTokens()
		for i := 0; i+2 < len(toks); i++ {
			// 左侧包名与 '.' 必须是代码 token（排除注释/字符串里写的示例）。
			if !codeKinds[toks[i].Kind] || !extPkgs[toks[i].Text] {
				continue
			}
			if !codeKinds[toks[i+1].Kind] || toks[i+1].Text != "." {
				continue
			}
			if toks[i+2].Kind != Ident {
				continue
			}
			sel := toks[i+2].Text
			full := toks[i].Text + "." + sel
			if keepUpper[full] {
				continue
			}
			if sel != "" && sel[0] >= 'A' && sel[0] <= 'Z' {
				t.Errorf("%s:%d 外部包符号应写小写: %s（产物虽能还原，但源码风格违规）",
					name, toks[i+2].Pos.Line, full)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("未扫描到任何外部包符号，测试失去意义")
	}
}
