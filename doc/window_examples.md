# Windows 窗口程序示例

Gox 可以配合各种 Go GUI 库创建窗口程序。以下是三种主流方案：

## 方案 1: Fyne (推荐 ⭐⭐⭐⭐⭐)

**跨平台** | **现代化** | **易用**

### 安装
```bash
go get fyne.io/fyne/v2
```

### 完整示例代码

```go
package main

import (
    "fyne.io/fyne/v2/app"
    "fyne.io/fyne/v2/widget"
    "fyne.io/fyne/v2/container"
)

func main() {
    myApp := app.New()
    myWindow := myApp.NewWindow("Gox + Fyne Demo")
    
    helloLabel := widget.NewLabel("Hello from Gox!")
    
    nameEntry := widget.NewEntry()
    nameEntry.SetPlaceHolder("Enter your name")
    
    clickButton := widget.NewButton("Click Me", func() {
        helloLabel.SetText("Hello, " + nameEntry.Text + "!")
    })
    
    content := container.NewVBox(
        helloLabel,
        nameEntry,
        clickButton,
    )
    
    myWindow.SetContent(content)
    myWindow.ShowAndRun()
}
```

### 使用 Gox 编写

创建 `window.gox`:
```gox
package main

import go "fyne.io/fyne/v2/app"
import go "fyne.io/fyne/v2/widget"
import go "fyne.io/fyne/v2/container"

public func Main() {
    let myApp = app.New()
    let myWindow = myApp.NewWindow("Gox + Fyne Demo")
    
    let helloLabel = widget.NewLabel("Hello from Gox!")
    let nameEntry = widget.NewEntry()
    nameEntry.SetPlaceHolder("Enter your name")
    
    let clickButton = widget.NewButton("Click Me", func() {
        helloLabel.SetText("Hello, " + nameEntry.Text + "!")
    })
    
    let content = container.NewVBox(
        helloLabel,
        nameEntry,
        clickButton,
    )
    
    myWindow.SetContent(content)
    myWindow.ShowAndRun()
}
```

### 编译运行
```bash
gox.exe window.gox -o window.go
go run window.go
```

---

## 方案 2: Giu (轻量级 ⭐⭐⭐⭐)

**轻量** | **快速** | **即时模式**

### 安装
```bash
go get github.com/AllenDang/giu
```

### 完整示例代码

```go
package main

import (
    g "github.com/AllenDang/giu"
)

var name = "World"

func loop() {
    g.SingleWindow().Layout(
        g.Label("Hello from Gox!"),
        g.InputText(&name).Label("Enter your name"),
        g.Button("Click Me", onClick),
    )
}

func onClick() {
    println("Hello, " + name)
}

func main() {
    g.Main(loop)
}
```

### 编译运行
```bash
gox.exe window_giu.gox -o window_giu.go
go run window_giu.go
```

---

## 方案 3: Walk (Windows 专用 ⭐⭐⭐)

**原生** | **小体积** | **仅 Windows**

### 安装
```bash
go get github.com/lxn/walk
```

### 完整示例代码

```go
package main

import (
    "github.com/lxn/walk"
    . "github.com/lxn/walk/declarative"
)

func main() {
    var nameEdit *walk.LineEdit
    
    MainWindow{
        Title:  "Gox + Walk Demo",
        Size:   Size{Width: 400, Height: 300},
        Layout: VBox{},
        Children: []Widget{
            Label{Text: "Hello from Gox!"},
            LineEdit{AssignTo: &nameEdit, Placeholder: "Enter your name"},
            PushButton{
                Text: "Click Me",
                OnClicked: func() {
                    walk.MsgBox(nil, "Greeting", "Hello, " + nameEdit.Text(), walk.MsgBoxOK)
                },
            },
        },
    }.Run()
}
```

---

## 对比总结

| 特性 | Fyne | Giu | Walk |
|------|------|-----|------|
| **跨平台** | ✅ Win/Mac/Linux | ✅ Win/Mac/Linux | ❌ 仅 Windows |
| **安装难度** | 简单 | 简单 | 中等 |
| **二进制大小** | 较大 (~10MB) | 中等 (~5MB) | 小 (~3MB) |
| **学习曲线** | 低 | 中 | 中 |
| **外观** | 现代化 | 极简 | 原生 Windows |
| **推荐度** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ |

---

## 推荐

### 初学者 → **Fyne**
- 文档最全
- API 最直观
- 社区最活跃
- 跨平台支持

### 追求轻量 → **Giu**
- 二进制最小
- 启动最快
- 适合工具类应用

### Windows 专用 → **Walk**
- 原生 Windows 外观
- 体积小巧
- 性能好

---

## 快速开始

1. **选择库** (推荐 Fyne)

2. **安装库**
   ```bash
   go get fyne.io/fyne/v2
   ```

3. **编写 Gox 代码**
   - 使用 `import go` 导入 GUI 库
   - 创建窗口和控件
   - 添加事件处理

4. **编译运行**
   ```bash
   gox.exe your_code.gox -o your_code.go
   go run your_code.go
   ```

---

## 注意事项

1. **闭包支持**: Gox 对闭包的支持还在完善中，复杂回调可能需要直接使用 Go 语法

2. **字段访问**: 结构体字段访问需要注意大小写转换（public 字段自动大写）

3. **事件处理**: 按钮点击等事件处理函数需要定义为单独的 public func

4. **依赖安装**: 首次运行前需要先安装 GUI 库依赖

---

## 示例项目结构

```
project/
├── main.gox           # Gox 源代码
├── main.go            # 生成的 Go 代码 (自动)
├── go.mod             # Go 模块配置
├── go.sum             # 依赖校验
└── output.png         # 输出文件 (如果有)
```

---

## 下一步

1. 查看官方文档获取更多信息
2. 运行 `test_gg_draw.gox` 查看简单的绘图示例
3. 尝试修改示例代码创建自己的窗口程序

祝你编程愉快！🎉
