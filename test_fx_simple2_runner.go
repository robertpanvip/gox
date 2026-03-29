package main

import (
	"fmt"
	"io/ioutil"
	"strings"
	"github.com/gox-lang/gox/parser"
	"github.com/gox-lang/gox/transformer"
)

func main() {
	fmt.Println("=== Testing test_fx_simple2.gox ===\n")
	
	// 读取测试文件
	src, err := ioutil.ReadFile("test/test_fx_simple2.gox")
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		return
	}
	
	fmt.Println("Source Code:")
	fmt.Println(string(src))
	fmt.Println("\n" + strings.Repeat("=", 60) + "\n")
	
	// 解析
	fmt.Println("Parsing...")
	p := parser.New(string(src))
	prog := p.ParseProgram()
	
	if len(p.Errors()) > 0 {
		fmt.Println("\nParser Errors:")
		for _, err := range p.Errors() {
			fmt.Printf("  - %v\n", err)
		}
		return
	}
	fmt.Println("✓ Parsing successful!")
	fmt.Println("\n" + strings.Repeat("=", 60) + "\n")
	
	// 转换
	fmt.Println("Transforming...")
	tfm := transformer.New()
	result := tfm.Transform(prog)
	
	fmt.Println("✓ Transformation successful!")
	fmt.Println("\n" + strings.Repeat("=", 60) + "\n")
	
	// 输出结果
	fmt.Println("Generated Go Code:")
	fmt.Println(result)
}
