package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

// TestTransformSigDecl 测试 sig 声明转换
func TestTransformSigDecl(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		validate func(t *testing.T, result string)
	}{
		{
			name: "simple sig int",
			input: `package main
func test() {
	sig count = 0
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "count := gox.New(0)") {
					t.Errorf("expected 'count := gox.New(0)' in output, got: %s", result)
				}
			},
		},
		{
			name: "sig with string",
			input: `package main
func test() {
	sig name = "World"
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, `name := gox.New("World")`) {
					t.Errorf("expected signal string declaration in output, got: %s", result)
				}
			},
		},
		{
			name: "sig with boolean",
			input: `package main
func test() {
	sig isActive = true
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "isActive := gox.New(true)") {
					t.Errorf("expected signal bool declaration in output, got: %s", result)
				}
			},
		},
		{
			name: "sig with float",
			input: `package main
func test() {
	sig price = 99.99
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "price := gox.New(99.99)") {
					t.Errorf("expected signal float declaration in output, got: %s", result)
				}
			},
		},
		{
			name: "multiple sig declarations",
			input: `package main
func test() {
	sig count = 0
	sig name = "test"
	sig active = true
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "count := gox.New(0)") {
					t.Errorf("expected first sig declaration, got: %s", result)
				}
				if !strings.Contains(result, "name := gox.New(\"test\")") {
					t.Errorf("expected second sig declaration, got: %s", result)
				}
				if !strings.Contains(result, "active := gox.New(true)") {
					t.Errorf("expected third sig declaration, got: %s", result)
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

// TestTransformSigGet 测试 Signal 自动 .Get() 转换
func TestTransformSigGet(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		validate func(t *testing.T, result string)
	}{
		{
			name: "sig variable usage in expression",
			input: `package main
func test() {
	sig count = 0
	let x: int = count
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "count := gox.New(0)") {
					t.Errorf("expected sig declaration, got: %s", result)
				}
				if !strings.Contains(result, "x := count.Get()") {
					t.Errorf("expected count.Get() in assignment, got: %s", result)
				}
			},
		},
		{
			name: "sig variable in function call",
			input: `package main
import "fmt"
func test() {
	sig count = 0
	fmt.Println(count)
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "fmt.Println(count.Get())") {
					t.Errorf("expected count.Get() in function call, got: %s", result)
				}
			},
		},
		{
			name: "sig variable in binary expression",
			input: `package main
func test() {
	sig count = 0
	let x: int = count + 1
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "x := count.Get() + 1") {
					t.Errorf("expected count.Get() in binary expr, got: %s", result)
				}
			},
		},
		{
			name: "multiple sig variables usage",
			input: `package main
func test() {
	sig a = 1
	sig b = 2
	let c: int = a + b
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "c := a.Get() + b.Get()") {
					t.Errorf("expected multiple sig vars with .Get(), got: %s", result)
				}
			},
		},
		{
			name: "sig in if condition",
			input: `package main
func test() {
	sig count = 0
	if count > 0 {
		println("positive")
	}
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "if count.Get() > 0") {
					t.Errorf("expected count.Get() in if condition, got: %s", result)
				}
			},
		},
		{
			name: "sig in for loop condition",
			input: `package main
func test() {
	sig count = 0
	for count < 10 {
		count = count + 1
	}
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "for count.Get() < 10") {
					t.Errorf("expected count.Get() in for condition, got: %s", result)
				}
				if !strings.Contains(result, "count.Set(count.Get() + 1)") {
					t.Errorf("expected count.Set() in loop body, got: %s", result)
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

// TestTransformSigSet 测试 Signal 自动 .Set() 转换
func TestTransformSigSet(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		validate func(t *testing.T, result string)
	}{
		{
			name: "simple sig assignment",
			input: `package main
func test() {
	sig count = 0
	count = 5
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "count.Set(5)") {
					t.Errorf("expected count.Set(5), got: %s", result)
				}
			},
		},
		{
			name: "sig assignment with expression",
			input: `package main
func test() {
	sig count = 0
	count = count + 1
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "count.Set(count.Get() + 1)") {
					t.Errorf("expected count.Set(count.Get() + 1), got: %s", result)
				}
			},
		},
		{
			name: "sig decrement",
			input: `package main
func test() {
	sig count = 10
	count = count - 1
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "count.Set(count.Get() - 1)") {
					t.Errorf("expected decrement with .Set(), got: %s", result)
				}
			},
		},
		{
			name: "sig multiplication",
			input: `package main
func test() {
	sig price = 100
	price = price * 2
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "price.Set(price.Get() * 2)") {
					t.Errorf("expected multiplication with .Set(), got: %s", result)
				}
			},
		},
		{
			name: "sig complex expression",
			input: `package main
func test() {
	sig a = 1
	sig b = 2
	a = a + b * 3
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "a.Set(a.Get() + b.Get() * 3)") {
					t.Errorf("expected complex expr with .Set() and .Get(), got: %s", result)
				}
			},
		},
		{
			name: "multiple sig assignments",
			input: `package main
func test() {
	sig x = 0
	sig y = 0
	x = x + 1
	y = y + 2
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "x.Set(x.Get() + 1)") {
					t.Errorf("expected x.Set(), got: %s", result)
				}
				if !strings.Contains(result, "y.Set(y.Get() + 2)") {
					t.Errorf("expected y.Set(), got: %s", result)
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

// TestTransformSigMixed 测试混合场景
func TestTransformSigMixed(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		validate func(t *testing.T, result string)
	}{
		{
			name: "counter component",
			input: `package main
import "fmt"
func Counter() {
	sig count = 0
	fmt.Println(count)
	count = count + 1
	fmt.Println(count)
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "count := gox.New(0)") {
					t.Errorf("expected sig declaration, got: %s", result)
				}
				if !strings.Contains(result, "fmt.Println(count.Get())") {
					t.Errorf("expected count.Get() in println, got: %s", result)
				}
				if !strings.Contains(result, "count.Set(count.Get() + 1)") {
					t.Errorf("expected count.Set(), got: %s", result)
				}
			},
		},
		{
			name: "sig with regular variables",
			input: `package main
func test() {
	sig count = 0
	let regular: int = count
	count = count + 1
	let another: int = regular + count
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "regular := count.Get()") {
					t.Errorf("expected regular := count.Get(), got: %s", result)
				}
				if !strings.Contains(result, "another := regular + count.Get()") {
					t.Errorf("expected mixed usage, got: %s", result)
				}
			},
		},
		{
			name: "sig in nested scope",
			input: `package main
func test() {
	sig count = 0
	if count > 0 {
		let temp: int = count
		count = temp + 1
	}
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "if count.Get() > 0") {
					t.Errorf("expected count.Get() in if, got: %s", result)
				}
				if !strings.Contains(result, "temp := count.Get()") {
					t.Errorf("expected temp := count.Get(), got: %s", result)
				}
			},
		},
		{
			name: "multiple sigs interaction",
			input: `package main
func test() {
	sig x = 1
	sig y = 2
	sig z = 0
	z = x + y
	x = z * 2
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "z.Set(x.Get() + y.Get())") {
					t.Errorf("expected z.Set with multiple sigs, got: %s", result)
				}
				if !strings.Contains(result, "x.Set(z.Get() * 2)") {
					t.Errorf("expected x.Set, got: %s", result)
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

// TestTransformSigEdgeCases 测试边界情况
func TestTransformSigEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		validate func(t *testing.T, result string)
	}{
		{
			name: "sig with zero value",
			input: `package main
func test() {
	sig zero = 0
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "zero := gox.New(0)") {
					t.Errorf("expected zero initialization, got: %s", result)
				}
			},
		},
		{
			name: "sig with negative value",
			input: `package main
func test() {
	sig neg = -5
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "neg := gox.New(-5)") {
					t.Errorf("expected negative value, got: %s", result)
				}
			},
		},
		{
			name: "sig with string concatenation",
			input: `package main
func test() {
	sig name = "World"
	let greeting: string = "Hello " + name
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "greeting := \"Hello \" + name.Get()") {
					t.Errorf("expected string concat with .Get(), got: %s", result)
				}
			},
		},
		{
			name: "sig different functions",
			input: `package main
func Func1() {
	sig count = 0
	count = count + 1
}
func Func2() {
	sig count = 10
	count = count + 1
}`,
			validate: func(t *testing.T, result string) {
				// Each function should have its own sig var
				count1 := strings.Count(result, "count := gox.New(0)")
				count2 := strings.Count(result, "count := gox.New(10)")
				if count1 != 1 || count2 != 1 {
					t.Errorf("expected separate sig declarations, got: %s", result)
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
