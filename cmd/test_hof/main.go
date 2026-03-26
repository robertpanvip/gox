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
		{"FuncAsParam", `public func apply(fn: func(int): int, x: int): int {
    return fn(x)
}`},
		{"FuncAsReturn", `public func makeAdder(x: int): func(int): int {
    return func(y: int): int => x + y
}`},
		{"ComplexHOF", `public func map(arr: int[], fn: func(int): int): int[] {
    return arr
}`},
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
}
