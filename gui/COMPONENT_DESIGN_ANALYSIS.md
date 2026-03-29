# 组件设计与 TSX 结合问题分析

## 📊 当前设计分析

### 1. 现有组件架构

#### 组件类型
```
Component (接口)
├── BaseComponent (基类)
│   ├── Div (容器组件)
│   ├── Label (文本组件)
│   └── Button (按钮组件)
└── BaseFxComponent (FX 组件基类)
    └── 用户自定义 FX 组件
```

#### 当前设计特点

**Div 组件**：
```go
type Div struct {
    BaseComponent
    Props DivProps
    Style *Style
}

func NewDiv(props interface{}, children ...Component) *Div
```

**Label 组件**：
```go
type Label struct {
    BaseComponent
    Props LabelProps
}

func NewLabel(props LabelProps) *Label
```

**Button 组件**：
```go
type Button struct {
    BaseComponent
    Props ButtonProps
    OnClickFunc func()
}

func NewButton(props ButtonProps) *Button
```

### 2. TSX 转换现状

#### TSX 输入
```typescript
<div style={{padding: "20px"}}>
    <label text={`Hello ${name}!`} fontSize={16} />
    <button onClick={() => {
        count++
    }} />
</div>
```

#### 当前生成的代码
```go
gui.NewDiv(&gui.Style{Padding: "20px"},
    gui.NewLabel(LabelProps{
        Text: fmt.Sprintf("Hello %v!", name),  // ❌ 问题：没有 c. 前缀
        FontSize: 16,
    }),
    gui.NewButton(ButtonProps{
        Text: "Click",
        OnClick: func() {
            count++  // ❌ 问题：没有 c. 前缀，没有 RequestUpdate()
        },
    }),
)
```

#### 期望生成的代码
```go
gui.NewDiv(&gui.Style{Padding: "20px"},
    gui.NewLabel(LabelProps{
        Text: fmt.Sprintf("Hello %v!", c.name),  // ✅ 有 c. 前缀
        FontSize: 16,
    }),
    gui.NewButton(ButtonProps{
        Text: "Click",
        OnClick: func() {
            c.count++           // ✅ 有 c. 前缀
            c.RequestUpdate()   // ✅ 自动调用
        },
    }),
)
```

## ❌ 当前设计的问题

### 问题 1: 组件属性设计不统一

**Div** 使用 `interface{}` 作为 props 类型：
```go
func NewDiv(props interface{}, children ...Component) *Div
```

**Label** 使用具体的 `LabelProps`：
```go
func NewLabel(props LabelProps) *Label
```

**问题**：
- TSX 转换时需要特殊处理
- 无法统一处理属性验证
- 代码生成复杂

### 问题 2: 事件处理器设计不一致

**Button** 有 `OnClickFunc` 字段：
```go
type Button struct {
    OnClickFunc func()
}

func (b *Button) SetOnClick(handler func()) {
    b.OnClickFunc = handler
}
```

**Div** 没有事件处理器：
```go
type Div struct {
    // 没有事件处理器字段
}
```

**问题**：
- 不是所有组件都支持事件
- TSX 中 `onClick` 属性无法统一处理
- 需要为每个组件单独实现事件处理

### 问题 3: FX 组件与普通组件混用问题

**FX 组件**：
```go
type Counter struct {
    BaseFxComponent
    count int  // 状态变量
}
```

**普通组件**：
```go
type Div struct {
    BaseComponent
    Props DivProps
}
```

**问题**：
- FX 组件的状态变量在结构体字段中
- 普通组件的状态在 Props 中
- 两种模式混用导致代码生成复杂

### 问题 4: 缺少统一的更新机制

**当前更新方式**：
1. FX 组件：`RequestUpdate()` → 更新 TemplateResult
2. 普通组件：手动调用 `SetText()`, `SetProps()` 等

**问题**：
- 更新机制不统一
- 无法实现统一的响应式系统
- 代码生成时需要区分处理

### 问题 5: TSX 转换缺少上下文

**问题**：
- TSX 转换时不知道是否在 FX 组件中
- 无法自动添加 `c.` 前缀
- 无法自动插入 `RequestUpdate()`

## 🔧 改进方案

### 方案 1: 统一组件属性设计（推荐）

所有组件使用统一的 Props 接口：

```go
// Props 所有组件属性的接口
type Props interface {
    Apply(component Component)
}

// Div 组件
type Div struct {
    BaseComponent
    Props Props
}

func NewDiv(props Props, children ...Component) *Div {
    d := &Div{}
    if props != nil {
        props.Apply(d)
    }
    // 添加 children
    for _, child := range children {
        d.AddChild(child)
    }
    return d
}

// Label 组件
type Label struct {
    BaseComponent
    Props Props
}

type LabelProps struct {
    Text      string
    FontSize  float64
    TextColor Color
}

func (p LabelProps) Apply(component Component) {
    if label, ok := component.(*Label); ok {
        label.Props = p
    }
}
```

**优点**：
- 统一的属性处理方式
- TSX 转换简单
- 易于扩展

### 方案 2: 统一事件处理器设计

为所有组件添加事件处理器支持：

```go
// EventProps 事件处理器接口
type EventProps interface {
    SetupEvents(component Component)
}

// BaseComponent 添加事件处理器
type BaseComponent struct {
    // ... 现有字段
    
    // 事件处理器
    OnClickFunc    func()
    OnMouseUpFunc  func()
    OnMouseDownFunc func()
    // ... 更多事件
}

// 实现事件处理
func (b *BaseComponent) OnClick(x, y int) {
    if b.OnClickFunc != nil {
        b.OnClickFunc()
    }
    // 递归调用子组件
    for _, child := range b.Children {
        child.OnClick(x, y)
    }
}
```

**TSX 转换**：
```typescript
<div onClick={() => {
    count++
}}>
```

```go
gui.NewDiv(&gui.Style{}, 
    gui.WithOnClick(func() {
        c.count++
        c.RequestUpdate()
    })
)
```

### 方案 3: FX 组件专用 TSX 转换

为 FX 组件创建专门的 TSX 转换函数：

```go
// transformTSXForFx 为 FX 组件转换 TSX
func (t *Transformer) transformTSXForFx(tsx *ast.TSXElement, context string, stateVars []FxStateVar) string {
    // 1. 分析事件处理器中的变量修改
    mutatedVars := t.findMutatedVariables(tsx)
    
    // 2. 转换 TSX，添加 c. 前缀
    goCode := t.transformExprWithPrefix(tsx, "c.")
    
    // 3. 为事件处理器添加 RequestUpdate()
    goCode = t.insertRequestUpdate(goCode, mutatedVars)
    
    return goCode
}
```

### 方案 4: 重新设计 FX 组件架构（最彻底）

完全采用响应式设计，类似 Solid.js：

```go
// Signal 响应式信号
type Signal[T any] struct {
    value T
    effects []func()
}

func (s *Signal[T]) Get() T {
    // 记录依赖
    return s.value
}

func (s *Signal[T]) Set(v T) {
    s.value = v
    // 触发所有依赖这个信号的效果
    for _, effect := range s.effects {
        effect()
    }
}

// FX 组件
type Counter struct {
    BaseFxComponent
    count *Signal[int]
    name  *Signal[string]
}

func NewCounter() *Counter {
    c := &Counter{
        count: NewSignal(0),
        name:  NewSignal("World"),
    }
    
    // 创建组件，自动绑定信号
    c.rootComponent = gui.NewDiv(&gui.Style{},
        gui.NewLabel(gui.LabelProps{
            Text: c.count.Get(), // 自动记录依赖
        }),
        gui.NewButton(gui.ButtonProps{
            OnClick: func() {
                c.count.Set(c.count.Get() + 1) // 自动触发更新
            },
        }),
    )
    
    return c
}
```

**TSX**：
```typescript
fx func Counter() {
    let count = 0  // 编译器自动转换为 *Signal[int]
    
    return <div>
        <label text={count} />  // 编译器自动转换为 count.Get()
        <button onClick={() => {
            count++  // 编译器自动转换为 count.Set(count.Get() + 1)
        }} />
    </div>
}
```

## 📝 建议的改进步骤

### 短期（立即修复）

1. **修复 TSX 转换器**
   - 为 FX 组件创建专门的转换函数
   - 在事件处理器中自动添加 `c.` 前缀
   - 自动插入 `RequestUpdate()`

2. **统一事件处理器**
   - 为 BaseComponent 添加事件处理器字段
   - 所有组件继承统一的事件处理机制

### 中期（架构优化）

3. **统一 Props 设计**
   - 所有组件使用统一的 Props 接口
   - 简化 TSX 转换逻辑

4. **完善 FX 组件**
   - 实现完整的依赖追踪
   - 优化更新机制

### 长期（彻底重构）

5. **响应式信号系统**
   - 实现 Signal 响应式原语
   - 编译器自动转换变量为 Signal
   - 完全自动化的响应式更新

## 🎯 结论

**当前最紧急的问题**：
1. ✅ TSX 转换缺少上下文（不知道在 FX 组件中）
2. ✅ 事件处理器中的变量访问没有 `c.` 前缀
3. ✅ 没有自动插入 `RequestUpdate()`

**建议的解决方案**：
- **短期**：修改 TSX 转换器，为 FX 组件创建专门的转换逻辑
- **中期**：统一组件属性和事件处理器设计
- **长期**：考虑采用响应式信号系统（类似 Solid.js）

**是否要彻底重构**：
- 如果项目处于早期阶段，建议采用**方案 4（响应式信号）**
- 如果已有大量代码，建议**逐步改进**，先解决 TSX 转换问题
