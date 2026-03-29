package lexer

import (
	"unicode"
	"unicode/utf8"

	"github.com/gox-lang/gox/token"
)

type Lexer struct {
	src    []byte
	pos    int
	line   int
	col    int
	start  int
	width  int
	tokens []token.Token
}

func New(src string) *Lexer {
	return &Lexer{
		src:    []byte(src),
		pos:    0,
		line:   1,
		col:    1,
		start:  0,
		tokens: make([]token.Token, 0),
	}
}

func (l *Lexer) Tokens() []token.Token {
	for {
		tok := l.NextToken()
		l.tokens = append(l.tokens, tok)
		if tok.Kind == token.EOF {
			break
		}
	}
	return l.tokens
}

func (l *Lexer) NextToken() token.Token {
	l.skipWhitespace()
	l.start = l.pos
	l.width = 0

	if l.pos >= len(l.src) {
		return token.Token{Kind: token.EOF, Pos: l.pos, Line: l.line, Col: l.col}
	}

	c := l.next()

	if isLetter(c) {
		lit := l.readIdentifier()
		kind := token.LookupKeyword(lit)
		return token.Token{Kind: kind, Literal: lit, Pos: l.start, Line: l.line, Col: l.col}
	}
	if isDigit(c) {
		return l.readNumber()
	}
	if c == '"' {
		return l.readString()
	}
	if c == '`' {
		return l.readRawString()
	}

	switch c {
	case '=':
		if l.peekByte() == '=' {
			l.next()
			return token.Token{Kind: token.EQUAL, Literal: "==", Pos: l.start, Line: l.line, Col: l.col}
		}
		if l.peekByte() == '>' {
			l.next()
			return token.Token{Kind: token.ARROW, Literal: "=>", Pos: l.start, Line: l.line, Col: l.col}
		}
		return token.Token{Kind: token.ASSIGN, Literal: "=", Pos: l.start, Line: l.line, Col: l.col}
	case '+':
		if l.peekByte() == '+' {
			l.next()
			return token.Token{Kind: token.INC, Literal: "++", Pos: l.start, Line: l.line, Col: l.col}
		}
		return token.Token{Kind: token.PLUS, Literal: "+", Pos: l.start, Line: l.line, Col: l.col}
	case '-':
		if l.peekByte() == '-' {
			l.next()
			return token.Token{Kind: token.DEC, Literal: "--", Pos: l.start, Line: l.line, Col: l.col}
		}
		if l.peekByte() == '>' {
			l.next()
			return token.Token{Kind: token.ARROW, Literal: "->", Pos: l.start, Line: l.line, Col: l.col}
		}
		return token.Token{Kind: token.MINUS, Literal: "-", Pos: l.start, Line: l.line, Col: l.col}
	case '*':
		return token.Token{Kind: token.STAR, Literal: "*", Pos: l.start, Line: l.line, Col: l.col}
	case '/':
		if l.peekByte() == '/' {
			l.skipLineComment()
			return l.NextToken()
		}
		return token.Token{Kind: token.SLASH, Literal: "/", Pos: l.start, Line: l.line, Col: l.col}
	case '%':
		return token.Token{Kind: token.PERCENT, Literal: "%", Pos: l.start, Line: l.line, Col: l.col}
	case '&':
		if l.peekByte() == '&' {
			l.next()
			return token.Token{Kind: token.LOGICAL_AND, Literal: "&&", Pos: l.start, Line: l.line, Col: l.col}
		}
		return token.Token{Kind: token.AMP, Literal: "&", Pos: l.start, Line: l.line, Col: l.col}
	case '|':
		if l.peekByte() == '|' {
			l.next()
			return token.Token{Kind: token.LOGICAL_OR, Literal: "||", Pos: l.start, Line: l.line, Col: l.col}
		}
		return token.Token{Kind: token.PIPE, Literal: "|", Pos: l.start, Line: l.line, Col: l.col}
	case '^':
		return token.Token{Kind: token.CARET, Literal: "^", Pos: l.start, Line: l.line, Col: l.col}
	case '~':
		return token.Token{Kind: token.TILDE, Literal: "~", Pos: l.start, Line: l.line, Col: l.col}
	case '!':
		if l.peekByte() == '=' {
			l.next()
			return token.Token{Kind: token.NOT_EQUAL, Literal: "!=", Pos: l.start, Line: l.line, Col: l.col}
		}
		return token.Token{Kind: token.BANG, Literal: "!", Pos: l.start, Line: l.line, Col: l.col}
	case '<':
		if l.peekByte() == '=' {
			l.next()
			return token.Token{Kind: token.LESS_EQUAL, Literal: "<=", Pos: l.start, Line: l.line, Col: l.col}
		}
		return token.Token{Kind: token.LESS, Literal: "<", Pos: l.start, Line: l.line, Col: l.col}
	case '>':
		if l.peekByte() == '=' {
			l.next()
			return token.Token{Kind: token.GREATER_EQUAL, Literal: ">=", Pos: l.start, Line: l.line, Col: l.col}
		}
		return token.Token{Kind: token.GREATER, Literal: ">", Pos: l.start, Line: l.line, Col: l.col}
	case '?':
		if l.peekByte() == '?' {
			l.next()
			return token.Token{Kind: token.NULL_COALESCE, Literal: "??", Pos: l.start, Line: l.line, Col: l.col}
		}
		if l.peekByte() == '.' {
			l.next()
			return token.Token{Kind: token.SAFE_DOT, Literal: "?.", Pos: l.start, Line: l.line, Col: l.col}
		}
		return token.Token{Kind: token.QUESTION, Literal: "?", Pos: l.start, Line: l.line, Col: l.col}
	case '(':
		return token.Token{Kind: token.LPAREN, Literal: "(", Pos: l.start, Line: l.line, Col: l.col}
	case ')':
		return token.Token{Kind: token.RPAREN, Literal: ")", Pos: l.start, Line: l.line, Col: l.col}
	case '{':
		return token.Token{Kind: token.LBRACE, Literal: "{", Pos: l.start, Line: l.line, Col: l.col}
	case '}':
		return token.Token{Kind: token.RBRACE, Literal: "}", Pos: l.start, Line: l.line, Col: l.col}
	case '[':
		return token.Token{Kind: token.LBRACK, Literal: "[", Pos: l.start, Line: l.line, Col: l.col}
	case ']':
		return token.Token{Kind: token.RBRACK, Literal: "]", Pos: l.start, Line: l.line, Col: l.col}
	case ';':
		return token.Token{Kind: token.SEMICOLON, Literal: ";", Pos: l.start, Line: l.line, Col: l.col}
	case ':':
		return token.Token{Kind: token.COLON, Literal: ":", Pos: l.start, Line: l.line, Col: l.col}
	case ',':
		return token.Token{Kind: token.COMMA, Literal: ",", Pos: l.start, Line: l.line, Col: l.col}
	case '.':
		return token.Token{Kind: token.DOT, Literal: ".", Pos: l.start, Line: l.line, Col: l.col}
	case '\n':
		l.line++
		l.col = 1
		return token.Token{Kind: token.NEWLINE, Literal: "\\n", Pos: l.start, Line: l.line - 1, Col: l.col}
	}

	return token.Token{Kind: token.EOF, Literal: "", Pos: l.pos, Line: l.line, Col: l.col}
}

func (l *Lexer) next() rune {
	if l.pos >= len(l.src) {
		l.width = 0
		return 0
	}
	r, w := utf8.DecodeRune(l.src[l.pos:])
	l.width = w
	l.pos += w
	l.col++
	return r
}

func (l *Lexer) peekByte() byte {
	if l.pos >= len(l.src) {
		return 0
	}
	return l.src[l.pos]
}

func (l *Lexer) readIdentifier() string {
	for {
		c := l.peekByte()
		if isLetter(rune(c)) || isDigit(rune(c)) {
			l.next()
		} else {
			break
		}
	}
	return string(l.src[l.start:l.pos])
}

func (l *Lexer) readNumber() token.Token {
	hasDecimal := false

	for {
		c := l.peekByte()
		if isDigit(rune(c)) {
			l.next()
		} else if c == '.' && !hasDecimal {
			hasDecimal = true
			l.next()
		} else {
			break
		}
	}

	lit := string(l.src[l.start:l.pos])
	if hasDecimal {
		return token.Token{Kind: token.FLOAT, Literal: lit, Pos: l.start, Line: l.line, Col: l.col}
	}
	return token.Token{Kind: token.INT, Literal: lit, Pos: l.start, Line: l.line, Col: l.col}
}

func (l *Lexer) readString() token.Token {
	l.next() // consume opening "
	hasTemplate := false
	for l.pos < len(l.src) {
		c := l.peekByte()
		if c == '"' {
			break
		}
		if c == '\\' {
			l.next()
			l.next()
		} else {
			// Check for ${ pattern
			if c == '$' && l.pos+1 < len(l.src) {
				nextChar := l.src[l.pos+1]
				if nextChar == '{' {
					hasTemplate = true
				}
			}
			l.next()
		}
	}
	l.next() // consume closing "
	lit := string(l.src[l.start:l.pos])
	
	if hasTemplate {
		return token.Token{Kind: token.TEMPLATE, Literal: lit, Pos: l.start, Line: l.line, Col: l.col}
	}
	return token.Token{Kind: token.STRING, Literal: lit, Pos: l.start, Line: l.line, Col: l.col}
}

func (l *Lexer) readRawString() token.Token {
	l.next()
	hasTemplate := false
	for l.peekByte() != '`' && l.pos < len(l.src) {
		if l.peekByte() == '\n' {
			l.line++
			l.col = 1
		}
		// Check for ${ pattern
		if l.peekByte() == '$' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '{' {
			hasTemplate = true
		}
		l.next()
	}
	l.next()
	lit := string(l.src[l.start:l.pos])
	
	if hasTemplate {
		return token.Token{Kind: token.TEMPLATE, Literal: lit, Pos: l.start, Line: l.line, Col: l.col}
	}
	return token.Token{Kind: token.STRING, Literal: lit, Pos: l.start, Line: l.line, Col: l.col}
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.src) {
		c := l.peekByte()
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			l.next()
		default:
			return
		}
	}
}

func (l *Lexer) skipLineComment() {
	l.next()
	l.next()
	for l.peekByte() != '\n' && l.pos < len(l.src) {
		l.next()
	}
}

func isLetter(c rune) bool {
	return unicode.IsLetter(c) || c == '_'
}

func isDigit(c rune) bool {
	return c >= '0' && c <= '9'
}
