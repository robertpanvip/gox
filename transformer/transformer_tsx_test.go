package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

func TestTransformer_TSXBasic(t *testing.T) {
	src := `package main
public func Main() {
<View />
}
`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `View(ViewProps{})`) {
		t.Error("expected TSX element, got:", result)
	}
}

func TestTransformer_TSXWithAttributes(t *testing.T) {
	src := `package main
public func Main() {
<View id="1" class="container" />
}
`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `View(ViewProps{Id: "1", Class: "container"})`) {
		t.Error("expected TSX with attributes, got:", result)
	}
}

func TestTransformer_TSXWithChildren(t *testing.T) {
	src := `package main
public func Main() {
<View>
    <Text>Hello</Text>
</View>
}
`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `View(ViewProps{}`) {
		t.Error("expected View component, got:", result)
	}
	if !strings.Contains(result, `Text(TextProps{}, "Hello")`) {
		t.Error("expected Text with children, got:", result)
	}
}

func TestTransformer_TSXNested(t *testing.T) {
	src := `package main
public func Main() {
<View id="app">
    <Header title="My App" />
    <Content>
        <Text>Hello</Text>
    </Content>
</View>
}
`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `View(ViewProps{Id: "app"}`) {
		t.Error("expected nested View, got:", result)
	}
	if !strings.Contains(result, `Header(HeaderProps{Title: "My App"})`) {
		t.Error("expected Header, got:", result)
	}
	if !strings.Contains(result, `Content(ContentProps{}`) {
		t.Error("expected Content, got:", result)
	}
}

func TestTransformer_TSXWithExpression(t *testing.T) {
	src := `package main
public func Main() {
let name = "World"
<View>{name}</View>
}
`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `View(ViewProps{}, name)`) {
		t.Errorf("expected TSX with expression, got: %s", result)
	}
}

func TestTransformer_TSXBooleanAttribute(t *testing.T) {
	src := `package main
public func Main() {
<Input disabled />
}
`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `Input(InputProps{Disabled: true})`) {
		t.Error("expected boolean attribute, got:", result)
	}
}
