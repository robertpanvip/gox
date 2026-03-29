package main

import (
	"fmt"
	"io/ioutil"

	"github.com/gox-lang/gox/parser"
	"github.com/gox-lang/gox/transformer"
)

func main() {
	// 读取 TSX GUI 演示文件
	code, err := ioutil.ReadFile("test/tsx_gui_demo.gox")
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		return
	}

	fmt.Println("=== TSX GUI Source Code ===")
	fmt.Printf("%s\n\n", string(code))

	// 解析
	p := parser.New(string(code))
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		fmt.Printf("Parser errors: %v\n", p.Errors())
		return
	}

	fmt.Println("✓ Parsing Successful\n")

	// 转换
	tfm := transformer.New()
	result := tfm.Transform(prog)

	fmt.Println("=== Transformed Go Code (Ready to Use) ===")
	fmt.Printf("%s\n", result)

	fmt.Println("\n✓ TSX to Go conversion successful!")
	fmt.Println("The generated Go code can be used with any Go GUI library.")
}
