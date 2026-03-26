# gg 库 - 简单的 Go 2D 图形库

## GitHub
https://github.com/fogleman/gg

## 安装
```bash
go get github.com/fogleman/gg
```

## 特点
- 纯 Go 实现（不需要 CGO）
- 零外部依赖
- 简单易用的 API
- 轻量级且快速

## 绘图功能
- 基本形状：矩形、圆形、椭圆、多边形
- 路径和曲线
- 文字渲染
- 图像加载和绘制
- 颜色填充和描边
- 变换（旋转、缩放、平移）
- 抗锯齿渲染
- 保存为 PNG 文件

## 示例代码

```go
package main

import (
    "github.com/fogleman/gg"
)

func main() {
    // 创建 800x600 画布
    dc := gg.NewContext(800, 600)
    
    // 设置背景色（浅灰色）
    dc.SetRGB(0.9, 0.9, 0.9)
    dc.Clear()
    
    // 绘制红色矩形
    dc.SetRGB(1, 0, 0)
    dc.DrawRectangle(100, 100, 200, 150)
    dc.Fill()
    
    // 绘制蓝色圆形
    dc.SetRGB(0, 0, 1)
    dc.DrawCircle(400, 300, 80)
    dc.SetLineWidth(5)
    dc.Stroke()
    
    // 绘制绿色椭圆
    dc.SetRGB(0, 1, 0)
    dc.DrawEllipse(600, 400, 100, 50)
    dc.Fill()
    
    // 绘制文字
    dc.SetRGB(0, 0, 0)
    dc.LoadFontFace("Arial", 24)
    dc.DrawString("Hello from Gox + gg!", 50, 50)
    
    // 保存为 PNG 文件
    dc.SavePNG("output.png")
}
```

## 使用方法

1. 安装 gg 库：
   ```bash
   go get github.com/fogleman/gg
   ```

2. 创建 Gox 源文件（例如 `test_gg.gox`）：
   ```gox
   package main
   
   import go "github.com/fogleman/gg"
   
   public func Main() {
       let dc = gg.NewContext(800, 600)
       dc.SetRGB(0.9, 0.9, 0.9)
       dc.Clear()
       dc.SetRGB(1, 0, 0)
       dc.DrawRectangle(100, 100, 200, 150)
       dc.Fill()
       dc.SavePNG("output.png")
   }
   ```

3. 编译并运行：
   ```bash
   gox.exe test_gg.gox -o test_gg.go
   go run test_gg.go
   ```

## 其他简单图形库

### 1. image 包（标准库）
Go 标准库的 `image` 和 `image/draw` 包可以进行基本的像素级绘图。

### 2. draw2d
- GitHub: github.com/llgcode/draw2d
- 2D 矢量图形库
- 支持多种后端（PNG、PDF、SVG）

### 3. rasterx
- GitHub: srthq/rasterx
- 高性能 2D 光栅化器
- 支持渐变、描边、填充

### 4. canvas
- GitHub: tdewolff/canvas
- 矢量图形库
- 支持 SVG、PDF、PS 输出

## 对比

| 库 | 优点 | 缺点 |
|---|---|---|
| **gg** | 最简单，纯 Go | 功能相对基础 |
| **draw2d** | 支持多种格式 | API 稍复杂 |
| **Skia** | 功能最强大 | 需要 CGO，安装复杂 |
| **Fyne** | 完整 GUI 框架 | 体积较大 |
| **giu** | 轻量级 GUI | 即时模式，样式有限 |

## 推荐

对于简单的 2D 图形绘制需求，**gg 库是最佳选择**：
- ✅ 安装简单（一行命令）
- ✅ 无需 CGO
- ✅ API 直观
- ✅ 文档齐全
- ✅ 社区活跃

如果需要完整的 GUI 应用，考虑使用 **Fyne** 或 **giu**。
如果需要专业的图形渲染，考虑使用 **Skia**（但安装复杂）。
