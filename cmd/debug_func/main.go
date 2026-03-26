package main

import (
	"fmt"

	"github.com/gox-lang/gox/ast"
	"github.com/gox-lang/gox/lexer"
	"github.com/gox-lang/gox/parser"
)

func main() {
	src := `public func add(x: int, y: int): int {
    return x + y
}`
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
	for i, decl := range prog.Decls {
		fmt.Printf("Decl %d: %T\n", i, decl)
		if fd, ok := decl.(*ast.FuncDecl); ok {
			fmt.Printf("  Name: %q\n", fd.Name)
			fmt.Printf("  Params: %d\n", len(fd.Params))
			for j, param := range fd.Params {
				fmt.Printf("    Param %d: %s: %v\n", j, param.Name, param.Type)
			}
			fmt.Printf("  ReturnType: %v\n", fd.ReturnType)
		}
	}
	fmt.Println("Errors:", p.Errors())
}
