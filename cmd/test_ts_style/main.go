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
		{"ArrayLiteral", `let numbers = [1, 2, 3]`},
		{"ExtendArray", `extend int[] {
    public func map(fn: func(int): int): int[] {
        return self
    }
}`},
		{"ArrayMethodCall", `let numbers: int[] = []
let result = numbers.map(func(x: int): int => x * 2)`},
		{"CompleteExample", `extend int[] {
    public func map(fn: func(int): int): int[] {
        return self
    }
}

let numbers = [1, 2, 3]
let doubled = numbers.map(func(x: int): int => x * 2)`},
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
