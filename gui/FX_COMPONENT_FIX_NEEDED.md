# FX Component 编译问题修复指南

## 问题描述

当前 gox.exe 和 gox_new.exe 二进制文件是从旧版本源代码编译的，不支持 `fx func` 语法。

### 症状

1. Token 输出显示 `ident(fx)` 而不是 `fx` 关键字
2. FX 函数被解析为普通函数而不是 FX 组件
3. 生成的代码是 `func counter()` 而不是 FX 组件结构体

### 根本原因

Lexer 中的 `keywords` map 虽然包含 `"fx": FX`，但现有的 gox.exe 二进制文件是从添加 FX 支持之前的代码编译的。

## 解决方案

需要重新编译 gox 编译器。

### 步骤

1. 确保 Go 环境已安装（需要 Go 1.21+）
2. 在项目根目录运行：
   ```bash
   go build -o gox.exe cmd/gox/main.go
   ```
3. 将生成的 gox.exe 复制到 test 目录：
   ```bash
   copy gox.exe test\
   ```

### 验证

运行测试：
```bash
cd test
.\gox.exe tsx_fx_component.gox
```

期望的 Token 输出应该显示：
```
fx
func
ident(Counter)
```

而不是：
```
ident(fx)
func
ident(Counter)
```

## 已完成的实现

一旦 gox 被正确重新编译，以下功能应该可以正常工作：

1. ✅ FX 函数解析（parser 已支持）
2. ✅ FX 组件转换（transformer_fx.go 已实现）
3. ✅ 状态变量检测（let 声明）
4. ✅ 模板字符串状态感知转换（`c.name`, `c.count` 前缀）
5. ✅ 事件处理器自动转换（添加 `c.` 前缀和 `RequestUpdate()`）
6. ✅ BaseFxComponent 运行时支持
7. ✅ TemplateResult 和 TemplatePart 细粒度更新

## 测试用例

test/tsx_fx_component.gox:

```gox
fx func Counter() {
    let count = 0
    let name = "World"
    
    return <div style={{padding: "20px", flexDirection: "column"}}>
        <label text={`Hello ${name}!`} fontSize={16} />
        <label text={`Count: ${count}`} fontSize={16} />
        <button text="Increment" onClick={() => {
            count++
        }} />
        <button text="Change Name" onClick={() => {
            name = "Gopher"
        }} />
    </div>
}
```

期望生成的 Go 代码：

```go
type Counter struct {
    gui.BaseFxComponent
    count int
    name string
    rootComponent gui.Component
    dynamicParts []gui.TemplatePart
}

func NewCounter() *Counter {
    c := &Counter{
        count: 0,
        name: "World",
    }
    
    // 创建根组件（带状态感知）
    c.rootComponent = gui.NewDiv(DivProps{
        Style: &gui.Style{Padding: "20px", FlexDirection: "column"},
    }, 
        gui.NewLabel(LabelProps{
            Text: fmt.Sprintf("Hello %v!", c.name),
            FontSize: 16,
        }),
        gui.NewLabel(LabelProps{
            Text: fmt.Sprintf("Count: %v", c.count),
            FontSize: 16,
        }),
        gui.NewButton(ButtonProps{
            Text: "Increment",
            OnClick: func() {
                c.count++
                c.RequestUpdate()
            },
        }),
        gui.NewButton(ButtonProps{
            Text: "Change Name",
            OnClick: func() {
                c.name = "Gopher"
                c.RequestUpdate()
            },
        }),
    )
    
    // 创建动态部分
    c.dynamicParts = make([]gui.TemplatePart, 0)
    c.dynamicParts = append(c.dynamicParts, gui.NewTextPart(nil, func() string {
        return fmt.Sprintf("%v", c.name)
    }))
    c.dynamicParts = append(c.dynamicParts, gui.NewTextPart(nil, func() string {
        return fmt.Sprintf("%v", c.count)
    }))
    
    // 设置模板结果
    c.SetTemplateResult(&gui.TemplateResult{
        StaticParts: []gui.Component{c.rootComponent},
        DynamicParts: c.dynamicParts,
    })
    
    return c
}
```

## 当前状态

- 源代码已更新 ✅
- 需要重新编译 gox ⏳
- 等待验证测试结果
