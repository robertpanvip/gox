# FX 组件实现 - 最后的问题

## ✅ 已完成的核心功能

1. **状态修改检测** ✅
   - `hasStateMutation()` - 检测函数体是否修改状态
   - `stmtMutatesState()` - 检测语句是否修改状态
   - 支持赋值、自增、自减等操作

2. **事件处理器转换逻辑** ✅
   - `transformEventHandler()` - 转换事件处理器
   - `transformStmtWithStatePrefix()` - 为语句添加 `c.` 前缀
   - `transformExprWithStatePrefix()` - 为表达式添加 `c.` 前缀
   - 自动插入 `RequestUpdate()`

3. **代码生成框架** ✅
   - 组件结构体生成
   - 构造函数生成
   - 动态部分生成

## ❌ 当前问题

### 问题：TSX 转换没有使用新的状态感知逻辑

**当前生成的代码**：
```go
c.rootComponent = gui.NewDiv(, 
    gui.NewLabel(LabelProps{
        text: fmt.Sprintf("Hello %v!", name),  // ❌ 没有 c. 前缀
        fontSize: 16
    }),
    gui.NewButton(ButtonProps{
        text: "Increment",
        onClick: func() {
            count + +  // ❌ 没有 c. 前缀，没有 RequestUpdate()
        }
    })
)
```

**期望生成的代码**：
```go
c.rootComponent = gui.NewDiv(, 
    gui.NewLabel(LabelProps{
        Text: fmt.Sprintf("Hello %v!", c.name),  // ✅ 有 c. 前缀
        FontSize: 16
    }),
    gui.NewButton(ButtonProps{
        Text: "Increment",
        OnClick: func() {
            c.count++  // ✅ 有 c. 前缀
            c.RequestUpdate()  // ✅ 自动插入
        }
    })
)
```

### 根本原因

`transformTSXWithMutationCheck` 函数试图调用 `transformExprWithStateCheck`，但这个函数内部又调用了 `transformTSXElementWithStateCheck`。

然而，问题在于：
1. `transformTSXElementWithStateCheck` 中的逻辑有问题
2. 生成的代码格式不对（属性名小写，而不是大写）
3. 没有正确调用事件处理器转换

## 🔧 解决方案

需要修改 `transformTSXElementWithStateCheck` 函数，使其：
1. 正确调用现有的 `transformExpr` 来转换普通属性
2. 只对事件处理器使用特殊的 `transformEventHandler`
3. 正确格式化组件创建代码

或者，更简单的方法是：
- 修改 `transformer_expr.go` 中的 `transformExpr` 函数
- 添加一个可选的 `stateVars` 参数
- 在 FX 函数中调用时传入状态变量列表

## 📊 当前状态

| 功能 | 状态 | 说明 |
|------|------|------|
| 状态检测 | ✅ 完成 | 正确检测状态修改 |
| 事件处理器转换 | ✅ 完成 | 正确的 `transformEventHandler` 逻辑 |
| TSX 元素转换 | ❌ 未正确集成 | 新逻辑没有替代旧逻辑 |
| 属性名格式化 | ❌ 有问题 | 生成小写而不是大写 |

## 🎯 下一步

**最关键的任务**：修复 TSX 元素转换逻辑

**选项 A**：修复 `transformTSXElementWithStateCheck`
- 确保正确调用 `transformEventHandler`
- 确保属性名大写（Text 而不是 text）
- 确保递归处理子元素时也使用状态感知转换

**选项 B**：修改 `transformer_expr.go`
- 为 `transformExpr` 添加可选的状态变量参数
- 在 FX 函数中使用特殊版本的转换

**推荐方案**：选项 A，因为不修改现有代码，风险更小。
