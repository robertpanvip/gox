package main

import (
	"fmt"

	"github.com/gox-lang/gox/parser"
	"github.com/gox-lang/gox/transformer"
)

func main() {
	tests := []struct {
		name string
		src  string
	}{
		{"StringLiteral", `let name = "Alice"`},
		{"TemplateString", `let greeting = "Hello, ${name}!"`},
		{"Struct", `public struct User {
    name: string
    age: int
}`},
		{"PrintCall", `println("Hello, World!")`},
		{"PrintTemplate", `let name = "Alice"
println("Hello, ${name}!")`},
	}

	for _, tt := range tests {
		fmt.Printf("=== %s ===\n", tt.name)
		fmt.Printf("Source: %s\n\n", tt.src)

		p := parser.New(tt.src)
		prog := p.ParseProgram()

		if len(p.Errors()) > 0 {
			fmt.Printf("❌ Parser Errors: %v\n\n", p.Errors())
			continue
		}

		tfm := transformer.New()
		result := tfm.Transform(prog)

		fmt.Printf("✅ Generated Go:\n%s\n\n", result)
		fmt.Printf("---\n\n")
	}
}
