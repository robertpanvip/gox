package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

func TestTransformer_StructLitNamedFields(t *testing.T) {
	src := `public struct User {
    public name: string
    public age: int
}

let u = User{name: "Alice", age: 25}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `User{Name: "Alice", Age: 25}`) {
		t.Error("expected struct literal with named fields, got:", result)
	}
}

func TestTransformer_StructLitPositional(t *testing.T) {
	src := `public struct Point {
    public x: int
    public y: int
}

let p = Point{10, 20}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `Point{10, 20}`) {
		t.Error("expected struct literal with positional fields, got:", result)
	}
}

func TestTransformer_StructLitMixed(t *testing.T) {
	src := `public struct Person {
    public name: string
    public age: int
    public email: string
}

let p = Person{name: "Bob", age: 30, email: "bob@example.com"}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `Person{Name: "Bob", Age: 30, Email: "bob@example.com"}`) {
		t.Error("expected struct literal with all fields, got:", result)
	}
}

func TestTransformer_StructLitWithExpressions(t *testing.T) {
	src := `public struct Rect {
    public x: int
    public y: int
    public width: int
    public height: int
}

let x = 10
let y = 20
let r = Rect{x: x, y: y, width: x + 100, height: y + 50}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `Rect`) {
		t.Error("expected Rect struct, got:", result)
	}
	if !strings.Contains(result, `x + 100`) {
		t.Error("expected expression in field, got:", result)
	}
}

func TestTransformer_StructLitEmpty(t *testing.T) {
	src := `public struct Empty {
}

let e = Empty{}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `Empty{}`) {
		t.Error("expected empty struct literal, got:", result)
	}
}
