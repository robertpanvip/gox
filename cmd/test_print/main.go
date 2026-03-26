package main

import (
	"fmt"
	"github.com/gox-lang/gox/parser"
	"github.com/gox-lang/gox/transformer"
)

func main() {
	testCases := []struct {
		name string
		src  string
	}{
		{
			name: "Basic print",
			src:  `print("Hello")`,
		},
		{
			name: "Basic println",
			src:  `println("World")`,
		},
		{
			name: "Template string with println",
			src:  `let name = "Alice"
println("Hello, ${name}!")`,
		},
		{
			name: "Multiple template strings",
			src:  `let x = 10
let y = 20
println("X: ${x}", "Y: ${y}")`,
		},
		{
			name: "Mixed args with template",
			src:  `let name = "Bob"
let age = 25
println("User:", name, "is ${age} years old")`,
		},
	}

	for _, tt := range testCases {
		fmt.Printf("=== %s ===\n", tt.name)
		fmt.Printf("Source:\n%s\n\n", tt.src)

		p := parser.New(tt.src)
		prog := p.ParseProgram()

		if len(p.Errors()) > 0 {
			fmt.Printf("Parser Errors: %v\n\n", p.Errors())
			continue
		}

		tfm := transformer.New()
		result := tfm.Transform(prog)

		fmt.Printf("Generated Go:\n%s\n", result)
		fmt.Printf("---\n\n")
	}
}
