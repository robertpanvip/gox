# FX 组件实现进度

## 已实现功能

### 1. 解析器支持 ✅

**文件**: [`parser/parser_decl.go`](file:///e:/Soft/JetBrains/WebStorm%20Work%20Space/go-ts/parser/parser_decl.go)

- 添加 `fx func` 语法支持
- 新增 `parseFxFuncDecl()` 函数
- 修改 AST 的 `FuncDecl` 添加 `IsFx` 字段

**使用示例**:
```typescript
fx function Counter() {
    let count = 0
    let name = "World"
    
    return <div>
        <label text={`Hello ${name}!`} />
        <label text={`Count: ${count}`} />
    </div>
}
```

### 2. 词法支持 ✅

**文件**: [`token/token.go`](file:///e:/Soft/JetBrains/WebStorm%20Work%20Space/go-ts/token/token.go)

- 添加 `FX` 关键字
- 注册到 keywords 映射表

### 3. AST 支持 ✅

**文件**: [`ast/ast.go`](file:///e:/Soft/JetBrains/WebStorm%20Work%20Space/go-ts/ast/ast.go)

- `FuncDecl` 添加 `IsFx` 字段标记 FX 函数

### 4. 运行时支持 ✅

**文件**: 
- [`gui/fx_component.go`](file:///e:/Soft/JetBrains/WebStorm%20Work%20Space/go-ts/gui/fx_component.go)
- [`gui/template_part.go`](file:///e:/Soft/JetBrains/WebStorm%20Work%20Space/go-ts/gui/template_part.go)

已实现：
- `BaseFxComponent` 基类
- `RequestUpdate()` 更新机制
- `TemplateResult` 模板结果管理
- `TextPart` 和 `AttributePart` 动态片段
- 细粒度更新支持

### 5. Transformer 支持 ✅

**文件**: [`transformer/transformer_fx.go`](file:///e:/Soft/JetBrains/WebStorm%20Work%20Space/go-ts/transformer/transformer_fx.go)

已实现：
- `transformFxFunc()` - FX 函数转换入口
- `collectStateVars()` - 收集状态变量
- `analyzeDependencies()` - 分析变量依赖
- `generateFxComponentStruct()` - 生成组件结构体
- `generateFxConstructor()` - 生成构造函数
- `transformTSXForFx()` - TSX 转换

## 编译器生成的代码示例

**输入** (TSX):
```typescript
fx function Counter() {
    let count = 0
    let name = "World"
    
    return <div style={{padding: "20px"}}>
        <label text={`Hello ${name}!`} />
        <label text={`Count: ${count}`} />
        <button text="Increment" onClick={() => {
            count++
            RequestUpdate()
        }} />
    </div>
}
```

**输出** (Go):
```go
// Counter FX 组件（lit-html 风格）
type Counter struct {
    gui.BaseFxComponent
    
    // 状态变量
    Count int
    Name  string
    
    // 静态组件（创建一次）
    rootComponent gui.Component
    
    // 动态部分（可更新）
    dynamicParts []gui.TemplatePart
}

// NewCounter 创建 Counter 组件
func NewCounter() *Counter {
    c := &Counter{
        Count: 0,
        Name:  "World",
    }
    
    // 创建根组件
    c.rootComponent = gui.NewDiv(&gui.Style{Padding: "20px"}, 
        gui.NewLabel(gui.LabelProps{Text: fmt.Sprintf("Hello %v!", c.Name)}),
        gui.NewLabel(gui.LabelProps{Text: fmt.Sprintf("Count: %v", c.Count)}),
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
    c.dynamicParts = append(c.dynamicParts, gui.NewTextPart(nil, func() string {
        return fmt.Sprintf("%v", c.Name)
    }))
    c.dynamicParts = append(c.dynamicParts, gui.NewTextPart(nil, func() string {
        return fmt.Sprintf("%v", c.Count)
    }))
    
    // 设置模板结果
    c.SetTemplateResult(&gui.TemplateResult{
        StaticParts: []gui.Component{c.rootComponent},
        DynamicParts: c.dynamicParts,
    })
    
    return c
}
```

## 工作原理

### 1. 编译时处理

1. **解析器**识别 `fx func` 关键字
2. **Transformer** 收集状态变量（let 声明）
3. **分析依赖**：遍历 TSX 找出变量使用位置
4. **生成代码**：
   - 组件结构体（包含状态变量字段）
   - 构造函数（创建静态组件）
   - 动态部分（绑定状态变量）

### 2. 运行时机制

1. **初始化**：
   - 执行一次构造函数
   - 创建所有静态组件
   - 创建动态部分绑定函数

2. **状态更新**：
   - 用户操作触发事件处理器
   - 修改状态变量
   - 调用 `RequestUpdate()`
   - 遍历 `dynamicParts` 调用 `Update()`
   - 只更新依赖的组件

### 3. 细粒度更新

```go
// 状态变化时
c.count++
c.RequestUpdate()

// 只更新依赖 count 的部分
for _, part := range c.dynamicParts {
    part.Update()  // 只更新 TextPart 对应的 Label
}
```

## 当前状态

✅ **已完成**:
- 解析器支持 `fx func` 语法
- 运行时基础架构（BaseFxComponent, TemplateResult, TemplatePart）
- Transformer 基础框架
- 状态变量收集
- 依赖分析框架

⏳ **待完善**:
- 完整的 TSX 到组件的映射生成
- 精确的依赖追踪（目前简化处理）
- 事件处理器中的变量修改分析
- 嵌套组件的依赖分析
- 列表渲染的细粒度更新

## 测试示例

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

## 下一步计划

1. **完善 TSX 生成**：
   - 完整的 TSX 元素转换
   - 支持嵌套组件
   - 支持列表映射

2. **优化依赖分析**：
   - 精确追踪每个变量的使用位置
   - 分析事件处理器中的修改
   - 生成最优的更新代码

3. **性能优化**：
   - 避免不必要的更新
   - 批量更新优化
   - 内存优化

4. **文档和示例**：
   - 完整的使用文档
   - 更多示例代码
   - 性能对比测试

## 参考

- [lit-html 官方文档](https://lit.dev/)
- [LitElement 源码分析](https://github.com/lit/lit)
- [Solid.js 细粒度响应式](https://www.solidjs.com/)
