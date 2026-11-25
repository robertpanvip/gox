// Package check 是 GX 语义层的 AST 侧类型检查器（方案 B）。
//
// 与 token 流分析不同，本包用 go/parser 解析「降级后的 Go 源码」拿到
// 完整 AST（作用域、控制流、表达式结构都由 Go 官方解析器提供），再把 AST
// 节点的 Go 坐标经 sourcemap 映射回 GX 坐标，从 compiler.Analysis 这张 side
// table 里取回 desugar 中被擦除的联合类型 / 收窄信息，应用 GX 自己的规则。
//
// 这就是 TS 对 JS 的关系在本项目的落地：
//   - 语法结构（AST）与"降级代码合法性"交给 go/parser 兜底，语法错误经
//     sourcemap 逐条回投 GX 坐标；类型级完整兜底在 CLI `gox check -check` /
//     桌面 gox-lsp 的 buildcheck（真实 go build + go/types），js/wasm 无工具链，
//     浏览器内 LSP 只保证「语法层 + GX 语义层」两类诊断。
//   - 联合成员、收窄、穷尽性等 GX 特有类型规则由本包负责，产出 GX 原生诊断。
//
// 诊断分两路，由 CheckAll 统一出口（GX 坐标、排序、去重）：
//   - AST 规则：union-assign / union-call-arg / union-return / union-operand / non-exhaustive
//   - Analysis 层：union-member / redundant-guard（token 流分析阶段产出）
package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"sort"
	"strings"

	"github.com/robertpanvip/gox/pkg/compiler"
	"github.com/robertpanvip/gox/pkg/sourcemap"
)

// Diagnostic 是一条 GX 类型检查诊断，位置为 GX 源码坐标（1 基）。
type Diagnostic struct {
	Line    int
	Col     int
	Code    string
	Message string
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%d:%d: %s: %s", d.Line, d.Col, d.Code, d.Message)
}

// Checker 持有 sourcemap 与 GX 语义层 side table，检查一份降级后的 Go 源码。
type Checker struct {
	sm *sourcemap.SourceMap
	an *compiler.Analysis
}

// New 构造检查器。an 或 sm 为 nil 时仍可检查 Go 语法，但 GX 规则会跳过。
func New(sm *sourcemap.SourceMap, an *compiler.Analysis) *Checker {
	return &Checker{sm: sm, an: an}
}

// Check 解析 goSrc 为 AST 并遍历，返回 AST 规则诊断（GX 坐标）。
// 语法解析失败时逐条产出 parse 诊断（parser.AllErrors，最多 maxParseDiags 条）：
// 位置优先经 sourcemap 回投 GX 坐标（消息只带 go/scanner 的裸错误文本，
// 不暴露用户看不见的生成文件坐标）；无映射时回退 Go 坐标。空源码返回无诊断
// ——InvalidGo 清空产物的场景下，诊断来自 Analysis 层，不得再叠 EOF 噪音。
func (c *Checker) Check(goSrc string) ([]Diagnostic, error) {
	if strings.TrimSpace(goSrc) == "" {
		return nil, nil
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "check.go", goSrc, parser.AllErrors)
	if err != nil {
		return c.parseDiags(err), nil
	}
	v := newVisitor(c, fset)
	ast.Walk(v, f)
	return v.diags, nil
}

// maxParseDiags 限制一次报出的语法错误条数：go/parser 的 AllErrors 在
// 括号失配等场景会产生级联噪音，首因最具定位价值，超出部分截断。
const maxParseDiags = 8

// parseDiags 把 go/parser 的 scanner.ErrorList 转成逐条、已映射 GX 坐标的诊断。
func (c *Checker) parseDiags(err error) []Diagnostic {
	out := []Diagnostic{}
	if el, ok := err.(scanner.ErrorList); ok {
		for _, e := range el {
			if len(out) >= maxParseDiags {
				break
			}
			line, col := e.Pos.Line, e.Pos.Column
			if c.sm != nil {
				if gp, mapped := c.sm.GoToGx(line, col); mapped {
					line, col = gp.Line, gp.Column
				}
			}
			if line < 1 {
				line = 1
			}
			if col < 1 {
				col = 1
			}
			out = append(out, Diagnostic{Line: line, Col: col, Code: "parse", Message: e.Msg})
		}
	}
	if len(out) == 0 {
		out = append(out, Diagnostic{Line: 1, Col: 1, Code: "parse", Message: err.Error()})
	}
	return out
}

// CheckAll 是统一诊断出口：合并 AST 规则诊断与 Analysis 层诊断，
// 统一为 GX 坐标，按 (行,列,码) 排序并去重。
func (c *Checker) CheckAll(goSrc string) ([]Diagnostic, error) {
	astDiags, err := c.Check(goSrc)
	if err != nil {
		return nil, err
	}
	all := append([]Diagnostic(nil), astDiags...)
	if c.an != nil {
		for _, gd := range c.an.Diagnostics() {
			all = append(all, Diagnostic{
				Line: gd.Pos.Line, Col: gd.Pos.Col,
				Code: gd.Code, Message: gd.Message,
			})
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Line != all[j].Line {
			return all[i].Line < all[j].Line
		}
		if all[i].Col != all[j].Col {
			return all[i].Col < all[j].Col
		}
		return all[i].Code < all[j].Code
	})
	// 去重：相同 (行,列,码,消息) 只保留一条
	out := all[:0]
	for _, d := range all {
		if len(out) > 0 && out[len(out)-1] == d {
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

// visitor 承载一次遍历的状态。
type visitor struct {
	c     *Checker
	fset  *token.FileSet
	diags []Diagnostic
	env   map[string]valueType // 值环境：标识符 → 已推断类型（const/:= /= 传播）
}

// newVisitor 创建遍历器，初始化值环境。
func newVisitor(c *Checker, fset *token.FileSet) *visitor {
	return &visitor{c: c, fset: fset, env: map[string]valueType{}}
}

// gxPos 把 AST 节点的 Go 坐标映射回 GX 坐标。映射失败时回退到 Go 坐标。
func (v *visitor) gxPos(p token.Pos) (int, int, bool) {
	gp := v.fset.Position(p)
	if v.c.sm == nil {
		return gp.Line, gp.Column, false
	}
	gx, ok := v.c.sm.GoToGx(gp.Line, gp.Column)
	if !ok {
		return gp.Line, gp.Column, false
	}
	return gx.Line, gx.Column, true
}

func (v *visitor) report(p token.Pos, code, msg string) {
	line, col, _ := v.gxPos(p)
	v.diags = append(v.diags, Diagnostic{Line: line, Col: col, Code: code, Message: msg})
}

// reportAtGx 用显式 GX 坐标报告（供需要精确位置的规则使用）。
func (v *visitor) reportAtGx(line, col int, code, msg string) {
	v.diags = append(v.diags, Diagnostic{Line: line, Col: col, Code: code, Message: msg})
}

// Visit 分派 AST 节点到各规则。
func (v *visitor) Visit(n ast.Node) ast.Visitor {
	if v.c.an == nil {
		return v
	}
	switch node := n.(type) {
	case *ast.ValueSpec:
		v.checkValueSpec(node)
	case *ast.AssignStmt:
		v.checkAssign(node)
	case *ast.CallExpr:
		v.checkCall(node)
	case *ast.ReturnStmt:
		v.checkReturn(node)
	case *ast.BinaryExpr:
		v.checkBinary(node)
	case *ast.UnaryExpr:
		v.checkUnary(node)
	case *ast.TypeSwitchStmt:
		v.checkTypeSwitch(node)
	}
	return v
}
