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
	childCond // 条件分支：if cond { ... } else { ... }
)

// jsxCond 描述一个条件分支节点。它编译成 gxscene.BranchGetter：
//   - branches[i].test 是第 i 个分支的选中条件（Go 表达式源码，最后一个分支 test 为空表示 else）
//   - branches[i].children 是该分支命中时要渲染的 JSX 子节点
type jsxCond struct {
	branches []jsxCondBranch
	pos      Pos
}

type jsxCondBranch struct {
	test     string // 条件表达式源码；为空表示 else 兜底分支
	children []jsxChild
	pos      Pos
}

type jsxChild struct {
	kind jsxChildKind
	text string
	node *jsxNode
	cond *jsxCond
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

// atLineComment 判断当前是否是行注释起始（'//'）。只在行首或前面是空白时成立，
// 避免把文本节点里的 URL（如 http://x）或 `a//b` 误当注释。
func (p *jsxParser) atLineComment() bool {
	if p.cur() != '/' || p.next() != '/' {
		return false
	}
	if p.i == 0 {
		return true
	}
	prev := p.src[p.i-1]
	return isHSpace(prev) || prev == '\n' || prev == '\r'
}

// skipLineComment 吞掉从当前 '//' 到行尾（不含换行）的注释内容。
func (p *jsxParser) skipLineComment() {
	for !p.eof() && p.cur() != '\n' && p.cur() != '\r' {
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
		// 属性行尾注释（class="a" // 类名）：跳过到行尾，避免注释文字被当成
		// 下一个属性名，生成 OpAttr{Key:"类名",Arg:"true"} 假属性（A7）。
		if p.atLineComment() {
			p.skipLineComment()
			continue
		}
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
	afterCond := false // 上一轮刚 append 条件块：其后紧跟的纯空白是排版残留（Bug 04）
	for !p.eof() {
		// 子节点行注释（> // 头注、</span> // 尾注）：跳过到行尾，避免被当成
		// 可见文本节点渲染出来（A7）。
		if p.atLineComment() {
			p.skipLineComment()
			continue
		}
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
			afterCond = false
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
			afterCond = false
			continue
		}
		// 条件分支：if <expr> { <children> } [else if <expr> { ... }]* [else { ... }]
		if p.atIfKeyword() {
			cpos := p.pos()
			cond := p.parseCond()
			if cond != nil {
				cond.pos = cpos
				n.children = append(n.children, jsxChild{kind: childCond, cond: cond, pos: cpos})
				afterCond = true
				continue
			}
		}
		cpos := p.pos()
		var b strings.Builder
		boundaryBreak := false // 是否因 if / 行注释边界提前 break（这类纯空白是排版残留）
		for !p.eof() && p.cur() != '<' && p.cur() != '{' {
			// 文本里的行注释（x // 尾注）：停下交给主循环跳过，别写进文本节点（A7）。
			if p.atLineComment() {
				boundaryBreak = true
				break
			}
			// 若当前积累的文本全是空白，且遇到 if 关键字，则停下交给下一轮解析条件块。
			if strings.TrimSpace(b.String()) == "" && p.atIfKeyword() {
				boundaryBreak = true
				break
			}
			b.WriteByte(p.advance())
		}
		// Bug 04：if / 注释边界 break 留下的纯空白（如 "if 前"、" }) " 后）以及
		// 紧跟条件块之后的纯空白是排版残留，不应产出独立的 " " 文本节点；但因
		// < / { / } / EOF 结束的 inline 空白（如 <b>A</b> <i>B</i> 的分隔空格）、
		// 或条件块后含内容的文本（如 " tail"）是有意的，必须保留。
		pureWS := strings.TrimSpace(b.String()) == ""
		dropWS := pureWS && (boundaryBreak || afterCond) // 仅边界/条件块后的纯空白丢弃
		if b.Len() > 0 && !dropWS {
			n.children = append(n.children, jsxChild{kind: childText, text: b.String(), pos: cpos})
		}
		afterCond = false
	}
}

// atIfKeyword 判断当前位置是否是条件分支的 `if ` 起始：
// 字面量 "if" 后必须跟空白（如 "if visible {"），避免误判标识符 "iffy"。
func (p *jsxParser) atIfKeyword() bool {
	if p.i+2 > len(p.src) || p.src[p.i:p.i+2] != "if" {
		return false
	}
	return p.i+2 < len(p.src) && isHSpace(p.src[p.i+2])
}

// atElseKeyword 类似 atIfKeyword，识别 `else ` 后跟 `{` 或 `if`。
func (p *jsxParser) atElseKeyword() bool {
	if p.i+4 > len(p.src) || p.src[p.i:p.i+4] != "else" {
		return false
	}
	if p.i+4 >= len(p.src) || !isHSpace(p.src[p.i+4]) {
		return false
	}
	j := p.i + 4
	for j < len(p.src) && isHSpace(p.src[j]) {
		j++
	}
	if j >= len(p.src) {
		return false
	}
	return p.src[j] == '{' || (j+2 <= len(p.src) && p.src[j:j+2] == "if")
}

// parseCond 解析 if/else if/else 条件块，返回 jsxCond。
// 调用时 p.cur() 指向 "if"。
func (p *jsxParser) parseCond() *jsxCond {
	c := &jsxCond{}
	for {
		if !p.atIfKeyword() {
			return nil
		}
		bpos := p.pos()
		p.advance() // 'i'
		p.advance() // 'f'
		p.ws()
		// 条件表达式：读到顶层 '{' 为止（括号内的 '{' 忽略，简化处理——
		// GX 条件多为比较/信号读，不含复合字面量）。
		exprStart := p.i
		depth := 0
		for !p.eof() {
			ch := p.cur()
			if ch == '(' || ch == '[' {
				depth++
			} else if ch == ')' || ch == ']' {
				depth--
			} else if ch == '{' && depth == 0 {
				break
			}
			p.advance()
		}
		test := strings.TrimSpace(p.src[exprStart:p.i])
		// 解析 then 块：{ ...children... }
		var kids []jsxChild
		if p.cur() == '{' {
			kids = p.parseCondBlock()
		}
		c.branches = append(c.branches, jsxCondBranch{test: test, children: kids, pos: bpos})

		// 跳过块之间的空白，检测 else。
		save := p.i
		p.ws()
		if p.atElseKeyword() {
			p.advance() // 'e'
			p.advance() // 'l'
			p.advance() // 's'
			p.advance() // 'e'
			p.ws()
			if p.atIfKeyword() {
				continue // else if：下一轮解析
			}
			if p.cur() == '{' {
				elseKids := p.parseCondBlock()
				c.branches = append(c.branches, jsxCondBranch{children: elseKids, pos: p.pos()})
			}
			break
		}
		p.i = save // 无 else：回退空白，交给外层继续
		break
	}
	return c
}

// parseCondBlock 解析一个 { ... } 块内的 JSX children，遇到匹配的 '}' 结束。
// 块内复用与 parseChildren 相同的子节点规则（元素 / 插值 / 文本 / 嵌套条件）。
func (p *jsxParser) parseCondBlock() []jsxChild {
	p.advance() // '{'
	var kids []jsxChild
	for !p.eof() {
		if p.cur() == '}' {
			p.advance()
			return kids
		}
		if p.cur() == '<' && p.next() == '/' {
			// 块内不应出现闭合标签，消费掉避免死循环。
			for !p.eof() && p.cur() != '}' {
				p.advance()
			}
			continue
		}
		if p.cur() == '<' && isIdentStart(p.next()) {
			cpos := p.pos()
			child := p.parseElement()
			child.pos = cpos
			kids = append(kids, jsxChild{kind: childElement, node: child, pos: cpos})
			continue
		}
		if p.cur() == '{' {
			cpos := p.pos()
			kids = append(kids, jsxChild{kind: childExpr, text: p.readBraced(), pos: cpos})
			continue
		}
		if p.atIfKeyword() {
			cpos := p.pos()
			cond := p.parseCond()
			if cond != nil {
				kids = append(kids, jsxChild{kind: childCond, cond: cond, pos: cpos})
				continue
			}
		}
		cpos := p.pos()
		var b strings.Builder
		for !p.eof() && p.cur() != '<' && p.cur() != '{' && p.cur() != '}' {
			if strings.TrimSpace(b.String()) == "" && p.atIfKeyword() {
				break
			}
			b.WriteByte(p.advance())
		}
		// Bug 04：条件块内的纯空白文本（块首尾缩进、相邻元素间换行排版）是残留，
		// 不产出独立 " " 节点；含实际内容的文本照常保留。
		if b.Len() > 0 && strings.TrimSpace(b.String()) != "" {
			kids = append(kids, jsxChild{kind: childText, text: b.String(), pos: cpos})
		}
	}
	return kids
}

// ---------------- JSX 文本 / 字面量规范化（scene 与解析共用） ----------------

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
