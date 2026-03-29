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
						case *ast.TSXElement:
							fmt.Fprintf(os.Stderr, "               TSXElement: tag=%s, Children=%d\n", val.TagName, len(val.Children))
							for k, child := range val.Children {
								fmt.Fprintf(os.Stderr, "                 Child[%d]: %T\n", k, child)
								if childTSX, ok := child.(*ast.TSXElement); ok {
									fmt.Fprintf(os.Stderr, "                   TSXElement: tag=%s, Children=%d\n", childTSX.TagName, len(childTSX.Children))
								}
							}
						case *ast.TemplateString:
							fmt.Fprintf(os.Stderr, "               TemplateString: Parts=%v, Exprs=%d\n", val.Parts, len(val.Exprs))
						case *ast.StringLit:
							fmt.Fprintf(os.Stderr, "               StringLit: %s\n", val.Value)
						default:
							fmt.Fprintf(os.Stderr, "               Other: %T\n", val)
						}
					case *ast.ExprStmt:
						fmt.Fprintf(os.Stderr, "            -> ExprStmt: %T\n", v.X)
						if call, ok := v.X.(*ast.CallExpr); ok {
							fmt.Fprintf(os.Stderr, "               CallExpr: Func=%T, Args=%d\n", call.Fun, len(call.Args))
							for k, arg := range call.Args {
								fmt.Fprintf(os.Stderr, "                 Arg[%d]: %T\n", k, arg)
								if tsx, ok := arg.(*ast.TSXElement); ok {
									fmt.Fprintf(os.Stderr, "                   TSXElement: tag=%s, Children=%d\n", tsx.TagName, len(tsx.Children))
								}
							}
						}
					default:
						fmt.Fprintf(os.Stderr, "            -> Not VarDecl: %T\n", v)
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
