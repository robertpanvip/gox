package transformer

import (
	"strings"
	"testing"

	"github.com/gox-lang/gox/parser"
)

// TestTransformer_StringBasic tests basic string declaration and assignment
func TestTransformer_StringBasic(t *testing.T) {
	code := `
package main
import go "fmt"
public func Main() {
    let str1: string = "hello"
    let str2 = "world"
    fmt.Println(str1)
    fmt.Println(str2)
}
`
	p := parser.New(code)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parser failed: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, `str1 := "hello"`) {
		t.Error("expected string declaration, got:", result)
	}
	if !strings.Contains(result, `str2 := "world"`) {
		t.Error("expected string assignment, got:", result)
	}
}

// TestTransformer_StringConcatenation tests string concatenation with + operator
func TestTransformer_StringConcatenation(t *testing.T) {
	code := `
package main
import go "fmt"
public func Main() {
    let a = "hello"
    let b = "world"
    let c = a + " " + b + "!"
    fmt.Println(c)
}
`
	p := parser.New(code)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parser failed: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "a + \" \" + b + \"!\"") {
		t.Error("expected string concatenation, got:", result)
	}
}

// TestTransformer_StringTemplate tests template string interpolation
func TestTransformer_StringTemplate(t *testing.T) {
	code := `
package main
import go "fmt"
public func Main() {
    let name = "Alice"
    let age = 25
    let greeting = ` + "`" + `Hello, ${name}!` + "`" + `
    let info = ` + "`" + `Age: ${age}` + "`" + `
    fmt.Println(greeting)
    fmt.Println(info)
}
`
	p := parser.New(code)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parser failed: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	// Template strings should be converted to fmt.Sprintf
	if !strings.Contains(result, "fmt.Sprintf") {
		t.Error("expected fmt.Sprintf for template string, got:", result)
	}
	if !strings.Contains(result, "Hello, %v!") {
		t.Error("expected format string, got:", result)
	}
}

// TestTransformer_StringComparison tests string comparison operations
func TestTransformer_StringComparison(t *testing.T) {
	code := `package main
import "fmt"
public func Main() {
    let a = "hello"
    let b = "world"
    let c = "hello"
    if (a == c) {
        fmt.Println("equal")
    }
    if (a != b) {
        fmt.Println("not equal")
    }
}
`
	p := parser.New(code)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parser failed: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "if a == c") {
		t.Error("expected equality comparison, got:", result)
	}
	if !strings.Contains(result, "if a != b") {
		t.Error("expected inequality comparison, got:", result)
	}
}

// TestTransformer_StringInSwitch tests string values in switch statements
func TestTransformer_StringInSwitch(t *testing.T) {
	code := `
package main
import go "fmt"
public func Main() {
    let day = "Monday"
    switch (day) {
    case "Monday":
        fmt.Println("Start")
    case "Friday":
        fmt.Println("End")
    default:
        fmt.Println("Other")
    }
}
`
	p := parser.New(code)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parser failed: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "switch day") {
		t.Error("expected switch statement, got:", result)
	}
	if !strings.Contains(result, "case \"Monday\":") {
		t.Error("expected case with string, got:", result)
	}
}

// TestTransformer_StringArray tests arrays of strings
func TestTransformer_StringArray(t *testing.T) {
	code := `
package main
import go "fmt"
public func Main() {
    let fruits: string[] = ["apple", "banana", "orange"]
    let first = fruits[0]
    fmt.Println(fruits)
    fmt.Println(first)
}
`
	p := parser.New(code)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parser failed: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "[]interface{}") {
		t.Error("expected array type, got:", result)
	}
	if !strings.Contains(result, "\"apple\"") {
		t.Error("expected string in array, got:", result)
	}
}

// TestTransformer_StringFunctionParams tests strings as function parameters
func TestTransformer_StringFunctionParams(t *testing.T) {
	code := `
package main
import go "fmt"
public func greet(name: string): string {
    return "Hello, " + name
}
public func Main() {
    let msg = greet("Alice")
    fmt.Println(msg)
}
`
	p := parser.New(code)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parser failed: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "func Greet(name string) string") {
		t.Error("expected function with string parameter, got:", result)
	}
	if !strings.Contains(result, "return \"Hello, \" + name") {
		t.Error("expected string concatenation in return, got:", result)
	}
}

// TestTransformer_StringStructField tests strings in struct fields
func TestTransformer_StringStructField(t *testing.T) {
	code := `
package main
import go "fmt"
public struct Person {
    public name: string
    public email: string
}
public func Main() {
    let p = Person{name: "Alice", email: "alice@example.com"}
    fmt.Println(p.name)
}
`
	p := parser.New(code)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parser failed: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "Name string") {
		t.Error("expected string field in struct, got:", result)
	}
	if !strings.Contains(result, "Email string") {
		t.Error("expected string field in struct, got:", result)
	}
}

// TestTransformer_StringMethod tests string return from method
func TestTransformer_StringMethod(t *testing.T) {
	code := `
package main
import go "fmt"
public struct Person {
    public name: string
}
public func (p: Person) GetName(): string {
    return p.name
}
public func Main() {
    let person = Person{name: "Bob"}
    fmt.Println(person.GetName())
}
`
	p := parser.New(code)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parser failed: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "func (p Person) GetName() string") {
		t.Error("expected method with string return, got:", result)
	}
}

// TestTransformer_StringConstant tests string constants
func TestTransformer_StringConstant(t *testing.T) {
	code := `
package main
import go "fmt"
public const GREETING: string = "Hello"
public const NAME = "Gox"
public func Main() {
    fmt.Println(GREETING)
    fmt.Println(NAME)
}
`
	p := parser.New(code)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parser failed: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "const GREETING string = \"Hello\"") {
		t.Error("expected string constant, got:", result)
	}
	if !strings.Contains(result, "const NAME") {
		t.Error("expected constant without type, got:", result)
	}
}

// TestTransformer_StringInCondition tests strings in if conditions
func TestTransformer_StringInCondition(t *testing.T) {
	code := `
package main
import go "fmt"
public func Main() {
    let str = "hello"
    if str == "hello" {
        fmt.Println("matched")
    }
    if str != "world" {
        fmt.Println("not matched")
    }
}
`
	p := parser.New(code)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parser failed: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "if str == \"hello\"") {
		t.Error("expected string in if condition, got:", result)
	}
}

// TestTransformer_StringSpecialChars tests strings with special characters
func TestTransformer_StringSpecialChars(t *testing.T) {
	code := `
package main
import go "fmt"
public func Main() {
    let newline = "hello\nworld"
    let tab = "hello\tworld"
    let quote = "hello \"world\""
    fmt.Println(newline)
    fmt.Println(tab)
    fmt.Println(quote)
}
`
	p := parser.New(code)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parser failed: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "\"hello\\nworld\"") {
		t.Error("expected newline escape, got:", result)
	}
	if !strings.Contains(result, "\"hello\\tworld\"") {
		t.Error("expected tab escape, got:", result)
	}
}

// TestTransformer_StringEmpty tests empty string handling
func TestTransformer_StringEmpty(t *testing.T) {
	code := `
package main
import go "fmt"
public func Main() {
    let empty = ""
    if empty == "" {
        fmt.Println("empty")
    }
}
`
	p := parser.New(code)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parser failed: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "empty := \"\"") {
		t.Error("expected empty string, got:", result)
	}
}

// TestTransformer_StringMultipleConcat tests multiple concatenation operations
func TestTransformer_StringMultipleConcat(t *testing.T) {
	code := `
package main
import go "fmt"
public func Main() {
    let a = "Hello"
    let b = "World"
    let c = "!"
    let result = a + " " + b + c
    fmt.Println(result)
}
`
	p := parser.New(code)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		t.Fatalf("Parser failed: %v", p.Errors())
	}

	tfm := New()
	result := tfm.Transform(prog)

	if !strings.Contains(result, "a + \" \" + b + c") {
		t.Error("expected multiple concatenation, got:", result)
	}
}
