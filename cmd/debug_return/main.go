package main

import (
	"fmt"

	"github.com/gox-lang/gox/ast"
	"github.com/gox-lang/gox/parser"
)

func main() {
	src := `package main

func add(x: int, y: int): int {
    return x + y
}`
	p := parser.New(src)
	prog := p.ParseProgram()

	fmt.Printf("Decls: %d\n", len(prog.Decls))
	for i, decl := range prog.Decls {
		fmt.Printf("Decl %d: %T\n", i, decl)
		if fd, ok := decl.(*ast.FuncDecl); ok {
			fmt.Printf("  Name: %q\n", fd.Name)
			if fd.Body != nil {
				fmt.Printf("  Body stmts: %d\n", len(fd.Body.List))
				for j, stmt := range fd.Body.List {
					fmt.Printf("    Stmt %d: %T\n", j, stmt)
					if rs, ok := stmt.(*ast.ReturnStmt); ok {
						fmt.Printf("      Result: %v (%T)\n", rs.Result, rs.Result)
					}
				}
			}
		}
	}
	fmt.Println("Errors:", p.Errors())
}
