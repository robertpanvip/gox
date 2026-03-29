# 后置自增/自减运算符问题分析

## 测试文件

`test/test_fx_simple2.gox`:

```gox
import "github.com/gox-lang/gox/gui"

fx func Counter() {
    let count = 0
    
    return <button text="Click" onClick={func() {
        count++
    }} />
}
```

## 解析流程

### 1. 词法分析 (Lexer)

词法分析器将 `count++` 分解为：
- `IDENT("count")`
- `INC("++")`

### 2. 语法分析 (Parser)

**文件**: `parser/parser_expr.go`

`parsePostfix()` 函数正确处理了后置运算符：

```go
case p.curTok.Kind == token.INC || p.curTok.Kind == token.DEC:
    // 后置自增/自减运算符
    op := p.curTok.Kind  // ✅ 保存运算符类型（INC 或 DEC）
    p.nextToken()
    x = &ast.UnaryExpr{Op: op, X: x, Post: true}  // ✅ 设置 Post = true
```

**AST 结构**:
```go
&ast.UnaryExpr{
    Op: token.INC,      // 或 token.DEC
    X: &ast.Ident{Name: "count"},
    Post: true,         // 标记为后置运算符
}
```

### 3. 代码转换 (Transformer)

#### ✅ 正确的部分 - `transformer_expr.go`

`transformExpr()` 函数（第 405-411 行）正确处理：

```go
case *ast.UnaryExpr:
    x := t.transformExpr(e.X)
    op := t.mapOp(e.Op)  // ✅ 使用 mapOp 获取正确的运算符
    if e.Post {
        return x + op    // ✅ 生成正确的后置运算符
    }
    return op + x
```

#### ❌ 错误的部分 - `transformer_fx.go` (已修复)

**修复前**的问题代码：

```go
// transformExprWithStatePrefix - 第 260-265 行
case *ast.UnaryExpr:
    x := t.transformExprWithStatePrefix(e.X, stateVars, prefix)
    if e.Post {
        return x + "++"  // ❌ 硬编码为 "++"，忽略了 e.Op
    }
    return t.mapOp(e.Op) + x

// transformStmtWithStatePrefix - 第 196-200 行
if unary.Post {
    sb.WriteString(fmt.Sprintf("    %s%s++\n", prefix, ident.Name))  // ❌ 硬编码 "++"
} else {
    sb.WriteString(fmt.Sprintf("    ++%s%s\n", prefix, ident.Name))
}
```

**问题**: 
- `count++` 会正确生成 `c.count++`
- `count--` 会**错误**生成 `c.count++`（应该是 `c.count--`）

**修复后**的代码：

```go
// transformExprWithStatePrefix
case *ast.UnaryExpr:
    x := t.transformExprWithStatePrefix(e.X, stateVars, prefix)
    op := t.mapOp(e.Op)  // ✅ 获取正确的运算符
    if e.Post {
        return x + op    // ✅ 使用正确的运算符
    }
    return op + x

// transformStmtWithStatePrefix
if unary.Post {
    op := t.mapOp(unary.Op)  // ✅ 获取正确的运算符
    sb.WriteString(fmt.Sprintf("    %s%s%s\n", prefix, ident.Name, op))  // ✅ 使用正确的运算符
} else {
    sb.WriteString(fmt.Sprintf("    %s%s%s\n", prefix, op, ident.Name))
}
```

## 为什么 Parser 是正确的但转换失败？

1. **Parser 只负责语法分析**：它正确地识别了 `++` 和 `--` 是两种不同的运算符，并保存在 `UnaryExpr.Op` 字段中。

2. **Transformer 负责代码生成**：在 FX 组件的上下文中，需要为状态变量添加前缀（如 `c.count`）。但在处理后置运算符时，硬编码了 `"++"`，忽略了从 AST 中读取的实际运算符类型。

3. **为什么普通表达式是正确的**：因为 `transformExpr()` 函数使用了 `t.mapOp(e.Op)`，而 `transformFx*` 函数没有。

## 完整的代码生成流程

### 输入
```typescript
fx func Counter() {
    let count = 0
    return <button onClick={func() {
        count++
    }} />
}
```

### Parser 生成的 AST
```
FuncDecl(IsFx=true)
  └─ Body:
      └─ ReturnStmt
          └─ TSXElement(button)
              └─ Attribute(onClick)
                  └─ FunctionLiteral
                      └─ Body:
                          └─ ExprStmt
                              └─ UnaryExpr
                                  ├─ Op: INC
                                  ├─ X: Ident("count")
                                  └─ Post: true
```

### Transformer 处理

1. **识别 FX 函数** → 生成组件结构体
2. **收集状态变量** → `count` 是状态变量
3. **转换事件处理器** → 需要为 `count` 添加前缀 `c.`
4. **后置运算符处理**（已修复）:
   - 从 AST 读取：`Op = INC`, `Post = true`
   - 调用 `mapOp(INC)` → 返回 `"++"`
   - 生成：`c.Count++` ✅

## 验证方法

运行测试：
```bash
go test ./transformer -run TestTransformer_FxPostIncrement -v
go test ./transformer -run TestTransformer_FxPostDecrement -v
go test ./transformer -run TestTransformer_FxPostIncrementAndDecrement -v
```

## 相关文件

- **Parser**: `parser/parser_expr.go` (第 160-164 行) - ✅ 正确
- **Transformer (普通)**: `transformer/transformer_expr.go` (第 405-411 行) - ✅ 正确
- **Transformer (FX)**: `transformer/transformer_fx.go` (第 196-200 行，第 260-265 行) - ✅ 已修复
- **AST**: `ast/ast.go` (第 499-503 行) - UnaryExpr 定义
- **Token**: `token/token.go` - INC 和 DEC 定义
