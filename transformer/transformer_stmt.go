package transformer

import (
	"fmt"
	"strings"

	"github.com/gox-lang/gox/ast"
)

func (t *Transformer) transformStmt(stmt ast.Stmt, isFuncThrows bool) string {
	var sb strings.Builder
	indentStr := strings.Repeat("    ", t.indent)

	switch s := stmt.(type) {
	case *ast.ExprStmt:
		sb.WriteString(indentStr)
		sb.WriteString(t.transformExpr(s.X))
		sb.WriteString("\n")

	case *ast.VarDecl:
		// Keep variable name as-is (Go convention is camelCase for local variables)
		name := s.Name

		if s.Value != nil {
			// Use := for variable declaration with value
			valueStr := t.transformExpr(s.Value)
			sb.WriteString(fmt.Sprintf("%s%s := %s\n", indentStr, name, valueStr))
		} else {
			// Use var for variable declaration without value
			typ := "interface{}"
			if s.Type != nil {
				typ = t.transformType(s.Type)
			}
			sb.WriteString(fmt.Sprintf("%svar %s %s\n", indentStr, name, typ))
		}

	case *ast.ConstDecl:
		sb.WriteString(indentStr)
		name := s.Name
		if s.Visibility.Public {
			name = strings.Title(name)
		} else if !s.Visibility.Private {
			name = strings.ToLower(name)
		}

		if s.Value != nil {
			if s.Type != nil {
				sb.WriteString(fmt.Sprintf("const %s %s = %s\n", name, t.transformType(s.Type), t.transformExpr(s.Value)))
			} else {
				sb.WriteString(fmt.Sprintf("const %s = %s\n", name, t.transformExpr(s.Value)))
			}
		}

	case *ast.AssignStmt:
		sb.WriteString(indentStr)
		sb.WriteString(t.transformExpr(s.LHS))
		sb.WriteString(" = ")
		sb.WriteString(t.transformExpr(s.RHS))
		sb.WriteString("\n")

	case *ast.ReturnStmt:
		sb.WriteString(indentStr)
		sb.WriteString("return")
		if s.Result != nil {
			if isFuncThrows {
				if call, ok := s.Result.(*ast.CallExpr); ok {
					call.HasThrows = true
				}
			}
			sb.WriteString(" ")
			sb.WriteString(t.transformExpr(s.Result))
			if isFuncThrows {
				sb.WriteString(", nil")
			}
		}
		sb.WriteString("\n")

	case *ast.IfStmt:
		sb.WriteString(indentStr)
		sb.WriteString("if ")
		sb.WriteString(t.transformExpr(s.Cond))
		sb.WriteString(" {\n")
		t.indent++
		for _, stmt := range s.Body.List {
			sb.WriteString(t.transformStmt(stmt, isFuncThrows))
		}
		t.indent--
		sb.WriteString(indentStr)
		sb.WriteString("}")

		if s.Else != nil {
			if ifStmt, ok := s.Else.(*ast.IfStmt); ok {
				sb.WriteString(" else ")
				sb.WriteString(t.transformStmt(ifStmt, isFuncThrows))
			} else {
				sb.WriteString(" else {\n")
				t.indent++
				elseStmts := s.Else.(*ast.BlockStmt).List
				for _, stmt := range elseStmts {
					sb.WriteString(t.transformStmt(stmt, isFuncThrows))
				}
				t.indent--
				sb.WriteString(indentStr)
				sb.WriteString("}")
			}
		}
		sb.WriteString("\n")

	case *ast.ForStmt:
		sb.WriteString(indentStr)
		sb.WriteString("for ")
		if s.Cond != nil {
			sb.WriteString(t.transformExpr(s.Cond))
		}
		sb.WriteString(" {\n")
		t.indent++
		for _, stmt := range s.Body.List {
			sb.WriteString(t.transformStmt(stmt, isFuncThrows))
		}
		t.indent--
		sb.WriteString(indentStr)
		sb.WriteString("}\n")

	case *ast.WhileStmt:
		sb.WriteString(indentStr)
		sb.WriteString("for ")
		if s.Cond != nil {
			sb.WriteString(t.transformExpr(s.Cond))
		}
		sb.WriteString(" {\n")
		t.indent++
		for _, stmt := range s.Body.List {
			sb.WriteString(t.transformStmt(stmt, isFuncThrows))
		}
		t.indent--
		sb.WriteString(indentStr)
		sb.WriteString("}\n")

	case *ast.BreakStmt:
		sb.WriteString(indentStr)
		sb.WriteString("break\n")

	case *ast.ContinueStmt:
		sb.WriteString(indentStr)
		sb.WriteString("continue\n")

	case *ast.SwitchStmt:
		sb.WriteString(indentStr)
		sb.WriteString("switch ")
		if s.Cond != nil {
			// Remove parentheses from condition if it's a ParenExpr
			cond := t.transformExpr(s.Cond)
			if paren, ok := s.Cond.(*ast.ParenExpr); ok {
				cond = t.transformExpr(paren.X)
			}
			sb.WriteString(cond)
		}
		sb.WriteString(" {\n")
		t.indent++
		for _, c := range s.Cases {
			sb.WriteString(indentStr)
			if c.Cond != nil {
				sb.WriteString("case ")
				sb.WriteString(t.transformExpr(c.Cond))
				sb.WriteString(":\n")
			} else {
				sb.WriteString("default:\n")
			}
			t.indent++
			for _, stmt := range c.Body.List {
				sb.WriteString(t.transformStmt(stmt, isFuncThrows))
			}
			t.indent--
		}
		t.indent--
		sb.WriteString(indentStr)
		sb.WriteString("}\n")

	case *ast.WhenStmt:
		sb.WriteString(indentStr)
		sb.WriteString("switch ")
		if s.Cond != nil {
			// Remove parentheses from condition if it's a ParenExpr
			cond := t.transformExpr(s.Cond)
			if paren, ok := s.Cond.(*ast.ParenExpr); ok {
				cond = t.transformExpr(paren.X)
			}
			sb.WriteString(cond)
		}
		sb.WriteString(" {\n")
		t.indent++
		for _, c := range s.Cases {
			sb.WriteString(indentStr)
			sb.WriteString("case ")
			sb.WriteString(t.transformExpr(c.Cond))
			sb.WriteString(":\n")
			t.indent++
			for _, stmt := range c.Body.List {
				sb.WriteString(t.transformStmt(stmt, isFuncThrows))
			}
			t.indent--
		}
		t.indent--
		sb.WriteString(indentStr)
		sb.WriteString("}\n")

	case *ast.TryStmt:
		if s.CatchBlock != nil {
			sb.WriteString(indentStr)
			sb.WriteString("{\n")
			t.indent++
			sb.WriteString(indentStr)
			sb.WriteString("res, err := func() {\n")
			t.indent++
			for _, stmt := range s.TryBlock.List {
				sb.WriteString(t.transformStmt(stmt, false))
			}
			t.indent--
			sb.WriteString(indentStr)
			sb.WriteString("}()\n")
			sb.WriteString(indentStr)
			sb.WriteString("if err != nil {\n")
			t.indent++
			for _, stmt := range s.CatchBlock.List {
				sb.WriteString(t.transformStmt(stmt, false))
			}
			t.indent--
			sb.WriteString(indentStr)
			sb.WriteString("}\n")
			t.indent--
			sb.WriteString(indentStr)
			sb.WriteString("}\n")
		} else {
			for _, stmt := range s.TryBlock.List {
				sb.WriteString(t.transformStmt(stmt, isFuncThrows))
			}
		}

	case *ast.BlockStmt:
		sb.WriteString(indentStr)
		sb.WriteString("{\n")
		t.indent++
		for _, stmt := range s.List {
			sb.WriteString(t.transformStmt(stmt, isFuncThrows))
		}
		t.indent--
		sb.WriteString(indentStr)
		sb.WriteString("}\n")
	}

	return sb.String()
}
