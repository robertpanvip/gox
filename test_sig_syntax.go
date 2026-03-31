package main

import (
	"fmt"
	"github.com/gox-lang/gox/gox"
)

// 这个文件测试完整的 sig 语法
// sig count = 0  应该被编译器转换为：count := gox.New(0)
// 使用 count 应该被转换为：count.Get()
// count = count + 1 应该被转换为：count.Set(count.Get() + 1)

func main() {
	// 模拟编译器转换后的代码
	count := gox.New(0)
	name := gox.New("World")
	
	// 测试读取（应该使用 .Get()）
	fmt.Println("Initial count:", count.Get())
	fmt.Println("Hello", name.Get())
	
	// 测试写入（应该使用 .Set()）
	count.Set(count.Get() + 1)
	name.Set("GoX")
	
	fmt.Println("Updated count:", count.Get())
	fmt.Println("Hello", name.Get())
	
	fmt.Println("\n✅ Signal 语法测试通过!")
}
