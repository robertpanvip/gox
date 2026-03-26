# Gox 编译器 - 完整控制流实现

## 📋 实现概览

本次更新实现了完整的控制流语句，包括：

1. ✅ **if/else** 条件语句
2. ✅ **for** 循环
3. ✅ **while** 循环（转译为 for）
4. ✅ **break/continue** 语句
5. ✅ **switch** 语句
6. ✅ **when** 表达式（类似 Kotlin）

---

## 🎯 功能详情

### 1. if/else 条件语句

**语法**:
```gox
if x > 10 {
    println("big")
} else if x > 5 {
    println("medium")
} else {
    println("small")
}
```

**转译**:
```go
if x > 10 {
    fmt.Sprintln("big")
} else if x > 5 {
    fmt.Sprintln("medium")
} else {
    fmt.Sprintln("small")
}
```

**AST 节点**: `IfStmt`
- `Cond` - 条件表达式
- `Body` - if 分支代码块
- `Else` - else 分支（可以是 IfStmt 或 BlockStmt）

---

### 2. while 循环

**语法**:
```gox
while i < 10 {
    i = i + 1
}
```

**转译**:
```go
for i < 10 {
    i = i + 1
}
```

**说明**: while 循环会直接转译为 Go 的 for 循环

**AST 节点**: `WhileStmt`
- `Cond` - 循环条件
- `Body` - 循环体

---

### 3. for 循环

**语法**:
```gox
for i < 10 {
    if i == 5 {
        break
    }
    i = i + 1
}
```

**转译**:
```go
for i < 10 {
    if i == 5 {
        break
    }
    i = i + 1
}
```

**AST 节点**: `ForStmt`
- `Cond` - 循环条件
- `Body` - 循环体

**待实现**:
- for 初始化语句：`for i := 0; i < 10; i++`
- for-in 循环：`for item in array`

---

### 4. break/continue 语句

**语法**:
```gox
for i < 10 {
    if i == 5 {
        break    // 跳出循环
    }
    if i % 2 == 0 {
        continue // 跳过本次迭代
    }
    i = i + 1
}
```

**转译**:
```go
for i < 10 {
    if i == 5 {
        break
    }
    if i % 2 == 0 {
        continue
    }
    i = i + 1
}
```

**AST 节点**:
- `BreakStmt` - break 语句
- `ContinueStmt` - continue 语句

---

### 5. switch 语句

**语法**:
```gox
switch day {
    case 1: {
        println("Monday")
    }
    case 2: {
        println("Tuesday")
    }
    default: {
        println("Other day")
    }
}
```

**转译**:
```go
switch day {
case 1:
    fmt.Sprintln("Monday")
case 2:
    fmt.Sprintln("Tuesday")
default:
    fmt.Sprintln("Other day")
}
```

**AST 节点**:
- `SwitchStmt` - switch 语句
  - `Cond` - 条件表达式
  - `Cases` - case 分支列表
- `SwitchCase` - case 分支
  - `Cond` - case 条件
  - `Body` - case 代码块

---

### 6. when 表达式（类似 Kotlin）

**语法**:
```gox
when grade {
    case "A": {
        println("Excellent")
    }
    case "B": {
        println("Good")
    }
    default: {
        println("Unknown")
    }
}
```

**转译**:
```go
switch grade {
case "A":
    fmt.Sprintln("Excellent")
case "B":
    fmt.Sprintln("Good")
default:
    fmt.Sprintln("Unknown")
}
```

**说明**: when 是 switch 的另一种语法形式，语义完全相同

**AST 节点**:
- `WhenStmt` - when 语句（类似 SwitchStmt）
- `WhenCase` - when 分支（类似 SwitchCase）

---

## 📝 文件修改清单

### 新增/修改的文件

1. **token/token.go**
   - 添加 `WHILE`, `SWITCH`, `CASE`, `WHEN`, `BREAK`, `CONTINUE` token
   - 添加到 keywords 映射

2. **ast/ast.go**
   - 添加 `WhileStmt` 节点
   - 添加 `ForInStmt` 节点（预留）
   - 添加 `BreakStmt` 节点
   - 添加 `ContinueStmt` 节点
   - 添加 `SwitchStmt` 和 `SwitchCase` 节点
   - 添加 `WhenStmt` 和 `WhenCase` 节点

3. **parser/parser.go**
   - 修改 `parseStmt()` 添加控制流语句解析
   - 添加 `parseWhileStmt()` 函数
   - 添加 `parseBreakStmt()` 函数
   - 添加 `parseContinueStmt()` 函数
   - 添加 `parseSwitchStmt()` 函数
   - 添加 `parseWhenStmt()` 函数

4. **transformer/transformer.go**
   - 添加 `WhileStmt` 转译（转为 for）
   - 添加 `BreakStmt` 转译
   - 添加 `ContinueStmt` 转译
   - 添加 `SwitchStmt` 转译
   - 添加 `WhenStmt` 转译

5. **Draft.MD**
   - 更新特性表格
   - 添加第 7.6 节：控制流

### 新增的测试文件

1. **transformer/transformer_control_flow_test.go**
   - `TestTransformer_IfElse` - if/else 测试
   - `TestTransformer_While` - while 循环测试
   - `TestTransformer_BreakContinue` - break/continue 测试
   - `TestTransformer_Switch` - switch 语句测试
   - `TestTransformer_When` - when 表达式测试

2. **test_control_flow.gox**
   - 综合测试文件

---

## 🎯 使用示例

### 完整示例

```gox
package main

// if/else
let x = 15
if x > 10 {
    println("x is big")
} else if x > 5 {
    println("x is medium")
} else {
    println("x is small")
}

// while
let i = 0
while i < 5 {
    println(`i is ${i}`)
    i = i + 1
}

// for with break/continue
for i < 10 {
    if i == 7 {
        break
    }
    if i % 2 == 0 {
        i = i + 1
        continue
    }
    println(`i is ${i}`)
    i = i + 1
}

// switch
let day = 3
switch day {
    case 1: {
        println("Monday")
    }
    case 2: {
        println("Tuesday")
    }
    case 3: {
        println("Wednesday")
    }
    default: {
        println("Other day")
    }
}

// when
let grade = "A"
when grade {
    case "A": {
        println("Excellent")
    }
    case "B": {
        println("Good")
    }
    default: {
        println("Unknown")
    }
}
```

---

## ✅ 测试覆盖

### 控制流测试
- ✅ if/else 条件语句
- ✅ else if 链式条件
- ✅ while 循环（转译为 for）
- ✅ for 循环
- ✅ break 语句
- ✅ continue 语句
- ✅ switch 语句
- ✅ when 表达式
- ✅ 嵌套控制流

---

## 🚀 下一步

- [ ] 实现 for-in 循环（数组遍历）
  ```gox
  for item in array {
      println(item)
  }
  ```
  
- [ ] 实现带初始化和迭代的 for 循环
  ```gox
  for i := 0; i < 10; i++ {
      println(i)
  }
  ```

- [ ] 实现 for-range 循环（map 遍历）
  ```gox
  for key, value in map {
      println(`${key}: ${value}`)
  }
  ```

- [ ] 实现 when 表达式的无参数形式（类似 Kotlin）
  ```gox
  when {
      x > 10 -> println("big")
      x > 5 -> println("medium")
      else -> println("small")
  }
  ```

---

## 📚 特性对比

| 特性 | Gox | Go | Kotlin | TypeScript |
|------|-----|----|--------|------------|
| if/else | ✅ | ✅ | ✅ | ✅ |
| for | ✅ | ✅ | ✅ | ✅ |
| while | ✅ | ✅ | ✅ | ✅ |
| break/continue | ✅ | ✅ | ✅ | ✅ |
| switch | ✅ | ✅ | ❌ | ✅ |
| when/match | ✅ | ❌ | ✅ | ❌ |
| for-in | ⏳ | ✅ | ✅ | ✅ |
| for-each | ⏳ | ✅ | ✅ | ✅ |

---

## 🎉 总结

本次更新成功实现了完整的控制流语句：

1. ✅ **if/else** - 条件判断
2. ✅ **for** - 基础循环
3. ✅ **while** - while 循环（转译为 for）
4. ✅ **break/continue** - 循环控制
5. ✅ **switch** - switch 语句
6. ✅ **when** - Kotlin 风格的 when 表达式

**关键特性**:
- 完整的条件判断和循环支持
- while 自动转译为 for
- switch 和 when 两种语法
- break/continue 循环控制
- 支持嵌套控制流
- 完整的测试覆盖

Gox 现在具备了完整的控制流能力！🎊
