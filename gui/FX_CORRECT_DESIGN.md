# FX 组件正确设计

## 📊 核心概念

### Props vs State

```typescript
fx func Counter(props) {
    // Props：从父组件传递进来的参数
    // - 由父组件控制
    // - 变化时触发组件更新
    let initialCount = props.initialCount  // ← props
    
    // State：组件内部的状态
    // - 由组件自己控制
    // - 变化时触发组件更新
    let count = 0      // ← state
    let name = "World" // ← state
    
    return <div>
        {/* props 变化时更新 */}
        <label text={`Initial: ${props.initialCount}`} />
        
        {/* state 变化时更新 */}
        <label text={`Count: ${count}`} />
        
        <button onClick={() => {
            count++  // state 变化
        }} />
    </div>
}
```

## ✅ 正确的设计

### 1. 两者都应该触发更新

| 变化来源 | 触发更新 | 示例 |
|---------|---------|------|
| Props 变化 | ✅ 是 | 父组件传递新值 |
| State 变化 | ✅ 是 | 内部状态变化 |

### 2. 生成的代码结构

```go
type Counter struct {
    gui.BaseFxComponent
    
    // Props（从参数来）
    InitialCount int
    
    // State（内部状态）
    Count int
    Name  string
}

func NewCounter(props CounterProps) *Counter {
    c := &Counter{
        // 初始化 props
        InitialCount: props.InitialCount,
        
        // 初始化 state
        Count: 0,
        Name:  "World",
    }
    
    // 创建组件
    c.rootComponent = gui.NewDiv(...,
        gui.NewLabel(gui.LabelProps{
            Text: fmt.Sprintf("Initial: %v", c.InitialCount),  // props
        }),
        gui.NewLabel(gui.LabelProps{
            Text: fmt.Sprintf("Count: %v", c.Count),  // state
        }),
    )
    
    // 创建动态部分
    c.dynamicParts = make([]gui.TemplatePart, 0)
    c.dynamicParts = append(c.dynamicParts, 
        // props 依赖
        gui.NewTextPart(nil, func() string {
            return fmt.Sprintf("Initial: %v", c.InitialCount)
        }),
        // state 依赖
        gui.NewTextPart(nil, func() string {
            return fmt.Sprintf("Count: %v", c.Count)
        }),
    )
    
    return c
}
```

### 3. 更新机制

#### Props 更新（由父组件触发）

```go
// 父组件调用
counter.InitialCount = 10
counter.RequestUpdate()  // 或者由框架自动调用
```

#### State 更新（由内部触发）

```go
// 事件处理器中
c.button.OnClick(func() {
    c.Count++
    c.RequestUpdate()  // 自动插入
})
```

## 🎯 关键点

### 1. Props 和 State 都是响应式的

```typescript
fx func Counter(props) {
    let count = 0
    
    return <div>
        {/* 两者都会触发更新 */}
        <label text={props.initialCount} />  // ✅ props 变化会更新
        <label text={count} />               // ✅ state 变化会更新
    </div>
}
```

### 2. 编译器需要做的

1. **识别 props**：函数参数
2. **识别 state**：let 声明的变量
3. **分析依赖**：哪些组件使用了哪些变量
4. **生成更新代码**：
   - props 变化 → 调用 `RequestUpdate()`
   - state 变化 → 自动插入 `RequestUpdate()`

### 3. 生成的依赖关系

```go
// 依赖分析结果
dependencies := []FxDependency{
    {
        VarName:     "initialCount",
        IsProp:      true,   // ← 是 props
        UsedIn:      []string{"label1"},
        NeedsUpdate: false,  // ← props 不会被修改
    },
    {
        VarName:     "count",
        IsProp:      false,  // ← 是 state
        UsedIn:      []string{"label2"},
        MutatedIn:   []string{"button.onClick"},
        NeedsUpdate: true,   // ← state 会被修改，需要自动更新
    },
}
```

## 📝 测试用例

### 输入

```typescript
fx func Counter(props) {
    let count = 0
    
    return <div>
        <label text={`Initial: ${props.initialCount}`} />
        <label text={`Count: ${count}`} />
        <button onClick={() => {
            count++
        }} />
    </div>
}
```

### 期望输出

```go
type Counter struct {
    gui.BaseFxComponent
    InitialCount int  // props
    Count        int  // state
}

func NewCounter(props CounterProps) *Counter {
    c := &Counter{
        InitialCount: props.InitialCount,
        Count:        0,
    }
    
    c.rootComponent = gui.NewDiv(...,
        gui.NewLabel(gui.LabelProps{
            Text: fmt.Sprintf("Initial: %v", c.InitialCount),
        }),
        gui.NewLabel(gui.LabelProps{
            Text: fmt.Sprintf("Count: %v", c.Count),
        }),
        gui.NewButton(gui.ButtonProps{
            OnClick: func() {
                c.Count++
                c.RequestUpdate()  // 自动插入
            },
        }),
    )
    
    c.dynamicParts = append(c.dynamicParts,
        gui.NewTextPart(nil, func() string {
            return fmt.Sprintf("Initial: %v", c.InitialCount)  // props 依赖
        }),
        gui.NewTextPart(nil, func() string {
            return fmt.Sprintf("Count: %v", c.Count)  // state 依赖
        }),
    )
    
    return c
}
```

## 🚀 实现步骤

1. ✅ 识别 props（函数参数）
2. ✅ 识别 state（let 声明）
3. ✅ 分析依赖（哪些组件使用了哪些变量）
4. ⏳ 生成更新代码（props 和 state 都支持更新）
5. ⏳ 事件处理器中自动插入 `RequestUpdate()`

## 💡 总结

- **Props**：外部传入，变化时触发更新
- **State**：内部管理，变化时触发更新
- **两者都是响应式的**
- **编译器需要为两者都生成更新代码**
