package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

func TestTransformer_PrintlnDoubleQuoteTemplate(t *testing.T) {
	src := `public func Main() {
let name = "Alice"
println("Hello, ${name}!")
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Should convert to fmt.Sprintln with fmt.Sprintf
	if !strings.Contains(result, `fmt.Sprintln`) {
		t.Error("expected fmt.Sprintln, got:", result)
	}
	if !strings.Contains(result, `fmt.Sprintf("Hello, %v!", name)`) {
		t.Error("expected fmt.Sprintf for template, got:", result)
	}
}

func TestTransformer_PrintDoubleQuoteTemplate(t *testing.T) {
	src := `let age = 25
print("Age: ${age}")`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `fmt.Sprint`) {
		t.Error("expected fmt.Sprint, got:", result)
	}
	if !strings.Contains(result, `fmt.Sprintf("Age: %v", age)`) {
		t.Error("expected fmt.Sprintf for template, got:", result)
	}
}

func TestTransformer_PrintlnDoubleQuoteMultipleTemplates(t *testing.T) {
	src := `let x = 100
let y = 200
println("X: ${x}", "Y: ${y}")`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `fmt.Sprintln`) {
		t.Error("expected fmt.Sprintln, got:", result)
	}
	if !strings.Contains(result, `fmt.Sprintf("X: %v", x)`) {
		t.Error("expected first fmt.Sprintf, got:", result)
	}
	if !strings.Contains(result, `fmt.Sprintf("Y: %v", y)`) {
		t.Error("expected second fmt.Sprintf, got:", result)
	}
}

func TestTransformer_PrintlnMixedDoubleQuoteAndBacktick(t *testing.T) {
	src := `let name = "Bob"
let age = 25
println("User:", name, ` + "`is ${age} years old`" + `)`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `fmt.Sprintln`) {
		t.Error("expected fmt.Sprintln, got:", result)
	}
	if !strings.Contains(result, `"User:"`) {
		t.Error("expected string literal, got:", result)
	}
	if !strings.Contains(result, `name`) {
		t.Error("expected name variable, got:", result)
	}
	if !strings.Contains(result, `fmt.Sprintf("is %v years old", age)`) {
		t.Error("expected fmt.Sprintf for backtick template, got:", result)
	}
}

func TestTransformer_PrintlnDoubleQuoteNoTemplate(t *testing.T) {
	src := `println("Hello World")`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Should be regular println, not fmt.Sprintf
	if strings.Contains(result, `fmt.Sprintf`) {
		t.Error("should not use fmt.Sprintf for regular string, got:", result)
	}
	if !strings.Contains(result, `println("Hello World")`) {
		t.Error("expected regular println, got:", result)
	}
}
