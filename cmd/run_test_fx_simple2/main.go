package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/gox-lang/gox/lexer"
	"github.com/gox-lang/gox/parser"
	"github.com/gox-lang/gox/transformer"
)

func main() {
	src := `import "github.com/gox-lang/gox/gui"

fx func Counter() {
    let count = 0
    
    return <button text="Click" onClick={func() {
        count++
    }} />
}`

	fmt.Println("╔════════════════════════════════════════════════════════╗")
	fmt.Println("║        测试文件：test_fx_simple2.gox                  ║")
	fmt.Println("╚════════════════════════════════════════════════════════╝")
	fmt.Println()

	fmt.Println("【源代码】")
	fmt.Println(src)
	fmt.Println()

	// 词法分析
	l := lexer.New(src)
	_ = l.Tokens()

	// 语法分析
	p := parser.New(src)
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		fmt.Println("【解析器错误】")
		for _, err := range p.Errors() {
			fmt.Println("  ❌", err)
		}
		os.Exit(1)
	}

	fmt.Println("【解析结果】")
	fmt.Println("  ✅ 解析成功！")
	fmt.Println()

	// 代码转换
	t := transformer.New()
	goCode := t.Transform(prog)

	fmt.Println("【生成的 Go 代码】")
	fmt.Println(strings.Repeat("─", 60))
	fmt.Println(goCode)
	fmt.Println(strings.Repeat("─", 60))
	fmt.Println()

	// 验证
	fmt.Println("【验证结果】")
	
	checks := []struct {
		name string
		text string
	}{
		{"后置自增运算符", "Count++"},
		{"状态变量前缀", "c.Count++"},
		{"自动更新机制", "RequestUpdate()"},
	}
	
	allPassed := true
	for _, check := range checks {
		if strings.Contains(goCode, check.text) {
			fmt.Printf("  ✅ %-20s: %s\n", check.name, check.text)
		} else {
			fmt.Printf("  ❌ %-20s: 未找到 %s\n", check.name, check.text)
			allPassed = false
		}
	}
	
	fmt.Println()
	if allPassed {
		fmt.Println("╔════════════════════════════════════════════════════════╗")
		fmt.Println("║  🎉 成功：所有检查通过！修复已生效！                   ║")
		fmt.Println("╚════════════════════════════════════════════════════════╝")
	} else {
		fmt.Println("╔════════════════════════════════════════════════════════╗")
		fmt.Println("║  ❌ 失败：部分检查未通过！                            ║")
		fmt.Println("╚════════════════════════════════════════════════════════╝")
		os.Exit(1)
	}
}
