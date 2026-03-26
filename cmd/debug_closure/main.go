package main

import (
	"fmt"

	"github.com/gox-lang/gox/ast"
	"github.com/gox-lang/gox/parser"
)

func main() {
	src := `let add = func(a: int, b: int): int { return a + b }`
	
	p := parser.New(src)
	prog := p.ParseProgram()

	fmt.Printf("Decls: %d\n", len(prog.Decls))
	for i, decl := range prog.Decls {
		fmt.Printf("Decl %d: %T\n", i, decl)
		if vd, ok := decl.(*ast.VarDecl); ok {
			fmt.Printf("  Name: %q\n", vd.Name)
			fmt.Printf("  Value: %T\n", vd.Value)
			if fl, ok := vd.Value.(*ast.FunctionLiteral); ok {
				fmt.Printf("    FunctionLiteral:\n")
				fmt.Printf("      Params: %d\n", len(fl.Params))
				for j, param := range fl.Params {
					fmt.Printf("        [%d] Name=%q Type=%v\n", j, param.Name, param.Type)
				}
				fmt.Printf("      ReturnType: %v (%T)\n", fl.ReturnType, fl.ReturnType)
				fmt.Printf("      IsArrow: %v\n", fl.IsArrow)
				if fl.Body != nil {
					fmt.Printf("      Body stmts: %d\n", len(fl.Body.List))
					for k, stmt := range fl.Body.List {
						fmt.Printf("        [%d] %T\n", k, stmt)
						if rs, ok := stmt.(*ast.ReturnStmt); ok {
							fmt.Printf("            Result: %v (%T)\n", rs.Result, rs.Result)
						}
					}
				}
			}
		}
	}
	fmt.Println("\nErrors:", p.Errors())
}
