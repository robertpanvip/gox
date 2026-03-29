package main

import (
	"fmt"
	"os"

	"github.com/gox-lang/gox/lexer"
	"github.com/gox-lang/gox/parser"
	"github.com/gox-lang/gox/transformer"
)

func main() {
	// 测试后置自增和自减
	src := `import "github.com/gox-lang/gox/gui"

fx func Counter() {
    let count = 0
    
    return <div>
        <button text="Increment" onClick={func() {
            count++
        }} />
        <button text="Decrement" onClick={func() {
            count--
        }} />
    </div>
}`

	l := lexer.New(src)
	_ = l.Tokens()

	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		fmt.Println("=== Parser Errors ===")
		for _, err := range p.Errors() {
			fmt.Println(err)
		}
		os.Exit(1)
	}

	t := transformer.New()
	goCode := t.Transform(prog)

	fmt.Println("=== Generated Go Code ===")
	fmt.Println(goCode)

	// 检查生成的代码是否包含正确的运算符
	if contains(goCode, "count++") && contains(goCode, "count--") {
		fmt.Println("\n✅ SUCCESS: Both count++ and count-- are correctly generated!")
	} else {
		fmt.Println("\n❌ FAILURE: Operators not correctly generated!")
		if !contains(goCode, "count++") {
			fmt.Println("  - Missing count++")
		}
		if !contains(goCode, "count--") {
			fmt.Println("  - Missing count--")
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
