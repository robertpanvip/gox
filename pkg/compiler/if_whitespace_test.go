package compiler

import (
	"strings"
	"testing"
)

// Bug 04 回归：if 条件块前后、块内的排版空白不应产出独立的 OpChildText " " 节点。
// A7 在文本循环里对注释 break 沿用了「原样 append buffer」的模式，同类 if 边界
// break 未纳入修复，导致 `<span>x</span> if ok {...}` 的 if 前空格、条件块内
// `<b>A</b> <i>I</i>` 的相邻空格、以及 if 块后紧跟闭合标签的空格都变成可见文本。
//
// 修法（定点、保 inline 语义）：
//   - parseChildren：因 if/注释边界 break、或紧跟条件块之后的「纯空白」丢弃；
//     因 < / { / } / EOF 结束的 inline 空白（有意的单词/元素分隔）保留。
//   - parseCondBlock：条件块内纯空白文本节点一律丢弃（含内容的文本保留）。
func TestIfBlockWhitespaceNoTextNode(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		wantBare bool // 产物是否应含独立 OpChildText " "
	}{
		{
			name: "if 前后与块内空白",
			src: `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type P struct { ok bool }
func main() { p:=P{ok:true}; _ = <div><span>x</span> if p.ok { <b>y</b> } tail</div> }
`, wantBare: false,
		},
		{
			name: "if 块内相邻元素空格",
			src: `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type P struct { ok bool }
func main() { p:=P{ok:true}; _ = <div>if p.ok { <b>A</b> <i>I</i> }</div> }
`, wantBare: false,
		},
		{
			name: "if 后紧跟闭合标签",
			src: `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type P struct { ok bool }
func main() { p:=P{ok:true}; _ = <div>if p.ok { <b>y</b> } </div> }
`, wantBare: false,
		},
		{
			name: "有意 inline 空格须保留",
			src: `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
func main() { _ = <div><b>A</b> <i>B</i></div> }
`, wantBare: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := CompileScene("if-ws.gx", c.src)
			if err != nil {
				t.Fatal(err)
			}
			bare := strings.Contains(res.GoSource, `OpChildText,Arg:" ",`)
			if bare != c.wantBare {
				t.Errorf("独立空白文本节点 want=%v got=%v\n%s", c.wantBare, bare, res.GoSource)
			}
		})
	}
}
