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
// It's a function type with arrow syntax, restore and parse properly
p.curTok = savedCur
p.peekTok = savedPeek
return p.parseFuncType()
}

// Not a function type, restore position
p.curTok = savedCur
p.peekTok = savedPeek
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
p.nextToken() // consume 'func'

// Expect opening paren
p.expect(token.LPAREN)

params := make([]*ast.FuncParam, 0)

// Parse function type parameters: func(paramType1, paramType2): returnType
// or func(name1: Type1, name2: Type2): returnType
for p.curTok.Kind != token.RPAREN && p.curTok.Kind != token.EOF && p.curTok.Kind != token.COLON {
if p.curTok.Kind == token.COMMA {
p.nextToken()
continue
}

// Try to parse name: Type or just Type
if p.curTok.Kind == token.IDENT {
// Look ahead to check if it's name: Type or just Type (like int in func(int))
if p.peekToken().Kind == token.COLON {
// It's name: Type
name := p.curTok.Literal
p.nextToken()
p.expect(token.COLON)
typ := p.parseType()
params = append(params, &ast.FuncParam{Name: name, Type: typ})
} else if p.peekToken().Kind == token.COMMA || p.peekToken().Kind == token.RPAREN {
// It's just a type (like int in func(int))
typ := p.parseType()
params = append(params, &ast.FuncParam{Name: "", Type: typ})
} else {
// Default case - parse as type
typ := p.parseType()
params = append(params, &ast.FuncParam{Name: "", Type: typ})
}
} else {
// Parse type directly (for function types like func(int): int)
typ := p.parseType()
params = append(params, &ast.FuncParam{Name: "", Type: typ})
}
}

if p.curTok.Kind == token.RPAREN {
p.nextToken()
}

var returnType ast.Expr
if p.curTok.Kind == token.COLON {
p.nextToken()
returnType = p.parseType()
} else {
// No return type specified
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
