package parser

import (
	"testing"
)

func TestParser_SimpleStruct(t *testing.T) {
	src := `struct User { name: string age: int }`
	p := New(src)
	prog := p.ParseProgram()

	if len(prog.Decls) != 1 {
		t.Errorf("expected 1 decl, got %d", len(prog.Decls))
	}
}

func TestParser_SimpleFunc(t *testing.T) {
	src := `public func add(x: int, y: int): int { return x + y }`
	p := New(src)
	prog := p.ParseProgram()

	if len(prog.Decls) != 1 {
		t.Errorf("expected 1 decl, got %d", len(prog.Decls))
	}
}

func TestParser_VarDecl(t *testing.T) {
	src := `let a: int = 10`
	p := New(src)
	prog := p.ParseProgram()

	if len(prog.Decls) != 1 {
		t.Errorf("expected 1 decl, got %d", len(prog.Decls))
	}

	if len(p.Errors()) > 0 {
		t.Errorf("parser errors: %v", p.Errors())
	}
}

func TestParser_Extend(t *testing.T) {
	src := `extend string { func hello(): string { return "hello" } }`
	p := New(src)
	prog := p.ParseProgram()
	_ = prog

	if len(p.Errors()) > 0 {
		t.Errorf("parser errors: %v", p.Errors())
	}
}
