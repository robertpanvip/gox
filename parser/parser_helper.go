package parser

import (
"github.com/gox-lang/gox/token"
)

func (p *Parser) readNumber() token.Token {
hasDecimal := false
start := p.pos - 1

for {
c := p.peekByte()
if isDigit(c) {
p.nextByte()
} else if c == '.' && !hasDecimal {
hasDecimal = true
p.nextByte()
} else {
break
}
}

lit := p.src[start:p.pos]
if hasDecimal {
return token.Token{Kind: token.FLOAT, Literal: lit, Pos: start, Line: p.line, Col: p.col}
}
return token.Token{Kind: token.INT, Literal: lit, Pos: start, Line: p.line, Col: p.col}
}

func (p *Parser) readString() token.Token {
start := p.pos - 1
for p.peekByte() != '"' && p.pos < len(p.src) {
if p.peekByte() == '\\' {
p.nextByte()
p.nextByte()
} else {
p.nextByte()
}
}
p.nextByte()
lit := p.src[start:p.pos]
return token.Token{Kind: token.STRING, Literal: lit, Pos: start, Line: p.line, Col: p.col}
}

func (p *Parser) readRawString() token.Token {
start := p.pos - 1
hasTemplate := false
for p.peekByte() != '`' && p.pos < len(p.src) {
if p.peekByte() == '\n' {
p.line++
p.col = 1
}
if p.peekByte() == '$' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '{' {
hasTemplate = true
}
p.nextByte()
}
p.nextByte()
lit := p.src[start:p.pos]

if hasTemplate {
return token.Token{Kind: token.TEMPLATE, Literal: lit, Pos: start, Line: p.line, Col: p.col}
}
return token.Token{Kind: token.STRING, Literal: lit, Pos: start, Line: p.line, Col: p.col}
}
