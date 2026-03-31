package parser

import (
	"testing"

	"github.com/gox-lang/gox/ast"
)

// TestParser_ExpressionStatement 测试表达式语句
func TestParser_ExpressionStatement(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		validate func(t *testing.T, prog *ast.Program, errors []string)
	}{
		{
			name:    "simple identifier expression",
			input:   `func test() { x }`,
			wantErr: false,
			validate: func(t *testing.T, prog *ast.Program, errors []string) {
				// 检查是否解析为 ExprStmt
				if len(prog.Decls) != 1 {
					t.Errorf("expected 1 decl, got %d", len(prog.Decls))
				}
			},
		},
		{
			name:    "binary expression",
			input:   `func test() { x + y }`,
			wantErr: false,
			validate: func(t *testing.T, prog *ast.Program, errors []string) {
				if len(errors) > 0 {
					t.Errorf("parser errors: %v", errors)
				}
			},
		},
		{
			name:    "call expression",
			input:   `func test() { println("hello") }`,
			wantErr: false,
			validate: func(t *testing.T, prog *ast.Program, errors []string) {
				if len(prog.Decls) != 1 {
					t.Errorf("expected 1 decl, got %d", len(prog.Decls))
				}
			},
		},
		{
			name:    "member expression",
			input:   `func test() { obj.property }`,
			wantErr: false,
			validate: func(t *testing.T, prog *ast.Program, errors []string) {
				if len(prog.Decls) != 1 {
					t.Errorf("expected 1 decl, got %d", len(prog.Decls))
				}
			},
		},
		{
			name:    "assignment expression",
			input:   `func test() { x = 5 }`,
			wantErr: false,
			validate: func(t *testing.T, prog *ast.Program, errors []string) {
				if len(prog.Decls) != 1 {
					t.Errorf("expected 1 decl, got %d", len(prog.Decls))
				}
			},
		},
		{
			name:    "arrow function expression",
			input:   `func test() { () => x + 1 }`,
			wantErr: false,
			validate: func(t *testing.T, prog *ast.Program, errors []string) {
				if len(prog.Decls) != 1 {
					t.Errorf("expected 1 decl, got %d", len(prog.Decls))
				}
			},
		},
		{
			name:    "arrow function with block",
			input:   `func test() { () => { return x + 1 } }`,
			wantErr: false,
			validate: func(t *testing.T, prog *ast.Program, errors []string) {
				if len(prog.Decls) != 1 {
					t.Errorf("expected 1 decl, got %d", len(prog.Decls))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.input)
			prog := p.ParseProgram()

			if len(p.Errors()) > 0 && !tt.wantErr {
				t.Fatalf("parser errors: %v", p.Errors())
			}

			if tt.validate != nil {
				tt.validate(t, prog, p.Errors())
			}
		})
	}
}

// TestParser_ExpressionStatement_TSX 测试 TSX 中的表达式
func TestParser_ExpressionStatement_TSX(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		validate func(t *testing.T, prog *ast.Program, errors []string)
	}{
		{
			name:    "tsx with expression attribute",
			input:   `func test() { return <button text={count} /> }`,
			wantErr: false,
			validate: func(t *testing.T, prog *ast.Program, errors []string) {
				if len(prog.Decls) != 1 {
					t.Errorf("expected 1 decl, got %d", len(prog.Decls))
				}
			},
		},
		{
			name:    "tsx with arrow function attribute",
			input:   `func test() { return <button onClick={() => count = count + 1} /> }`,
			wantErr: false, // 现在应该能成功
			validate: func(t *testing.T, prog *ast.Program, errors []string) {
				if len(prog.Decls) != 1 {
					t.Errorf("expected 1 decl, got %d", len(prog.Decls))
				}
			},
		},
		{
			name:    "tsx with arrow function block attribute",
			input:   `func test() { return <button onClick={() => { count = count + 1 }} /> }`,
			wantErr: false,
			validate: func(t *testing.T, prog *ast.Program, errors []string) {
				if len(prog.Decls) != 1 {
					t.Errorf("expected 1 decl, got %d", len(prog.Decls))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.input)
			prog := p.ParseProgram()

			hasErrors := len(p.Errors()) > 0
			if hasErrors && !tt.wantErr {
				t.Fatalf("parser errors: %v", p.Errors())
			}

			if !hasErrors && tt.wantErr {
				t.Fatalf("expected errors but got none")
			}

			if tt.validate != nil {
				tt.validate(t, prog, p.Errors())
			}
		})
	}
}

// TestParser_ArrowFunction 专门测试箭头函数解析
func TestParser_ArrowFunction(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"no params expression body", `() => 1`, false},
		{"no params block body", `() => { return 1 }`, false},
		{"one param", `(x) => x + 1`, false},
		{"multiple params", `(x, y) => x + y`, false},
		{"assignment in body", `() => x = x + 1`, false},
		{"assignment in block", `() => { x = x + 1 }`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 包装在函数中测试
			fullInput := "func test() { " + tt.input + " }"
			p := New(fullInput)
			prog := p.ParseProgram()

			hasErrors := len(p.Errors()) > 0
			if hasErrors && !tt.wantErr {
				t.Fatalf("parser errors: %v", p.Errors())
			}

			if !hasErrors && tt.wantErr {
				t.Fatalf("expected errors but got none")
			}

			if len(prog.Decls) != 1 {
				t.Errorf("expected 1 decl, got %d", len(prog.Decls))
			}
		})
	}
}

// TestParser_ArrowFunctionAsStatement 测试箭头函数作为表达式语句
func TestParser_ArrowFunctionAsStatement(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		validate func(t *testing.T, prog *ast.Program, errors []string)
	}{
		{
			name:    "arrow function as statement",
			input:   `func test() { () => x + 1 }`,
			wantErr: false,
			validate: func(t *testing.T, prog *ast.Program, errors []string) {
				if len(prog.Decls) != 1 {
					t.Errorf("expected 1 decl, got %d", len(prog.Decls))
				}
			},
		},
		{
			name:    "arrow function assignment as statement",
			input:   `func test() { () => x = x + 1 }`,
			wantErr: false,
			validate: func(t *testing.T, prog *ast.Program, errors []string) {
				if len(prog.Decls) != 1 {
					t.Errorf("expected 1 decl, got %d", len(prog.Decls))
				}
			},
		},
		{
			name:    "arrow function assigned to variable",
			input:   `func test() { let fn = () => x + 1 }`,
			wantErr: false,
			validate: func(t *testing.T, prog *ast.Program, errors []string) {
				if len(prog.Decls) != 1 {
					t.Errorf("expected 1 decl, got %d", len(prog.Decls))
				}
			},
		},
		{
			name:    "arrow function with assignment in body",
			input:   `func test() { let fn = () => x = x + 1 }`,
			wantErr: false,
			validate: func(t *testing.T, prog *ast.Program, errors []string) {
				if len(prog.Decls) != 1 {
					t.Errorf("expected 1 decl, got %d", len(prog.Decls))
				}
			},
		},
		{
			name:    "arrow function body is exprstmt not returnstmt",
			input:   `func test() { let fn = () => x = x + 1 }`,
			wantErr: false,
			validate: func(t *testing.T, prog *ast.Program, errors []string) {
				// 验证箭头函数体是 ExprStmt 而不是 ReturnStmt
				fnDecl := prog.Decls[0].(*ast.FuncDecl)
				varDecl := fnDecl.Body.List[0].(*ast.VarDecl)
				arrowFn := varDecl.Value.(*ast.FunctionLiteral)
				
				// Body 应该是 BlockStmt
				block := arrowFn.Body
				
				// Block 中应该只有一个语句
				if len(block.List) != 1 {
					t.Errorf("expected 1 statement in body, got %d", len(block.List))
					return
				}
				
				// 语句应该是 ExprStmt，而不是 ReturnStmt
				exprStmt, ok := block.List[0].(*ast.ExprStmt)
				if !ok {
					t.Errorf("expected statement to be ExprStmt, got %T", block.List[0])
					return
				}
				
				// ExprStmt 的 X 应该是 BinaryExpr (赋值表达式)
				if _, ok := exprStmt.X.(*ast.BinaryExpr); !ok {
					t.Errorf("expected expression to be BinaryExpr, got %T", exprStmt.X)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := New(tt.input)
			prog := p.ParseProgram()

			hasErrors := len(p.Errors()) > 0
			if hasErrors && !tt.wantErr {
				t.Fatalf("parser errors: %v", p.Errors())
			}

			if !hasErrors && tt.wantErr {
				t.Fatalf("expected errors but got none")
			}

			if tt.validate != nil {
				tt.validate(t, prog, p.Errors())
			}
		})
	}
}
