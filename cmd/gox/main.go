package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/gox-lang/gox/ast"
	"github.com/gox-lang/gox/lexer"
	"github.com/gox-lang/gox/parser"
	"github.com/gox-lang/gox/transformer"
)

func main() {
	outputFile := flag.String("o", "", "Output file (default: print to stdout)")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Println("Usage: gox <source.gox> [-o output.go]")
		os.Exit(1)
	}

	src, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	l := lexer.New(string(src))
	tokens := l.Tokens()

	fmt.Println("=== Tokens ===")
	for _, tok := range tokens {
		fmt.Println(tok)
	}
	fmt.Println()

	p := parser.New(string(src))
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		fmt.Println("=== Parser Errors ===")
		for _, err := range p.Errors() {
			fmt.Println(err)
		}
		fmt.Println()
	}

	fmt.Println("=== AST ===")
	fmt.Printf("Program with %d declarations\n", len(prog.Decls))
	for i, decl := range prog.Decls {
		fmt.Printf("  [%d] Processing %T...\n", i, decl)
		if fn, ok := decl.(*ast.FuncDecl); ok {
			fmt.Printf("    ✓ Function: %s, Body=nil? %v\n", fn.Name, fn.Body == nil)
			if fn.Body == nil {
				fmt.Printf("      ERROR: Function body is nil!\n")
			} else {
				fmt.Fprintf(os.Stderr, "      Statements: %d\n", len(fn.Body.List))
				for j, stmt := range fn.Body.List {
					fmt.Fprintf(os.Stderr, "        [%d] Type: %T\n", j, stmt)
					// Check if it's VarDecl
					switch v := stmt.(type) {
					case *ast.VarDecl:
						fmt.Fprintf(os.Stderr, "            -> VarDecl: name=%s, Value=%T\n", v.Name, v.Value)
						switch val := v.Value.(type) {
						case *ast.TemplateString:
							fmt.Fprintf(os.Stderr, "               TemplateString: Parts=%v, Exprs=%d\n", val.Parts, len(val.Exprs))
						case *ast.StringLit:
							fmt.Fprintf(os.Stderr, "               StringLit: %s\n", val.Value)
						default:
							fmt.Fprintf(os.Stderr, "               Other: %T\n", val)
						}
					default:
						fmt.Fprintf(os.Stderr, "            -> Not VarDecl: %T\n", v)
						// Check if it's ExprStmt with CallExpr
						if es, ok := v.(*ast.ExprStmt); ok {
							fmt.Fprintf(os.Stderr, "               ExprStmt: %T\n", es.X)
							if ce, ok := es.X.(*ast.CallExpr); ok {
								fmt.Fprintf(os.Stderr, "               CallExpr with %d args\n", len(ce.Args))
								for k, arg := range ce.Args {
									fmt.Fprintf(os.Stderr, "                 Arg %d: %T\n", k, arg)
									if ts, ok := arg.(*ast.TemplateString); ok {
										fmt.Fprintf(os.Stderr, "                   TemplateString: Parts=%v\n", ts.Parts)
									} else if sl, ok := arg.(*ast.StringLit); ok {
										fmt.Fprintf(os.Stderr, "                   StringLit: %s\n", sl.Value)
									}
								}
							}
						}
					}
				}
			}
		} else {
			fmt.Printf("    ✗ Not a FuncDecl, it's %T\n", decl)
		}
	}
	fmt.Println("=== End AST ===")
	fmt.Println()

	t := transformer.New()
	goCode := t.Transform(prog)

	fmt.Println("=== Generated Go Code ===")
	fmt.Println(goCode)

	// Write to output file if specified
	if *outputFile != "" {
		err := os.WriteFile(*outputFile, []byte(goCode), 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error writing file: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Go code written to: %s\n", *outputFile)
	}
}
