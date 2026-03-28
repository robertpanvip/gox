package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

func TestTransformer_VarDecl(t *testing.T) {
	src := `let a: int = 10`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	expected := "a := 10"
	if !strings.Contains(result, expected) {
		t.Errorf("expected %q in output, got: %s", expected, result)
	}
}

func TestTransformer_Func(t *testing.T) {
	src := `public func add(x: int, y: int): int { return x + y }`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "func Add(x int, y int) int") {
		t.Error("expected func Add in output, got:", result)
	}
	if !strings.Contains(result, "return x + y") {
		t.Error("expected return x + y in output, got:", result)
	}
}

func TestTransformer_Extend(t *testing.T) {
	src := `extend string { func hello(): string { return "hello" } }`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "func stringStringHello(self string) string") {
		t.Error("expected stringStringHello in output, got:", result)
	}
	if !strings.Contains(result, `return "hello"`) {
		t.Error("expected return \"hello\" in output, got:", result)
	}
}

func TestTransformer_PackageAndFunc(t *testing.T) {
	src := `package main

public func add(x: int, y: int): int { return x + y }`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "package main") {
		t.Error("expected package main in output, got:", result)
	}
	if !strings.Contains(result, "func Add(x int, y int) int") {
		t.Error("expected func Add in output, got:", result)
	}
}

func TestTransformer_MultiVarDecl(t *testing.T) {
	src := `let a: int = 10
let b: int = 20`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "a := 10") {
		t.Error("expected a := 10 in output, got:", result)
	}
	if !strings.Contains(result, "b := 20") {
		t.Error("expected b := 20 in output, got:", result)
	}
}

func TestTransformer_Closure(t *testing.T) {
	src := `let add = func(a: int, b: int): int { return a + b }`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "add := func(a int, b int) int") {
		t.Error("expected closure in output, got:", result)
	}
	if !strings.Contains(result, "return a + b") {
		t.Error("expected return a + b in output, got:", result)
	}
}

func TestTransformer_ClosureWithCapture(t *testing.T) {
	src := `let x = 10
let adder = func(y: int): int { return x + y }`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "x := 10") {
		t.Error("expected x := 10 in output, got:", result)
	}
	if !strings.Contains(result, "adder := func(y int) int") {
		t.Error("expected closure with captured variable, got:", result)
	}
	if !strings.Contains(result, "return x + y") {
		t.Error("expected return x + y in output, got:", result)
	}
}
