package main

import (
"fmt"
"github.com/gox-lang/gox/gox"
)

func main() {
// 测试 Signal 基础功能
count := gox.New(0)
name := gox.New("World")

fmt.Println("Initial count:", count.Get())
fmt.Println("Hello", name.Get())

// 更新 Signal
count.Set(count.Get() + 1)
name.Set("GoX")

fmt.Println("Updated count:", count.Get())
fmt.Println("Hello", name.Get())

fmt.Println("\n Signal 基础测试通过!")
}
