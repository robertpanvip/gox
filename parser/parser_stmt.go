package parser

import (
	"fmt"
	"github.com/gox-lang/gox/ast"
	"github.com/gox-lang/gox/token"
)

func (p *Parser) parseStmt() ast.Stmt {
	switch p.curTok.Kind {
	case token.LET, token.CONST, token.VAR:
		return p.parseVarDeclStmt()
	case token.SIG:
		return p.parseSigDeclStmt()
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
		if p.curTok.Kind == token.IDENT || p.curTok.Kind == token.SELF || p.curTok.Kind == token.LESS {
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

func (p *Parser) parseSigDeclStmt() ast.Stmt {
	// sig 声明不需要 visibility，直接解析
	p.nextToken()

	name := p.expect(token.IDENT).Literal

	// sig 声明不需要类型注解，直接 = value
	var value ast.Expr
	if p.curTok.Kind == token.ASSIGN {
		p.nextToken()
		value = p.parseExpr()
	}

	return &ast.SigDecl{Name: name, Value: value}
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

	// Support both while x {} and while (x) {}
	var cond ast.Expr
	if p.curTok.Kind == token.LPAREN {
		p.nextToken()
		cond = p.parseExpr()
		p.expect(token.RPAREN)
	} else {
		cond = p.parseExpr()
	}
	body := p.parseBlock()

	return &ast.WhileStmt{Cond: cond, Body: body}
}

func (p *Parser) parseSwitchStmt() ast.Stmt {
	p.nextToken()

	var cond ast.Expr
	// Support both switch x {} and switch (x) {}
	if p.curTok.Kind == token.LPAREN {
		p.nextToken()
		cond = p.parseExpr()
		p.expect(token.RPAREN)
	} else {
		// No parentheses, parse expression directly
		cond = p.parseExpr()
	}

	// Consume LBRACE
	if p.curTok.Kind != token.LBRACE {
		p.errors = append(p.errors, fmt.Sprintf("expected '{' after switch condition, got %v", p.curTok.Kind))
		return &ast.SwitchStmt{Cond: cond, Cases: nil}
	}
	p.nextToken()

	cases := make([]*ast.SwitchCase, 0)
	// Parse cases
	for p.curTok.Kind != token.RBRACE && p.curTok.Kind != token.EOF {
		if p.curTok.Kind == token.NEWLINE {
			p.nextToken()
			continue
		}

		if p.curTok.Kind == token.CASE {
			p.nextToken()
			caseCond := p.parseExpr()
			if p.curTok.Kind != token.COLON {
				p.errors = append(p.errors, fmt.Sprintf("expected ':' after case condition, got %v", p.curTok.Kind))
			} else {
				p.nextToken()
			}
			// Parse statements until next case/default or }
			caseBody := p.parseSwitchCaseBody()
			cases = append(cases, &ast.SwitchCase{Cond: caseCond, Body: caseBody})
		} else if p.curTok.Kind == token.DEFAULT {
			p.nextToken()
			if p.curTok.Kind != token.COLON {
				p.errors = append(p.errors, fmt.Sprintf("expected ':' after default, got %v", p.curTok.Kind))
			} else {
				p.nextToken()
			}
			// Parse statements until next case/default or }
			caseBody := p.parseSwitchCaseBody()
			// default case has nil Cond
			cases = append(cases, &ast.SwitchCase{Cond: nil, Body: caseBody})
		} else {
			// Skip unexpected token
			p.nextToken()
		}
	}

	if p.curTok.Kind != token.RBRACE {
		p.errors = append(p.errors, fmt.Sprintf("expected '}}' to close switch, got %v", p.curTok.Kind))
	} else {
		p.nextToken()
	}

	return &ast.SwitchStmt{Cond: cond, Cases: cases}
}

// parseSwitchCaseBody parses statements until next case/default or }
func (p *Parser) parseSwitchCaseBody() *ast.BlockStmt {
	stmts := make([]ast.Stmt, 0)
	for p.curTok.Kind != token.RBRACE && p.curTok.Kind != token.EOF &&
		p.curTok.Kind != token.CASE && p.curTok.Kind != token.DEFAULT {
		if p.curTok.Kind == token.NEWLINE {
			p.nextToken()
			continue
		}
		stmt := p.parseStmt()
		if stmt != nil {
			stmts = append(stmts, stmt)
		} else {
			p.nextToken()
		}
	}
	return &ast.BlockStmt{List: stmts}
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
