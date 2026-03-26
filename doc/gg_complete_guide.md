# Gox + gg 绘图完整指南

## 🎨 什么是 gg 库？

**gg** 是一个纯 Go 实现的 2D 图形绘制库：
- ✅ **零依赖** - 不需要 CGO，不需要 C++ 编译器
- ✅ **简单易用** - API 直观，类似 Python 的 PIL
- ✅ **功能丰富** - 支持形状、文字、图像、路径
- ✅ **跨平台** - Windows、Mac、Linux 都能运行
- ✅ **输出 PNG** - 直接保存为图片文件

GitHub: https://github.com/fogleman/gg

---

## 🚀 快速开始

### 1️⃣ 编写 Gox 代码

创建 `draw.gox` 文件：

```gox
package main

import go "github.com/fogleman/gg"

public func Main() {
    // 创建 800x600 画布
    let dc = gg.NewContext(800, 600)
    
    // 设置背景色（白色）
    dc.SetRGB(1, 1, 1)
    dc.Clear()
    
    // 绘制红色矩形
    dc.SetRGB(1, 0, 0)
    dc.DrawRectangle(100, 100, 200, 150)
    dc.Fill()
    
    // 绘制蓝色圆形
    dc.SetRGB(0, 0, 1)
    dc.DrawCircle(400, 300, 80)
    dc.Stroke()
    
    // 绘制文字
    dc.SetRGB(0, 0, 0)
    dc.DrawString("Hello from Gox!", 50, 50)
    
    // 保存为 PNG
    dc.SavePNG("output.png")
}
```

### 2️⃣ 编译为 Go 代码

```powershell
.\gox_new.exe -o draw.go draw.gox
```

### 3️⃣ 安装 gg 库

```powershell
$env:GOPROXY="https://goproxy.cn,direct"
go get github.com/fogleman/gg
go mod tidy
```

### 4️⃣ 运行程序

```powershell
go run draw.go
```

### 5️⃣ 查看图片

生成的 `output.png` 可以用任何图片查看器打开：

```powershell
# Windows 上直接用默认查看器打开
start output.png
```

---

## 📊 完整示例：gg + walk 显示窗口

如果想**直接在窗口中显示**绘制的图片，可以结合 **walk** 库（Windows 专用）：

### 示例代码

```gox
package main

import go "fmt"
import go "github.com/fogleman/gg"
import go "github.com/lxn/walk"
import . "github.com/lxn/walk/declarative"

public func Main() {
    // 1. 使用 gg 绘制图片
    let dc = gg.NewContext(800, 600)
    dc.SetRGB(1, 1, 1)
    dc.Clear()
    
    // 绘制图形
    dc.SetRGB(1, 0, 0)
    dc.DrawRectangle(100, 100, 200, 150)
    dc.Fill()
    
    dc.SetRGB(0, 0, 1)
    dc.DrawCircle(400, 300, 80)
    dc.Stroke()
    
    // 保存到临时文件
    dc.SavePNG("temp.png")
    
    // 2. 使用 walk 创建窗口显示图片
    let img, _ = walk.LoadImageFromFile("temp.png")
    
    MainWindow{
        Title:  "Gox + gg + walk Demo",
        Size:   Size{Width: 900, Height: 700},
        Layout: VBox{},
        Children: []Widget{
            Label{Text: "Drawing created with gg library!"},
            ImageView{
                Image: img,
                Size:  Size{Width: 800, Height: 600},
            },
            PushButton{
                Text: "Save Image",
                OnClicked: func() {
                    fmt.println("Image saved!")
                },
            },
        },
    }.Run()
}
```

### 安装依赖

```powershell
go get github.com/lxn/walk
```

### 运行

```powershell
go run window.go
```

---

## 🎯 gg 库支持的绘图功能

### 基本形状
- `DrawRectangle(x, y, w, h)` - 矩形
- `DrawCircle(x, y, r)` - 圆形
- `DrawEllipse(x, y, rx, ry)` - 椭圆
- `DrawPolygon(points)` - 多边形
- `DrawLine(x1, y1, x2, y2)` - 直线

### 路径和曲线
- `MoveTo(x, y)` - 移动起点
- `LineTo(x, y)` - 画线
- `QuadraticTo(x1, y1, x, y)` - 二次贝塞尔曲线
- `CubicTo(x1, y1, x2, y2, x, y)` - 三次贝塞尔曲线
- `ArcTo(x, y, radius, startAngle, endAngle)` - 圆弧

### 填充和描边
- `Fill()` - 填充当前路径
- `Stroke()` - 描边当前路径
- `FillStroke()` - 填充并描边
- `SetLineWidth(width)` - 设置线宽
- `SetRGB(r, g, b)` - 设置颜色
- `SetAlpha(alpha)` - 设置透明度

### 文字渲染
- `DrawString(text, x, y)` - 绘制文字
- `DrawStringAnchored(text, x, y, anchor)` - 锚点文字
- `LoadFontFace(family, size)` - 加载字体

### 图像处理
- `DrawImage(img, x, y)` - 绘制图片
- `DrawImageScaled(img, x, y, scale)` - 缩放绘制
- `DrawImageRotated(img, x, y, angle)` - 旋转绘制

### 变换
- `Translate(x, y)` - 平移
- `Rotate(angle)` - 旋转（弧度）
- `Scale(x, y)` - 缩放
- `Transform(matrix)` - 矩阵变换

### 保存输出
- `SavePNG(path)` - 保存为 PNG
- `SaveJPG(path, quality)` - 保存为 JPG
- `SaveSVG(path)` - 保存为 SVG

---

## 📁 项目结构

```
myapp/
├── draw.gox          # Gox 源代码
├── draw.go           # 生成的 Go 代码
├── go.mod            # Go 模块配置
├── go.sum            # 依赖校验
├── output.png        # 生成的图片
└── temp.png          # 临时文件（如果有）
```

---

## 💡 实用技巧

### 1. 抗锯齿渲染
gg 默认开启抗锯齿，无需额外配置。

### 2. 透明度处理
```gox
dc.SetRGB(1, 0, 0)  // 红色
dc.SetAlpha(0.5)     // 50% 透明
```

### 3. 渐变填充
```gox
// 线性渐变
gradient := gg.NewLinearGradient(x1, y1, x2, y2)
gradient.AddColorStop(0, color.RGBA{255, 0, 0, 255})
gradient.AddColorStop(1, color.RGBA{0, 0, 255, 255})
dc.SetFillStyle(gradient)
```

### 4. 加载外部图片
```gox
img, _ := gg.LoadImage("photo.jpg")
dc.DrawImage(img, 0, 0)
```

---

## 🔧 常见问题

### Q: 为什么窗口不显示？
A: gg 库只生成图片文件，不创建窗口。要显示窗口需要配合其他 GUI 库（如 walk、fyne、giu）。

### Q: 如何实时预览绘图？
A: 
1. 保存为 PNG 文件
2. 使用图片查看器打开
3. 或者用 GUI 库加载并显示图片

### Q: 绘图性能如何？
A: gg 是纯 Go 实现，性能中等。对于复杂图形或动画，建议使用 Skia 或其他 GPU 加速库。

### Q: 支持中文吗？
A: 支持！需要加载中文字体：
```gox
dc.LoadFontFace("SimHei", 24)  // 黑体
dc.LoadFontFace("SimSun", 24)  // 宋体
```

---

## 🎨 示例项目

### 示例 1: 绘制图表
```gox
// 绘制柱状图
for i := 0; i < 5; i++ {
    height := data[i] * 2
    dc.DrawRectangle(i*50, 300-height, 40, height)
    dc.Fill()
}
```

### 示例 2: 生成二维码背景
```gox
// 创建彩色背景
dc.SetRGB(0.1, 0.1, 0.5)
dc.Clear()

// 绘制装饰图案
for i := 0; i < 100; i++ {
    x := rand.Float64() * 800
    y := rand.Float64() * 600
    dc.DrawCircle(x, y, rand.Float64()*10)
    dc.Fill()
}
```

### 示例 3: 制作表情包
```gox
// 加载底图
img, _ := gg.LoadImage("base.png")
dc.DrawImage(img, 0, 0)

// 添加文字
dc.LoadFontFace("Arial", 48)
dc.SetRGB(1, 1, 1)
dc.DrawString("WHEN YOU CODE", 50, 100)
dc.DrawString("AND IT WORKS", 50, 160)

dc.SavePNG("meme.png")
```

---

## 📚 相关资源

- **gg 官方仓库**: https://github.com/fogleman/gg
- **gg 示例代码**: https://github.com/fogleman/gg/tree/master/examples
- **walk GUI 库**: https://github.com/lxn/walk
- **Fyne GUI 库**: https://fyne.io/
- **Giu GUI 库**: https://github.com/AllenDang/giu

---

## 🎯 总结

**gg 库的优势**：
- ✅ 简单易用，API 直观
- ✅ 零依赖，安装方便
- ✅ 功能丰富，支持多种图形
- ✅ 跨平台，Windows/Mac/Linux 通用
- ✅ 纯 Go 实现，无需 CGO

**适用场景**：
- ✅ 数据可视化（图表、图形）
- ✅ 图片处理和合成
- ✅ 生成艺术和图案
- ✅ 制作表情包和海报
- ✅ 教学演示和原型设计

**配合 Gox 使用**：
- ✅ 语法更简洁
- ✅ 类型自动推断
- ✅ 可见性自动转换
- ✅ 编译速度快

现在就开始你的 gg 绘图之旅吧！🎨✨
