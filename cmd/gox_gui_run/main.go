// 一键运行 Gox GUI 程序
// 用法：gox_gui_run.exe test_fx_simple2.gox

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
	fmt.Println("║        Gox GUI Runner                          ║")
	fmt.Println("╚════════════════════════════════════════════════╝")
	fmt.Println()
	
	// 检查参数
	if len(os.Args) < 2 {
		fmt.Println("Usage: gox_gui_run.exe <file.gox>")
		fmt.Println()
		fmt.Println("Example:")
		fmt.Println("  gox_gui_run.exe test/test_fx_simple2.gox")
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
	
	// 生成完整的 GUI 代码
	fmt.Println("✨ Generating GUI code...")
	guiCode := generateGUICode(goCode, goxFile)
	
	// 写入到 test 目录
	outputFile := "test/gui_temp.go"
	err = os.WriteFile(outputFile, []byte(guiCode), 0644)
	if err != nil {
		fmt.Printf("❌ Error writing file: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Generated: %s\n", outputFile)
	fmt.Println()
	
	// 编译
	fmt.Println("╔════════════════════════════════════════════════╗")
	fmt.Println("║        Compiling...                            ║")
	fmt.Println("╚════════════════════════════════════════════════╝")
	fmt.Println()
	
	exeFile := "test/gui_temp.exe"
	compileCmd := exec.Command("./runtime/go/bin/go.exe", "build", "-o", exeFile, outputFile)
	compileCmd.Dir = "."
	compileOutput, compileErr := compileCmd.CombinedOutput()
	
	if compileErr != nil {
		fmt.Printf("❌ Compile error: %v\n", compileErr)
		fmt.Println(string(compileOutput))
		os.Remove(outputFile)
		os.Exit(1)
	}
	
	fmt.Printf("✓ Compiled: %s\n", exeFile)
	fmt.Println()
	
	// 运行
	fmt.Println("╔════════════════════════════════════════════════╗")
	fmt.Println("║        Running GUI...                          ║")
	fmt.Println("╚════════════════════════════════════════════════╝")
	fmt.Println()
	
	cmd := exec.Command("./" + exeFile)
	cmd.Dir = "."
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	runErr := cmd.Run()
	
	// 清理
	os.Remove(outputFile)
	os.Remove(exeFile)
	
	if runErr != nil {
		fmt.Printf("\n❌ Runtime error: %v\n", runErr)
		os.Exit(1)
	}
	
	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════╗")
	fmt.Println("║        ✅ SUCCESS!                             ║")
	fmt.Println("╚════════════════════════════════════════════════╝")
}

func generateGUICode(goCode string, goxFile string) string {
	var sb strings.Builder
	
	// 添加包声明和导入
	sb.WriteString("package main\n\n")
	sb.WriteString("import (\n")
	sb.WriteString("\t\"fmt\"\n")
	sb.WriteString("\t\"log\"\n")
	sb.WriteString("\t\"image/color\"\n")
	sb.WriteString("\t\"github.com/hajimehoshi/ebiten/v2\"\n")
	sb.WriteString("\t\"github.com/hajimehoshi/ebiten/v2/ebitenutil\"\n")
	sb.WriteString("\t\"github.com/gox-lang/gox/gui\"\n")
	sb.WriteString(")\n\n")
	
	// 添加生成的代码（去掉重复的 package 和 import）
	lines := strings.Split(goCode, "\n")
	skipPackage := true
	skipImport := true
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		
		if skipPackage && strings.HasPrefix(trimmed, "package ") {
			continue
		}
		skipPackage = false
		
		if skipImport && strings.HasPrefix(trimmed, "import ") {
			continue
		}
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
		
		sb.WriteString(line + "\n")
	}
	
	// 添加 GUI 代码
	sb.WriteString("\n// Game GUI 游戏结构\n")
	sb.WriteString("type Game struct {\n")
	sb.WriteString("\tcounter *Counter\n")
	sb.WriteString("}\n\n")
	
	sb.WriteString("func (g *Game) Update() error {\n")
	sb.WriteString("\tif ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) {\n")
	sb.WriteString("\t\tx, y := ebiten.CursorPosition()\n")
	sb.WriteString("\t\tif x >= 220 && x <= 420 && y >= 200 && y <= 250 {\n")
	sb.WriteString("\t\t\tg.counter.Count++\n")
	sb.WriteString("\t\t\tfmt.Printf(\"count++ → Count: %d\\n\", g.counter.Count)\n")
	sb.WriteString("\t\t}\n")
	sb.WriteString("\t}\n")
	sb.WriteString("\tif ebiten.IsKeyPressed(ebiten.KeyEscape) {\n")
	sb.WriteString("\t\treturn fmt.Errorf(\"ESC\")\n")
	sb.WriteString("\t}\n")
	sb.WriteString("\treturn nil\n")
	sb.WriteString("}\n\n")
	
	sb.WriteString("func (g *Game) Draw(screen *ebiten.Image) {\n")
	sb.WriteString("\tscreen.Fill(color.RGBA{30, 30, 40, 255})\n")
	sb.WriteString("\tebitenutil.DebugPrintAt(screen, \"Counter FX Component\\n\\nClick the button!\", 220, 140)\n")
	sb.WriteString("\tebitenutil.DebugPrintAt(screen, fmt.Sprintf(\"Count: %d\", g.counter.Count), 270, 180)\n")
	sb.WriteString("\tbtnColor := color.RGBA{0, 128, 255, 255}\n")
	sb.WriteString("\tebitenutil.DrawRect(screen, 220, 200, 200, 50, btnColor)\n")
	sb.WriteString("\tebitenutil.DebugPrintAt(screen, \"CLICK ME\", 270, 215)\n")
	sb.WriteString("}\n\n")
	
	sb.WriteString("func (g *Game) Layout(w, h int) (int, int) {\n")
	sb.WriteString("\treturn 640, 480\n")
	sb.WriteString("}\n\n")
	
	// 添加 main 函数
	sb.WriteString("func main() {\n")
	sb.WriteString("\tfmt.Println(\"🚀 Running GUI from: " + goxFile + "\")\n")
	sb.WriteString("\tfmt.Println()\n")
	sb.WriteString("\n\tgame := &Game{counter: NewCounter()}\n")
	sb.WriteString("\tebiten.SetWindowSize(640, 480)\n")
	sb.WriteString("\tebiten.SetWindowTitle(\"Gox GUI - " + goxFile + "\")\n")
	sb.WriteString("\n\tif err := ebiten.RunGame(game); err != nil {\n")
	sb.WriteString("\t\tlog.Fatal(err)\n")
	sb.WriteString("\t}\n")
	sb.WriteString("}\n")
	
	return sb.String()
}
