package main

import (
	"fmt"
	"github.com/gox-lang/gox/gui"
)

func main() {
	// 创建 GUI 应用
	app := gui.NewApp("Gox TSX Demo", 800, 600)
	
	fmt.Println("GUI Application created successfully!")
	fmt.Printf("Title: %s, Size: %dx%d\n", app.Title, app.Width, app.Height)
	
	// 运行应用
	fmt.Println("Starting GUI application...")
	app.Run()
}
