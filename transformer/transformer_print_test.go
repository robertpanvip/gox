package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

func TestTransformer_PrintBasic(t *testing.T) {
	src := `print("Hello")`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `print("Hello")`) {
		t.Error("expected basic print call, got:", result)
	}
}

func TestTransformer_PrintlnBasic(t *testing.T) {
	src := `println("World")`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `println("World")`) {
		t.Error("expected basic println call, got:", result)
	}
}

func TestTransformer_PrintlnTemplateString(t *testing.T) {
	src := `package main
public func Main() {
let name = "Alice"
println("Hello, ${name}!")
}
`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Should contain fmt.Sprintln with fmt.Sprintf
	if !strings.Contains(result, `fmt.Sprintln`) {
		t.Error("expected fmt.Sprintln, got:", result)
	}
	if !strings.Contains(result, `fmt.Sprintf("Hello, %v!", name)`) {
		t.Error("expected fmt.Sprintf for template string, got:", result)
	}
}

func TestTransformer_PrintTemplateString(t *testing.T) {
	src := `let age = 25
print("Age: ${age}")`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Should contain fmt.Sprint with fmt.Sprintf
	if !strings.Contains(result, `fmt.Sprint`) {
		t.Error("expected fmt.Sprint, got:", result)
	}
	if !strings.Contains(result, `fmt.Sprintf("Age: %v", age)`) {
		t.Error("expected fmt.Sprintf for template string, got:", result)
	}
}

func TestTransformer_PrintlnMultipleTemplateStrings(t *testing.T) {
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

	// Should contain fmt.Sprintln with two fmt.Sprintf calls
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

func TestTransformer_PrintlnMixedArgs(t *testing.T) {
	src := `let name = "Bob"
let age = 25
println("User:", name, "is ${age} years old")`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Should contain fmt.Sprintln with mixed args
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
		t.Error("expected fmt.Sprintf for template string, got:", result)
	}
}

func TestTransformer_PrintlnAddsImport(t *testing.T) {
	src := `println("Hello, ${name}!")`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Should add fmt import
	if !strings.Contains(result, `import "fmt"`) {
		t.Error("expected fmt import, got:", result)
	}
}
