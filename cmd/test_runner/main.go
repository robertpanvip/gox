package main

import (
	"fmt"
	"os"

	"github.com/gox-lang/gox/parser"
	"github.com/gox-lang/gox/transformer"
)

func main() {
	tests := []struct {
		name string
		src  string
	}{
		{"VarDecl", "let a: int = 10"},
		{"Func", "public func add(x: int, y: int): int { return x + y }"},
		{"Struct", "public struct User { name: string age: int }"},
		{"Closure", "let add = func(a: int, b: int): int { return a + b }"},
		{"ArrowFunc", "let add = func(a: int, b: int): int => a + b"},
		{"Extend", "extend string { func hello(): string { return \"hello\" } }"},
	}

	for _, tt := range tests {
		fmt.Printf("=== %s ===\n", tt.name)
		fmt.Printf("Source: %s\n\n", tt.src)

		p := parser.New(tt.src)
		prog := p.ParseProgram()

		if len(p.Errors()) > 0 {
			fmt.Printf("Parser Errors: %v\n\n", p.Errors())
			continue
		}

		tfm := transformer.New()
		result := tfm.Transform(prog)

		fmt.Printf("Generated Go:\n%s\n\n", result)
		fmt.Printf("---\n\n")
	}

	os.Exit(0)
}
