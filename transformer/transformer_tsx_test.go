package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

func TestTransformer_TSXBasic(t *testing.T) {
	src := `<View />`
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
	src := `<View id="1" class="container" />`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `View(ViewProps{ID: "1", Class: "container"})`) {
		t.Error("expected TSX with attributes, got:", result)
	}
}

func TestTransformer_TSXWithChildren(t *testing.T) {
	src := `<View>
    <Text>Hello</Text>
</View>`
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
	src := `<View id="app">
    <Header title="My App" />
    <Content>
        <Text>Hello</Text>
    </Content>
</View>`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `View(ViewProps{ID: "app"}`) {
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
	src := `let name = "World"
<View>{name}</View>`
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `View(ViewProps{}, name)`) {
		t.Error("expected TSX with expression, got:", result)
	}
}

func TestTransformer_TSXBooleanAttribute(t *testing.T) {
	src := `<Input disabled />`
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
