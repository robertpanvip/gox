package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

func TestTransformer_ArrayType(t *testing.T) {
	src := `public func sum(arr: int[]): int {
    return 0
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "func Sum(arr []int) int") {
		t.Error("expected array type in output, got:", result)
	}
}

func TestTransformer_NestedArrayType(t *testing.T) {
	src := `public func process(matrix: int[][]): int {
    return 0
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "func Process(matrix [][]int) int") {
		t.Error("expected nested array type in output, got:", result)
	}
}

func TestTransformer_ArrayWithFunction(t *testing.T) {
	src := `public func map(arr: int[], fn: func(int): int): int[] {
    return arr
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "func Map(arr []int, fn func(int)int) []int") {
		t.Error("expected array and function type in output, got:", result)
	}
}
