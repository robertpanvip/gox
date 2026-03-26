# Gox + Skia 窗口程序示例

## 功能特性

✅ **Go 模块导入支持** - 使用 `import go` 显式声明 Go 包
✅ **自动可见性转换** - Go 包函数调用自动小写转大写
✅ **结构体定义** - 支持 Go 风格结构体
✅ **方法定义** - 支持 Go receiver 风格方法
✅ **结构体字面量** - 支持简写语法和类型推断

## 源代码 (test_skia_window.gox)

```gox
package main

// 导入 Go 包（自动转换可见性）
import go "github.com/go-skynet/go-skia/skia"
import go "fmt"

public struct Window {
    public width: int
    public height: int
    public title: string
}

public func NewWindow(width: int, height: int, title: string): Window {
    return Window{
        width: width,
        height: height,
        title: title,
    }
}

public func (w: Window) Draw() {
    // 创建 Skia 位图
    bitmap := skia.NewBitmap(skia.ImageInfo{
        Width: w.width,
        Height: w.height,
        ColorType: skia.ColorTypeN32Premul,
        AlphaType: skia.AlphaTypePremul,
    })
    
    // 创建画布
    canvas := skia.NewCanvas(bitmap.Bounds())
    canvas.Clear(skia.ColorWHITE)
    
    // 绘制背景
    paint := skia.NewPaint()
    paint.SetColor(skia.ColorSetARGB(255, 100, 150, 200))
    canvas.DrawRect(0, 0, float64(w.width), float64(w.height), paint)
    
    // 绘制文字
    paint.SetColor(skia.ColorBLACK)
    paint.SetAntiAlias(true)
    canvas.DrawString("Hello from Gox + Skia!", 50, 100, nil, paint)
    
    // 绘制圆形
    paint.SetColor(skia.ColorSetARGB(255, 255, 100, 100))
    canvas.DrawCircle(150, 200, 50, paint)
    
    // 绘制矩形
    paint.SetColor(skia.ColorSetARGB(255, 100, 255, 100))
    canvas.DrawRect(250, 150, 100, 100, paint)
    
    fmt.Println("Window created: " + w.title)
    fmt.Printf("Size: %d x %d\n", w.width, w.height)
    fmt.Println("Drawing completed!")
}

public func Main() {
    // 创建窗口
    win := NewWindow(800, 600, "Gox + Skia Demo")
    
    // 绘制
    win.Draw()
    
    fmt.println("Program finished!")
}
```

## 转译后的 Go 代码

```go
package main

import "github.com/go-skynet/go-skia/skia"
import "fmt"

type Window struct {
    Width int
    Height int
    Title string
}

func NewWindow(width int, height int, title string) Window {
    return Window{Width: width, Height: height, Title: title}
}

func (w Window) Draw() {
    bitmap := skia.NewBitmap(skia.ImageInfo{
        Width: w.width,
        Height: w.height,
        ColorType: skia.ColorTypeN32Premul,
        AlphaType: skia.AlphaTypePremul,
    })
    
    canvas := skia.NewCanvas(bitmap.Bounds())
    canvas.Clear(skia.ColorWHITE)
    
    paint := skia.NewPaint()
    paint.SetColor(skia.ColorSetARGB(255, 100, 150, 200))
    canvas.DrawRect(0, 0, float64(w.width), float64(w.height), paint)
    
    paint.SetColor(skia.ColorBLACK)
    paint.SetAntiAlias(true)
    canvas.DrawString("Hello from Gox + Skia!", 50, 100, nil, paint)
    
    paint.SetColor(skia.ColorSetARGB(255, 255, 100, 100))
    canvas.DrawCircle(150, 200, 50, paint)
    
    paint.SetColor(skia.ColorSetARGB(255, 100, 255, 100))
    canvas.DrawRect(250, 150, 100, 100, paint)
    
    fmt.Println("Window created: " + w.title)
    fmt.Printf("Size: %d x %d\n", w.width, w.height)
    fmt.Println("Drawing completed!")
}

func Main() {
    win := NewWindow(800, 600, "Gox + Skia Demo")
    win.Draw()
    fmt.Println("Program finished!")
}
```

## 关键特性演示

### 1. Go 模块导入

```gox
// Gox 源码
import go "fmt"
import go "github.com/go-skynet/go-skia/skia"

fmt.println("Hello")      // → fmt.Println("Hello")
skia.newBitmap(...)       // → skia.NewBitmap(...)
```

### 2. 结构体和方法

```gox
// Gox 源码
public struct Window {
    public width: int
}

public func (w: Window) Draw() {
    // 方法实现
}
```

转译为：

```go
// Go 代码
type Window struct {
    Width int
}

func (w Window) Draw() {
    // 方法实现
}
```

### 3. 结构体字面量

```gox
// Gox 源码 - 支持简写
return Window{
    width: width,
    height: height,
}
```

## 运行步骤

1. **安装 Skia 绑定**：
   ```bash
   go get github.com/go-skynet/go-skia/skia
   ```

2. **转译 Gox 代码**：
   ```bash
   gox.exe test/test_skia_window.gox
   ```

3. **编译并运行生成的 Go 代码**：
   ```bash
   go run test/test_skia_window.go
   ```

## 注意事项

- Skia 库需要 CGO 支持
- 确保已安装 Skia 开发库
- Windows 上可能需要额外的配置

## 功能验证

✅ 导入语法：`import go "package"`
✅ 可见性转换：`fmt.println` → `fmt.Println`
✅ 结构体定义和方法
✅ Receiver 语法
✅ 结构体字面量
✅ Skia API 调用
