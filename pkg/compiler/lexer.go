package compiler

import "strings"

// Lexer 把 GX 源码切成 token 流。空白/换行/注释保留为 token，
// 使未被转换层改写的部分能原样出现在输出中（可读 + 精确 sourcemap）。
type Lexer struct {
	src  string
	i    int
	line int
	col  int

	// prev 记录上一个「有意义」token 的文本，用于区分 JSX 的 '<' 与小于号。
	prev string
}

func New(src string) *Lexer {
	return &Lexer{src: src, line: 1, col: 1}
}

func (l *Lexer) eof() bool { return l.i >= len(l.src) }

func (l *Lexer) peek() byte {
	if l.eof() {
		return 0
	}
	return l.src[l.i]
}

func (l *Lexer) peekAt(n int) byte {
	j := l.i + n
	if j >= len(l.src) {
		return 0
	}
	return l.src[j]
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || c >= '0' && c <= '9'
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\v' || c == '\f' }

// advance 消费一个字节并维护 1 基行/列。\r\n 视为一个换行。
func (l *Lexer) advance() byte {
	c := l.src[l.i]
	if c == '\r' {
		l.i++
		if l.i < len(l.src) && l.src[l.i] == '\n' {
			l.i++
		}
		l.line++
		l.col = 1
		return '\n'
	}
	l.i++
	if c == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return c
}

func (l *Lexer) pos() Pos { return Pos{Offset: l.i, Line: l.line, Col: l.col} }

// prevCanEndExpr 判断上一个 token 是否为一个表达式的结尾（用于 '<' 消歧）。
func (l *Lexer) prevCanEndExpr() bool {
	if l.prev == "" {
		return false
	}
	// 这些前缀后面期待表达式，允许 '<' 作为 JSX 起始
	switch l.prev {
	case "return", "=", "(", ",", ":", "{", ";", "=>", "!":
		return false
	}
	switch l.prev {
	case ")", "]", "}":
		return true
	}
	c := l.prev[0]
	return isIdentStart(c) || isDigit(c) || c == '"' || c == '`' || c == '\''
}

// 多字符运算符，按长度降序匹配。
var multiOps = []string{
	"&^=", "<<=", ">>=", "...",
	":=", "==", "!=", "<=", ">=", "&&", "||", "<-", "++", "--",
	"+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "<<", ">>", "&^",
}

// AllTokens 返回完整 token 流（末尾含 EOF）。
func (l *Lexer) AllTokens() []Token {
	var out []Token
	for !l.eof() {
		out = append(out, l.next())
	}
	out = append(out, Token{Kind: EOF, Text: "", Pos: l.pos()})
	return out
}

func (l *Lexer) next() Token {
	p := l.pos()
	c := l.peek()

	// 空白
	if isHSpace(c) {
		var b strings.Builder
		for !l.eof() && isHSpace(l.peek()) {
			b.WriteByte(l.advance())
		}
		return Token{Kind: Space, Text: b.String(), Pos: p}
	}

	// 换行
	if c == '\n' || c == '\r' {
		l.advance() // \r\n 由 advance 合并为一次
		return Token{Kind: Newline, Text: "\n", Pos: p}
	}

	// 行注释
	if c == '/' && l.peekAt(1) == '/' {
		var b strings.Builder
		for !l.eof() && l.peek() != '\n' && l.peek() != '\r' {
			b.WriteByte(l.advance())
		}
		t := Token{Kind: Comment, Text: b.String(), Pos: p}
		return t
	}

	// 块注释
	if c == '/' && l.peekAt(1) == '*' {
		var b strings.Builder
		b.WriteByte(l.advance())
		b.WriteByte(l.advance())
		for !l.eof() {
			if l.peek() == '*' && l.peekAt(1) == '/' {
				b.WriteByte(l.advance())
				b.WriteByte(l.advance())
				break
			}
			b.WriteByte(l.advance())
		}
		return Token{Kind: Comment, Text: b.String(), Pos: p}
	}

	// 字符串 / rune / 原始字符串
	if c == '"' || c == '\'' {
		q := c
		var b strings.Builder
		b.WriteByte(l.advance())
		for !l.eof() {
			ch := l.peek()
			if ch == '\\' {
				b.WriteByte(l.advance())
				if !l.eof() {
					b.WriteByte(l.advance())
				}
				continue
			}
			b.WriteByte(l.advance())
			if ch == q {
				break
			}
		}
		kind := String
		if q == '\'' {
			kind = Rune
		}
		return Token{Kind: kind, Text: b.String(), Pos: p}
	}
	if c == '`' {
		var b strings.Builder
		b.WriteByte(l.advance())
		for !l.eof() {
			b.WriteByte(l.advance())
			if l.src[l.i-1] == '`' {
				break
			}
		}
		kind := String
		if strings.Contains(b.String(), "${") {
			kind = Template
		}
		t := Token{Kind: kind, Text: b.String(), Pos: p}
		l.prev = "`" // 模板/raw string 可作为表达式结尾，供后续 '<' 消歧
		return t
	}

	// JSX：'<' 紧跟字母，且上下文不像比较表达式
	if c == '<' && isIdentStart(l.peekAt(1)) && !l.prevCanEndExpr() {
		raw, _ := l.scanJSX()
		l.prev = ";" // 让后续 '<' 仍可判为 JSX 起始
		return Token{Kind: JSX, Text: raw, Pos: p}
	}

	// 标识符 / 关键字
	if isIdentStart(c) {
		var b strings.Builder
		for !l.eof() && isIdentPart(l.peek()) {
			b.WriteByte(l.advance())
		}
		t := Token{Kind: Ident, Text: b.String(), Pos: p}
		l.prev = t.Text
		return t
	}

	// 数字
	if isDigit(c) || c == '.' && isDigit(l.peekAt(1)) {
		var b strings.Builder
		for !l.eof() {
			ch := l.peek()
			if isIdentPart(ch) || ch == '.' {
				b.WriteByte(l.advance())
				continue
			}
			if (ch == '+' || ch == '-') && b.Len() > 0 {
				last := b.String()[b.Len()-1]
				if last == 'e' || last == 'E' || last == 'p' || last == 'P' {
					b.WriteByte(l.advance())
					continue
				}
			}
			break
		}
		t := Token{Kind: Number, Text: b.String(), Pos: p}
		l.prev = t.Text
		return t
	}

	// 运算符 / 标点
	for _, op := range multiOps {
		if strings.HasPrefix(l.src[l.i:], op) {
			for range op {
				l.advance()
			}
			t := Token{Kind: Operator, Text: op, Pos: p}
			l.prev = op
			return t
		}
	}
	l.advance()
	t := Token{Kind: Operator, Text: string(c), Pos: p}
	l.prev = t.Text
	return t
}
