# FX 组件状态更新机制实现进展

## ✅ 已完成的工作

### 1. 核心架构
- ✅ `BaseFxComponent` 基类
- ✅ `RequestUpdate()` 更新机制
- ✅ `TemplateResult` 和 `TemplatePart` 系统

### 2. 编译器支持
- ✅ `fx func` 语法解析
- ✅ 状态变量收集（let 声明）
- ✅ 依赖分析（识别哪些变量被使用）

### 3. 状态修改检测
- ✅ `hasStateMutation()` - 检查函数体是否修改状态
- ✅ `stmtMutatesState()` - 检查语句是否修改状态
  - ✅ 赋值语句：`count = 1`
  - ✅ 自增/自减：`count++`（通过 `UnaryExpr.Post` 检测）
  - ✅ 复合赋值：`count += 1`（作为 AssignStmt 处理）
  - ✅ 递归检查：if/else/for/块语句

### 4. 代码生成
- ✅ 生成组件结构体
- ✅ 生成构造函数
- ✅ 生成动态部分（TextPart）

## ❌ 待完成的工作

### 关键问题：事件处理器转换

**当前生成的代码**：
```go
gui.NewButton(ButtonProps{
    OnClick: func() {
        count++  // ❌ 没有 c. 前缀
        // ❌ 没有 RequestUpdate()
    }
})
```

**期望生成的代码**：
```go
gui.NewButton(ButtonProps{
    OnClick: func() {
        c.count++  // ✅ 添加 c. 前缀
        c.RequestUpdate()  // ✅ 自动插入
    }
})
```

### 需要修改的地方

**文件**：`transformer/transformer_expr.go`

**修改点**：
1. 在转换事件处理器时，为变量访问添加 `c.` 前缀
2. 在检测到状态修改后，在函数末尾添加 `c.RequestUpdate()`

## 🔧 实现方案

### 方案 A：修改 `transformFunctionLiteral`

在 `transformer_expr.go` 中找到 `transformFunctionLiteral` 函数，添加特殊处理：

```go
func (t *Transformer) transformFunctionLiteral(l *ast.FunctionLiteral, isEventHandler bool) string {
    var sb strings.Builder
    
    sb.WriteString("func() {\n")
    
    // 转换函数体
    if l.Body != nil {
        for _, stmt := range l.Body.List {
            // 为语句添加 c. 前缀
            sb.WriteString(t.transformStmtWithPrefix(stmt, "c."))
        }
    }
    
    // 如果是事件处理器且有状态修改，添加 RequestUpdate()
    if isEventHandler && t.hasStateMutation(l.Body, stateVars) {
        sb.WriteString("\nc.RequestUpdate()")
    }
    
    sb.WriteString("\n}")
    return sb.String()
}
```

### 方案 B：后处理生成的代码

在 `transformTSXWithMutationCheck` 中，对生成的代码进行后处理：

```go
func (t *Transformer) transformTSXWithMutationCheck(tsx *ast.TSXElement, stateVars []FxStateVar) string {
    goCode := t.transformExpr(tsx)
    
    // 后处理：查找事件处理器
    goCode = t.postProcessEventHandlers(goCode, stateVars)
    
    return goCode
}

func (t *Transformer) postProcessEventHandlers(code string, stateVars []FxStateVar) string {
    // 使用正则表达式查找并替换
    // 1. 添加 c. 前缀
    // 2. 插入 RequestUpdate()
    return processedCode
}
```

## 📊 当前状态

| 功能 | 状态 | 说明 |
|------|------|------|
| 词法分析 | ✅ 完成 | `fx func` 正确识别 |
| 语法分析 | ✅ 完成 | FX 函数正确解析 |
| 状态收集 | ✅ 完成 | let 声明正确收集 |
| 依赖分析 | ✅ 完成 | 识别变量使用位置 |
| 修改检测 | ✅ 完成 | 检测赋值、自增等操作 |
| 代码生成 | ⚠️ 部分完成 | 结构体和构造函数正确，但事件处理器需要修复 |
| 事件处理器转换 | ❌ 未完成 | 需要添加 c. 前缀和 RequestUpdate() |

## 🎯 下一步

**最关键的步骤**：修改 `transformer_expr.go` 中的 TSX 转换逻辑

**具体任务**：
1. 在转换事件处理器时，识别是否是 FX 函数的回调
2. 为回调函数中的变量添加 `c.` 前缀
3. 检测是否有状态修改
4. 在函数末尾添加 `c.RequestUpdate()`

**预计工作量**：
- 修改 `transformer_expr.go`：~50-100 行代码
- 测试和调试：~1-2 小时

## 💡 总结

**核心逻辑已经实现**：
- ✅ 检测状态修改
- ✅ 生成组件结构
- ✅ 创建动态部分

**只差最后一步**：
- ❌ 事件处理器中的变量添加 `c.` 前缀
- ❌ 自动插入 `RequestUpdate()`

完成这两步后，FX 组件系统就可以正常工作了！
