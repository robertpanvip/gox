package parser

import (
	"fmt"
	"strings"

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
	for isLetter(p.peekByte()) || isDigit(p.peekByte()) {
		p.nextByte()
	}
	return p.src[start:p.pos]
}

func isLetter(c rune) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || c >= 0x80
}

func isDigit(c rune) bool {
	return c >= '0' && c <= '9'
}

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

func (p *Parser) readNumber() token.Token {
	hasDecimal := false
	start := p.pos - 1
	
	for {
		c := p.peekByte()
		if isDigit(rune(c)) {
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

func (p *Parser) parseDecl() ast.Decl {
	switch p.curTok.Kind {
	case token.PACKAGE:
		return p.parsePackageClause()
	case token.IMPORT:
		return p.parseImportDecl()
	case token.PUBLIC:
		p.nextToken()
		if p.curTok.Kind == token.FUNC {
			return p.parseFuncDecl(token.Visibility{Public: true})
		} else if p.curTok.Kind == token.CONST {
			return p.parseConstDecl(token.Visibility{Public: true})
		} else if p.curTok.Kind == token.VAR {
			return p.parseVarDecl(token.Visibility{Public: true})
		} else if p.curTok.Kind == token.STRUCT {
			return p.parseStructDecl(token.Visibility{Public: true})
		} else if p.curTok.Kind == token.INTERFACE {
			return p.parseInterfaceDecl(token.Visibility{Public: true})
		} else if p.curTok.Kind == token.EXTEND {
			return p.parseExtendDecl(token.Visibility{Public: true})
		}
	case token.PRIVATE:
		p.nextToken()
		if p.curTok.Kind == token.FUNC {
			return p.parseFuncDecl(token.Visibility{Private: true})
		} else if p.curTok.Kind == token.CONST {
			return p.parseConstDecl(token.Visibility{Private: true})
		} else if p.curTok.Kind == token.VAR {
			return p.parseVarDecl(token.Visibility{Private: true})
		} else if p.curTok.Kind == token.STRUCT {
			return p.parseStructDecl(token.Visibility{Private: true})
		} else if p.curTok.Kind == token.INTERFACE {
			return p.parseInterfaceDecl(token.Visibility{Private: true})
		} else if p.curTok.Kind == token.EXTEND {
			return p.parseExtendDecl(token.Visibility{Private: true})
		}
	case token.FUNC:
		return p.parseFuncDecl(token.Visibility{})
	case token.CONST:
		return p.parseConstDecl(token.Visibility{})
	case token.VAR:
		return p.parseVarDecl(token.Visibility{})
	case token.STRUCT:
		return p.parseStructDecl(token.Visibility{})
	case token.INTERFACE:
		return p.parseInterfaceDecl(token.Visibility{})
	case token.EXTEND:
		return p.parseExtendDecl(token.Visibility{})
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

func (p *Parser) parseFuncDecl(vis token.Visibility) *ast.FuncDecl {
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

func (p *Parser) parseVisibility() token.Visibility {
	if p.curTok.Kind == token.PUBLIC {
		p.nextToken()
		return token.Visibility{Public: true}
	} else if p.curTok.Kind == token.PRIVATE {
		p.nextToken()
		return token.Visibility{Private: true}
	}
	return token.Visibility{}
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
	
	cases := make([]*ast.CaseClause, 0)
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
			cases = append(cases, &ast.CaseClause{Cond: caseCond, Body: caseBody})
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
	
	cases := make([]*ast.CaseClause, 0)
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
			cases = append(cases, &ast.CaseClause{Cond: caseCond, Body: caseBody})
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

func (p *Parser) parseConstDecl(vis token.Visibility) *ast.ConstDecl {
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

func (p *Parser) parseVarDecl(vis token.Visibility) *ast.VarDecl {
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

func (p *Parser) parseStructDecl(vis token.Visibility) *ast.StructDecl {
	p.nextToken()
	
	name := p.expect(token.IDENT).Literal
	
	p.expect(token.LBRACE)
	
	fields := make([]*ast.StructField, 0)
	for p.curTok.Kind != token.RBRACE && p.curTok.Kind != token.EOF {
		if p.curTok.Kind == token.NEWLINE || p.curTok.Kind == token.COMMA {
			p.nextToken()
			continue
		}
		
		fieldName := p.expect(token.IDENT).Literal
		p.expect(token.COLON)
		fieldType := p.parseType()
		
		fields = append(fields, &ast.StructField{Name: fieldName, Type: fieldType})
	}
	
	p.expect(token.RBRACE)
	
	return &ast.StructDecl{Visibility: vis, Name: name, Fields: fields}
}

func (p *Parser) parseInterfaceDecl(vis token.Visibility) *ast.InterfaceDecl {
	p.nextToken()
	
	name := p.expect(token.IDENT).Literal
	
	p.expect(token.LBRACE)
	
	methods := make([]*ast.FuncParam, 0)
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
		
		methods = append(methods, &ast.FuncParam{Name: methodName, Params: params, ReturnType: returnType})
	}
	
	p.expect(token.RBRACE)
	
	return &ast.InterfaceDecl{Visibility: vis, Name: name, Methods: methods}
}

func (p *Parser) parseExtendDecl(vis token.Visibility) *ast.ExtendDecl {
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
	
	return &ast.ExtendDecl{Type: &ast.Ident{Name: typeName}, Methods: methods}
}


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

func (p *Parser) parseExpr() ast.Expr {
	return p.parseNullCoalesce()
}

func (p *Parser) parseNullCoalesce() ast.Expr {
	x := p.parseOr()
	for p.check(token.NULL_COALESCE) {
		p.nextToken()
		y := p.parseOr()
		x = &ast.NilCoalesceExpr{X: x, Y: y}
	}
	return x
}

func (p *Parser) parseOr() ast.Expr {
	x := p.parseAnd()
	for p.curTok.Kind == token.LOGICAL_OR {
		p.nextToken()
		y := p.parseAnd()
		x = &ast.BinaryExpr{Op: token.LOGICAL_OR, X: x, Y: y}
	}
	return x
}

func (p *Parser) parseAnd() ast.Expr {
	x := p.parseEquality()
	for p.curTok.Kind == token.LOGICAL_AND {
		p.nextToken()
		y := p.parseEquality()
		x = &ast.BinaryExpr{Op: token.LOGICAL_AND, X: x, Y: y}
	}
	return x
}

func (p *Parser) parseEquality() ast.Expr {
	x := p.parseRelational()
	for p.curTok.Kind == token.EQUAL || p.curTok.Kind == token.NOT_EQUAL {
		op := p.curTok.Kind
		p.nextToken()
		y := p.parseRelational()
		x = &ast.BinaryExpr{Op: op, X: x, Y: y}
	}
	return x
}

func (p *Parser) parseRelational() ast.Expr {
	x := p.parseAdditive()
	for p.curTok.Kind == token.LESS || p.curTok.Kind == token.LESS_EQUAL || p.curTok.Kind == token.GREATER || p.curTok.Kind == token.GREATER_EQUAL {
		op := p.curTok.Kind
		p.nextToken()
		y := p.parseAdditive()
		x = &ast.BinaryExpr{Op: op, X: x, Y: y}
	}
	return x
}

func (p *Parser) parseAdditive() ast.Expr {
	x := p.parseMultiplicative()
	for p.curTok.Kind == token.PLUS || p.curTok.Kind == token.MINUS {
		op := p.curTok.Kind
		p.nextToken()
		y := p.parseMultiplicative()
		x = &ast.BinaryExpr{Op: op, X: x, Y: y}
	}
	return x
}

func (p *Parser) parseMultiplicative() ast.Expr {
	x := p.parseUnary()
	for p.curTok.Kind == token.STAR || p.curTok.Kind == token.SLASH || p.curTok.Kind == token.PERCENT {
		op := p.curTok.Kind
		p.nextToken()
		y := p.parseUnary()
		x = &ast.BinaryExpr{Op: op, X: x, Y: y}
	}
	return x
}

func (p *Parser) parseUnary() ast.Expr {
	if p.curTok.Kind == token.BANG || p.curTok.Kind == token.MINUS || p.curTok.Kind == token.PLUS || p.curTok.Kind == token.TILDE {
		op := p.curTok.Kind
		p.nextToken()
		x := p.parseUnary()
		return &ast.UnaryExpr{Op: op, X: x}
	}
	
	if p.curTok.Kind == token.TRY {
		p.nextToken()
		x := p.parseUnary()
		if call, ok := x.(*ast.CallExpr); ok {
			call.HasThrows = true
			return call
		}
		return &ast.TryExpr{X: x, Throws: true}
	}
	
	return p.parsePostfix()
}

func (p *Parser) parsePostfix() ast.Expr {
	x := p.parsePrimary()
	
	for true {
		switch {
		case p.curTok.Kind == token.DOT:
			p.nextToken()
			if p.curTok.Kind == token.IDENT {
				name := p.curTok.Literal
				p.nextToken()
				x = &ast.MemberExpr{X: x, Name: name, HasSafe: false}
			}
		case p.curTok.Kind == token.SAFE_DOT:
			p.nextToken()
			if p.curTok.Kind == token.IDENT {
				name := p.curTok.Literal
				p.nextToken()
				x = &ast.MemberExpr{X: x, Name: name, HasSafe: true}
			}
		case p.curTok.Kind == token.LBRACK:
			p.nextToken()
			index := p.parseExpr()
			if p.curTok.Kind == token.RBRACK {
				p.nextToken()
			}
			x = &ast.IndexExpr{X: x, Index: index}
		case p.curTok.Kind == token.LPAREN:
			p.nextToken()
			args := p.parseCallArgs()
			if p.curTok.Kind == token.RPAREN {
				p.nextToken()
			}
			x = &ast.CallExpr{Fun: x, Args: args}
		case p.curTok.Kind == token.LBRACE:
			var typeExpr ast.Expr
			if ident, ok := x.(*ast.Ident); ok {
				typeExpr = ident
			} else if member, ok := x.(*ast.MemberExpr); ok {
				typeExpr = member
			} else {
				return x
			}
			p.nextToken()
			fields := p.parseStructFields()
			x = &ast.StructLit{Type: typeExpr, Fields: fields}
		case p.curTok.Kind == token.LESS:
			if p.peekTok.Kind == token.IDENT {
				p.nextToken()
				x = p.parseTSXElement()
			} else {
				return x
			}
		default:
			return x
		}
	}
	
	return x
}

func (p *Parser) parseStructFields() []*ast.StructField {
	fields := make([]*ast.StructField, 0)
	
	for p.curTok.Kind != token.RBRACE && p.curTok.Kind != token.EOF {
		if p.curTok.Kind == token.NEWLINE || p.curTok.Kind == token.COMMA {
			p.nextToken()
			continue
		}
		
		if p.curTok.Kind == token.IDENT {
			if p.peekTok.Kind == token.COLON {
				name := p.curTok.Literal
				p.nextToken()
				p.nextToken()
				value := p.parseExpr()
				fields = append(fields, &ast.StructField{Name: name, Value: value})
			} else {
				value := p.parseExpr()
				fields = append(fields, &ast.StructField{Name: "", Value: value})
			}
		} else {
			value := p.parseExpr()
			fields = append(fields, &ast.StructField{Name: "", Value: value})
		}
		
		if p.curTok.Kind == token.COMMA || p.curTok.Kind == token.NEWLINE {
			p.nextToken()
		}
		
		if p.curTok.Kind == token.RBRACE {
			p.nextToken()
			break
		}
	}
	
	return fields
}


func (p *Parser) parseTSXElement() ast.Expr {
	pos := ast.Position{Line: p.curTok.Line, Col: p.curTok.Col}
	
	tagName := p.curTok.Literal
	p.nextToken()
	
	attributes := make([]*ast.TSXAttr, 0)
	for p.curTok.Kind != token.GREATER && p.curTok.Kind != token.SLASH && p.curTok.Kind != token.EOF {
		if p.check(token.NEWLINE) {
			p.nextToken()
			continue
		}
		
		if p.curTok.Kind == token.IDENT {
			attrName := p.curTok.Literal
			p.nextToken()
			
			var attrValue ast.Expr
			if p.check(token.ASSIGN) {
				p.nextToken()
				if p.curTok.Kind == token.STRING {
					attrValue = &ast.StringLit{Value: strings.Trim(p.curTok.Literal, `"`), P: pos}
					p.nextToken()
				} else if p.curTok.Kind == token.LBRACE {
					p.nextToken()
					attrValue = p.parseExpr()
					if p.curTok.Kind == token.RBRACE {
						p.nextToken()
					}
				}
			} else {
				attrValue = &ast.BoolLit{Value: true, P: pos}
			}
			
			attributes = append(attributes, &ast.TSXAttr{Name: attrName, Value: attrValue, P: pos})
		} else {
			p.nextToken()
		}
	}
	
	selfClosing := false
	if p.check(token.SLASH) {
		p.nextToken()
		selfClosing = true
	}
	
	if p.check(token.GREATER) {
		p.nextToken()
	}
	
	children := make([]ast.Expr, 0)
	if !selfClosing {
		for {
			if p.curTok.Kind == token.LESS && p.peekTok.Kind == token.SLASH {
				p.nextToken()
				p.nextToken()
				if p.curTok.Kind == token.IDENT {
					p.nextToken()
				}
				if p.check(token.GREATER) {
					p.nextToken()
				}
				break
			}
			
			if p.curTok.Kind == token.LESS && p.peekTok.Kind == token.IDENT {
				p.nextToken()
				children = append(children, p.parseTSXElement())
			} else if p.curTok.Kind == token.LBRACE {
				p.nextToken()
				children = append(children, p.parseExpr())
				if p.curTok.Kind == token.RBRACE {
					p.nextToken()
				}
			} else if p.curTok.Kind == token.STRING || p.curTok.Kind == token.IDENT || p.curTok.Kind == token.INT {
				text := p.curTok.Literal
				p.nextToken()
				children = append(children, &ast.StringLit{Value: text, P: pos})
			} else if p.check(token.NEWLINE) {
				p.nextToken()
			} else if p.curTok.Kind != token.EOF {
				p.nextToken()
			} else {
				break
			}
		}
	}
	
	return &ast.TSXElement{TagName: tagName, Attributes: attributes, Children: children, SelfClosing: selfClosing, P: pos}
}

func (p *Parser) parseFunctionLiteral() ast.Expr {
	pos := ast.Position{Line: p.curTok.Line, Col: p.curTok.Col}
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
	
	if p.curTok.Kind == token.ARROW {
		p.nextToken()
		body := p.parseExpr()
		return &ast.FunctionLiteral{
			Params:     params,
			ReturnType: returnType,
			Body:       &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{Result: body, P: pos}}},
			IsArrow:    true,
			P:          pos,
		}
	}
	
	body := p.parseBlock()
	return &ast.FunctionLiteral{Params: params, ReturnType: returnType, Body: body, IsArrow: false, P: pos}
}

func (p *Parser) parseCallArgs() []ast.Expr {
	args := make([]ast.Expr, 0)
	
	if p.curTok.Kind == token.RPAREN {
		return args
	}
	
	for {
		for p.curTok.Kind == token.COMMA || p.curTok.Kind == token.NEWLINE {
			p.nextToken()
			if p.curTok.Kind == token.RPAREN {
				return args
			}
		}
		
		if p.curTok.Kind == token.RPAREN || p.curTok.Kind == token.EOF {
			break
		}
		
		args = append(args, p.parseExpr())
		
		if p.curTok.Kind == token.COMMA {
			p.nextToken()
		}
		
		if p.curTok.Kind == token.RPAREN || p.curTok.Kind == token.EOF {
			break
		}
	}
	
	return args
}


func (p *Parser) parsePrimary() ast.Expr {
	switch p.curTok.Kind {
	case token.IDENT:
		name := p.curTok.Literal
		p.nextToken()
		return &ast.Ident{Name: name}
	case token.INT:
		var val int64
		fmt.Sscanf(p.curTok.Literal, "%d", &val)
		p.nextToken()
		return &ast.IntLit{Value: val}
	case token.FLOAT:
		var val float64
		fmt.Sscanf(p.curTok.Literal, "%f", &val)
		p.nextToken()
		return &ast.FloatLit{Value: val}
	case token.STRING:
		val := p.curTok.Literal
		p.nextToken()
		return &ast.StringLit{Value: strings.Trim(val, `"`)}
	case token.TEMPLATE:
		val := p.curTok.Literal
		p.nextToken()
		return p.parseTemplateString(val)
	case token.TRUE, token.FALSE:
		val := p.curTok.Kind == token.TRUE
		p.nextToken()
		return &ast.BoolLit{Value: val}
	case token.NIL:
		p.nextToken()
		return &ast.NilLit{}
	case token.SELF:
		p.nextToken()
		return &ast.Ident{Name: "self"}
	case token.LBRACK:
		p.nextToken()
		elts := make([]ast.Expr, 0)
		for p.curTok.Kind != token.RBRACK && p.curTok.Kind != token.EOF {
			if p.curTok.Kind == token.COMMA {
				p.nextToken()
				continue
			}
			elts = append(elts, p.parseExpr())
			if p.curTok.Kind == token.COMMA {
				p.nextToken()
			}
			if p.curTok.Kind == token.RBRACK {
				p.nextToken()
				return &ast.ArrayLit{Elements: elts}
			}
		}
		return &ast.ArrayLit{Elements: elts}
	case token.LPAREN:
		p.nextToken()
		x := p.parseExpr()
		p.expect(token.RPAREN)
		return &ast.ParenExpr{X: x}
	case token.LBRACE:
		p.nextToken()
		fields := p.parseStructFields()
		return &ast.StructLit{Type: nil, Fields: fields}
	case token.FUNC:
		return p.parseFunctionLiteral()
	default:
		p.errors = append(p.errors, fmt.Sprintf("unexpected token in expression: %v", p.curTok.Kind))
		p.nextToken()
		return &ast.Ident{Name: ""}
	}
}

func (p *Parser) parseTemplateString(val string) ast.Expr {
	parts := make([]string, 0)
	exprs := make([]ast.Expr, 0)
	
	content := strings.Trim(val, "`")
	
	for {
		idx := strings.Index(content, "${")
		if idx == -1 {
			parts = append(parts, content)
			break
		}
		
		parts = append(parts, content[:idx])
		content = content[idx+2:]
		
		endIdx := strings.Index(content, "}")
		if endIdx == -1 {
			parts = append(parts, "${"+content)
			break
		}
		
		exprStr := content[:endIdx]
		content = content[endIdx+1:]
		
		exprs = append(exprs, &ast.Ident{Name: strings.TrimSpace(exprStr)})
	}
	
	return &ast.TemplateString{Parts: parts, Exprs: exprs}
}