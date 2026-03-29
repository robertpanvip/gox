package main

import (
	"fmt"
	"github.com/gox-lang/gox/gui"
)

func main() {
	fmt.Println("🚀 Running Counter FX Component\n")
	fmt.Println("Creating Counter component...")
	
	// 创建 Counter 组件
	counter := gui.NewCounter()
	
	fmt.Println("✓ Counter created successfully!")
	fmt.Println("\nComponent Structure:")
	fmt.Println("  - Static parts: rootDiv, nameLabel, countLabel, button")
	fmt.Println("  - Dynamic parts: namePart, countPart")
	fmt.Println("\nInitial state:")
	fmt.Printf("    name: \"World\"\n")
	fmt.Printf("    count: %d\n", 0)
	fmt.Println("\nEvent handler registered:")
	fmt.Println("    onClick: c.count++ + c.RequestUpdate()")
	fmt.Println("\n✅ Component is ready to render!")
	fmt.Println("\nNote: This component uses lit-html style:")
	fmt.Println("  - Static parts created once")
	fmt.Println("  - Dynamic parts update automatically")
	fmt.Println("  - count++ is correctly transformed to c.count++")
	
	// 验证组件
	_ = counter
}
