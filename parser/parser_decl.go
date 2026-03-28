package parser

import (
"fmt"

"github.com/gox-lang/gox/ast"
"github.com/gox-lang/gox/token"
)

func (p *Parser) parseDecl() ast.Decl {
switch p.curTok.Kind {
case token.PACKAGE:
return p.parsePackageClause()
case token.IMPORT:
return p.parseImportDecl()
case token.PUBLIC:
p.nextToken()
if p.curTok.Kind == token.FUNC {
return p.parseFuncDecl(ast.Visibility{Public: true})
} else if p.curTok.Kind == token.CONST {
return p.parseConstDecl(ast.Visibility{Public: true})
} else if p.curTok.Kind == token.VAR {
return p.parseVarDecl(ast.Visibility{Public: true})
} else if p.curTok.Kind == token.STRUCT {
return p.parseStructDecl(ast.Visibility{Public: true})
} else if p.curTok.Kind == token.INTERFACE {
return p.parseInterfaceDecl(ast.Visibility{Public: true})
} else if p.curTok.Kind == token.EXTEND {
return p.parseExtendDecl(ast.Visibility{Public: true})
}
case token.PRIVATE:
p.nextToken()
if p.curTok.Kind == token.FUNC {
return p.parseFuncDecl(ast.Visibility{Private: true})
} else if p.curTok.Kind == token.CONST {
return p.parseConstDecl(ast.Visibility{Private: true})
} else if p.curTok.Kind == token.VAR {
return p.parseVarDecl(ast.Visibility{Private: true})
} else if p.curTok.Kind == token.STRUCT {
return p.parseStructDecl(ast.Visibility{Private: true})
} else if p.curTok.Kind == token.INTERFACE {
return p.parseInterfaceDecl(ast.Visibility{Private: true})
} else if p.curTok.Kind == token.EXTEND {
return p.parseExtendDecl(ast.Visibility{Private: true})
}
case token.FUNC:
return p.parseFuncDecl(ast.Visibility{})
case token.CONST:
return p.parseConstDecl(ast.Visibility{})
case token.VAR:
return p.parseVarDecl(ast.Visibility{})
case token.STRUCT:
return p.parseStructDecl(ast.Visibility{})
case token.INTERFACE:
return p.parseInterfaceDecl(ast.Visibility{})
case token.EXTEND:
return p.parseExtendDecl(ast.Visibility{})
case token.LET:
return p.parseLetDecl()
default:
p.errors = append(p.errors, fmt.Sprintf("unexpected token: %v", p.curTok.Kind))
p.nextToken()
return nil
}

return nil
}

func (p *Parser) parsePackageClause() *ast.PackageClause {
p.nextToken()
name := p.expect(token.IDENT).Literal
return &ast.PackageClause{Name: name}
}

func (p *Parser) parseImportDecl() *ast.ImportDecl {
p.nextToken()

sourceType := "gox"
if p.curTok.Kind == token.GO || p.curTok.Kind == token.GOX {
sourceType = p.curTok.Literal
p.nextToken()
}

path := p.expect(token.STRING).Literal

return &ast.ImportDecl{Path: path, SourceType: sourceType}
}

func (p *Parser) parseFuncDecl(vis ast.Visibility) *ast.FuncDecl {
p.nextToken()

var receiver *ast.FuncParam
if p.curTok.Kind == token.LPAREN {
p.nextToken()
recvName := p.expect(token.IDENT).Literal
p.expect(token.COLON)
recvType := p.parseType()
p.expect(token.RPAREN)
receiver = &ast.FuncParam{Name: recvName, Type: recvType}
}

name := p.expect(token.IDENT).Literal

// Parse type parameters [T, U, ...]
var typeParams []*ast.TypeParam
if p.curTok.Kind == token.LBRACK {
p.nextToken()
for {
if p.curTok.Kind == token.RBRACK || p.curTok.Kind == token.EOF {
break
}
if p.curTok.Kind == token.COMMA {
p.nextToken()
continue
}
typeName := p.expect(token.IDENT).Literal
var constraint ast.Expr
if p.curTok.Kind == token.IDENT {
constraint = p.parseType()
}
typeParams = append(typeParams, &ast.TypeParam{Name: typeName, Constraint: constraint})
}
p.expect(token.RBRACK)
}

p.expect(token.LPAREN)
params := p.parseFuncParams()
p.expect(token.RPAREN)

var returnType ast.Expr
if p.curTok.Kind == token.COLON {
p.nextToken()
returnType = p.parseType()
}

throws := false
if p.curTok.Kind == token.THROWS {
p.nextToken()
throws = true
}

var body *ast.BlockStmt
if p.curTok.Kind == token.LBRACE {
body = p.parseBlock()
}

return &ast.FuncDecl{
Visibility:   vis,
Name:         name,
TypeParams:   typeParams,
Params:       params,
ReturnType:   returnType,
Throws:       throws,
Body:         body,
Receiver:     receiver,
}
}

func (p *Parser) parseFuncParams() []*ast.FuncParam {
params := make([]*ast.FuncParam, 0)

if p.curTok.Kind == token.RPAREN {
return params
}

for {
if p.curTok.Kind == token.COMMA {
p.nextToken()
}

if p.curTok.Kind == token.RPAREN || p.curTok.Kind == token.EOF {
break
}

name := p.expect(token.IDENT).Literal
var typ ast.Expr
if p.curTok.Kind == token.COLON {
p.nextToken()
typ = p.parseType()
}
// If no colon, typ remains nil (for arrow functions without type annotations)

params = append(params, &ast.FuncParam{Name: name, Type: typ})
}

return params
}

func (p *Parser) parseBlock() *ast.BlockStmt {
p.expect(token.LBRACE)

stmts := make([]ast.Stmt, 0)
for p.curTok.Kind != token.RBRACE && p.curTok.Kind != token.EOF {
if p.curTok.Kind == token.NEWLINE {
p.nextToken()
continue
}

stmt := p.parseStmt()
if stmt != nil {
stmts = append(stmts, stmt)
} else {
// If parseStmt returns nil, skip the current token to avoid infinite loop
p.nextToken()
}
}

p.expect(token.RBRACE)

return &ast.BlockStmt{List: stmts}
}

func (p *Parser) parseLetDecl() *ast.VarDecl {
p.nextToken()

name := p.expect(token.IDENT).Literal

var typ ast.Expr
if p.curTok.Kind == token.COLON {
p.nextToken()
typ = p.parseType()
}

var value ast.Expr
if p.curTok.Kind == token.ASSIGN {
p.nextToken()
value = p.parseExpr()
}

return &ast.VarDecl{Name: name, Type: typ, Value: value}
}

func (p *Parser) parseConstDecl(vis ast.Visibility) *ast.ConstDecl {
p.nextToken()

name := p.expect(token.IDENT).Literal

var typ ast.Expr
if p.curTok.Kind == token.COLON {
p.nextToken()
typ = p.parseType()
}

var value ast.Expr
if p.curTok.Kind == token.ASSIGN {
p.nextToken()
value = p.parseExpr()
}

return &ast.ConstDecl{Visibility: vis, Name: name, Type: typ, Value: value}
}

func (p *Parser) parseVarDecl(vis ast.Visibility) *ast.VarDecl {
p.nextToken()

name := p.expect(token.IDENT).Literal

var typ ast.Expr
if p.curTok.Kind == token.COLON {
p.nextToken()
typ = p.parseType()
}

var value ast.Expr
if p.curTok.Kind == token.ASSIGN {
p.nextToken()
value = p.parseExpr()
}

return &ast.VarDecl{Visibility: vis, Name: name, Type: typ, Value: value}
}

func (p *Parser) parseStructDecl(vis ast.Visibility) *ast.StructDecl {
p.nextToken()

name := p.expect(token.IDENT).Literal

// Parse type parameters [T, U, ...]
var typeParams []*ast.TypeParam
if p.curTok.Kind == token.LBRACK {
p.nextToken()
for {
if p.curTok.Kind == token.RBRACK || p.curTok.Kind == token.EOF {
break
}
if p.curTok.Kind == token.COMMA {
p.nextToken()
continue
}
typeName := p.expect(token.IDENT).Literal
var constraint ast.Expr
if p.curTok.Kind == token.IDENT {
constraint = p.parseType()
}
typeParams = append(typeParams, &ast.TypeParam{Name: typeName, Constraint: constraint})
}
p.expect(token.RBRACK)
}

// Check for 'mixed' keyword
var mixed []*ast.BaseType
if p.curTok.Kind == token.MIXED {
p.nextToken()
mixedName := p.expect(token.IDENT).Literal
mixed = append(mixed, &ast.BaseType{Name: mixedName})
}

p.expect(token.LBRACE)

fields := make([]*ast.Field, 0)
for p.curTok.Kind != token.RBRACE && p.curTok.Kind != token.EOF {
if p.curTok.Kind == token.NEWLINE || p.curTok.Kind == token.COMMA {
p.nextToken()
continue
}

// Check for mixed inside struct body
if p.curTok.Kind == token.MIXED {
p.nextToken()
mixedName := p.expect(token.IDENT).Literal
mixed = append(mixed, &ast.BaseType{Name: mixedName})
continue
}

fieldVis := p.parseVisibility()
fieldName := p.expect(token.IDENT).Literal
p.expect(token.COLON)
fieldType := p.parseType()

fields = append(fields, &ast.Field{Visibility: fieldVis, Name: fieldName, Type: fieldType})
}

p.expect(token.RBRACE)

return &ast.StructDecl{Visibility: vis, Name: name, TypeParams: typeParams, Mixed: mixed, Fields: fields}
}

func (p *Parser) parseInterfaceDecl(vis ast.Visibility) *ast.InterfaceDecl {
p.nextToken()

name := p.expect(token.IDENT).Literal

p.expect(token.LBRACE)

methods := make([]*ast.FuncDecl, 0)
mixed := make([]*ast.BaseType, 0)

for p.curTok.Kind != token.RBRACE && p.curTok.Kind != token.EOF {
if p.curTok.Kind == token.NEWLINE || p.curTok.Kind == token.COMMA {
p.nextToken()
continue
}

// Check for mixed
if p.curTok.Kind == token.MIXED {
p.nextToken()
mixedName := p.expect(token.IDENT).Literal
mixed = append(mixed, &ast.BaseType{Name: mixedName})
continue
}

// Skip visibility keywords in interface
if p.curTok.Kind == token.PUBLIC || p.curTok.Kind == token.PRIVATE {
p.nextToken()
}

methodName := p.expect(token.IDENT).Literal
p.expect(token.LPAREN)
params := p.parseFuncParams()
p.expect(token.RPAREN)

var returnType ast.Expr
if p.curTok.Kind == token.COLON {
p.nextToken()
returnType = p.parseType()
}

methods = append(methods, &ast.FuncDecl{Name: methodName, Params: params, ReturnType: returnType})
}

p.expect(token.RBRACE)

return &ast.InterfaceDecl{Visibility: vis, Name: name, Methods: methods, Mixed: mixed}
}

func (p *Parser) parseExtendDecl(vis ast.Visibility) *ast.ExtendDecl {
p.nextToken()

typeName := p.expect(token.IDENT).Literal

p.expect(token.LBRACE)

methods := make([]*ast.FuncDecl, 0)
for p.curTok.Kind != token.RBRACE && p.curTok.Kind != token.EOF {
if p.curTok.Kind == token.NEWLINE || p.curTok.Kind == token.COMMA {
p.nextToken()
continue
}

if p.curTok.Kind == token.FUNC {
method := p.parseFuncDecl(vis)
methods = append(methods, method)
} else {
p.nextToken()
}
}

p.expect(token.RBRACE)

return &ast.ExtendDecl{Type: &ast.BaseType{Name: typeName}, Methods: methods}
}
