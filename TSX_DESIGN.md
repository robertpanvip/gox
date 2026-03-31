# TSX 函数式组件系统设计文档

## 🎯 核心设计理念

### 1. 函数式组件（Function Components）

**核心思想**：组件是纯函数，接收 Props，返回 TemplateResult

```tsx
// TSX 源码
fx func Counter(props) {
    let count = props.initialCount
    return <button text={`Count: ${count}`} />
}

fx func App() {
    return <Counter initialCount={5} />
}
```

```go
// 编译后的 Go 代码
func Counter(props CounterProps) gui.TemplateResult {
    count := useSignal(props.initialCount)
    return gui.TemplateResult{
        Static: []gui.Component{
            gui.NewButton(ButtonProps{Text: "Count: "}),
        },
        Dynamic: []gui.TemplateBinding{
            {Path: "Text", Value: func() string { 
                return fmt.Sprintf("Count: %d", count.Get()) 
            }},
        },
    }
}

func App() gui.TemplateResult {
    return Counter(CounterProps{InitialCount: 5})  // ✅ 返回 TemplateResult
}
```

**关键点**：
- ✅ 所有 `fx func` 都返回 `gui.TemplateResult`
- ✅ 组件调用返回 `TemplateResult`
- ✅ `TemplateResult` 可以嵌套（组件返回的 TemplateResult 会被包装）

### 2. 组件分类

#### 内置组件（Built-in Components）
- 小写标签：`<div>`, `<view>`, `<button>`, `<text>`
- 使用 `NewXxx` 构造函数
- 返回 `gui.Component`

```tsx
<div text="Hello" />
```

```go
gui.NewDiv(DivProps{Text: "Hello"})
```

#### 自定义组件（Custom Components）
- 大写标签：`<Counter>`, `<UserCard>`, `<MyComponent>`
- 直接调用组件函数
- 返回 `gui.TemplateResult`

```tsx
<Counter initialCount={5} />
```

```go
Counter(CounterProps{InitialCount: 5})
```

---

## 📐 编译转换规则

### 规则 1：标签名映射

| TSX 标签 | 组件类型 | 编译结果 |
|----------|----------|----------|
| `<div>` | 内置 | `gui.NewDiv(...)` |
| `<view>` | 内置 | `gui.NewView(...)` |
| `<button>` | 内置 | `gui.NewButton(...)` |
| `<Counter>` | 自定义 | `Counter(...)` |
| `<UserCard>` | 自定义 | `UserCard(...)` |

**判断规则**：
- 首字母小写 → 内置组件 → `gui.NewXxx()`
- 首字母大写 → 自定义组件 → `Xxx()`

### 规则 2：Props 传递

```tsx
// 静态属性
<button text="Click Me" width="100px" />

// 动态属性（模板字符串）
<button text={`Count: ${count}`} />

// 事件处理器
<button onClick={() => count++} />

// 子元素
<div>
    <button text="Child" />
</div>
```

```go
// 静态属性
gui.NewButton(ButtonProps{
    Text: "Click Me",
    Width: "100px",
})

// 动态属性（模板字符串）
gui.NewButton(ButtonProps{
    Text: "Count: ",
})
// Dynamic 部分单独处理

// 事件处理器
gui.NewButton(ButtonProps{
    OnClick: func() { count.Set(count.Get() + 1) },
})

// 子元素
gui.NewDiv(DivProps{
    Children: []gui.Component{
        gui.NewButton(ButtonProps{Text: "Child"}),
    },
})
```

### 规则 3：状态变量转换

```tsx
fx func Counter() {
    let count = 0      // ← 声明状态变量
    count++            // ← 更新状态
    return <button text={`Count: ${count}`} />
}
```

```go
func Counter() gui.TemplateResult {
    Count := useSignal(0)  // ← 转换为 useSignal
    
    // 更新操作
    Count.Set(Count.Get() + 1)
    
    // 读取值
    return gui.TemplateResult{
        Dynamic: []gui.TemplateBinding{
            {Path: "Text", Value: func() string {
                return fmt.Sprintf("Count: %d", Count.Get())
            }},
        },
    }
}
```

---

## 🏗️ 运行时架构

### 1. TemplateResult 结构

```go
type TemplateResult struct {
    Static  []Component       // 静态组件（作为 key 复用）
    Dynamic []TemplateBinding // 动态绑定（需要更新的部分）
}
```

**设计思想**：
- **Static**：不变的组件结构，作为 Diff 的 key
- **Dynamic**：动态的绑定，状态变化时更新

### 2. 渲染流程

```
App.Run()
  ↓
App.Draw()
  ↓
Root.Render()
  ↓
FxWrapper.Render()
  ↓
TemplateResult.Render()
  ↓
Component.Render()
```

### 3. 更新流程

```
State.Set(newValue)
  ↓
Signal.Notify()
  ↓
Component.RequestUpdate()
  ↓
FxWrapper.template = componentFunc()
  ↓
TemplateResult.Update()
  ↓
Diff & Patch
  ↓
Re-render
```

---

## 📝 完整示例

### 示例 1：基础计数器

```tsx
// counter.gox
import "github.com/gox-lang/gox/gui"

fx func Counter(props) {
    let count = props.initialCount
    
    return <div>
        <text text={`Count: ${count}`} />
        <button text="Increment" onClick={() => count++} />
        <button text="Decrement" onClick={() => count--} />
    </div>
}

fx func App() {
    return <Counter initialCount={0} />
}

func Main() {
    let app = gui.NewApp("Counter", 400, 300)
    app.SetRootComponentFunc(App)
    app.Run()
}
```

编译后：

```go
package main

import (
    "fmt"
    "github.com/gox-lang/gox/gui"
)

func Counter(props CounterProps) gui.TemplateResult {
    Count := useSignal(props.initialCount)
    
    return gui.TemplateResult{
        Static: []gui.Component{
            gui.NewDiv(DivProps{
                Children: []gui.Component{
                    gui.NewText(TextProps{Text: "Count: "}),
                    gui.NewButton(ButtonProps{Text: "Increment"}),
                    gui.NewButton(ButtonProps{Text: "Decrement"}),
                },
            }),
        },
        Dynamic: []gui.TemplateBinding{
            {Path: "Text", Value: func() string {
                return fmt.Sprintf("Count: %d", Count.Get())
            }},
            {Path: "OnClick", Value: func() {
                Count.Set(Count.Get() + 1)
            }},
            {Path: "OnClick", Value: func() {
                Count.Set(Count.Get() - 1)
            }},
        },
    }
}

func App() gui.TemplateResult {
    return Counter(CounterProps{InitialCount: 0})
}

func main() {
    app := gui.NewApp("Counter", 400, 300)
    app.SetRootComponentFunc(App)
    app.Run()
}
```

### 示例 2：组件组合

```tsx
fx func UserCard(props) {
    return <div>
        <text text={props.name} />
        <text text={props.email} />
    </div>
}

fx func UserList(props) {
    return <div>
        {props.users.map(user => 
            <UserCard name={user.name} email={user.email} />
        )}
    </div>
}
```

---

## 🔧 Transformer 实现要点

### 1. 组件类型判断

```go
func (t *Transformer) isCustomComponent(tagName string) bool {
    return len(tagName) > 0 && tagName[0] >= 'A' && tagName[0] <= 'Z'
}
```

### 2. 内置组件转换

```go
func transformBuiltinComponent(tsx) string {
    return fmt.Sprintf("gui.New%s(%sProps{...})", componentName, props)
}
```

### 3. 自定义组件转换

```go
func transformCustomComponent(tsx) string {
    return fmt.Sprintf("%s(%sProps{...})", componentName, props)
}
```

### 4. 状态变量收集

```go
func collectStateVars(body *ast.BlockStmt) []StateVar {
    // 遍历所有 let 声明
    // 标记为状态变量
}
```

### 5. 模板字符串处理

```go
// `Click Me${count}` → 
// Static: "Click Me"
// Dynamic: count
func parseTemplateString(s string) (format string, exprs []string)
```

---

## 🎨 组件设计原则

### 1. 纯函数

组件应该是纯函数：
- 相同的 Props → 相同的 TemplateResult
- 副作用放在事件处理器中

### 2. 单向数据流

```
Parent (state) → Props → Child
                          ↓
                    Event → Parent (update state)
```

### 3. 状态提升

共享状态应该提升到共同的父组件：

```tsx
fx func App() {
    let count = 0  // ← 共享状态
    
    return <div>
        <Counter count={count} />
        <Counter count={count} />
    </div>
}
```

---

## 📊 性能优化策略

### 1. 组件复用

基于 Static 部分作为 key 复用组件实例

### 2. 增量更新

只更新 Dynamic 部分变化的绑定

### 3. 惰性求值

Dynamic 绑定的 Value 函数只在需要时调用

---

## 🔮 未来扩展

### 1. Hooks 支持

```tsx
fx func MyComponent() {
    let [count, setCount] = useState(0)
    
    useEffect(() => {
        println("Count changed:", count)
    }, [count])
    
    return <button text={count} />
}
```

### 2. Context 支持

```tsx
let theme = useContext(ThemeContext)
```

### 3. Memoization

```tsx
let ExpensiveComponent = memo((props) => {
    return <div>{props.value}</div>
})
```

---

**文档版本**: v2.0  
**最后更新**: 2026-03-30  
**状态**: 设计中
