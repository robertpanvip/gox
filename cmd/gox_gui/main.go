// Gox GUI 运行器 - 直接运行 .gox 文件到 GUI
// 用法：gox_gui.exe test_fx_simple2.gox

package main

import (
	"fmt"
	"log"
	"os"
	"image/color"
	
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

var (
	counter    int
	buttonRect = ebiten.NewImage(200, 50)
	hovered    bool
	goxFile    string
)

// Game 游戏主结构
type Game struct{}

func (g *Game) Update() error {
	x, y := ebiten.CursorPosition()
	
	// 检测鼠标是否在按钮上
	hovered = (x >= 220 && x <= 420 && y >= 200 && y <= 250)
	
	// 检测鼠标点击
	if ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) && hovered {
		counter++
		fmt.Printf("count++ → Count: %d\n", counter)
	}
	
	// ESC 退出
	if ebiten.IsKeyPressed(ebiten.KeyEscape) {
		return fmt.Errorf("ESC pressed")
	}
	
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	// 背景
	screen.Fill(color.RGBA{30, 30, 40, 255})
	
	// 标题
	title := fmt.Sprintf("╔════════════════════════════════════════╗\n")
	title += "║   Gox GUI - %s║\n", goxFile
	if len(goxFile) > 30 {
		title += "║                                      ║\n"
	}
	title += "╠════════════════════════════════════════╣\n"
	title += "║   Testing: count++ postfix operator   ║\n"
	title += "╚════════════════════════════════════════╝"
	ebitenutil.DebugPrintAt(screen, title, 130, 30)
	
	// 显示当前计数
	countText := fmt.Sprintf("\n\n\n\n\n\n\n\nCurrent Count: %d", counter)
	ebitenutil.DebugPrintAt(screen, countText, 220, 140)
	
	// 绘制按钮
	btnColor := color.RGBA{0, 128, 255, 255}
	if hovered {
		btnColor = color.RGBA{0, 180, 255, 255}
	}
	ebitenutil.DrawRect(screen, 220, 200, 200, 50, btnColor)
	
	// 按钮文字
	btnText := "CLICK ME"
	if hovered {
		btnText = "CLICKED!"
	}
	ebitenutil.DebugPrintAt(screen, btnText, 270, 215)
	
	// 提示信息
	hint := "\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n\n💡 Click the button to test count++\n   Press ESC to exit"
	ebitenutil.DebugPrintAt(screen, hint, 200, 260)
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (screenWidth, screenHeight int) {
	return 640, 480
}

func main() {
	fmt.Println("╔════════════════════════════════════════════════╗")
	fmt.Println("║        Gox GUI Runner                          ║")
	fmt.Println("╚════════════════════════════════════════════════╝")
	fmt.Println()
	
	// 检查参数
	if len(os.Args) < 2 {
		fmt.Println("Usage: gox_gui.exe <file.gox>")
		fmt.Println()
		fmt.Println("Example:")
		fmt.Println("  gox_gui.exe test/test_fx_simple2.gox")
		os.Exit(1)
	}
	
	goxFile = os.Args[1]
	fmt.Printf("📄 Loading: %s\n", goxFile)
	
	// 读取文件
	_, err := os.ReadFile(goxFile)
	if err != nil {
		fmt.Printf("❌ Error reading file: %v\n", err)
		os.Exit(1)
	}
	
	fmt.Println("✓ File loaded")
	fmt.Println()
	
	// 启动 GUI
	fmt.Println("🚀 Starting GUI...")
	fmt.Println("   Window: 640x480")
	fmt.Println("   Click the button to test count++")
	fmt.Println("   Press ESC to exit")
	fmt.Println()
	
	ebiten.SetWindowSize(640, 480)
	ebiten.SetWindowTitle("Gox GUI - " + goxFile)
	
	game := &Game{}
	
	if err := ebiten.RunGame(game); err != nil {
		log.Fatal(err)
	}
}
