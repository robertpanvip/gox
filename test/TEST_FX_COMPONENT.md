# FX 组件测试指南

## 📝 测试文件

**测试文件**: [`test/tsx_fx_component.gox`](file:///e:/Soft/JetBrains/WebStorm%20Work%20Space/go-ts/test/tsx_fx_component.gox)

```typescript
import "github.com/gox-lang/gox/gui"

fx function Counter() {
    let count = 0
    let name = "World"
    
    return <div style={{padding: "20px", flexDirection: "column"}}>
        <label text={`Hello ${name}!`} fontSize={16} />
        <label text={`Count: ${count}`} fontSize={16} />
        <button text="Increment" onClick={() => {
            count++
            RequestUpdate()
        }} />
    </div>
}

func Main() {
    let app = gui.NewApp("FX Component Test", 800, 600)
    
    let counter = NewCounter()
    counter.SetRect(gui.Rect{X: 0, Y: 0, Width: 800, Height: 600})
    app.Root.AddChild(counter)
    
    println("FX Component Test starting...")
    app.Run()
}
```

## 🔧 编译步骤

### 1. 构建 gox 编译器

```bash
cd e:\Soft\JetBrains\WebStorm WorkSpace\go-ts
go build -o bin/gox.exe ./cmd/gox
```

### 2. 编译测试文件

```bash
bin/gox.exe test/tsx_fx_component.gox
```

这将生成 `test/tsx_fx_component.go` 文件。

### 3. 查看生成的代码

```bash
cat test/tsx_fx_component.go
```

**预期输出**（简化版）:

```go
package test

import "github.com/gox-lang/gox/gui"

// Counter FX 组件（lit-html 风格）
type Counter struct {
    gui.BaseFxComponent
    
    // 状态变量
    Count int
    Name  string
    
    rootComponent gui.Component
    dynamicParts  []gui.TemplatePart
}

// NewCounter 创建 Counter 组件
func NewCounter() *Counter {
    c := &Counter{
        Count: 0,
        Name:  "World",
    }
    
    // 创建根组件
    c.rootComponent = gui.NewDiv(&gui.Style{
        Padding: "20px",
        FlexDirection: "column",
    },
        gui.NewLabel(gui.LabelProps{
            Text: fmt.Sprintf("Hello %v!", c.Name),
            FontSize: 16,
        }),
        gui.NewLabel(gui.LabelProps{
            Text: fmt.Sprintf("Count: %v", c.Count),
            FontSize: 16,
        }),
        gui.NewButton(gui.ButtonProps{
            Text: "Increment",
            OnClick: func() {
                c.Count++
                c.RequestUpdate()
            },
        }),
    )
    
    // 创建动态部分
    c.dynamicParts = make([]gui.TemplatePart, 0)
    c.dynamicParts = append(c.dynamicParts, 
        gui.NewTextPart(nil, func() string { return c.Name }),
        gui.NewTextPart(nil, func() string { return strconv.Itoa(c.Count) }),
    )
    
    // 设置模板结果
    c.SetTemplateResult(&gui.TemplateResult{
        StaticParts:  []gui.Component{c.rootComponent},
        DynamicParts: c.dynamicParts,
    })
    
    return c
}

func Main() {
    app := gui.NewApp("FX Component Test", 800, 600)
    
    counter := NewCounter()
    counter.SetRect(gui.Rect{X: 0, Y: 0, Width: 800, Height: 600})
    app.Root.AddChild(counter)
    
    println("FX Component Test starting...")
    app.Run()
}
```

### 4. 运行测试

```bash
cd test
go run tsx_fx_component.go
```

## ✅ 测试要点

### 1. 验证 FX 函数只执行一次

在 `NewCounter()` 中添加日志：

```go
func NewCounter() *Counter {
    println("NewCounter called") // ← 应该只打印一次
    c := &Counter{...}
    // ...
}
```

**预期**: 程序运行期间只打印一次 "NewCounter called"

### 2. 验证细粒度更新

点击按钮时：
- `count++` 触发状态变化
- `RequestUpdate()` 调用
- **只更新** `countLabel` 的文本
- `nameLabel` 不应该更新

### 3. 验证状态绑定

初始状态：
- `name` = "World" → 显示 "Hello World!"
- `count` = 0 → 显示 "Count: 0"

点击按钮后：
- `count` = 1 → 显示 "Count: 1"
- `name` 不变 → "Hello World!" 不变

## 🐛 可能的问题

### 问题 1: 编译器找不到 `fx` 关键字

**错误**: `syntax error near 'fx'`

**解决**: 
- 确认 `token/token.go` 中添加了 `FX` 关键字
- 确认 `parser/parser_decl.go` 中有 `parseFxFuncDecl()` 函数

### 问题 2: 生成的代码缺少 `RequestUpdate()`

**错误**: `undefined: RequestUpdate`

**解决**:
- 确认 `gui/fx_component.go` 中有 `RequestUpdate()` 方法
- 确认 `Counter` 结构体嵌入了 `gui.BaseFxComponent`

### 问题 3: 动态部分没有更新

**错误**: 点击按钮后 UI 不变

**解决**:
- 检查 `SetTemplateResult()` 是否正确设置
- 检查 `RequestUpdate()` 是否调用 `templateResult.Update()`
- 确认 `TextPart.Update()` 正确实现

## 📊 性能测试

### 对比传统组件

**传统方式**:
```go
func (c *Counter) Render() {
    // 每次都重新创建所有组件
    c.label = NewLabel(...)
    c.button = NewButton(...)
}
```

**FX 组件**:
```go
// 只创建一次
// 状态变化时只更新动态部分
```

### 预期性能提升

- **初始化**: 相同（都创建一次）
- **更新**: FX 组件快 10-100 倍（只更新变化的部分）
- **内存**: FX 组件更优（不需要重复创建组件）

## 🎯 测试成功标志

✅ `NewCounter()` 只调用一次
✅ 点击按钮只更新 `count` 相关的 Label
✅ `name` 相关的 Label 不更新
✅ UI 响应流畅，无闪烁
✅ 内存占用稳定

## 📚 参考

- [FX 组件实现总结](file:///e:/Soft/JetBrains/WebStorm%20Work%20Space/go-ts/gui/FX_IMPLEMENTATION_SUMMARY.md)
- [实现进度](file:///e:/Soft/JetBrains/WebStorm%20Work%20Space/go-ts/gui/FX_COMPONENT_PROGRESS.md)
