package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"

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

	fmt.Println("=== Source Code ===")
	fmt.Printf("%s\n\n", string(code))

	// 解析
	p := parser.New(string(code))
	prog := p.ParseProgram()

	if len(p.Errors()) > 0 {
		fmt.Printf("Parser errors: %v\n", p.Errors())
		for _, err := range p.Errors() {
			fmt.Printf("  Error: %s\n", err)
		}
		return
	}

	fmt.Println("✓ Parsing Successful\n")

	// 转换
	tfm := transformer.New()
	result := tfm.Transform(prog)

	fmt.Println("=== Transformed Go Code ===")
	fmt.Printf("%s\n\n", result)

	// 保存转换后的代码为 Go 文件
	outputFile := "test/tsx_gui_demo_output.go"
	err = ioutil.WriteFile(outputFile, []byte(result), 0644)
	if err != nil {
		fmt.Printf("Error writing output file: %v\n", err)
		return
	}
	fmt.Printf("✓ Saved to %s\n\n", outputFile)

	// 尝试编译
	fmt.Println("=== Building Go Program ===")
	cmd := exec.Command(".\\runtime\\go\\bin\\go.exe", "build", "-o", "test/tsx_gui_demo.exe", outputFile)
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Build error: %v\n%s\n", err, string(output))
		return
	}
	fmt.Println("✓ Build Successful\n")

	// 运行程序
	fmt.Println("=== Running GUI Program ===")
	fmt.Println("Starting GUI application...")
	cmd = exec.Command("test/tsx_gui_demo.exe")
	cmd.Dir = "."
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	err = cmd.Start()
	if err != nil {
		fmt.Printf("Run error: %v\n", err)
		return
	}
	fmt.Println("✓ GUI Program Started!")
	fmt.Println("Check for the GUI window...")
}
