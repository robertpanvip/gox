package main

import (
	"flag"
	"fmt"
	"os"

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
	for _, decl := range prog.Decls {
		fmt.Printf("  - %T\n", decl)
	}
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
