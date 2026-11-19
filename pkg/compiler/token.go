// Package compiler 把 .gx 源码转译为等价的、可直接 go build 的标准 Go 源码。
//
// GX = Go 语法 + 四项语法糖（pub / enum / JSX / 外部符号小写调用）。
// 转译在 token 流上分层进行，每一层都保留原始位置，最终 emit 时产出 sourcemap 原始数据。
package compiler

import "unicode"

// Kind 是 token 类别。Space/Newline 作为 token 保留，使输出无损还原未改写的源码格式。
type Kind int

const (
	Space Kind = iota // 横向空白（空格/Tab）
	Newline           // \n 或 \r\n
	Ident             // 标识符 / 关键字
	Number            // 数字字面量
	String            // 字符串字面量（含引号）
	Rune              // rune 字面量
	Comment           // 行注释或块注释
	Operator          // 运算符 / 标点
	JSX               // 一个完整 JSX 元素的原文
	EOF
)

func (k Kind) String() string {
	switch k {
	case Space:
		return "space"
	case Newline:
		return "newline"
	case Ident:
		return "ident"
	case Number:
		return "number"
	case String:
		return "string"
	case Rune:
		return "rune"
	case Comment:
		return "comment"
	case Operator:
		return "operator"
	case JSX:
		return "jsx"
	case EOF:
		return "eof"
	}
	return "?"
}

// Pos 是 1 基的源码位置，Offset 是 0 基字节偏移。
type Pos struct {
	Offset int
	Line   int
	Col    int
}

var NoPos = Pos{}

func (p Pos) valid() bool { return p.Line > 0 }

// Token 是词法单元。Skip 供各转换层标记「删除」；Origin 供生成 token 标注来源。
type Token struct {
	Kind  Kind
	Text  string
	Pos   Pos
	Skip  bool
	IsGen bool // 由转换层生成（sourcemap 仍按 Pos 映射）
}

func (t Token) is(text string) bool { return t.Kind == Ident && t.Text == text }

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// ---------- token 构造 helper（供 jsx/enum 生成使用）----------

func tokIdent(s string, p Pos) Token   { return Token{Kind: Ident, Text: s, Pos: p, IsGen: true} }
func tokString(s string, p Pos) Token  { return Token{Kind: String, Text: s, Pos: p, IsGen: true} }
func tokOp(s string, p Pos) Token      { return Token{Kind: Operator, Text: s, Pos: p, IsGen: true} }
func tokNewline(p Pos) Token           { return Token{Kind: Newline, Text: "\n", Pos: p, IsGen: true} }
func tokSpace(s string, p Pos) Token   { return Token{Kind: Space, Text: s, Pos: p, IsGen: true} }
