# Gox + GLFW + gg 完整指南

## 🎯 为什么选择 GLFW + gg？

- **GLFW** - 跨平台窗口和输入管理
- **gg** - 简单易用的 2D 绘图
- **OpenGL** - GPU 加速渲染
- **实时显示** - 直接在窗口中看到绘图结果

---

## 📦 安装依赖

```bash
# 安装 GLFW
go get github.com/go-gl/glfw/v3.3/glfw

# 安装 gg
go get github.com/fogleman/gg

# 安装 OpenGL
go get github.com/go-gl/gl/v3.3-core/gl
```

---

## 🚀 完整示例代码

### 方案 1: 简单版本（仅演示）

```gox
package main

import go "fmt"
import go "github.com/go-gl/glfw/v3.3/glfw"
import go "github.com/fogleman/gg"

public func Main() {
    // 初始化 GLFW
    glfw.Init()
    defer glfw.Terminate()
    
    // 创建窗口
    window, _ := glfw.CreateWindow(800, 600, "Gox + GLFW + gg", nil, nil)
    window.MakeContextCurrent()
    
    // 使用 gg 绘图
    let dc = gg.NewContext(800, 600)
    dc.SetRGB(0.9, 0.9, 0.9)
    dc.Clear()
    
    dc.SetRGB(1, 0, 0)
    dc.DrawCircle(400, 300, 100)
    dc.Fill()
    
    dc.SavePNG("output.png")
    
    fmt.println("Image created!")
    
    // 保持窗口
    for !window.ShouldClose() {
        glfw.WaitEvents()
    }
}
```

### 方案 2: 完整 OpenGL 渲染（推荐）

创建 `glfw_opengl_demo.go`：

```go
package main

import (
    "fmt"
    "image"
    "log"
    "runtime"
    "unsafe"

    "github.com/go-gl/gl/v3.3-core/gl"
    "github.com/go-gl/glfw/v3.3/glfw"
    "github.com/fogleman/gg"
)

func init() {
    runtime.LockOSThread()
}

func main() {
    // 初始化 GLFW
    if err := glfw.Init(); err != nil {
        log.Fatalln("failed to initialize GLFW:", err)
    }
    defer glfw.Terminate()

    // 创建窗口
    window, err := glfw.CreateWindow(800, 600, "Gox + GLFW + gg OpenGL", nil, nil)
    if err != nil {
        panic(err)
    }
    window.MakeContextCurrent()

    // 初始化 OpenGL
    if err := gl.Init(); err != nil {
        panic(err)
    }

    // 设置 vsync
    window.SwapInterval(1)

    // 创建纹理
    var texture uint32
    gl.GenTextures(1, &texture)
    gl.BindTexture(gl.TEXTURE_2D, texture)
    gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
    gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
    gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
    gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)

    // 设置键盘回调
    window.SetKeyCallback(func(window *glfw.Window, key int, scancode int, action glfw.Action, mods glfw.ModifierKey) {
        if key == glfw.KeyEscape && action == glfw.Press {
            window.SetShouldClose(true)
        }
    })

    // 主循环
    for !window.ShouldClose() {
        // 1. 使用 gg 绘图
        dc := gg.NewContext(800, 600)
        
        // 背景
        dc.SetRGB(0.1, 0.1, 0.3)
        dc.Clear()
        
        // 绘制图形
        dc.SetRGB(1, 0, 0)
        dc.DrawCircle(400, 300, 100)
        dc.Fill()
        
        dc.SetRGB(0, 1, 0)
        dc.DrawRectangle(200, 200, 150, 150)
        dc.Fill()
        
        dc.SetRGB(0, 0, 1)
        dc.DrawEllipse(600, 400, 120, 60)
        dc.Fill()
        
        // 文字
        dc.SetRGB(1, 1, 1)
        dc.DrawString("Hello from Gox + GLFW + gg!", 50, 50)
        dc.DrawString("Press ESC to exit", 50, 80)

        // 2. 获取 gg 生成的图像
        img := dc.Image()

        // 3. 更新 OpenGL 纹理
        bounds := img.Bounds()
        pixels := make([]uint8, bounds.Dx()*bounds.Dy()*4)
        index := 0
        for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
            for x := bounds.Min.X; x < bounds.Max.X; x++ {
                r, g, b, a := img.At(x, y).RGBA()
                pixels[index] = uint8(r >> 8)
                pixels[index+1] = uint8(g >> 8)
                pixels[index+2] = uint8(b >> 8)
                pixels[index+3] = uint8(a >> 8)
                index += 4
            }
        }

        gl.BindTexture(gl.TEXTURE_2D, texture)
        gl.TexImage2D(
            gl.TEXTURE_2D,
            0,
            gl.RGBA,
            int32(bounds.Dx()),
            int32(bounds.Dy()),
            0,
            gl.RGBA,
            gl.UNSIGNED_BYTE,
            gl.Ptr(pixels),
        )

        // 4. 渲染纹理到屏幕
        gl.ClearColor(0.0, 0.0, 0.0, 1.0)
        gl.Clear(gl.COLOR_BUFFER_BIT)

        // 简单的全屏四边形渲染
        // (这里省略了 VAO/VBO 设置代码，实际使用需要完整的 OpenGL 渲染管线)

        window.SwapBuffers()
        glfw.PollEvents()
    }

    fmt.Println("Window closed.")
}
```

---

## 🎨 更简单的替代方案

如果觉得 OpenGL 太复杂，可以使用以下替代方案：

### 方案 A: gg + 保存图片

```gox
package main

import go "github.com/fogleman/gg"

public func Main() {
    let dc = gg.NewContext(800, 600)
    dc.SetRGB(1, 1, 1)
    dc.Clear()
    
    dc.SetRGB(1, 0, 0)
    dc.DrawCircle(400, 300, 100)
    dc.Fill()
    
    dc.SavePNG("output.png")
}
```

然后用系统图片查看器打开。

### 方案 B: gg + walk（Windows 专用）

```gox
package main

import go "github.com/fogleman/gg"
import go "github.com/lxn/walk"
import . "github.com/lxn/walk/declarative"

public func Main() {
    // 使用 gg 绘图
    let dc = gg.NewContext(800, 600)
    dc.SetRGB(1, 1, 1)
    dc.Clear()
    dc.SetRGB(1, 0, 0)
    dc.DrawCircle(400, 300, 100)
    dc.Fill()
    dc.SavePNG("temp.png")
    
    // 使用 walk 显示
    let img, _ = walk.LoadImageFromFile("temp.png")
    
    MainWindow{
        Title: "gg Demo",
        Size: Size{Width: 850, Height: 700},
        Children: []Widget{
            ImageView{Image: img},
        },
    }.Run()
}
```

### 方案 C: 使用 Ebiten（推荐 ⭐⭐⭐⭐⭐）

**Ebiten** 是一个超简单的 2D 游戏库，完美结合了窗口、输入和绘图：

```gox
package main

import go "fmt"
import go "github.com/hajimehoshi/ebiten/v2"
import go "github.com/fogleman/gg"
import go "image"

public type Game struct {
    img: image.Image
}

public func (g: Game) Update(): error {
    return nil
}

public func (g: Game) Draw(screen: *ebiten.Image) {
    // 直接使用 gg 生成的图像
    screen.WritePixels(g.img)
}

public func (g: Game) Layout(outsideWidth: int, outsideHeight: int): (int, int) {
    return 800, 600
}

public func Main() {
    // 使用 gg 绘图
    let dc = gg.NewContext(800, 600)
    dc.SetRGB(1, 1, 1)
    dc.Clear()
    
    dc.SetRGB(1, 0, 0)
    dc.DrawCircle(400, 300, 100)
    dc.Fill()
    
    dc.SetRGB(0, 0, 1)
    dc.DrawRectangle(200, 200, 150, 150)
    dc.Fill()
    
    let img = dc.Image()
    
    // 使用 Ebiten 显示
    ebiten.RunGame(&Game{img: img})
}
```

**安装 Ebiten**:
```bash
go get github.com/hajimehoshi/ebiten/v2
```

---

## 📊 方案对比

| 方案 | 难度 | 依赖 | 实时显示 | 推荐度 |
|------|------|------|----------|--------|
| **gg + 保存 PNG** | ⭐简单 | gg | ❌ | ⭐⭐⭐ |
| **gg + walk** | ⭐⭐中等 | gg + walk | ✅ | ⭐⭐⭐⭐ |
| **gg + GLFW + OpenGL** | ⭐⭐⭐⭐难 | gg + glfw + gl | ✅ | ⭐⭐⭐ |
| **gg + Ebiten** | ⭐⭐简单 | gg + ebiten | ✅ | ⭐⭐⭐⭐⭐ |

---

## 🎯 推荐方案

### 初学者 → **Ebiten**
- 代码最简单
- 文档齐全
- 跨平台
- 性能好

### Windows 专用 → **walk**
- 原生外观
- 简单易用
- 体积小

### 学习 OpenGL → **GLFW + gg**
- 学习图形编程
- 完全控制
- 适合游戏开发

---

## 📚 相关资源

- **Ebiten**: https://ebiten.org/
- **GLFW**: https://www.glfw.org/
- **gg**: https://github.com/fogleman/gg
- **walk**: https://github.com/lxn/walk
- **OpenGL**: https://www.opengl.org/

---

## 🚀 快速开始（Ebiten 方案）

1. **安装 Ebiten**:
   ```bash
   go get github.com/hajimehoshi/ebiten/v2
   ```

2. **编写代码**:
   ```gox
   import go "github.com/hajimehoshi/ebiten/v2"
   import go "github.com/fogleman/gg"
   
   public func Main() {
       let dc = gg.NewContext(800, 600)
       dc.SetRGB(1, 1, 1)
       dc.Clear()
       dc.SetRGB(1, 0, 0)
       dc.DrawCircle(400, 300, 100)
       dc.Fill()
       
       ebiten.RunGame(&Game{img: dc.Image()})
   }
   ```

3. **运行**:
   ```bash
   go run main.go
   ```

窗口立即显示！🎉

---

## 💡 总结

**GLFW + gg** 是完全可行的，但需要 OpenGL 知识来渲染图像到窗口。

**最简单的实时显示方案**是使用 **Ebiten**，它封装了所有复杂的 OpenGL 代码，让你专注于绘图！
