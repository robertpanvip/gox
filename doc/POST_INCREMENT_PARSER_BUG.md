# 后置自增运算符 Parser Bug 分析

## 问题描述

`count++` 被错误解析为 `count + +`，导致：
- AST 类型为 `BinaryExpr` 而不是 `UnaryExpr`
- 生成的代码错误：`count + +`
- Parser 报错：`unexpected token in expression: 15` (RBRACE)

## 测试用例

### 最简单的测试
```gox
func test() {
    let x = 0
    x++
}
```

**结果**：❌ 失败
- AST: `ExprStmt: *ast.BinaryExpr`
- 生成代码：`x + +`

### 箭头函数测试
```gox
func Main() {
    let count = 0
    let increment = () => {
        count++
    }
}
```

**结果**：❌ 失败
- Parser 调试输出显示：`parseArrowFunction: after block body, curTok=0`
- 函数体包含了后面所有的代码

### FX 组件测试
```gox
fx func Counter() {
    let count = 0
    return <button onClick={func() {
        count++
    }} />
}
```

**结果**：❌ 失败
- Parser 错误：`unexpected token in expression: 15`
- 生成代码：`count + +`

## Token 分析

词法分析器正确识别了 `++`：
```
ident(x)
++        ← 正确的 INC token
\n
```

## Parser 调用链

```
parseExpr()
  → parseNullCoalesce()
    → parseOr()
      → parseAnd()
        → parseEquality()
          → parseRelational()
            → parseAdditive()  ← 检查 + 或 -
              → parseMultiplicative()
                → parseUnary()
                  → parsePostfix()  ← 解析 x++
                    → parsePrimary()  ← 解析 x
```

## 可能的原因

### 假设 1: parsePostfix() 循环问题

`parsePostfix()` 在第 160-164 行解析 `++` 后，循环继续，可能导致问题。

**测试**：添加 `return x` 立即返回
**结果**：❌ 没有改善

### 假设 2: parseAdditive() 误判

`parseAdditive()` 在第 69 行检查 `PLUS` 或 `MINUS`，可能误判了 `INC`。

**分析**：`INC` (token 33) ≠ `PLUS` (token 29)，不应该误判。

### 假设 3: Token 流被重复消费

可能在某个地方，Token 被重复消费或回退不正确。

**证据**：调试输出显示 `parseArrowFunction: after block body, curTok=0`
- 解析完函数体后，current token 变成了 EOF
- 说明 `parseBlock()` 消耗了太多 token

## 关键发现

从调试输出来看，问题出在 `parseBlock()` 解析函数体时：

```
DEBUG parseArrowFunction: after block body, curTok=0
```

这意味着 `parseBlock()` 没有在 `}` 处停止，一直解析到了 EOF！

## 根本原因推测

`parseBlock()` 的循环条件是 `p.curTok.Kind != token.RBRACE`。

在循环内部，调用 `parseStmt()` 解析语句。

`parseStmt()` 对于表达式语句，调用 `parseExpr()`。

`parseExpr()` 解析 `count++` 时，可能：
1. 没有正确识别 `++` 为一个整体
2. 或者在解析后没有正确停止
3. 导致继续解析后面的 token

最终导致 `parseBlock()` 一直解析到 EOF，而不是在 `}` 处停止。

## 下一步调试方向

1. **在 parsePostfix() 中添加详细调试输出**
   - 打印每次循环的 current token
   - 打印返回时的 current token

2. **检查 parseExpr() 的返回值**
   - 确认返回的是 `UnaryExpr` 还是其他类型

3. **检查 Token 流**
   - 确认 `++` 确实是一个 token，而不是两个 `PLUS`

4. **查看是否有其他地方消费了 `++` token**
   - 可能在 `parsePostfix()` 之前就被消费了

## 临时解决方案

在修复 Parser 之前，可以使用以下变通方法：

```gox
// 使用赋值语句代替自增
count = count + 1

// 或使用前置自增（如果支持）
++count
```

## 相关文件

- **Parser**: `parser/parser_expr.go`
  - `parsePostfix()` (第 110-171 行)
  - `parseAdditive()` (第 67-76 行)
- **Lexer**: `lexer/lexer.go` (第 79-84 行，正确识别 `++`)
- **AST**: `ast/ast.go` (`UnaryExpr` 定义)

## 影响范围

- ❌ 所有使用 `++` 的代码
- ❌ 所有使用 `--` 的代码
- ❌ FX 组件中的事件处理器
- ❌ 箭头函数中的自增/自减

---

**分析日期**: 2026-03-29  
**状态**: 🔍 调试中  
**优先级**: 🔴 高（基础功能 bug）
