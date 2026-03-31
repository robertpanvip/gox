package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

// TestTransformTSXLitHTML lit-html 风格的 TSX 转换测试（使用普通 func）
func TestTransformTSXLitHTML(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		validate func(t *testing.T, result string)
	}{
		{
			name: "simple div with static text",
			input: `package main
func test() {
	return <div text="Hello" />
}`,
			validate: func(t *testing.T, result string) {
				// 应该生成 gui.NewDiv
				if !strings.Contains(result, "gui.NewDiv") {
					t.Errorf("expected gui.NewDiv, got: %s", result)
				}
			},
		},
		{
			name: "div with dynamic text from signal",
			input: `package main
func test() {
	sig count = 0
	return <div text={count} />
}`,
			validate: func(t *testing.T, result string) {
				// 应该生成 gui.NewDiv
				if !strings.Contains(result, "gui.NewDiv") {
					t.Errorf("expected gui.NewDiv, got: %s", result)
				}
				// count 应该使用 .Get()
				if !strings.Contains(result, "count.Get()") {
					t.Errorf("expected count.Get(), got: %s", result)
				}
			},
		},
		{
			name: "button with dynamic text",
			input: `package main
func test() {
	sig count = 0
	return <button text={count} />
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "gui.NewButton") {
					t.Errorf("expected gui.NewButton, got: %s", result)
				}
				// count 应该使用 .Get()
				if !strings.Contains(result, "count.Get()") {
					t.Errorf("expected count.Get(), got: %s", result)
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
				if !strings.Contains(result, "gui.NewDiv") {
					t.Errorf("expected gui.NewDiv, got: %s", result)
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
				if !strings.Contains(result, "gui.NewDiv") {
					t.Errorf("expected gui.NewDiv, got: %s", result)
				}
				if !strings.Contains(result, "gui.NewButton") {
					t.Errorf("expected gui.NewButton, got: %s", result)
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
				// Counter 应该生成 gui.NewButton
				if !strings.Contains(result, "gui.NewButton") {
					t.Errorf("expected gui.NewButton in Counter, got: %s", result)
				}
				// App 应该调用 Counter
				if !strings.Contains(result, "Counter(") {
					t.Errorf("expected Counter call, got: %s", result)
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

// TestTransformTSXDynamicParts 测试动态部分（Part 系统）
func TestTransformTSXDynamicParts(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		validate func(t *testing.T, result string)
	}{
		{
			name: "multiple dynamic values",
			input: `package main
func test() {
	sig a = 1
	sig b = 2
	return <div text={a} width={b} />
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "gui.NewDiv") {
					t.Errorf("expected gui.NewDiv, got: %s", result)
				}
				// 应该有多个动态值
				if !strings.Contains(result, "a.Get()") {
					t.Errorf("expected a.Get(), got: %s", result)
				}
				if !strings.Contains(result, "b.Get()") {
					t.Errorf("expected b.Get(), got: %s", result)
				}
			},
		},
		{
			name: "dynamic expression",
			input: `package main
func test() {
	sig count = 0
	return <div text={count + 1} />
}`,
			validate: func(t *testing.T, result string) {
				if !strings.Contains(result, "gui.NewDiv") {
					t.Errorf("expected gui.NewDiv, got: %s", result)
				}
				// 应该有表达式
				if !strings.Contains(result, "count.Get() + 1") {
					t.Errorf("expected expression, got: %s", result)
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
