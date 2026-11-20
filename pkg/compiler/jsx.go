package compiler

import (
	"strconv"
	"strings"
)

// ---------------- Lexer 侧的 JSX 原文扫描 ----------------

func isJSXNameChar(c byte) bool {
	return isIdentPart(c) || c == '-' || c == '.' || c == ':'
}

// scanJSX 从当前 '<' 起扫描一个完整 JSX 元素，返回原文与结束位置。
func (l *Lexer) scanJSX() (string, Pos) {
	start := l.i
	l.skipElement()
	return l.src[start:l.i], l.pos()
}

func (l *Lexer) skipWS() {
	for !l.eof() && (isHSpace(l.peek()) || l.peek() == '\n' || l.peek() == '\r') {
		l.advance()
	}
}

func (l *Lexer) skipElement() {
	if l.peek() != '<' {
		return
	}
	l.advance()
	for !l.eof() && isJSXNameChar(l.peek()) {
		l.advance()
	}
	for !l.eof() {
		l.skipWS()
		if l.eof() {
			return
		}
		if l.peek() == '/' && l.peekAt(1) == '>' {
			l.advance()
			l.advance()
			return
		}
		if l.peek() == '>' {
			l.advance()
			l.skipChildren()
			return
		}
		if l.peek() == '<' && isIdentStart(l.peekAt(1)) {
			l.skipElement()
			continue
		}
		if isJSXNameChar(l.peek()) {
			for !l.eof() && isJSXNameChar(l.peek()) {
				l.advance()
			}
			l.skipWS()
			if l.peek() == '=' {
				l.advance()
				l.skipWS()
				l.skipJSXValue()
			}
			continue
		}
		l.advance()
	}
}

func (l *Lexer) skipChildren() {
	for !l.eof() {
		if l.peek() == '<' && l.peekAt(1) == '/' {
			l.advance()
			l.advance()
			for !l.eof() && isJSXNameChar(l.peek()) {
				l.advance()
			}
			l.skipWS()
			if l.peek() == '>' {
				l.advance()
			}
			return
		}
		if l.peek() == '<' && isIdentStart(l.peekAt(1)) {
			l.skipElement()
			continue
		}
		if l.peek() == '<' {
			for !l.eof() && l.peek() != '>' {
				l.advance()
			}
			if !l.eof() {
				l.advance()
			}
			continue
		}
		if l.peek() == '{' {
			l.skipBalanced('{', '}')
			continue
		}
		l.advance()
	}
}

func (l *Lexer) skipJSXValue() {
	if l.eof() {
		return
	}
	switch l.peek() {
	case '"', '\'', '`':
		q := l.peek()
		l.advance()
		for !l.eof() {
			c := l.peek()
			if c == '\\' {
				l.advance()
				if !l.eof() {
					l.advance()
				}
				continue
			}
			l.advance()
			if c == q {
				return
			}
		}
	case '{':
		l.skipBalanced('{', '}')
	default:
		for !l.eof() && isJSXNameChar(l.peek()) {
			l.advance()
		}
	}
}

func (l *Lexer) skipBalanced(open, close byte) {
	if l.peek() != open {
		return
	}
	depth := 0
	for !l.eof() {
		c := l.peek()
		if c == '"' || c == '\'' || c == '`' {
			l.skipJSXValue()
			continue
		}
		if c == '/' && l.peekAt(1) == '/' {
			for !l.eof() && l.peek() != '\n' && l.peek() != '\r' {
				l.advance()
			}
			continue
		}
		if c == '/' && l.peekAt(1) == '*' {
			l.advance()
			l.advance()
			for !l.eof() {
				if l.peek() == '*' && l.peekAt(1) == '/' {
					l.advance()
					l.advance()
					break
				}
				l.advance()
			}
			continue
		}
		if c == '<' && isIdentStart(l.peekAt(1)) {
			l.skipElement()
			continue
		}
		if c == open {
			depth++
		}
		l.advance()
		if c == close {
			depth--
			if depth == 0 {
				return
			}
		}
	}
}

// ---------------- JSX 解析树 ----------------

type jsxAttrKind int

const (
	attrBool jsxAttrKind = iota
	attrString
	attrExpr
)

type jsxAttr struct {
	name  string
	value string
	kind  jsxAttrKind
	pos   Pos
}

type jsxChildKind int

const (
	childText jsxChildKind = iota
	childExpr
	childElement
)

type jsxChild struct {
	kind jsxChildKind
	text string
	node *jsxNode
	pos  Pos
}

type jsxNode struct {
	name      string
	attrs     []jsxAttr
	children  []jsxChild
	component bool
	pos       Pos
}

type jsxParser struct {
	src  string
	i    int
	line int
	col  int
}

func newJSXParser(raw string, base Pos) *jsxParser {
	return &jsxParser{src: raw, line: base.Line, col: base.Col}
}

func (p *jsxParser) eof() bool { return p.i >= len(p.src) }
func (p *jsxParser) cur() byte {
	if p.eof() {
		return 0
	}
	return p.src[p.i]
}
func (p *jsxParser) next() byte {
	if p.i+1 >= len(p.src) {
		return 0
	}
	return p.src[p.i+1]
}
func (p *jsxParser) pos() Pos { return Pos{Offset: p.i, Line: p.line, Col: p.col} }

func (p *jsxParser) advance() byte {
	if p.eof() {
		return 0
	}
	c := p.src[p.i]
	p.i++
	if c == '\r' {
		if p.i < len(p.src) && p.src[p.i] == '\n' {
			p.i++
		}
		p.line++
		p.col = 1
		return '\n'
	}
	if c == '\n' {
		p.line++
		p.col = 1
	} else {
		p.col++
	}
	return c
}

func (p *jsxParser) ws() {
	for !p.eof() && (isHSpace(p.cur()) || p.cur() == '\n' || p.cur() == '\r') {
		p.advance()
	}
}

func (p *jsxParser) readName() string {
	start := p.i
	for !p.eof() && isJSXNameChar(p.cur()) {
		p.advance()
	}
	return p.src[start:p.i]
}

func (p *jsxParser) readQuoted() string {
	q := p.cur()
	start := p.i
	p.advance()
	for !p.eof() {
		c := p.cur()
		if c == '\\' {
			p.advance()
			if !p.eof() {
				p.advance()
			}
			continue
		}
		p.advance()
		if c == q {
			return p.src[start:p.i]
		}
	}
	return p.src[start:p.i]
}

// readBraced 读 { ... } 内部源码（不含两端花括号），处理字符串/注释/嵌套花括号/嵌套 JSX。
func (p *jsxParser) readBraced() string {
	start := p.i
	p.advance() // '{'
	depth := 1
	for !p.eof() && depth > 0 {
		c := p.cur()
		if c == '"' || c == '\'' || c == '`' {
			p.readQuoted()
			continue
		}
		if c == '/' && p.next() == '/' {
			for !p.eof() && p.cur() != '\n' && p.cur() != '\r' {
				p.advance()
			}
			continue
		}
		if c == '/' && p.next() == '*' {
			p.advance()
			p.advance()
			for !p.eof() {
				if p.cur() == '*' && p.next() == '/' {
					p.advance()
					p.advance()
					break
				}
				p.advance()
			}
			continue
		}
		if c == '<' && isIdentStart(p.next()) {
			p.parseElement() // 嵌套 JSX
			continue
		}
		p.advance()
		if c == '{' {
			depth++
		}
		if c == '}' {
			depth--
		}
	}
	end := p.i
	if end > start+1 && p.src[end-1] == '}' {
		end--
	}
	return p.src[start+1 : end]
}

func isComponentName(name string) bool {
	if name == "" {
		return false
	}
	r := []rune(name)
	if r[0] >= 'A' && r[0] <= 'Z' {
		return true
	}
	return strings.Contains(name, ".")
}

func (p *jsxParser) parseElement() *jsxNode {
	nodePos := p.pos()
	p.advance() // '<'
	name := p.readName()
	n := &jsxNode{name: name, pos: nodePos, component: isComponentName(name)}
	for !p.eof() {
		p.ws()
		if p.eof() {
			break
		}
		if p.cur() == '/' && p.next() == '>' {
			p.advance()
			p.advance()
			return n
		}
		if p.cur() == '>' {
			p.advance()
			p.parseChildren(n)
			return n
		}
		if p.cur() == '<' && isIdentStart(p.next()) {
			p.parseElement()
			continue
		}
		if isJSXNameChar(p.cur()) {
			apos := p.pos()
			aname := p.readName()
			p.ws()
			if p.cur() == '=' {
				p.advance()
				p.ws()
				switch p.cur() {
				case '"', '\'', '`':
					n.attrs = append(n.attrs, jsxAttr{aname, p.readQuoted(), attrString, apos})
				case '{':
					n.attrs = append(n.attrs, jsxAttr{aname, p.readBraced(), attrExpr, apos})
				default:
					n.attrs = append(n.attrs, jsxAttr{aname, p.readName(), attrExpr, apos})
				}
			} else {
				n.attrs = append(n.attrs, jsxAttr{aname, "", attrBool, apos})
			}
			continue
		}
		p.advance()
	}
	return n
}

func (p *jsxParser) parseChildren(n *jsxNode) {
	for !p.eof() {
		if p.cur() == '<' && p.next() == '/' {
			p.advance()
			p.advance()
			p.readName()
			p.ws()
			if p.cur() == '>' {
				p.advance()
			}
			return
		}
		if p.cur() == '<' && isIdentStart(p.next()) {
			cpos := p.pos()
			child := p.parseElement()
			child.pos = cpos
			n.children = append(n.children, jsxChild{kind: childElement, node: child, pos: cpos})
			continue
		}
		if p.cur() == '<' {
			for !p.eof() && p.cur() != '>' {
				p.advance()
			}
			if !p.eof() {
				p.advance()
			}
			continue
		}
		if p.cur() == '{' {
			cpos := p.pos()
			n.children = append(n.children, jsxChild{kind: childExpr, text: p.readBraced(), pos: cpos})
			continue
		}
		cpos := p.pos()
		var b strings.Builder
		for !p.eof() && p.cur() != '<' && p.cur() != '{' {
			b.WriteByte(p.advance())
		}
		if b.Len() > 0 {
			n.children = append(n.children, jsxChild{kind: childText, text: b.String(), pos: cpos})
		}
	}
}

// ---------------- JSX → createElement token 生成 ----------------

func normalizeStringLit(raw string) string {
	if raw == "" {
		return strconv.Quote("")
	}
	switch raw[0] {
	case '"':
		if v, err := strconv.Unquote(raw); err == nil {
			return strconv.Quote(v)
		}
	case '`':
		if len(raw) >= 2 {
			return strconv.Quote(raw[1 : len(raw)-1])
		}
	case '\'':
		if len(raw) >= 2 {
			return strconv.Quote(raw[1 : len(raw)-1])
		}
	}
	return strconv.Quote(raw)
}

func normalizeText(s string) string {
	multiline := strings.ContainsAny(s, "\n\r")
	if multiline && strings.TrimSpace(s) == "" {
		return ""
	}
	if multiline {
		return strings.TrimSpace(s)
	}
	return s
}

// lexExpr 把 {expr} 内的源码重新词法化；若 expr 内含 JSX 则递归转换。
// 所有 token 的位置统一归一到 base（JSON sourcemap 只需粗粒度到元素起点）。
func lexExpr(expr string, base Pos) []Token {
	toks := New(expr).AllTokens()
	// 对象字面量自动推断（同 scene 后端）：插值里的 { k: v } 编成带类型匿名 struct。
	if converted, err := transformObjectLitExpr(toks); err == nil {
		toks = converted
	}
	// 插值内可能含嵌套 JSX，递归展开即可；signal 读改写由最外层
	// transformJSXCore 对展开结果统一补做（传 nil 避免此处重复改写）。
	toks = transformJSXCore(toks, false, nil)
	var out []Token
	for _, t := range toks {
		if t.Kind == EOF {
			continue
		}
		t.Pos = base
		t.IsGen = true
		out = append(out, t)
	}
	return out
}

// genJSXNode 生成 createElement token。b/at 用于对插值表达式补 signal 读改写：
// at 是整个 JSX token 在原始源码中的位置（全局 Offset），用于 ResolveAt 判遮蔽。
// 改写只作用于 attrExpr/childExpr 插值片段，不碰组件名/标签名/属性名/文本。
func genJSXNode(n *jsxNode, b *Bindings, at Pos) []Token {
	p := n.pos
	out := []Token{
		tokIdent("gxvdom", p), tokOp(".", p), tokIdent("CreateElement", p), tokOp("(", p),
	}

	if n.component {
		parts := strings.Split(n.name, ".")
		for i, part := range parts {
			if i > 0 {
				out = append(out, tokOp(".", p))
			}
			out = append(out, tokIdent(part, p))
		}
	} else {
		out = append(out, tokString(strconv.Quote(n.name), p))
	}
	out = append(out, tokOp(",", p), tokSpace(" ", p))

	if len(n.attrs) == 0 {
		out = append(out, tokIdent("nil", p))
	} else {
		out = append(out,
			tokIdent("map", p), tokOp("[", p), tokIdent("string", p), tokOp("]", p),
			tokIdent("any", p), tokOp("{", p))
		for i, a := range n.attrs {
			if i > 0 {
				out = append(out, tokOp(",", p), tokSpace(" ", p))
			}
			out = append(out, tokString(strconv.Quote(a.name), a.pos), tokOp(":", p), tokSpace(" ", p))
			switch a.kind {
			case attrBool:
				out = append(out, tokIdent("true", a.pos))
			case attrString:
				out = append(out, tokString(normalizeStringLit(a.value), a.pos))
			case attrExpr:
				frag := lexExpr(a.value, a.pos)
				out = append(out, rewriteSignalReadsInTemplateExpr(frag, b, at)...)
			}
		}
		out = append(out, tokOp("}", p))
	}

	for _, c := range n.children {
		add := []Token{tokOp(",", p), tokSpace(" ", p)}
		switch c.kind {
		case childText:
			txt := normalizeText(c.text)
			if txt == "" {
				continue
			}
			add = append(add, tokString(strconv.Quote(txt), c.pos))
		case childExpr:
			frag := lexExpr(c.text, c.pos)
			add = append(add, rewriteSignalReadsInTemplateExpr(frag, b, at)...)
		case childElement:
			add = append(add, genJSXNode(c.node, b, at)...)
		}
		out = append(out, add...)
	}
	if len(n.children) > 0 {
		out = append(out, tokOp(",", p))
	}
	out = append(out, tokOp(")", p))
	return out
}

func hasVdomImport(toks []Token) bool {
	for i, t := range toks {
		if t.is("import") {
			for j := i; j < len(toks) && j < i+60; j++ {
				if toks[j].Kind == String && strings.Contains(toks[j].Text, "gox/runtime/vdom") {
					return true
				}
			}
		}
	}
	return false
}

// injectVdomImport 在 package 子句之后插入 vdom 运行时 import。
func injectVdomImport(toks []Token, p Pos) []Token {
	if hasVdomImport(toks) {
		return toks
	}
	insert := 0
	for i, t := range toks {
		if t.is("package") {
			for j := i + 1; j < len(toks); j++ {
				if toks[j].Kind == Newline {
					insert = j + 1
					break
				}
			}
			break
		}
	}
	imp := []Token{
		tokNewline(p),
		tokIdent("import", p), tokSpace(" ", p), tokIdent("gxvdom", p), tokSpace(" ", p),
		tokString(strconv.Quote("github.com/robertpanvip/gox/runtime/vdom"), p), tokNewline(p),
	}
	out := append([]Token{}, toks[:insert]...)
	out = append(out, imp...)
	out = append(out, toks[insert:]...)
	return out
}

// transformJSXCore 替换 JSX token；injectImport 控制是否注入 vdom import。
// b 非 nil 时，对展开后的插值表达式补 signal 读改写（name → name()）：
// JSX 在 lexer 阶段被整块吞成一个 token，transformSignals 阶段扫不到内部的
// signal 引用，等这里展开成普通 token 时读取写阶段已过。用 ResolveAt(name, at)
// 按名字 + JSX 原始位置补改写，遮蔽/形态排除与主流改写一致。
func transformJSXCore(in []Token, injectImport bool, b *Bindings) []Token {
	found := false
	for _, t := range in {
		if t.Kind == JSX {
			found = true
			break
		}
	}
	if !found {
		return in
	}
	var out []Token
	for _, t := range in {
		if t.Kind == JSX {
			parser := newJSXParser(t.Text, t.Pos)
			node := parser.parseElement()
			// 补 signal 读改写：只对插值片段做，组件名/标签名/属性名/文本不动。
			// t.Pos 是整块 JSX 在原始源码的位置（全局 Offset），用于 ResolveAt 判遮蔽。
			out = append(out, genJSXNode(node, b, t.Pos)...)
			continue
		}
		out = append(out, t)
	}
	if injectImport {
		out = injectVdomImport(out, in[0].Pos)
	}
	return out
}

// transformJSX 是管线入口。import 由 ReconcileImports 统一注入。
func transformJSX(in []Token, b *Bindings) []Token { return transformJSXCore(in, false, b) }
