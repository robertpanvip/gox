# Lit-HTML 风格实现指南

## 核心设计

### 1. TemplateResult 结构
```go
type TemplateResult struct {
    StaticCode string        // 静态模板标识
    Dynamic    []interface{} // 动态值数组
    Factory    func() (Component, []Part) // 工厂函数
}
```

### 2. Part 系统
```go
type Part interface {
    Update(value interface{})
}

// TextPart - 文本动态部分
type TextPart struct {
    Placeholder *Comment
}

// AttributePart - 属性动态部分
type AttributePart struct {
    Element Component
    Name    string
}

// ChildPart - 子组件动态部分
type ChildPart struct {
    Placeholder *Comment
    Current     Component
}
```

### 3. Comment 占位符
```go
type Comment struct {
    BaseComponent
    Data     string    // 注释数据
    RealNode Component // 实际替换的组件
}
```

## 编译器转换规则

### 文本插值
```tsx
// 源代码
<button>{message}</button>

// 生成代码
return gui.TemplateResult{
    StaticCode: `<button>`,
    Dynamic: []interface{}{message},
    Factory: func() (gui.Component, []gui.Part) {
        comment := gui.NewComment("dynamic-0")
        textPart := gui.NewTextPart(comment)
        button := gui.NewButton(gui.ButtonProps{
            Children: []gui.Component{comment},
        })
        return button, []gui.Part{textPart}
    },
}
```

### 属性插值
```tsx
// 源代码
<div text={title} />

// 生成代码
return gui.TemplateResult{
    StaticCode: `<div>`,
    Dynamic: []interface{}{title},
    Factory: func() (gui.Component, []gui.Part) {
        div := gui.NewDiv(gui.DivProps{})
        attrPart := gui.NewAttributePart(div, "text")
        return div, []gui.Part{attrPart}
    },
}
```

### 条件渲染
```tsx
// 源代码
<div>{show ? <A /> : <B />}</div>

// 生成代码
return gui.TemplateResult{
    StaticCode: `<div>`,
    Dynamic: []interface{}{show},
    Factory: func() (gui.Component, []gui.Part) {
        comment := gui.NewComment("dynamic-0")
        childPart := gui.NewChildPart(comment)
        div := gui.NewDiv(gui.DivProps{
            Children: []gui.Component{comment},
        })
        return div, []gui.Part{childPart}
    },
}
```

## 运行时更新流程

### 第一次渲染
1. 执行组件函数获取 TemplateResult
2. 调用 Factory 创建组件树和 Parts
3. 遍历 Dynamic 数组，调用 Parts[i].Update(value)
4. 渲染根组件

### 后续渲染
1. 执行组件函数获取新的 TemplateResult
2. 比较 StaticCode：
   - 相同：比较 Dynamic，更新变化的 Parts
   - 不同：重新调用 Factory 创建新组件树
3. 渲染根组件

## 文件清单

### 核心运行时
- gui/fx_component.go - TemplateResult 和 FxWrapper
- gui/comment.go - Comment 占位符节点
- gui/part.go - Part 接口和实现
- gui/div.go - Div 组件（DOM API）
- gui/button.go - Button 组件

### 编译器
- transformer/transformer_lit.go - lit-html 风格转换器
- parser/parser.go - 解析器（支持新的语法）

### 测试文件
- test/lit_test.go - lit-html 风格测试

## 待删除的旧文件

### TSX 相关
- ast/ast.go 中的 TSXElement 相关代码
- transformer/transformer_hoc.go - 旧的 HOC 转换器
- transformer/transformer_fx.go - 旧的 FX 转换器
- transformer/transformer_expr.go - TSX 表达式转换
- 所有 .gox 测试文件

### 文档
- TSX_COMPONENT_ARCHITECTURE.md
- 所有旧的 FX 设计文档
