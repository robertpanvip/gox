package parser

import (
"fmt"


"github.com/gox-lang/gox/ast"
"github.com/gox-lang/gox/token"
)

type Parser struct {
src      string
pos      int
line     int
col      int
curTok   token.Token
peekTok  token.Token
errors   []string
}

func New(src string) *Parser {
p := &Parser{src: src, line: 1, col: 1}
p.nextToken()
p.nextToken()
return p
}

func (p *Parser) Errors() []string {
return p.errors
}

func (p *Parser) nextByte() byte {
if p.pos >= len(p.src) {
return 0
}
c := p.src[p.pos]
p.pos++
p.col++
return c
}

func (p *Parser) skipWhitespace() {
for p.pos < len(p.src) {
c := p.src[p.pos]
if c == ' ' || c == '\t' || c == '\r' {
p.nextByte()
} else if c == '\n' {
p.nextByte()
p.line++
p.col = 1
} else if c == '/' && p.pos+1 < len(p.src) && p.src[p.pos+1] == '/' {
p.skipLineComment()
} else {
break
}
}
}

func (p *Parser) skipLineComment() {
for p.pos < len(p.src) && p.src[p.pos] != '\n' {
p.nextByte()
}
}

func (p *Parser) peekByte() byte {
if p.pos >= len(p.src) {
return 0
}
return p.src[p.pos]
}

func (p *Parser) readIdentifier() string {
start := p.pos - 1
for isLetter(byte(p.peekByte())) || isDigit(byte(p.peekByte())) {
p.nextByte()
}
return p.src[start:p.pos]
}

func isLetter(c byte) bool {
return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

func isDigit(c byte) bool {
return c >= '0' && c <= '9'
}

func (p *Parser) check(kind token.TokenKind) bool {
return p.curTok.Kind == kind
}

func (p *Parser) expect(kind token.TokenKind) token.Token {
if p.curTok.Kind != kind {
p.errors = append(p.errors, fmt.Sprintf("expected token %v, got %v", kind, p.curTok.Kind))
}
tok := p.curTok
p.nextToken()
return tok
}

func (p *Parser) ParseProgram() *ast.Program {
prog := &ast.Program{Decls: make([]ast.Decl, 0)}

for p.curTok.Kind != token.EOF {
if p.curTok.Kind == token.NEWLINE {
p.nextToken()
continue
}

decl := p.parseDecl()
if decl != nil {
prog.Decls = append(prog.Decls, decl)
}
}

return prog
}
