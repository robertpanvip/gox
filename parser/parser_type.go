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

// Check for function type: (params) => returnType
if p.curTok.Kind == token.LPAREN {
// Look ahead to check if it's a function type
savedCur := p.curTok
savedPeek := p.peekTok

// Try to parse params and look for =>
parenCount := 1
p.nextToken()
for parenCount > 0 && p.curTok.Kind != token.EOF {
if p.curTok.Kind == token.LPAREN {
parenCount++
} else if p.curTok.Kind == token.RPAREN {
parenCount--
}
if parenCount > 0 {
p.nextToken()
}
}

if p.curTok.Kind == token.ARROW {
// It's a function type, restore and parse properly
p.curTok = savedCur
p.peekTok = savedPeek
return p.parseFunctionType()
}

// Not a function type, restore position
p.curTok = savedCur
p.peekTok = savedPeek
}

return typ
}

func (p *Parser) parseFunctionType() *ast.FuncType {
// Parse params
p.expect(token.LPAREN)
params := make([]*ast.FuncParam, 0)
for p.curTok.Kind != token.RPAREN && p.curTok.Kind != token.EOF {
if p.curTok.Kind == token.COMMA {
p.nextToken()
continue
}
name := p.expect(token.IDENT).Literal
var typ ast.Expr
if p.curTok.Kind == token.COLON {
p.nextToken()
typ = p.parseType()
}
params = append(params, &ast.FuncParam{Name: name, Type: typ})
}
p.expect(token.RPAREN)

// Expect arrow
p.expect(token.ARROW)

// Parse return type
returnType := p.parseType()

return &ast.FuncType{Params: params, ReturnType: returnType}
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

// Check for type parameters [T, U, ...]
// But not for array type []
if p.curTok.Kind == token.LBRACK {
// Look ahead to check if it's array type [] or generic type [T]
if p.peekToken().Kind == token.RBRACK {
// It's array type [], don't consume the bracket, let parseArrayOrBaseType handle it
return &ast.Ident{Name: name}
}

// It's generic type [T]
p.nextToken()
typeArgs := make([]ast.Expr, 0)
for {
if p.curTok.Kind == token.RBRACK || p.curTok.Kind == token.EOF {
break
}
if p.curTok.Kind == token.COMMA {
p.nextToken()
continue
}
typeArgs = append(typeArgs, p.parseType())
}
if len(typeArgs) > 0 {
p.expect(token.RBRACK)
return &ast.IndexExpr{X: &ast.Ident{Name: name}, Index: typeArgs[0]}
}
}

return &ast.Ident{Name: name}
case token.LPAREN:
p.nextToken()
typ := p.parseType()
p.expect(token.RPAREN)
return &ast.ParenExpr{X: typ}
default:
if p.curTok.Kind == token.EOF {
return &ast.Ident{Name: ""}
}
name := p.curTok.Literal
p.nextToken()
return &ast.BaseType{Name: name}
}
}
