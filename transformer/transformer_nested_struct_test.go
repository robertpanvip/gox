package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

func TestTransformer_NestedStructLiteral(t *testing.T) {
	src := `
package main

public struct Address {
	public city: string
}

public struct Person {
	public name: string
	public address: Address
}

public func show(p: Person) {
	println(p.name)
}

public func Main() {
	show({name: "Alice", address: {city: "Beijing"}})
}
`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Logf("parser errors (expected): %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Check if nested struct literal is inferred
	if !strings.Contains(result, `Person{`) {
		t.Error("expected outer struct literal, got:", result)
	}
	if !strings.Contains(result, `Address{`) {
		t.Error("expected inner struct literal (Address), got:", result)
	}
}
