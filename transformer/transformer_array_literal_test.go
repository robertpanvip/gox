package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

func TestTransformer_ArrayLiteral(t *testing.T) {
	src := `let numbers = [1, 2, 3]`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "numbers := []interface{}{1, 2, 3}") {
		t.Error("expected array literal in output, got:", result)
	}
}

func TestTransformer_EmptyArray(t *testing.T) {
	src := `let arr: int[] = []`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "arr := []interface{}{}") {
		t.Error("expected empty array in output, got:", result)
	}
}

func TestTransformer_ArrayExtension(t *testing.T) {
	src := `extend int[] {
    public func map(fn: func(int): int): int[] {
        return self
    }
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// 扩展方法被注册到 extendFuncs，不直接生成代码
	// 这是预期的行为
	_ = result // 忽略结果，只要不 panic 即可
}

func TestTransformer_ArrayMethodCall(t *testing.T) {
	src := `let numbers: int[] = []
let result = numbers.map(func(x: int): int => x * 2)`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "numbers := []interface{}{}") {
		t.Error("expected empty array in output, got:", result)
	}
	if !strings.Contains(result, "result := numbers.map(func(x int) int { return x * 2 })") {
		t.Error("expected method call in output, got:", result)
	}
}

func TestTransformer_CompleteArrayExample(t *testing.T) {
	src := `extend int[] {
    public func map(fn: func(int): int): int[] {
        return self
    }
}

let numbers = [1, 2, 3]
let doubled = numbers.map(func(x: int): int => x * 2)`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "numbers := []interface{}{1, 2, 3}") {
		t.Error("expected array literal in output, got:", result)
	}
	if !strings.Contains(result, "doubled := numbers.map(func(x int) int { return x * 2 })") {
		t.Error("expected method call in output, got:", result)
	}
}

func TestTransformer_ArrayWithString(t *testing.T) {
	src := `let names = ["Alice", "Bob", "Charlie"]`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `names := []interface{}{"Alice", "Bob", "Charlie"}`) {
		t.Error("expected string array in output, got:", result)
	}
}

func TestTransformer_NestedArrayLiteral(t *testing.T) {
	src := `let matrix = [[1, 2], [3, 4]]`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "matrix := []interface{}{[]interface{}{1, 2}, []interface{}{3, 4}}") {
		t.Error("expected nested array in output, got:", result)
	}
}
