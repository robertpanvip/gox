package typecheck

// 语义 token / 补全项惰性文档 / 参数提示——GX 文档文本的语法级三件套核心层。
// Tokens 直接对文档文本（.gx 或生成 Go）扫描：词法层走 go/scanner（GX 语法在
// token 级是 Go 的超集），类别判定用语境规则（package/import/type/var/const/func
// 后的标识符 + 后随 '(' 视为函数）。DocOf 复用 Lookup 虚拟 FS，带 ParseComments
// 解析声明；ParamHints 走持久类型图，只对"字面量实参且形参具名"给提示。

import (
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"go/types"
	"sort"
	"strings"
)

// STok 是一个语法 token：Line/Col 1 基，Len 字节长度，Type 取 Legend() 中的名字。
type STok struct {
	Line int
	Col  int
	Len  int
	Type string
}

// Legend 固定 token 类型表；EncodeTokens 的下标语义与之对齐。
func Legend() []string {
	return []string{
		"namespace", "type", "function", "method", "variable", "parameter",
		"constant", "string", "number", "keyword", "comment", "operator",
	}
}

// Tokens 对 src 做语法级分类扫描；无有效 token 返回 false。
// 分类规则（语法级，不做类型推断）：package 名→namespace；import 块内字符串/路径→string；
// type 名→type；var 名→variable；const 名→constant；func 名与后随 "(" 的标识符→function；
// 关键字/注释/字面量/运算符按 token 词类；其余标识符→variable。
func Tokens(src string) ([]STok, bool) {
	var sc scanner.Scanner
	fset := token.NewFileSet()
	f := fset.AddFile("tokens.go", fset.Base(), len(src))
	sc.Init(f, []byte(src), func(token.Position, string) {}, scanner.ScanComments)
	type rawTok struct {
		line, col, len_ int
		tok             token.Token
	}
	var raws []rawTok
	for {
		pos, tk, lit := sc.Scan()
		if tk == token.EOF {
			break
		}
		n := len(lit)
		if n == 0 {
			continue // 合成词（换行分号等）不参与着色
		}
		p := fset.Position(pos)
		raws = append(raws, rawTok{p.Line, p.Column, n, tk})
	}
	if len(raws) == 0 {
		return nil, false
	}
	out := make([]STok, 0, len(raws))
	prevKw := token.Token(0)
	inImport := false
	for i, r := range raws {
		typ := ""
		switch {
		case r.tok == token.COMMENT:
			typ = "comment"
		case r.tok == token.STRING:
			typ = "string"
		case r.tok == token.CHAR || r.tok == token.INT || r.tok == token.FLOAT:
			typ = "number"
		case r.tok.IsKeyword():
			typ = "keyword"
			if r.tok == token.IMPORT {
				inImport = true
			} else {
				inImport = false // 任何其它关键字都终结 import 语境（块形态的括号不是关键字）
			}
			prevKw = r.tok
		case r.tok == token.IDENT:
			switch {
			case inImport:
				typ = "string"
			case prevKw == token.PACKAGE:
				typ = "namespace"
			case prevKw == token.TYPE:
				typ = "type"
			case prevKw == token.CONST:
				typ = "constant"
			case prevKw == token.VAR:
				typ = "variable"
			case i+1 < len(raws) && raws[i+1].tok == token.LPAREN:
				typ = "function"
			case prevKw == token.FUNC:
				typ = "function"
			default:
				typ = "variable"
			}
			prevKw = 0
		default:
			typ = "operator"
			prevKw = 0
		}
		out = append(out, STok{Line: r.line, Col: r.col, Len: r.len_, Type: typ})
	}
	return out, true
}

// EncodeTokens 转 LSP 编码 [deltaLine, deltaStart, length, typeIdx, modifiers]。
func EncodeTokens(toks []STok) []int {
	idx := map[string]int{}
	for i, n := range Legend() {
		idx[n] = i
	}
	out := make([]int, 0, len(toks)*5)
	pline, pcol := 0, 0
	for _, t := range toks {
		ti, ok := idx[t.Type]
		if !ok {
			ti = idx["operator"]
		}
		ds := t.Col - pcol
		if t.Line != pline {
			ds = t.Col - 1
		}
		out = append(out, t.Line-pline, ds, t.Len, ti, 0)
		pline, pcol = t.Line, t.Col
	}
	return out
}

// DocOf 取包内符号声明的 doc 注释首段；file/line 为包内源坐标（1 基）。
func DocOf(pkg, name string, lookup Lookup) (doc, file string, line int, ok bool) {
	files, found := lookup(pkg)
	if !found {
		return "", "", 0, false
	}
	for _, fd := range files {
		fs := token.NewFileSet()
		f, err := parser.ParseFile(fs, fd.Name, fd.Src, parser.ParseComments)
		if err != nil {
			continue
		}
		var d *ast.CommentGroup
		var nd ast.Node
		for _, decl := range f.Decls {
			switch x := decl.(type) {
			case *ast.FuncDecl:
				if x.Name != nil && x.Name.Name == name {
					d, nd = x.Doc, x
				}
			case *ast.GenDecl:
				for _, sp := range x.Specs {
					switch v := sp.(type) {
					case *ast.ValueSpec:
						for _, n := range v.Names {
							if n.Name == name {
								if v.Doc != nil {
									d, nd = v.Doc, v
								} else {
									d, nd = x.Doc, x
								}
							}
						}
					case *ast.TypeSpec:
						if v.Name.Name == name {
							if v.Doc != nil {
								d, nd = v.Doc, v
							} else {
								d, nd = x.Doc, x
							}
						}
					}
				}
			}
			if nd != nil {
				break
			}
		}
		if nd == nil {
			continue
		}
		p := fs.Position(nd.Pos())
		doc := ""
		if d != nil {
			doc = firstPara(d.Text())
		}
		return doc, fd.Name, p.Line, true
	}
	return "", "", 0, false
}

func firstPara(text string) string {
	ls := strings.Split(strings.TrimSpace(text), "\n")
	out := []string{}
	for _, l := range ls {
		t := strings.TrimSpace(l)
		if t == "" && len(out) > 0 {
			break
		}
		if strings.HasPrefix(t, "@") {
			break
		}
		out = append(out, t)
	}
	return strings.Join(out, "\n")
}

// Hint 是参数提示：Line/Col 为检查源（生成 Go）1 基坐标。
type Hint struct {
	Line  int
	Col   int
	Label string
}

// ParamHints 扫描调用实参：形参具名 + 实参是基本字面量 + 字面量不是参数默认值形态时提示。
// 可变参数的额外实参不提示。依赖语法完整，源码坏时返回 false。
func (in *Importer) ParamHints(src string) ([]Hint, bool) {
	_, info, file, ok := in.checkWithInfo(src)
	if !ok || info == nil || file == nil {
		return nil, false
	}
	var out []Hint
	ast.Inspect(file, func(n ast.Node) bool {
		c, is := n.(*ast.CallExpr)
		if !is || len(c.Args) == 0 {
			return true
		}
		sg := sigOfFunc(info.TypeOf(c.Fun))
		if sg == nil {
			return true
		}
		pl := sg.Params()
		nFixed := pl.Len()
		if sg.Variadic() {
			nFixed--
		}
		for i := 0; i < nFixed && i < len(c.Args); i++ {
			p := pl.At(i)
			if p.Name() == "" {
				continue
			}
			bl, isLit := c.Args[i].(*ast.BasicLit)
			if !isLit {
				continue
			}
			pp := in.fset.Position(bl.Pos())
			out = append(out, Hint{Line: pp.Line, Col: pp.Column, Label: p.Name() + ":"})
		}
		return true
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Col < out[j].Col
	})
	if len(out) > 200 {
		out = out[:200]
	}
	return out, true
}

func sigOfFunc(t types.Type) *types.Signature {
	for {
		switch x := t.(type) {
		case *types.Signature:
			return x
		case *types.Pointer:
			t = x.Elem()
		case *types.Named:
			t = x.Underlying()
		default:
			return nil
		}
	}
}
