package parser

import (
"fmt"
"strings"

"github.com/gox-lang/gox/ast"
"github.com/gox-lang/gox/token"
)

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
return p.parseTSXElement()
}
return x
default:
return x
}
}

return x
}

func (p *Parser) parsePrimary() ast.Expr {
switch p.curTok.Kind {
case token.IDENT:
name := p.curTok.Literal
pos := p.curTok
p.nextToken()

// Check if this is a single-parameter arrow function: param => body
if p.curTok.Kind == token.ARROW {
p.nextToken()
// Parse body
if p.curTok.Kind == token.LBRACE {
block := p.parseBlock()
return &ast.FunctionLiteral{
Params: []*ast.FuncParam{{Name: name}},
Body: block,
IsArrow: true,
P: ast.Position{Line: pos.Line, Col: pos.Col},
}
} else {
body := p.parseExpr()
return &ast.FunctionLiteral{
Params: []*ast.FuncParam{{Name: name}},
Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{Result: body}}},
IsArrow: true,
P: ast.Position{Line: pos.Line, Col: pos.Col},
}
}
}

// Check if this is a struct literal: Type{}
if p.curTok.Kind == token.LBRACE {
p.nextToken()
fields := p.parseStructFields()
return &ast.StructLit{Type: &ast.Ident{Name: name}, Fields: fields}
}

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
		fmt.Printf("DEBUG parseExpr TEMPLATE: About to call parseTemplateString with %q\n", p.curTok.Literal)
		val := p.curTok.Literal
		p.nextToken()
		result := p.parseTemplateString(val)
		fmt.Printf("DEBUG parseExpr TEMPLATE: parseTemplateString returned %T\n", result)
		return result
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
// Could be parenthesized expression or arrow function (params) => body
p.nextToken()

// Check if this is an arrow function by looking for params followed by =>
if p.isArrowFunction() {
return p.parseArrowFunction()
}

// Otherwise it's a parenthesized expression
x := p.parseExpr()
p.expect(token.RPAREN)
return &ast.ParenExpr{X: x}
case token.LBRACE:
p.nextToken()
fields := p.parseStructFields()
return &ast.StructLit{Type: nil, Fields: fields}
case token.FUNC:
return p.parseFunctionLiteral()
case token.LESS:
		if p.peekTok.Kind == token.IDENT {
			p.nextToken()
			return p.parseTSXElement()
		}
		fallthrough
default:
p.errors = append(p.errors, fmt.Sprintf("unexpected token in expression: %v", p.curTok.Kind))
p.nextToken()
return &ast.Ident{Name: ""}
}
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

// Expect opening paren
if p.curTok.Kind == token.LPAREN {
p.nextToken()
}

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

// isArrowFunction checks if we have an arrow function syntax: (params) => body
func (p *Parser) isArrowFunction() bool {
// Save current lexer state
savedPos := p.pos
savedCurTok := p.curTok
savedPeekTok := p.peekTok

// Try to find matching ) and check if next token is =>
parenCount := 1
for p.curTok.Kind != token.EOF {
if p.curTok.Kind == token.LPAREN {
parenCount++
} else if p.curTok.Kind == token.RPAREN {
parenCount--
if parenCount == 0 {
// Found matching ), check next token
p.nextToken()
isArrow := p.curTok.Kind == token.ARROW
// Restore lexer state
p.pos = savedPos
p.curTok = savedCurTok
p.peekTok = savedPeekTok
return isArrow
}
}
p.nextToken()
}

// Restore lexer state
p.pos = savedPos
p.curTok = savedCurTok
p.peekTok = savedPeekTok
return false
}

// parseArrowFunction parses arrow function: (params) => body
func (p *Parser) parseArrowFunction() ast.Expr {
pos := ast.Position{Line: p.curTok.Line, Col: p.curTok.Col}

// Parse params (we're already after the opening paren)
params := p.parseFuncParams()

// Expect )
if p.curTok.Kind != token.RPAREN {
p.expect(token.RPAREN)
} else {
p.nextToken()
}

// Expect arrow
if p.curTok.Kind != token.ARROW {
p.errors = append(p.errors, fmt.Sprintf("expected =>, got %v", p.curTok.Kind))
} else {
p.nextToken()
}

// Check if body is expression or block
if p.curTok.Kind == token.LBRACE {
// Block body
block := p.parseBlock()
return &ast.FunctionLiteral{Params: params, Body: block, IsArrow: true, P: pos}
} else {
// Expression body
body := p.parseExpr()
return &ast.FunctionLiteral{
Params: params,
Body: &ast.BlockStmt{List: []ast.Stmt{&ast.ReturnStmt{Result: body}}},
IsArrow: true,
P: pos,
}
}
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

// Handle empty struct literal: Type{}
if p.curTok.Kind == token.RBRACE {
p.nextToken()
}

return fields
}

func (p *Parser) parseTemplateString(val string) ast.Expr {
	fmt.Printf("DEBUG parseTemplateString: val=%q\n", val)
debugTemplateString(val)
parts := make([]string, 0)
exprs := make([]ast.Expr, 0)

// Remove surrounding quotes (both backtick and double quote)
content := strings.Trim(val, "`\"")

// Parse template string
start := 0
for {
idx := strings.Index(content[start:], "${")
if idx == -1 {
// Add remaining content
if start < len(content) {
parts = append(parts, content[start:])
}
break
}

// Add content before ${
parts = append(parts, content[start:start+idx])

// Find closing }
exprStart := start + idx + 2
endIdx := strings.Index(content[exprStart:], "}")
if endIdx == -1 {
// No closing }, treat as literal
parts = append(parts, "${"+content[exprStart:])
break
}

// Extract expression
exprStr := content[exprStart : exprStart+endIdx]
exprs = append(exprs, &ast.Ident{Name: strings.TrimSpace(exprStr)})

// Move start position
start = exprStart + endIdx + 1
}

return &ast.TemplateString{Parts: parts, Exprs: exprs}
}

// Debug function to print template string parsing
func debugTemplateString(val string) {
	content := strings.Trim(val, "`\"")
	parts := make([]string, 0)
	start := 0
	for {
		idx := strings.Index(content[start:], "${")
		if idx == -1 {
			if start < len(content) {
				parts = append(parts, content[start:])
			}
			break
		}
		parts = append(parts, content[start:start+idx])
		exprStart := start + idx + 2
		endIdx := strings.Index(content[exprStart:], "}")
		if endIdx == -1 {
			parts = append(parts, "${"+content[exprStart:])
			break
		}
		exprStr := content[exprStart : exprStart+endIdx]
		_ = exprStr
		start = exprStart + endIdx + 1
	}
	fmt.Printf("DEBUG: Template %q -> Parts=%v\n", val, parts)
}

func init() {
	// Enable debug by calling debugTemplateString
	_ = debugTemplateString
}
