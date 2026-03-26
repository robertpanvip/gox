package main

import (
	"fmt"

	"github.com/gox-lang/gox/ast"
	"github.com/gox-lang/gox/parser"
)

func main() {
	src := `package main

let result: int = add(a, b)`
	p := parser.New(src)
	prog := p.ParseProgram()

	fmt.Printf("Decls: %d\n", len(prog.Decls))
	for i, decl := range prog.Decls {
		fmt.Printf("Decl %d: %T\n", i, decl)
		if vd, ok := decl.(*ast.VarDecl); ok {
			fmt.Printf("  Name: %q\n", vd.Name)
			fmt.Printf("  Value: %v (%T)\n", vd.Value, vd.Value)
			if ce, ok := vd.Value.(*ast.CallExpr); ok {
				fmt.Printf("    Fun: %v\n", ce.Fun)
				fmt.Printf("    Args: %d\n", len(ce.Args))
				for j, arg := range ce.Args {
					fmt.Printf("      Arg %d: %v (%T)\n", j, arg, arg)
				}
			}
		}
	}
	fmt.Println("Errors:", p.Errors())
}
