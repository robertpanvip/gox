package parser

import "github.com/gox-lang/gox/token"

func (p *Parser) nextToken() {
	p.skipWhitespace()

	p.curTok = p.peekTok

	tok := token.Token{}
	tok.Pos = p.pos
	tok.Line = p.line
	tok.Col = p.col

	if p.pos >= len(p.src) {
		tok.Kind = token.EOF
		tok.Literal = ""
		p.peekTok = tok
		return
	}

	c := p.nextByte()

	if isLetter(c) {
		lit := p.readIdentifier()
		kind := token.LookupKeyword(lit)
		tok.Kind = kind
		tok.Literal = lit
		p.peekTok = tok
		return
	}

	if isDigit(c) {
		tok = p.readNumber()
		p.peekTok = tok
		return
	}

	if c == '"' {
		tok = p.readString()
		p.peekTok = tok
		return
	}

	if c == '`' {
		tok = p.readRawString()
		p.peekTok = tok
		return
	}

	switch c {
	case '=':
		if p.peekByte() == '=' {
			p.nextByte()
			tok.Kind = token.EQUAL
			tok.Literal = "=="
		} else if p.peekByte() == '>' {
			p.nextByte()
			tok.Kind = token.ARROW
			tok.Literal = "=>"
		} else {
			tok.Kind = token.ASSIGN
			tok.Literal = "="
		}
	case '+':
		tok.Kind = token.PLUS
		tok.Literal = "+"
	case '-':
		if p.peekByte() == '>' {
			p.nextByte()
			tok.Kind = token.ARROW
			tok.Literal = "->"
		} else {
			tok.Kind = token.MINUS
			tok.Literal = "-"
		}
	case '*':
		tok.Kind = token.STAR
		tok.Literal = "*"
	case '/':
		tok.Kind = token.SLASH
		tok.Literal = "/"
	case '%':
		tok.Kind = token.PERCENT
		tok.Literal = "%"
	case '&':
		if p.peekByte() == '&' {
			p.nextByte()
			tok.Kind = token.LOGICAL_AND
			tok.Literal = "&&"
		} else {
			tok.Kind = token.AMP
			tok.Literal = "&"
		}
	case '|':
		if p.peekByte() == '|' {
			p.nextByte()
			tok.Kind = token.LOGICAL_OR
			tok.Literal = "||"
		} else {
			tok.Kind = token.PIPE
			tok.Literal = "|"
		}
	case '^':
		tok.Kind = token.CARET
		tok.Literal = "^"
	case '~':
		tok.Kind = token.TILDE
		tok.Literal = "~"
	case '!':
		if p.peekByte() == '=' {
			p.nextByte()
			tok.Kind = token.NOT_EQUAL
			tok.Literal = "!="
		} else {
			tok.Kind = token.BANG
			tok.Literal = "!"
		}
	case '<':
		if p.peekByte() == '=' {
			p.nextByte()
			tok.Kind = token.LESS_EQUAL
			tok.Literal = "<="
		} else {
			tok.Kind = token.LESS
			tok.Literal = "<"
		}
	case '>':
		if p.peekByte() == '=' {
			p.nextByte()
			tok.Kind = token.GREATER_EQUAL
			tok.Literal = ">="
		} else {
			tok.Kind = token.GREATER
			tok.Literal = ">"
		}
	case '?':
		if p.peekByte() == '?' {
			p.nextByte()
			tok.Kind = token.NULL_COALESCE
			tok.Literal = "??"
		} else if p.peekByte() == '.' {
			p.nextByte()
			tok.Kind = token.SAFE_DOT
			tok.Literal = "?."
		} else {
			tok.Kind = token.QUESTION
			tok.Literal = "?"
		}
	case '(':
		tok.Kind = token.LPAREN
		tok.Literal = "("
	case ')':
		tok.Kind = token.RPAREN
		tok.Literal = ")"
	case '{':
		tok.Kind = token.LBRACE
		tok.Literal = "{"
	case '}':
		tok.Kind = token.RBRACE
		tok.Literal = "}"
	case '[':
		tok.Kind = token.LBRACK
		tok.Literal = "["
	case ']':
		tok.Kind = token.RBRACK
		tok.Literal = "]"
	case ';':
		tok.Kind = token.SEMICOLON
		tok.Literal = ";"
	case ':':
		tok.Kind = token.COLON
		tok.Literal = ":"
	case ',':
		tok.Kind = token.COMMA
		tok.Literal = ","
	case '.':
		tok.Kind = token.DOT
		tok.Literal = "."
	case '\n':
		tok.Kind = token.NEWLINE
		tok.Literal = "\\n"
		tok.Line = p.line - 1
		tok.Col = p.col
	default:
		tok.Kind = token.EOF
		tok.Literal = ""
	}

	p.peekTok = tok
}
