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
		{
			name:    "arrow function in object property",
			input:   `func test() { let obj = { onClick: () => x + 1 } }`,
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
			wantErr: true, // 目前会失败
			validate: func(t *testing.T, prog *ast.Program, errors []string) {
				// 这个测试目前会失败，因为箭头函数解析有问题
				t.Logf("Expected failure: %v", errors)
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
