# Lit-HTML 风格组件系统设计文档

**日期**: 2024-01-XX  
**版本**: 1.0  
**状态**: 已实现

---

## 目录

1. [概述](#概述)
2. [核心设计思想](#核心设计思想)
3. [架构设计](#架构设计)
4. [运行时实现](#运行时实现)
5. [编译器转换](#编译器转换)
6. [渲染流程](#渲染流程)
7. [更新机制](#更新机制)
8. [API 设计](#api 设计)
9. [示例代码](#示例代码)
10. [性能优化](#性能优化)

---

## 概述

### 设计目标

实现一个类似 lit-html 的函数式组件系统，具有以下特点：

1. **精确更新** - 只更新实际变化的部分，不重新渲染整个组件树
2. **高性能** - 通过 Part 系统实现细粒度更新
3. **简洁 API** - 使用 `fx func` 声明组件，类似 lit-html 的模板语法
4. **闭包状态** - 状态变量作为闭包变量，自然持久化

### 设计灵感

- **lit-html** - Part 系统和模板渲染机制
- **SolidJS** - 细粒度响应式更新
- **Go 语言** - 简洁的语法和类型系统

---

## 核心设计思想

### 1. TemplateResult 结构

```go
type TemplateResult struct {
    StaticCode string                    // 静态模板标识
    Dynamic    []interface{}             // 动态值数组
    Factory    func() (Component, []Part) // 工厂函数
}
```

**设计理念**：
- `StaticCode` 用于快速比较模板是否相同
- `Dynamic` 保存动态值，用于比较变化
- `Factory` 延迟创建组件树，避免重复创建

### 2. Part 系统

```go
type Part interface {
    Update(value interface{})
}
```

**Part 的类型**：
- `TextPart` - 文本动态部分
- `AttributePart` - 属性动态部分
- `ChildPart` - 子组件动态部分

**设计理念**：
- Part 持有占位符节点的引用
- Part 负责更新特定位置的内容
- 通过 Part 实现精确更新

### 3. Comment 占位符

```go
type Comment struct {
    BaseComponent
    Data     string    // 注释数据
    RealNode Component // 实际替换的组件
}
```

**设计理念**：
- Comment 作为占位符，标记动态内容的位置
- Comment 可以替换为实际的组件
- 类似 lit-html 的注释占位符 `<!--?-->`

---

## 架构设计

### 整体架构图

```
┌─────────────────────────────────────────────────┐
│              源代码 (fx func)                    │
└──────────────────┬──────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────┐
│           编译器 (Transformer)                    │
│  - 解析 TSX                                     │
│  - 提取动态部分                                  │
│  - 生成 TemplateResult 代码                      │
└──────────────────┬──────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────┐
│              生成的 Go 代码                        │
│  func Counter() func() TemplateResult { ... }   │
└──────────────────┬──────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────┐
│              运行时 (Runtime)                     │
│  - FxWrapper                                    │
│  - TemplateResult                               │
│  - Part 系统                                     │
└──────────────────┬──────────────────────────────┘
                   │
                   ▼
┌─────────────────────────────────────────────────┐
│              渲染结果                             │
│  - Component 树                                  │
│  - 精确更新                                      │
└─────────────────────────────────────────────────┘
```

### 文件结构

```
go-ts/
├── gui/
│   ├── fx_component.go      # TemplateResult 和 FxWrapper
│   ├── comment.go           # Comment 占位符节点
│   ├── part.go              # Part 接口和实现
│   ├── div.go               # Div 组件（DOM API）
│   ├── button.go            # Button 组件
│   └── types.go             # 类型定义
├── transformer/
│   ├── transformer.go       # 主转换器
│   ├── transformer_lithtml.go # lit-html 风格转换器
│   └── transformer_lit_test.go # 测试用例
└── test/
    └── *.gox                # 测试文件
```

---

## 运行时实现

### TemplateResult

```go
type TemplateResult struct {
    StaticCode string
    Dynamic    []interface{}
    Factory    func() (Component, []Part)
}

// Render 渲染模板结果（第一次渲染）
func (t *TemplateResult) Render(screen *ebiten.Image) {
    if t.Factory != nil {
        root, _ := t.Factory()
        if root != nil && root.IsVisible() {
            root.Render(screen)
        }
    }
}

// Update 从新的 TemplateResult 更新
func (t *TemplateResult) Update(new TemplateResult, parts []Part) {
    // 比较 Dynamic 数组
    for i, newValue := range new.Dynamic {
        if i < len(t.Dynamic) {
            oldValue := t.Dynamic[i]
            
            // 值变化了，更新对应的 Part
            if newValue != oldValue && i < len(parts) {
                parts[i].Update(newValue)
            }
        }
    }
    
    // 更新 Dynamic 数组
    t.Dynamic = new.Dynamic
}
```

### FxWrapper

```go
type FxWrapper struct {
    BaseComponent
    componentFunc func() TemplateResult
    lastTemplate  *TemplateResult  // 上次的 TemplateResult
    parts         []Part           // Parts 数组引用
    root          Component        // 根组件
}

func (f *FxWrapper) Render(screen *ebiten.Image) {
    // 执行组件函数，获取 TemplateResult
    newTemplate := f.componentFunc()
    
    // 检查是否有上次的 TemplateResult
    if f.lastTemplate != nil {
        // 比较 StaticCode
        if newTemplate.StaticCode == f.lastTemplate.StaticCode {
            // 模板相同，比较并更新 Dynamic
            f.lastTemplate.Update(newTemplate, f.parts)
        } else {
            // 模板不同，重新创建组件树
            root, parts := newTemplate.Factory()
            f.root = root
            f.parts = parts
            
            // 初始化 Dynamic 值
            for i, value := range newTemplate.Dynamic {
                if i < len(parts) {
                    parts[i].Update(value)
                }
            }
        }
    } else {
        // 第一次渲染，创建组件树
        root, parts := newTemplate.Factory()
        f.root = root
        f.parts = parts
        
        // 初始化 Dynamic 值
        for i, value := range newTemplate.Dynamic {
            if i < len(parts) {
                parts[i].Update(value)
            }
        }
    }
    
    // 保存当前的 TemplateResult
    f.lastTemplate = &newTemplate
    
    // 渲染根组件
    if f.root != nil {
        f.root.Render(screen)
    }
}
```

### Comment 节点

```go
type Comment struct {
    BaseComponent
    Data     string
    RealNode Component
}

func (c *Comment) AppendChild(child Component) {
    c.RealNode = child
}

func (c *Comment) RemoveChild(child Component) {
    if c.RealNode == child {
        c.RealNode = nil
    }
}

func (c *Comment) ReplaceChild(newChild, oldChild Component) {
    if c.RealNode == oldChild {
        c.RealNode = newChild
    }
}

func (c *Comment) Render(screen *ebiten.Image) {
    if c.RealNode != nil {
        c.RealNode.Render(screen)
    }
}
```

### Part 实现

```go
// TextPart 文本动态部分
type TextPart struct {
    Placeholder *Comment
}

func (p *TextPart) Update(value interface{}) {
    if text, ok := value.(string); ok {
        textNode := NewText(text)
        p.Placeholder.AppendChild(textNode)
    }
}

// AttributePart 属性动态部分
type AttributePart struct {
    Element Component
    Name    string
}

func (p *AttributePart) Update(value interface{}) {
    if str, ok := value.(string); ok {
        if element, ok := p.Element.(interface{ SetAttribute(string, string) }); ok {
            element.SetAttribute(p.Name, str)
        }
    }
}

// ChildPart 子组件动态部分
type ChildPart struct {
    Placeholder *Comment
    Current     Component
}

func (p *ChildPart) Update(value interface{}) {
    if component, ok := value.(Component); ok {
        if p.Current != nil {
            p.Placeholder.ReplaceChild(component, p.Current)
        } else {
            p.Placeholder.AppendChild(component)
        }
        p.Current = component
    }
}
```

---

## 编译器转换

### 转换规则

#### 1. 文本插值

**源代码**：
```tsx
fx func Counter() {
    let message = "Hello"
    return <button>{message}</button>
}
```

**生成代码**：
```go
func Counter() func() gui.TemplateResult {
    message := "Hello"
    return func() gui.TemplateResult {
        return gui.TemplateResult{
            StaticCode: `<button>`,
            Dynamic: []interface{}{message},
            Factory: func() (gui.Component, []gui.Part) {
                comment0 := gui.NewComment("dynamic-0")
                part0 := gui.NewTextPart(comment0)
                root := gui.NewButton(gui.ButtonProps{
                    Children: []gui.Component{comment0},
                })
                return root, []gui.Part{part0}
            },
        }
    }
}
```

#### 2. 属性插值

**源代码**：
```tsx
fx func Title() {
    let title = "My App"
    return <div text={title} />
}
```

**生成代码**：
```go
func Title() func() gui.TemplateResult {
    title := "My App"
    return func() gui.TemplateResult {
        return gui.TemplateResult{
            StaticCode: `<div>`,
            Dynamic: []interface{}{title},
            Factory: func() (gui.Component, []gui.Part) {
                comment0 := gui.NewComment("dynamic-0")
                part0 := gui.NewAttributePart(comment0, "text")
                root := gui.NewDiv(gui.DivProps{})
                return root, []gui.Part{part0}
            },
        }
    }
}
```

#### 3. 多个动态部分

**源代码**：
```tsx
fx func Greeting() {
    let name = "World"
    let count = 42
    return <div>{name}: {count}</div>
}
```

**生成代码**：
```go
func Greeting() func() gui.TemplateResult {
    name := "World"
    count = 42
    return func() gui.TemplateResult {
        return gui.TemplateResult{
            StaticCode: `<div>`,
            Dynamic: []interface{}{name, count},
            Factory: func() (gui.Component, []gui.Part) {
                comment0 := gui.NewComment("dynamic-0")
                comment1 := gui.NewComment("dynamic-1")
                part0 := gui.NewTextPart(comment0)
                part1 := gui.NewTextPart(comment1)
                root := gui.NewDiv(gui.DivProps{
                    Children: []gui.Component{comment0, comment1},
                })
                return root, []gui.Part{part0, part1}
            },
        }
    }
}
```

### 转换器实现

核心方法：
- `transformFxFunc()` - 转换 FX 函数
- `transformTSX()` - 转换 TSX 元素
- `extractDynamicValues()` - 提取动态值
- `createComponent()` - 创建组件

---

## 渲染流程

### 第一次渲染

```
1. 调用组件函数，获取 TemplateResult
2. 调用 Factory 创建组件树和 Parts
3. 遍历 Dynamic 数组，调用 Parts[i].Update(value)
4. 渲染根组件
```

### 后续渲染

```
1. 调用组件函数，获取新的 TemplateResult
2. 比较 StaticCode：
   - 相同：比较 Dynamic，更新变化的 Parts
   - 不同：重新调用 Factory 创建新组件树
3. 渲染根组件
```

---

## 更新机制

### 精确更新流程

```
用户操作 → 状态变化 → 重新渲染
    ↓
FxWrapper.Render()
    ↓
比较 StaticCode
    ↓
相同？→ 是 → 比较 Dynamic
    ↓         ↓
   否      值变化？
    ↓         ↓
重新创建   是 → Part.Update()
组件树      ↓
          更新 DOM
```

### 性能优势

1. **不重新创建组件** - 组件树只创建一次
2. **精确更新** - 只更新变化的 Part
3. **值缓存** - Part 内部缓存当前值，避免不必要的更新

---

## API 设计

### 组件声明

```go
fx func ComponentName() {
    // 状态变量
    let state = initialValue
    
    // 返回 TSX
    return <jsx>...</jsx>
}
```

### DOM API

```go
// Div 组件支持标准 DOM API
div.AppendChild(child Component)
div.InsertBefore(newChild, refChild Component)
div.RemoveChild(child Component)
div.ReplaceChild(newChild, oldChild Component)
div.SetAttribute(name, value string)
div.GetAttribute(name string) string
```

### Comment API

```go
comment.AppendChild(child Component)
comment.RemoveChild(child Component)
comment.ReplaceChild(newChild, oldChild Component)
comment.SetData(data string)
comment.GetData() string
```

---

## 示例代码

### 计数器组件

```tsx
fx func Counter() {
    let count = 0
    
    return <button 
        text={`Count: ${count}`} 
        onClick={func() {
            count = count + 1
        }} 
    />
}
```

### 列表组件

```tsx
fx func TodoList() {
    let items = []string{"A", "B", "C"}
    
    return <ul>
        {items.map(item => <li>{item}</li>)}
    </ul>
}
```

### 条件渲染

```tsx
fx func Conditional() {
    let show = true
    
    return <div>
        {show ? <A /> : <B />}
    </div>
}
```

---

## 性能优化

### 1. 组件复用

- 组件树只创建一次
- Parts 持有引用，直接更新

### 2. 精确更新

- 只更新变化的 Part
- 不触发整个组件树的重新渲染

### 3. 值比较

- Part 内部缓存当前值
- 值未变化时跳过更新

### 4. 延迟创建

- Factory 函数延迟创建组件
- 避免不必要的创建开销

---

## 总结

本设计文档详细描述了一个 lit-html 风格的组件系统，核心特点包括：

1. **TemplateResult** - 包含静态模板、动态值和工厂函数
2. **Part 系统** - 实现精确更新的更新器
3. **Comment 占位符** - 标记动态内容的位置
4. **FxWrapper** - 组件包装器，支持缓存和更新

通过这套设计，我们实现了类似 lit-html 的高性能渲染机制，同时保持了 Go 语言的简洁性和类型安全性。

---

**附录**：
- [LIT_HTML_STYLE_GUIDE.md](./LIT_HTML_STYLE_GUIDE.md) - 实现指南
- [transformer/transformer_lit_test.go](./transformer/transformer_lit_test.go) - 测试用例
