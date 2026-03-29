# 后置自增/自减运算符解析失败修复

## 问题描述

在 FX 组件中，后置自增运算符 `count++` 和后置自减运算符 `count--` 在事件处理器中无法正确解析和转换。

### 问题代码示例

```typescript
fx func Counter() {
    let count = 0
    
    return <button text="Click" onClick={func() {
        count++  // ← 解析失败
    }} />
}
```

## 问题原因

在 `transformer/transformer_fx.go` 文件中，处理后置运算符时硬编码了 `"++"`，没有根据实际的运算符类型（`++` 或 `--`）进行映射。

### 错误代码位置

**文件**: `transformer/transformer_fx.go`

**问题 1**: `transformExprWithStatePrefix` 函数（第 260-265 行）
```go
case *ast.UnaryExpr:
    x := t.transformExprWithStatePrefix(e.X, stateVars, prefix)
    if e.Post {
        return x + "++"  // ❌ 硬编码为 "++"
    }
    return t.mapOp(e.Op) + x
```

**问题 2**: `transformStmtWithStatePrefix` 函数（第 190-210 行）
```go
case *ast.ExprStmt:
    if unary, ok := s.X.(*ast.UnaryExpr); ok {
        if ident, ok := unary.X.(*ast.Ident); ok {
            if containsStateVar(stateVars, ident.Name) {
                if unary.Post {
                    sb.WriteString(fmt.Sprintf("    %s%s++\n", prefix, ident.Name))  // ❌ 硬编码为 "++"
                } else {
                    sb.WriteString(fmt.Sprintf("    ++%s%s\n", prefix, ident.Name))
                }
            }
        }
    }
```

## 修复方案

使用 `t.mapOp(e.Op)` 来正确映射运算符，而不是硬编码 `"++"`。

### 修复后的代码

**修复 1**: `transformExprWithStatePrefix` 函数
```go
case *ast.UnaryExpr:
    x := t.transformExprWithStatePrefix(e.X, stateVars, prefix)
    op := t.mapOp(e.Op)  // ✅ 使用 mapOp 获取正确的运算符
    if e.Post {
        return x + op    // ✅ 使用正确的运算符
    }
    return op + x
```

**修复 2**: `transformStmtWithStatePrefix` 函数
```go
case *ast.ExprStmt:
    if unary, ok := s.X.(*ast.UnaryExpr); ok {
        if ident, ok := unary.X.(*ast.Ident); ok {
            if containsStateVar(stateVars, ident.Name) {
                op := t.mapOp(unary.Op)  // ✅ 使用 mapOp 获取正确的运算符
                if unary.Post {
                    sb.WriteString(fmt.Sprintf("    %s%s%s\n", prefix, ident.Name, op))  // ✅ 使用正确的运算符
                } else {
                    sb.WriteString(fmt.Sprintf("    %s%s%s\n", prefix, op, ident.Name))
                }
            }
        }
    }
```

## 验证

### 测试用例

创建了 `transformer_fx_increment_test.go` 来验证修复：

```go
func TestTransformer_FxPostIncrementAndDecrement(t *testing.T) {
    src := `import "github.com/gox-lang/gox/gui"

fx func Counter() {
    let count = 0
    
    return <div>
        <button text="Increment" onClick={func() {
            count++
        }} />
        <button text="Decrement" onClick={func() {
            count--
        }} />
    </div>
}`
    
    // 验证生成的代码同时包含 c.Count++ 和 c.Count--
}
```

### 期望输出

**输入**:
```typescript
fx func Counter() {
    let count = 0
    
    return <div>
        <button text="Increment" onClick={func() {
            count++
        }} />
        <button text="Decrement" onClick={func() {
            count--
        }} />
    </div>
}
```

**期望输出**:
```go
type Counter struct {
    gui.BaseFxComponent
    Count int
    rootComponent gui.Component
    dynamicParts []gui.TemplatePart
}

func NewCounter() *Counter {
    c := &Counter{
        Count: 0,
    }
    
    c.rootComponent = gui.NewDiv(nil,
        gui.NewButton(gui.ButtonProps{
            Text: "Increment",
            OnClick: func() {
                c.Count++  // ✅ 正确的后置自增
                c.RequestUpdate()
            },
        }),
        gui.NewButton(gui.ButtonProps{
            Text: "Decrement",
            OnClick: func() {
                c.Count--  // ✅ 正确的后置自减
                c.RequestUpdate()
            },
        }),
    )
    
    // ...
}
```

## 相关文件

- **解析器**: `parser/parser_expr.go` - `parsePostfix()` 函数（第 160-164 行）
- **Transformer**: `transformer/transformer_fx.go` - 已修复
- **Transformer 基础**: `transformer/transformer_expr.go` - `transformExpr()` 函数（第 405-411 行，此部分原本就是正确的）
- **AST 定义**: `ast/ast.go` - `UnaryExpr` 结构体（第 499-503 行）

## 运算符映射

`mapOp()` 函数定义在 `transformer/transformer_expr.go`（第 85-101 行）：

```go
func (t *Transformer) mapOp(op token.TokenKind) string {
    switch op {
    case token.INC:
        return "++"
    case token.DEC:
        return "--"
    // ... 其他运算符
    }
}
```

## 总结

修复前：
- ❌ `count++` → 正确
- ❌ `count--` → 错误（生成 `count++`）

修复后：
- ✅ `count++` → `c.Count++`
- ✅ `count--` → `c.Count--`

## 参考文档

- [FX 组件自动更新机制](../gui/FX_AUTO_UPDATE_TODO.md)
- [FX 组件实现总结](../gui/FX_IMPLEMENTATION_SUMMARY.md)
