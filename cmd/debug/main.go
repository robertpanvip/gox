package main

import (
	"fmt"

	"github.com/gox-lang/gox/ast"
	"github.com/gox-lang/gox/lexer"
	"github.com/gox-lang/gox/parser"
)

func main() {
	src := `let a: int = 10`
	fmt.Println("=== Tokens ===")
	l := lexer.New(src)
	tokens := l.Tokens()
	for i, tok := range tokens {
		fmt.Printf("[%d] Token: %v (%d) Literal: %q\n", i, tok.Kind, tok.Kind, tok.Literal)
	}

	fmt.Println("\n=== Parsing ===")
	p := parser.New(src)
	prog := p.ParseProgram()

	fmt.Printf("Decls: %d\n", len(prog.Decls))
	if len(prog.Decls) > 0 {
		if vd, ok := prog.Decls[0].(*ast.VarDecl); ok {
			fmt.Printf("VarDecl: name=%q, type=%v, value=%v\n", vd.Name, vd.Type, vd.Value)
		}
	}
	fmt.Println("Errors:", p.Errors())
}
