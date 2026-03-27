package parser

import (
"github.com/gox-lang/gox/ast"
"github.com/gox-lang/gox/token"
)

func (p *Parser) parseStmt() ast.Stmt {
switch p.curTok.Kind {
case token.LET, token.CONST, token.VAR:
return p.parseVarDeclStmt()
case token.IF:
return p.parseIfStmt()
case token.FOR:
return p.parseForStmt()
case token.WHILE:
return p.parseWhileStmt()
case token.SWITCH:
return p.parseSwitchStmt()
case token.WHEN:
return p.parseWhenStmt()
case token.TRY:
return p.parseTryStmt()
case token.RETURN:
return p.parseReturnStmt()
case token.BREAK:
p.nextToken()
return &ast.BreakStmt{}
case token.CONTINUE:
p.nextToken()
return &ast.ContinueStmt{}
case token.LBRACE:
return p.parseBlockStmt()
default:
if p.curTok.Kind == token.IDENT || p.curTok.Kind == token.SELF {
expr := p.parseExpr()
if p.curTok.Kind == token.ASSIGN {
p.nextToken()
rhs := p.parseExpr()
return &ast.AssignStmt{LHS: expr, RHS: rhs}
}
return &ast.ExprStmt{X: expr}
}
return nil
}
}

func (p *Parser) parseVarDeclStmt() ast.Stmt {
vis := p.parseVisibility()
isConst := p.curTok.Kind == token.CONST
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

if isConst {
return &ast.ConstDecl{Visibility: vis, Name: name, Type: typ, Value: value}
}
return &ast.VarDecl{Visibility: vis, Name: name, Type: typ, Value: value}
}

func (p *Parser) parseVisibility() ast.Visibility {
if p.curTok.Kind == token.PUBLIC {
p.nextToken()
return ast.Visibility{Public: true}
} else if p.curTok.Kind == token.PRIVATE {
p.nextToken()
return ast.Visibility{Private: true}
}
return ast.Visibility{}
}

func (p *Parser) parseIfStmt() ast.Stmt {
p.nextToken()

if p.curTok.Kind == token.LPAREN {
p.nextToken()
}

cond := p.parseExpr()

if p.curTok.Kind == token.RPAREN {
p.nextToken()
}

body := p.parseBlock()

var elseBlock ast.Stmt
if p.curTok.Kind == token.ELSE {
p.nextToken()
if p.curTok.Kind == token.IF {
elseBlock = p.parseIfStmt()
} else if p.curTok.Kind == token.LBRACE {
elseBlock = p.parseBlock()
}
}

return &ast.IfStmt{Cond: cond, Body: body, Else: elseBlock}
}

func (p *Parser) parseForStmt() ast.Stmt {
p.nextToken()

var cond ast.Expr
if p.curTok.Kind != token.LBRACE {
cond = p.parseExpr()
}

body := p.parseBlock()

return &ast.ForStmt{Cond: cond, Body: body}
}

func (p *Parser) parseWhileStmt() ast.Stmt {
p.nextToken()

cond := p.parseExpr()
body := p.parseBlock()

return &ast.WhileStmt{Cond: cond, Body: body}
}

func (p *Parser) parseSwitchStmt() ast.Stmt {
p.nextToken()

var cond ast.Expr
if p.curTok.Kind != token.LBRACE {
cond = p.parseExpr()
}

p.expect(token.LBRACE)

cases := make([]*ast.SwitchCase, 0)
for p.curTok.Kind != token.RBRACE && p.curTok.Kind != token.EOF {
if p.curTok.Kind == token.NEWLINE {
p.nextToken()
continue
}

if p.curTok.Kind == token.CASE {
p.nextToken()
caseCond := p.parseExpr()
p.expect(token.COLON)
caseBody := p.parseBlock()
cases = append(cases, &ast.SwitchCase{Cond: caseCond, Body: caseBody})
} else {
p.nextToken()
}
}

p.expect(token.RBRACE)

return &ast.SwitchStmt{Cond: cond, Cases: cases}
}

func (p *Parser) parseWhenStmt() ast.Stmt {
p.nextToken()

var cond ast.Expr
if p.curTok.Kind != token.LBRACE {
cond = p.parseExpr()
}

p.expect(token.LBRACE)

cases := make([]*ast.WhenCase, 0)
for p.curTok.Kind != token.RBRACE && p.curTok.Kind != token.EOF {
if p.curTok.Kind == token.NEWLINE {
p.nextToken()
continue
}

if p.curTok.Kind == token.CASE {
p.nextToken()
caseCond := p.parseExpr()
p.expect(token.COLON)
caseBody := p.parseBlock()
cases = append(cases, &ast.WhenCase{Cond: caseCond, Body: caseBody})
} else {
p.nextToken()
}
}

p.expect(token.RBRACE)

return &ast.WhenStmt{Cond: cond, Cases: cases}
}

func (p *Parser) parseTryStmt() ast.Stmt {
p.nextToken()

tryBlock := p.parseBlock()

var catchBlock *ast.BlockStmt
if p.curTok.Kind == token.CATCH {
p.nextToken()
catchBlock = p.parseBlock()
}

return &ast.TryStmt{TryBlock: tryBlock, CatchBlock: catchBlock}
}

func (p *Parser) parseReturnStmt() ast.Stmt {
p.nextToken()

var result ast.Expr
if p.curTok.Kind != token.NEWLINE && p.curTok.Kind != token.RBRACE && p.curTok.Kind != token.EOF {
result = p.parseExpr()
}

return &ast.ReturnStmt{Result: result}
}

func (p *Parser) parseBlockStmt() ast.Stmt {
return p.parseBlock()
}
