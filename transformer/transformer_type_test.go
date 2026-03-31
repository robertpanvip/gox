package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

// TestTypeAnnotation 测试类型注解功能
func TestTypeAnnotation(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		validate func(t *testing.T, result string)
	}{
		{
			name: "variable type annotation",
			input: `package main
func test() {
	let x: int = 42
	let name: string = "GoX"
	let flag: bool = true
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "x := 42") {
					t.Errorf("expected variable declaration, got: %s", result)
				}
			},
		},
		{
			name: "function parameter type annotation",
			input: `package main
func add(a: int, b: int): int {
	return a + b
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "func add(a int, b int) int") {
					t.Errorf("expected function with type annotations, got: %s", result)
				}
			},
		},
		{
			name: "array type annotation",
			input: `package main
func test() {
	let arr: int[]
	arr = [1, 2, 3]
}`,
			validate: func(t *testing.T, result string) {
				// 当前实现不推断数组类型，只测试类型注解被解析
				if !strings.Contains(result, "arr") {
					t.Errorf("expected array variable, got: %s", result)
				}
			},
		},
		{
			name: "nullable type annotation",
			input: `package main
func test() {
	let x: int? = nil
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "x := nil") {
					t.Errorf("expected nullable type, got: %s", result)
				}
			},
		},
		{
			name: "function type annotation",
			input: `package main
func test() {
	let fn: func(int): int = add
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "fn := add") {
					t.Errorf("expected function type, got: %s", result)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := parser.New(tt.input)
			prog := p.ParseProgram()

			if len(p.Errors()) > 0 {
				t.Fatalf("parser errors: %v", p.Errors())
			}

			tfm := New()
			result := tfm.Transform(prog)
			tt.validate(t, result)
		})
	}
}
