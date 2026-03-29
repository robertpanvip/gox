# FX 组件自动更新机制 - 待实现

## 目标

实现编译器自动识别变量修改并插入 `RequestUpdate()` 调用。

## 当前状态

### ✅ 已完成
1. 词法分析支持 `fx func`
2. 解析器正确解析 FX 函数
3. 收集状态变量（let 声明）
4. 分析依赖关系（识别哪些变量被使用）
5. 检测变量修改（识别赋值语句和自增/自减）

### ❌ 待完成
1. **事件处理器中的变量访问添加 `c.` 前缀**
   - 输入：`count++`
   - 输出：`c.count++`

2. **自动插入 `RequestUpdate()` 调用**
   - 输入：
     ```typescript
     onClick={() => {
         count++
     }}
     ```
   - 输出：
     ```go
     OnClick: func() {
         c.count++
         c.RequestUpdate()  // ← 自动插入
     }
     ```

## 实现思路

### 方案 1：修改 TSX 转换器（推荐）

在 `transformer_expr.go` 的 `transformTSXElement` 函数中，特殊处理事件处理器属性：

```go
func (t *Transformer) transformTSXElement(e *ast.TSXElement) string {
    for _, attr := range e.Attributes {
        if strings.HasPrefix(attr.Name, "on") {
            // 事件处理器
            if funcLit, ok := attr.Value.(*ast.FunctionLiteral); ok {
                // 1. 分析函数体中的变量修改
                mutatedVars := t.findMutatedVariables(funcLit.Body)
                
                // 2. 转换函数体，添加 c. 前缀
                bodyCode := t.transformFunctionBodyWithPrefix(funcLit.Body, "c.")
                
                // 3. 如果有变量被修改，在末尾添加 RequestUpdate()
                if len(mutatedVars) > 0 {
                    bodyCode += "\nc.RequestUpdate()"
                }
                
                return fmt.Sprintf("gui.%sProps{OnClick: func() {\n%s\n}}", 
                    componentName, bodyCode)
            }
        }
    }
}
```

### 方案 2：后处理生成的代码

在 `transformFxFunc` 函数返回前，对生成的代码进行后处理：

```go
func (t *Transformer) transformFxFunc(f *ast.FuncDecl) string {
    code := t.generateFxComponent(...)
    
    // 后处理：在事件处理器中添加 c. 前缀和 RequestUpdate()
    code = t.postProcessEventHandler(code)
    
    return code
}
```

## 具体实现

### 1. 检测变量修改

```go
func (t *Transformer) findMutatedVariables(body *ast.BlockStmt) []string {
    mutated := make([]string, 0)
    
    for _, stmt := range body.List {
        switch s := stmt.(type) {
        case *ast.AssignStmt:
            if ident, ok := s.LHS.(*ast.Ident); ok {
                mutated = append(mutated, ident.Name)
            }
        case *ast.ExprStmt:
            if unary, ok := s.X.(*ast.UnaryExpr); ok {
                if unary.Op == token.INC || unary.Op == token.DEC {
                    if ident, ok := unary.X.(*ast.Ident); ok {
                        mutated = append(mutated, ident.Name)
                    }
                }
            }
        }
    }
    
    return mutated
}
```

### 2. 转换函数体并添加前缀

```go
func (t *Transformer) transformFunctionBodyWithPrefix(body *ast.BlockStmt, prefix string) string {
    var sb strings.Builder
    
    for _, stmt := range body.List {
        sb.WriteString("        ")
        
        switch s := stmt.(type) {
        case *ast.AssignStmt:
            // 添加前缀
            if ident, ok := s.LHS.(*ast.Ident); ok {
                sb.WriteString(fmt.Sprintf("%s%s", prefix, ident.Name))
                sb.WriteString(" " + s.Op + " ")
                sb.WriteString(t.transformExpr(s.RHS))
                sb.WriteString("\n")
            }
        case *ast.ExprStmt:
            if unary, ok := s.X.(*ast.UnaryExpr); ok {
                if unary.Op == token.INC || unary.Op == token.DEC {
                    if ident, ok := unary.X.(*ast.Ident); ok {
                        sb.WriteString(fmt.Sprintf("%s%s%s\n", prefix, ident.Name, unary.Op))
                    }
                }
            }
        }
    }
    
    return sb.String()
}
```

### 3. 生成事件处理器代码

```go
func (t *Transformer) generateEventHandler(funcLit *ast.FunctionLiteral, stateVars []FxStateVar) string {
    var sb strings.Builder
    
    sb.WriteString("func() {\n")
    
    // 分析哪些变量被修改
    mutatedVars := t.findMutatedVariables(funcLit.Body)
    
    // 转换函数体，添加 c. 前缀
    bodyCode := t.transformFunctionBodyWithPrefix(funcLit.Body, "c.")
    sb.WriteString(bodyCode)
    
    // 如果有变量被修改，添加 RequestUpdate()
    if len(mutatedVars) > 0 {
        sb.WriteString("\n        c.RequestUpdate()")
    }
    
    sb.WriteString("\n    }")
    
    return sb.String()
}
```

## 测试用例

### 输入
```typescript
fx func Counter() {
    let count = 0
    let name = "World"
    
    return <div>
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
    Count interface{}
    Name  interface{}
    rootComponent gui.Component
    dynamicParts []gui.TemplatePart
}

func NewCounter() *Counter {
    c := &Counter{
        Count: 0,
        Name:  "World",
    }
    
    c.rootComponent = gui.NewDiv(nil,
        gui.NewLabel(gui.LabelProps{
            Text: fmt.Sprintf("Count: %v", c.Count),
        }),
        gui.NewButton(gui.ButtonProps{
            Text: "Click",
            OnClick: func() {
                c.Count++              // ← 自动添加 c. 前缀
                c.RequestUpdate()      // ← 自动插入
            },
        }),
    )
    
    c.dynamicParts = make([]gui.TemplatePart, 0)
    c.dynamicParts = append(c.dynamicParts,
        gui.NewTextPart(nil, func() string {
            return fmt.Sprintf("Count: %v", c.Count)
        }),
    )
    
    c.SetTemplateResult(&gui.TemplateResult{
        StaticParts:  []gui.Component{c.rootComponent},
        DynamicParts: c.dynamicParts,
    })
    
    return c
}
```

## 下一步

1. 实现 `findMutatedVariables()` 函数
2. 实现 `transformFunctionBodyWithPrefix()` 函数
3. 修改 `transformTSXElement()` 或创建专门的 `transformFXEventHandler()` 函数
4. 测试并验证生成的代码
