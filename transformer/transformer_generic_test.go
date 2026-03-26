package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

func TestTransformer_GenericFunction(t *testing.T) {
	src := `public func identity[T](x: T): T {
    return x
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "func Identity[T any](x T) T") {
		t.Error("expected generic function in output, got:", result)
	}
}

func TestTransformer_GenericWithConstraint(t *testing.T) {
	src := `public func print[T any](x: T) {
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "func Print[T any](x T)") {
		t.Error("expected generic with constraint in output, got:", result)
	}
}
