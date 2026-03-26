package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

func TestTransformer_FunctionAsParameter(t *testing.T) {
	src := `public func apply(fn: func(int): int, x: int): int {
    return fn(x)
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "func Apply(fn func(int)int, x int) int") {
		t.Error("expected function as parameter in output, got:", result)
	}
	if !strings.Contains(result, "return fn(x)") {
		t.Error("expected fn(x) call in output, got:", result)
	}
}

func TestTransformer_FunctionAsReturnType(t *testing.T) {
	src := `public func makeAdder(x: int): func(int): int {
    return func(y: int): int => x + y
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "func MakeAdder(x int) func(int)int") {
		t.Error("expected function as return type in output, got:", result)
	}
	if !strings.Contains(result, "return func(y int) int { return x + y }") {
		t.Error("expected closure return in output, got:", result)
	}
}

func TestTransformer_ClosureReturn(t *testing.T) {
	src := `public func makeAdder(): func(int): int {
    return func(x: int): int => x + 1
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "func MakeAdder() func(int)int") {
		t.Error("expected function returning closure in output, got:", result)
	}
	if !strings.Contains(result, "return func(x int) int { return x + 1 }") {
		t.Error("expected closure return in output, got:", result)
	}
}
