package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

// TestTransformTSX TSX 转换测试（TemplateResult）
func TestTransformTSX(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		validate func(t *testing.T, result string)
	}{
		{
			name: "simple button with static text",
			input: `package main
func test() {
	return <button text="Click Me" />
}`,
			validate: func(t *testing.T, result string) {
				// 应该生成 gui.TemplateResult
				if !strings.Contains(result, "gui.TemplateResult") {
					t.Errorf("expected gui.TemplateResult, got: %s", result)
				}
				// 应该有 StaticCode
				if !strings.Contains(result, "StaticCode:") {
					t.Errorf("expected StaticCode, got: %s", result)
				}
				// 应该有 Factory 函数
				if !strings.Contains(result, "Factory:") {
					t.Errorf("expected Factory function, got: %s", result)
				}
			},
		},
		{
			name: "button with dynamic text from signal",
			input: `package main
func test() {
	sig count = 0
	return <button text={count} />
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "gui.TemplateResult") {
					t.Errorf("expected gui.TemplateResult, got: %s", result)
				}
				// 应该有 Dynamic 数组
				if !strings.Contains(result, "Dynamic:") {
					t.Errorf("expected Dynamic array, got: %s", result)
				}
				// count 应该在 Dynamic 数组中
				if !strings.Contains(result, "count") {
					t.Errorf("expected count in Dynamic, got: %s", result)
				}
			},
		},
		{
			name: "div with text children",
			input: `package main
func test() {
	sig message = "Hello"
	return <div>{message}</div>
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "gui.TemplateResult") {
					t.Errorf("expected gui.TemplateResult, got: %s", result)
				}
				// 应该有 Dynamic 数组
				if !strings.Contains(result, "Dynamic:") {
					t.Errorf("expected Dynamic array, got: %s", result)
				}
			},
		},
		{
			name: "custom component",
			input: `package main
func Counter() {
	sig count = 0
	return <button text={count} />
}

func App() {
	return <Counter />
}`,
			validate: func(t *testing.T, result string) {
				// Counter 应该生成 TemplateResult
				if !strings.Contains(result, "gui.TemplateResult") {
					t.Errorf("expected gui.TemplateResult in Counter, got: %s", result)
				}
				// App 也应该生成 TemplateResult
				if !strings.Contains(result, "StaticCode: `<Counter>`") {
					t.Errorf("expected Counter StaticCode, got: %s", result)
				}
			},
		},
		{
			name: "nested components",
			input: `package main
func test() {
	sig count = 0
	return <div>
		<button text={count} />
		<button text="Increment" />
	</div>
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "gui.TemplateResult") {
					t.Errorf("expected gui.TemplateResult, got: %s", result)
				}
				// 应该有 StaticCode
				if !strings.Contains(result, "StaticCode:") {
					t.Errorf("expected StaticCode, got: %s", result)
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

// TestTransformTSXSignalIntegration 测试 Signal 与 TSX 的集成
func TestTransformTSXSignalIntegration(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		validate func(t *testing.T, result string)
	}{
		{
			name: "counter component with signal",
			input: `package main
func Counter() {
	sig count = 0
	return <button text={count} />
}`,
			validate: func(t *testing.T, result string) {
				// 应该有 sig 声明
				if !strings.Contains(result, "count := gox.New(0)") {
					t.Errorf("expected sig declaration, got: %s", result)
				}
				// 应该有 TemplateResult
				if !strings.Contains(result, "gui.TemplateResult") {
					t.Errorf("expected TemplateResult, got: %s", result)
				}
			},
		},
		{
			name: "multiple signals in component",
			input: `package main
func test() {
	sig a = 1
	sig b = 2
	return <div text={a} width={b} />
}`,
			validate: func(t *testing.T, result string) {
				// 应该有多个 sig 声明
				if !strings.Contains(result, "a := gox.New(1)") {
					t.Errorf("expected sig a declaration, got: %s", result)
				}
				if !strings.Contains(result, "b := gox.New(2)") {
					t.Errorf("expected sig b declaration, got: %s", result)
				}
				// 应该有 Dynamic 数组
				if !strings.Contains(result, "Dynamic:") {
					t.Errorf("expected Dynamic array, got: %s", result)
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
