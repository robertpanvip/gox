package transformer

import (
	"fmt"
	"testing"

	"github.com/gox-lang/gox/ast"
	"github.com/gox-lang/gox/parser"
)

func TestTransformer_NestedStructDebug(t *testing.T) {
	src := `
package main

public struct Inner {
	public x: int
}

public struct Outer {
	public inner: Inner
}

public func test(o: Outer) {
	println("test")
}

public func Main() {
	test({inner: {x: 10}})
}
`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Logf("parser errors: %v", p.Errors())
	}

	// Print AST
	for _, decl := range prog.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name == "Main" {
			fmt.Printf("Main function body: %+v\n", fn.Body)
		}
	}

	tfm := New()
	result := tfm.Transform(prog)
	t.Logf("result: %s", result)
}
