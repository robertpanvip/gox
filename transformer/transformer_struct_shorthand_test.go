package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

func TestTransformer_StructLiteralShorthand(t *testing.T) {
	src := `
package main

public struct ViewProps {
	public id: string
	public class: string
}

public func render(props: ViewProps) {
	println("Rendering: " + props.id)
}

public func Main() {
	render({id: "app", class: "main"})
}
`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `render(ViewProps{Id: "app", Class: "main"})`) {
		t.Error("expected struct literal shorthand to be expanded, got:", result)
	}
}

func TestTransformer_StructLiteralShorthandMultiple(t *testing.T) {
	src := `
package main

public struct Config {
	public name: string
	public value: int
}

public struct ViewProps {
	public id: string
}

public func create(item: Config, title: string, settings: ViewProps) {
	println("Create")
}

public func Main() {
	create(Config{name: "test", value: 42}, "My Title", ViewProps{id: "view1"})
}
`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `create(Config{Name: "test", Value: 42}, "My Title", ViewProps{Id: "view1"})`) {
		t.Error("expected multiple struct literal shorthands to be expanded, got:", result)
	}
}
