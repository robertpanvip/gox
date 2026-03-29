# Lit-HTML 风格的 FX 组件实现

## 核心思路

按照 lit-html 的设计模式，实现一个**只执行一次**的 fx 函数组件，状态变化时**只更新变化的部分**（细粒度更新）。

## lit-html 的核心机制

### 1. 模板字符串静态分析

```javascript
// lit-html 示例
function render() {
  return html`
    <div>
      <h1>${title}</h1>
      <p>Count: ${count}</p>
      <button @click=${increment}>Click</button>
    </div>
  `;
}
```

lit-html 会将模板分为：
- **静态部分**：HTML 结构（`<div>`, `<h1>`, `<p>`, `<button>`）
- **动态部分**：`${title}`, `${count}`, `${increment}`

### 2. Parts 数组

lit-html 创建一个 **parts 数组**来保存每个动态部分的更新函数：

```javascript
const parts = [
  { type: 'text', target: h1Element, render: () => title },
  { type: 'text', target: pElement, render: () => count },
  { type: 'event', target: button, render: increment }
];
```

### 3. 细粒度更新

当 `count` 变化时：
1. 调用 `requestUpdate()`
2. 只执行 `parts[1].update()` 
3. 其他部分不受影响

## Go 实现方案

### 1. 基础架构

```go
// BaseFxComponent fx 组件基类
type BaseFxComponent struct {
    templateResult *TemplateResult
}

// RequestUpdate 请求更新
func (b *BaseFxComponent) RequestUpdate() {
    b.templateResult.Update() // 只更新动态部分
}
```

### 2. 模板结果（TemplateResult）

```go
type TemplateResult struct {
    StaticParts []Component    // 静态组件（创建一次）
    DynamicParts []TemplatePart // 动态片段（可更新）
}

func (t *TemplateResult) Update() {
    for _, part := range t.DynamicParts {
        part.Update() // 只更新动态部分
    }
}
```

### 3. 动态片段（TemplatePart）

```go
// TextPart 文本片段
type TextPart struct {
    Target *Label
    Render func() string
}

func (p *TextPart) Update() {
    p.Target.SetText(p.Render())
}
```

### 4. FX 函数组件示例

```go
// fx function Counter() {
//     let count = 0
//     let name = "World"
//     
//     return <div>
//         <label text={`Hello ${name}!`} />
//         <label text={`Count: ${count}`} />
//         <button onClick={() => {
//             count++
//             RequestUpdate()
//         }} />
//     </div>
// }
```

### 5. 编译器生成的代码

```go
type Counter struct {
    gui.BaseFxComponent
    count int
    name  string
    
    // 静态组件
    rootDiv    *gui.Div
    nameLabel  *gui.Label
    countLabel *gui.Label
    button     *gui.Button
    
    // 动态部分
    namePart  *gui.TextPart
    countPart *gui.TextPart
}

func NewCounter() *Counter {
    c := &Counter{
        count: 0,
        name:  "World",
    }
    
    // 创建静态组件（只执行一次）
    c.rootDiv = gui.NewDiv(&gui.Style{...})
    c.nameLabel = gui.NewLabel(gui.LabelProps{...})
    c.countLabel = gui.NewLabel(gui.LabelProps{...})
    c.button = gui.NewButton(gui.ButtonProps{...})
    
    // 创建动态部分（绑定状态变量）
    c.namePart = gui.NewTextPart(c.nameLabel, func() string {
        return "Hello " + c.name + "!"
    })
    
    c.countPart = gui.NewTextPart(c.countLabel, func() string {
        return "Count: " + strconv.Itoa(c.count)
    })
    
    // 设置初始值
    c.namePart.Update()
    c.countPart.Update()
    
    // 事件处理器
    c.button.OnClick(func() {
        c.count++
        c.RequestUpdate() // 触发更新
    })
    
    // 添加到根组件
    c.rootDiv.AddChild(c.nameLabel)
    c.rootDiv.AddChild(c.countLabel)
    c.rootDiv.AddChild(c.button)
    
    // 设置模板结果
    c.SetTemplateResult(&gui.TemplateResult{
        StaticParts: []Component{c.rootDiv},
        DynamicParts: []TemplatePart{c.namePart, c.countPart},
    })
    
    return c
}
```

## 编译器需要实现的功能

### 1. 识别 `fx func` 关键字

```typescript
// 解析器需要支持
fx function Counter() { ... }
```

### 2. 收集状态变量

```typescript
// 扫描函数体内的 let 声明
let count = 0   // → 加入状态列表
let name = "A"  // → 加入状态列表
```

### 3. 分析 TSX 中的依赖

```typescript
// 分析模板中的变量使用
<label text={`Hello ${name}!`} />  // → name 依赖
<label text={`Count: ${count}`} /> // → count 依赖
```

### 4. 分析事件处理器中的修改

```typescript
<button onClick={() => {
    count++        // → count 被修改
    RequestUpdate() // → 触发更新
}} />
```

### 5. 生成优化代码

```go
// 为每个状态变量生成更新逻辑
// 只更新依赖该变量的组件
```

## 优势

1. **fx 函数只执行一次**：初始化后不再重新执行
2. **细粒度更新**：状态变化只更新依赖的部分
3. **高性能**：避免不必要的重新渲染
4. **类似 lit-html**：符合现代前端开发习惯

## 下一步

1. ✅ 实现 `BaseFxComponent` 基类
2. ✅ 实现 `TemplateResult` 和 `TemplatePart`
3. ✅ 创建示例代码验证设计
4. ⏳ 修改解析器支持 `fx func` 语法
5. ⏳ 实现变量依赖分析
6. ⏳ 生成优化的更新代码
