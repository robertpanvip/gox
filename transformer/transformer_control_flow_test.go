package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

func TestTransformer_IfElse(t *testing.T) {
	src := `package main
public func Main() {
if x > 10 {
    println("big")
} else {
    println("small")
}
}
`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `if x > 10`) {
		t.Error("expected if condition, got:", result)
	}
	if !strings.Contains(result, `else`) {
		t.Error("expected else, got:", result)
	}
}

func TestTransformer_While(t *testing.T) {
	src := `while i < 10 {
    i = i + 1
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// While should be converted to for
	if !strings.Contains(result, `for i < 10`) {
		t.Error("expected for loop (from while), got:", result)
	}
}

func TestTransformer_BreakContinue(t *testing.T) {
	src := `for i < 10 {
    if i == 5 {
        break
    }
    if i == 3 {
        continue
    }
    i = i + 1
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `break`) {
		t.Error("expected break, got:", result)
	}
	if !strings.Contains(result, `continue`) {
		t.Error("expected continue, got:", result)
	}
}

func TestTransformer_Switch(t *testing.T) {
	src := `switch x {
    case 1: {
        println("one")
    }
    case 2: {
        println("two")
    }
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `switch x`) {
		t.Error("expected switch, got:", result)
	}
	if !strings.Contains(result, `case 1:`) {
		t.Error("expected case 1, got:", result)
	}
	if !strings.Contains(result, `case 2:`) {
		t.Error("expected case 2, got:", result)
	}
}

func TestTransformer_When(t *testing.T) {
	src := `when x {
    case 1: {
        println("one")
    }
    case 2: {
        println("two")
    }
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `switch x`) {
		t.Error("expected switch (from when), got:", result)
	}
	if !strings.Contains(result, `case 1:`) {
		t.Error("expected case 1, got:", result)
	}
}

func TestTransformer_IfWithParentheses(t *testing.T) {
	src := `if (x > 10) {
    println("big")
} else {
    println("small")
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `if x > 10`) {
		t.Error("expected if condition, got:", result)
	}
	if !strings.Contains(result, `else`) {
		t.Error("expected else, got:", result)
	}
}

func TestTransformer_WhileWithParentheses(t *testing.T) {
	src := `while (i < 10) {
    i = i + 1
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// While should be converted to for
	if !strings.Contains(result, `for i < 10`) {
		t.Error("expected for loop (from while), got:", result)
	}
}

func TestTransformer_SwitchWithParentheses(t *testing.T) {
	src := `switch (x) {
    case 1: {
        println("one")
    }
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `switch x`) {
		t.Error("expected switch, got:", result)
	}
}
