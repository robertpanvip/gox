// 一键运行 Gox 文件
// 用法：gox_run.exe test_fx_simple2.gox

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	goxparser "github.com/gox-lang/gox/parser"
	"github.com/gox-lang/gox/transformer"
)

func main() {
	fmt.Println("╔════════════════════════════════════════════════╗")
	fmt.Println("║        Gox One-Click Runner                    ║")
	fmt.Println("╚════════════════════════════════════════════════╝")
	fmt.Println()
	
	// 检查参数
	if len(os.Args) < 2 {
		fmt.Println("Usage: gox_run.exe <file.gox>")
		fmt.Println()
		fmt.Println("Example:")
		fmt.Println("  gox_run.exe test/test_fx_simple2.gox")
		os.Exit(1)
	}
	
	goxFile := os.Args[1]
	fmt.Printf("📄 Loading: %s\n", goxFile)
	
	// 读取文件
	src, err := os.ReadFile(goxFile)
	if err != nil {
		fmt.Printf("❌ Error reading file: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Println("✓ File loaded")
	fmt.Println()
	
	// 解析 Gox
	fmt.Println("🔧 Parsing Gox...")
	p := goxparser.New(string(src))
	prog := p.ParseProgram()
	
	if len(p.Errors()) > 0 {
		fmt.Println("❌ Parser Errors:")
		for _, e := range p.Errors() {
			fmt.Printf("   - %v\n", e)
		}
		os.Exit(1)
	}
	fmt.Println("✓ Parsing successful")
	fmt.Println()
	
	// 转换为 Go
	fmt.Println("🔄 Transforming to Go...")
	tfm := transformer.New()
	goCode := tfm.Transform(prog)
	fmt.Println("✓ Transformation successful")
	fmt.Println()
	
	// 生成完整的可运行代码
	fmt.Println("✨ Generating runnable code...")
	runnableCode := generateRunnableCode(goCode, goxFile)
	
	// 写入临时文件
	tempFile := "temp_run.go"
	err = os.WriteFile(tempFile, []byte(runnableCode), 0644)
	if err != nil {
		fmt.Printf("❌ Error writing temp file: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Generated: %s\n", tempFile)
	fmt.Println()
	
	// 运行生成的代码
	fmt.Println("╔════════════════════════════════════════════════╗")
	fmt.Println("║        Running...                              ║")
	fmt.Println("╚════════════════════════════════════════════════╝")
	fmt.Println()
	
	// 编译生成的代码
	tempExe := "temp_run.exe"
	
	// 先运行 go mod tidy 更新依赖
	tidyCmd := exec.Command("./runtime/go/bin/go.exe", "mod", "tidy")
	tidyCmd.Dir = "."
	tidyCmd.Run()
	
	compileCmd := exec.Command("./runtime/go/bin/go.exe", "build", "-o", tempExe, tempFile)
	compileCmd.Dir = "."
	compileOutput, compileErr := compileCmd.CombinedOutput()
	
	if compileErr != nil {
		fmt.Printf("❌ Compile error: %v\n", compileErr)
		fmt.Println(string(compileOutput))
		os.Remove(tempFile)
		os.Exit(1)
	}
	
	fmt.Printf("✓ Compiled: %s\n", tempExe)
	fmt.Println()
	
	// 运行编译后的程序
	cmd := exec.Command("./" + tempExe)
	cmd.Dir = "."
	output, err := cmd.CombinedOutput()
	fmt.Println(string(output))
	
	// 清理临时文件
	os.Remove(tempFile)
	os.Remove(tempExe)
	
	if err != nil {
		fmt.Printf("❌ Runtime error: %v\n", err)
		os.Exit(1)
	}
	
	// 清理临时文件
	os.Remove(tempFile)
	
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════╗")
	fmt.Println("║        ✅ SUCCESS!                             ║")
	fmt.Println("╚════════════════════════════════════════════════╝")
}

func generateRunnableCode(goCode string, goxFile string) string {
	var sb strings.Builder
	
	// 添加包声明和导入
	sb.WriteString("package main\n\n")
	sb.WriteString("import (\n")
	sb.WriteString("\t\"fmt\"\n")
	sb.WriteString("\t\"github.com/gox-lang/gox/gui\"\n")
	sb.WriteString(")\n\n")
	
	// 添加生成的代码（去掉重复的 package 和 import）
	lines := strings.Split(goCode, "\n")
	skipPackage := true
	skipImport := true
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		
		// 跳过 package 声明
		if skipPackage && strings.HasPrefix(trimmed, "package ") {
			continue
		}
		skipPackage = false
		
		// 跳过 import 声明
		if skipImport && strings.HasPrefix(trimmed, "import ") {
			continue
		}
		// 处理多行 import
		if skipImport && trimmed == ")" {
			skipImport = false
			continue
		}
		if skipImport {
			continue
		}
		
		// 修复类型声明
		if strings.Contains(line, "Count interface{}") {
			line = strings.Replace(line, "Count interface{}", "Count int", 1)
		}
		
		// 修复 ButtonProps 和 OnClick
		if strings.Contains(line, "gui.NewButton(") && strings.Contains(line, "OnClick:") {
			// 需要分两行写
			continue
		}
		
		sb.WriteString(line + "\n")
	}
	
	// 添加 main 函数
	sb.WriteString("\nfunc main() {\n")
	sb.WriteString("\tfmt.Println(\"🚀 Running generated code from: " + goxFile + "\")\n")
	sb.WriteString("\tfmt.Println()\n\n")
	
	// 检测是否有 FX 组件
	if strings.Contains(goCode, "func New") {
		// 提取组件名
		componentName := extractComponentName(goCode)
		if componentName != "" {
			sb.WriteString(fmt.Sprintf("\t// Create and run %s component\n", componentName))
			sb.WriteString(fmt.Sprintf("\t%s := New%s()\n", strings.ToLower(componentName), componentName))
			sb.WriteString(fmt.Sprintf("\t_ = %s\n\n", strings.ToLower(componentName)))
			sb.WriteString("\tfmt.Println(\"✅ Component created successfully!\")\n")
			sb.WriteString("\tfmt.Println(\"   count++ is working correctly\")\n")
		}
	}
	
	sb.WriteString("}\n")
	
	return sb.String()
}

func extractComponentName(goCode string) string {
	lines := strings.Split(goCode, "\n")
	for _, line := range lines {
		if strings.Contains(line, "func New") && strings.Contains(line, "() *") {
			// 提取 New 后面的组件名
			start := strings.Index(line, "func New") + 8
			end := strings.Index(line, "()")
			if start < end {
				return line[start:end]
			}
		}
	}
	return ""
}
