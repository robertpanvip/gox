package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

func TestTransformer_StructBasic(t *testing.T) {
	src := `public struct User {
    public name: string
    private age: int
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Should contain struct definition
	if !strings.Contains(result, `type User struct`) {
		t.Error("expected struct type definition, got:", result)
	}
	if !strings.Contains(result, `Name string`) {
		t.Error("expected public field Name, got:", result)
	}
	if !strings.Contains(result, `age int`) {
		t.Error("expected private field age, got:", result)
	}
}

func TestTransformer_StructPrivate(t *testing.T) {
	src := `private struct internal {
    value: int
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Should contain lowercase struct name
	if !strings.Contains(result, `type internal struct`) {
		t.Error("expected lowercase struct name, got:", result)
	}
}

func TestTransformer_StructWithMultipleFields(t *testing.T) {
	src := `public struct Product {
    public id: int
    public name: string
    public price: float64
    private stock: int
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `type Product struct`) {
		t.Error("expected Product struct, got:", result)
	}
	if !strings.Contains(result, `Id int`) {
		t.Error("expected Id field, got:", result)
	}
	if !strings.Contains(result, `Name string`) {
		t.Error("expected Name field, got:", result)
	}
	if !strings.Contains(result, `Price float64`) {
		t.Error("expected Price field, got:", result)
	}
	if !strings.Contains(result, `stock int`) {
		t.Error("expected stock field, got:", result)
	}
}

func TestTransformer_StructWithNullableType(t *testing.T) {
	src := `public struct Person {
    public name: string?
    public email: string?
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Nullable types should be pointers
	if !strings.Contains(result, `Name *string`) {
		t.Error("expected nullable Name as pointer, got:", result)
	}
	if !strings.Contains(result, `Email *string`) {
		t.Error("expected nullable Email as pointer, got:", result)
	}
}

func TestTransformer_StructWithArrayType(t *testing.T) {
	src := `public struct Team {
    public members: string[]
    public scores: int[]
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `Members []string`) {
		t.Error("expected Members array field, got:", result)
	}
	if !strings.Contains(result, `Scores []int`) {
		t.Error("expected Scores array field, got:", result)
	}
}
