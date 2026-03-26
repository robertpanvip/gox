# 控制流括号支持

## 功能说明

现在 Gox 编译器的控制流语句支持两种语法风格：

### 1. 带括号（类似 C/Java）

```gox
if (x > 10) {
    println("big")
}

while (i < 5) {
    i = i + 1
}

for (i < 10) {
    i = i + 1
}

switch (day) {
    case 1: {
        println("Monday")
    }
}

when (grade) {
    case "A": {
        println("Excellent")
    }
}
```

### 2. 不带括号（类似 Go/Rust）

```gox
if x > 10 {
    println("big")
}

while i < 5 {
    i = i + 1
}

for i < 10 {
    i = i + 1
}

switch day {
    case 1: {
        println("Monday")
    }
}

when grade {
    case "A": {
        println("Excellent")
    }
}
```

## 实现细节

### Parser 修改

为所有控制流语句添加了可选的括号支持：

```go
func (p *Parser) parseIfStmt() ast.Stmt {
    // Optional parentheses around condition
    if p.check(token.LPAREN) {
        p.nextToken()
    }

    cond := p.parseExpr()

    // Close parentheses if opened
    if p.check(token.RPAREN) {
        p.nextToken()
    }

    // ... rest of parsing
}
```

同样的模式应用于：
- `parseIfStmt()` - if/else 语句
- `parseWhileStmt()` - while 循环
- `parseForStmt()` - for 循环
- `parseSwitchStmt()` - switch 语句
- `parseWhenStmt()` - when 表达式

## 示例对比

### if/else

**带括号**:
```gox
if (x > 10) {
    println("big")
} else if (x > 5) {
    println("medium")
} else {
    println("small")
}
```

**不带括号**:
```gox
if x > 10 {
    println("big")
} else if x > 5 {
    println("medium")
} else {
    println("small")
}
```

**转译为 Go** (两种语法生成相同代码):
```go
if x > 10 {
    fmt.Sprintln("big")
} else if x > 5 {
    fmt.Sprintln("medium")
} else {
    fmt.Sprintln("small")
}
```

### while 循环

**带括号**:
```gox
while (i < 10) {
    i = i + 1
}
```

**不带括号**:
```gox
while i < 10 {
    i = i + 1
}
```

**转译为 Go**:
```go
for i < 10 {
    i = i + 1
}
```

### for 循环

**带括号**:
```gox
for (i < 10) {
    i = i + 1
}
```

**不带括号**:
```gox
for i < 10 {
    i = i + 1
}
```

### switch 语句

**带括号**:
```gox
switch (day) {
    case 1: {
        println("Monday")
    }
    case 2: {
        println("Tuesday")
    }
}
```

**不带括号**:
```gox
switch day {
    case 1: {
        println("Monday")
    }
    case 2: {
        println("Tuesday")
    }
}
```

## 测试覆盖

新增测试用例：
- ✅ `TestTransformer_IfWithParentheses` - if 带括号
- ✅ `TestTransformer_WhileWithParentheses` - while 带括号
- ✅ `TestTransformer_ForWithParentheses` - for 带括号
- ✅ `TestTransformer_SwitchWithParentheses` - switch 带括号

测试文件：
- `transformer/transformer_control_flow_test.go` - 单元测试
- `test_control_flow_parens.gox` - 综合测试

## 语法灵活性

Gox 现在支持多种语法风格，开发者可以根据个人喜好选择：

| 风格 | 示例 | 类似语言 |
|------|------|----------|
| C/Java 风格 | `if (x > 10) { }` | C, C++, Java, C#, JavaScript |
| Go/Rust 风格 | `if x > 10 { }` | Go, Rust, Swift |

两种语法完全等价，生成的 Go 代码相同。

## 修改的文件

1. **parser/parser.go**
   - 修改 `parseIfStmt()` 支持括号
   - 修改 `parseWhileStmt()` 支持括号
   - 修改 `parseForStmt()` 支持括号
   - 修改 `parseSwitchStmt()` 支持括号
   - 修改 `parseWhenStmt()` 支持括号

2. **Draft.MD**
   - 更新 7.6 节控制流文档
   - 添加带括号和不带括号的示例

3. **测试文件**
   - `transformer/transformer_control_flow_test.go` - 新增 4 个测试用例
   - `test_control_flow_parens.gox` - 综合测试两种语法

## 注意事项

1. **括号是可选的** - 两种语法都支持，可以混用
2. **推荐一致性** - 建议在同一项目中保持统一的语法风格
3. **转译相同** - 两种语法生成的 Go 代码完全相同

## 总结

现在 Gox 的控制流语句具有更大的灵活性：

✅ 支持 C/Java 风格的括号语法
✅ 支持 Go/Rust 风格的无括号语法
✅ 两种语法完全等价
✅ 完整的测试覆盖
✅ 详细的文档说明

开发者可以根据自己的背景和喜好选择合适的语法风格！🎉
