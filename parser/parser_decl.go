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
if p.curTok.Kind == token.IDENT && (p.curTok.Literal == "go" || p.curTok.Literal == "gox") {
sourceType = p.curTok.Literal
p.nextToken()
}

path := p.expect(token.STRING).Literal

return &ast.ImportDecl{Path: path, SourceType: sourceType}
}

func (p *Parser) parseFuncDecl(vis ast.Visibility) *ast.FuncDecl {
p.nextToken()

name := p.expect(token.IDENT).Literal

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
Visibility: vis,
Name:       name,
Params:     params,
ReturnType: returnType,
Throws:     throws,
Body:       body,
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
p.expect(token.COLON)
typ := p.parseType()

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

p.expect(token.LBRACE)

fields := make([]*ast.Field, 0)
for p.curTok.Kind != token.RBRACE && p.curTok.Kind != token.EOF {
if p.curTok.Kind == token.NEWLINE || p.curTok.Kind == token.COMMA {
p.nextToken()
continue
}

fieldName := p.expect(token.IDENT).Literal
p.expect(token.COLON)
fieldType := p.parseType()

fields = append(fields, &ast.Field{Visibility: ast.Visibility{}, Name: fieldName, Type: fieldType})
}

p.expect(token.RBRACE)

return &ast.StructDecl{Visibility: vis, Name: name, Fields: fields}
}

func (p *Parser) parseInterfaceDecl(vis ast.Visibility) *ast.InterfaceDecl {
p.nextToken()

name := p.expect(token.IDENT).Literal

p.expect(token.LBRACE)

methods := make([]*ast.FuncDecl, 0)
for p.curTok.Kind != token.RBRACE && p.curTok.Kind != token.EOF {
if p.curTok.Kind == token.NEWLINE || p.curTok.Kind == token.COMMA {
p.nextToken()
continue
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

return &ast.InterfaceDecl{Visibility: vis, Name: name, Methods: methods}
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
