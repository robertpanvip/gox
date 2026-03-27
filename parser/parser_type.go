package parser

import (
"github.com/gox-lang/gox/ast"
"github.com/gox-lang/gox/token"
)

func (p *Parser) parseType() ast.Expr {
return p.parseNullableType()
}

func (p *Parser) parseNullableType() ast.Expr {
typ := p.parseArrayOrBaseType()
if p.curTok.Kind == token.QUESTION {
p.nextToken()
return &ast.NullableType{Element: typ}
}
return typ
}

func (p *Parser) parseArrayOrBaseType() ast.Expr {
if p.curTok.Kind == token.FUNC {
return p.parseFuncType()
}

typ := p.parseBaseType()

for p.curTok.Kind == token.LBRACK {
p.nextToken()
if p.curTok.Kind == token.RBRACK {
p.nextToken()
}
typ = &ast.ArrayType{Element: typ}
}

return typ
}

func (p *Parser) parseFuncType() *ast.FuncType {
p.nextToken()
params := p.parseFuncParams()

if p.curTok.Kind == token.RPAREN {
p.nextToken()
}

var returnType ast.Expr
if p.curTok.Kind == token.COLON {
p.nextToken()
returnType = p.parseType()
}

return &ast.FuncType{Params: params, ReturnType: returnType}
}

func (p *Parser) parseBaseType() ast.Expr {
switch p.curTok.Kind {
case token.IDENT:
name := p.curTok.Literal
p.nextToken()
return &ast.Ident{Name: name}
case token.LPAREN:
p.nextToken()
typ := p.parseType()
p.expect(token.RPAREN)
return &ast.ParenExpr{X: typ}
default:
name := p.curTok.Literal
p.nextToken()
return &ast.BaseType{Name: name}
}
}
