package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

func TestTransformer_StructMixed(t *testing.T) {
	src := `public struct Base {
    public value: int
}

public struct Derived mixed Base {
    public name: string
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Should contain embedded struct
	if !strings.Contains(result, `type Base struct`) {
		t.Error("expected Base struct, got:", result)
	}
	if !strings.Contains(result, `type Derived struct`) {
		t.Error("expected Derived struct, got:", result)
	}
	if !strings.Contains(result, "Base\n") {
		t.Error("expected embedded Base in Derived, got:", result)
	}
}

func TestTransformer_StructMixedMultiple(t *testing.T) {
	src := `public struct A {
    public a: int
}

public struct B {
    public b: string
}

public struct C mixed A mixed B {
    public c: float64
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Should contain all embedded structs
	if !strings.Contains(result, `type A struct`) {
		t.Error("expected A struct, got:", result)
	}
	if !strings.Contains(result, `type B struct`) {
		t.Error("expected B struct, got:", result)
	}
	if !strings.Contains(result, `type C struct`) {
		t.Error("expected C struct, got:", result)
	}
}

func TestTransformer_InterfaceBasic(t *testing.T) {
	src := `public interface Writer {
    public func Write(data: string): int
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Should contain interface definition
	if !strings.Contains(result, `type Writer interface`) {
		t.Error("expected Writer interface, got:", result)
	}
	if !strings.Contains(result, `Write(string) int`) {
		t.Error("expected Write method signature, got:", result)
	}
}

func TestTransformer_InterfaceMultipleMethods(t *testing.T) {
	src := `public interface ReadWriter {
    public func Read(): string
    public func Write(data: string): int
    public func Close()
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Should contain all method signatures
	if !strings.Contains(result, `type ReadWriter interface`) {
		t.Error("expected ReadWriter interface, got:", result)
	}
	if !strings.Contains(result, `Read() string`) {
		t.Error("expected Read method, got:", result)
	}
	if !strings.Contains(result, `Write(string) int`) {
		t.Error("expected Write method, got:", result)
	}
	if !strings.Contains(result, `Close()`) {
		t.Error("expected Close method, got:", result)
	}
}

func TestTransformer_InterfacePrivate(t *testing.T) {
	src := `private interface internal {
    func getValue(): int
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Should contain lowercase interface name
	if !strings.Contains(result, `type internal interface`) {
		t.Error("expected lowercase internal interface, got:", result)
	}
}

func TestTransformer_StructMixedWithMethods(t *testing.T) {
	src := `public struct Base {
    public value: int
}

public func (b: Base) GetValue(): int {
    return b.value
}

public struct Derived mixed Base {
    public name: string
}

public func (d: Derived) GetName(): string {
    return d.name
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Should contain embedded struct and methods
	if !strings.Contains(result, "Base\n") {
		t.Error("expected embedded Base, got:", result)
	}
	if !strings.Contains(result, `func (b Base) GetValue() int`) {
		t.Error("expected Base method, got:", result)
	}
	if !strings.Contains(result, `func (d Derived) GetName() string`) {
		t.Error("expected Derived method, got:", result)
	}
}
