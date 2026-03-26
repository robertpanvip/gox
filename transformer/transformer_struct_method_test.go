package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

func TestTransformer_StructMethod(t *testing.T) {
	src := `public struct User {
    public name: string
    private age: int
}

public func (u: User) GetName(): string {
    return u.name
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Should contain receiver method
	if !strings.Contains(result, `func (u User) GetName() string`) {
		t.Error("expected receiver method, got:", result)
	}
}

func TestTransformer_StructMethodMultiple(t *testing.T) {
	src := `public struct User {
    public name: string
}

public func (u: User) GetName(): string {
    return u.name
}

public func (u: User) SetName(name: string) {
    u.name = name
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `func (u User) GetName() string`) {
		t.Error("expected GetName method, got:", result)
	}
	if !strings.Contains(result, `func (u User) SetName(name string)`) {
		t.Error("expected SetName method, got:", result)
	}
}

func TestTransformer_StructMethodWithPointer(t *testing.T) {
	src := `public struct Counter {
    value: int
}

public func (c: Counter) Increment() {
    c.value = c.value + 1
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `func (c Counter) Increment()`) {
		t.Error("expected Increment method, got:", result)
	}
}

func TestTransformer_TemplateStringES6(t *testing.T) {
	src := "let greeting = `Hello, ${name}!`"
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Should convert to fmt.Sprintf
	if !strings.Contains(result, `fmt.Sprintf("Hello, %v!", name)`) {
		t.Error("expected fmt.Sprintf, got:", result)
	}
}

func TestTransformer_TemplateStringMultipleExpressions(t *testing.T) {
	src := "let message = `The value of ${x} and ${y} is ${result}`"
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `fmt.Sprintf("The value of %v and %v is %v", x, y, result)`) {
		t.Error("expected multiple expressions, got:", result)
	}
}

func TestTransformer_TemplateStringWithPrint(t *testing.T) {
	src := "let name = \"Alice\"\nprintln(`Hello, ${name}!`)"
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Should use fmt.Sprintln with fmt.Sprintf
	if !strings.Contains(result, `fmt.Sprintln`) {
		t.Error("expected fmt.Sprintln, got:", result)
	}
	if !strings.Contains(result, `fmt.Sprintf("Hello, %v!", name)`) {
		t.Error("expected fmt.Sprintf, got:", result)
	}
}
