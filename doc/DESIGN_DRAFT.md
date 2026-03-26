# Gox 编译器 - 设计草稿

**最后更新**: 2024-03-26  
**版本**: v0.3.0

---

## 📋 概述

Gox 是一个将类 TypeScript/Go 语法编译为 Go 代码的转译器。目标是提供现代化的语法糖，同时保持 Go 的性能和类型安全。

---

## ✅ 已实现功能

### 1. 核心语法

#### 变量声明
```gox
// 类型推断
let a = 10              // → a := 10

// 类型标注
let b: int = 20         // → b := 20

// 常量
const PI = 3.14         // → const PI = 3.14
```

#### 函数
```gox
// 普通函数
public func add(x: int, y: int): int {
    return x + y
}
// → func Add(x int, y int) int { return x + y }

// 箭头函数
let subtract = func(a: int, b: int): int => a - b
// → subtract := func(a int, b int) int { return a - b }

// 闭包
let multiply = func(x: int, y: int): int {
    return x * y
}
```

#### 泛型
```gox
public func identity[T](x: T): T {
    return x
}
// → func Identity[T any](x T) T { return x }

public func print[T any](x: T) {
    fmt.Println(x)
}
```

---

### 2. 类型系统

#### 基本类型
- ✅ `int`, `float`, `string`, `bool`
- ✅ 类型推断
- ✅ 类型标注

#### 数组类型
```gox
// 一维数组
let arr: int[] = [1, 2, 3]
// → arr := []interface{}{1, 2, 3}

// 嵌套数组
let matrix: int[][] = [[1, 2], [3, 4]]
// → matrix := [][]interface{}{{1, 2}, {3, 4}}

// 数组字面量
let numbers = [1, 2, 3]
let empty: int[] = []
```

#### 函数类型
```gox
// 函数作为参数
public func apply(fn: func(int): int, x: int): int {
    return fn(x)
}

// 函数作为返回值
public func makeAdder(x: int): func(int): int {
    return func(y: int): int => x + y
}
```

---

### 3. 字符串

#### 普通字符串
```gox
let name = "Alice"
// → name := "Alice"
```

#### 模板字符串 (新增 ✨)
```gox
let greeting = "Hello, ${name}!"
// → greeting := fmt.Sprintf("Hello, %v!", name)

let message = "The value is ${x} and ${y}"
// → message := fmt.Sprintf("The value is %v and %v", x, y)
```

**实现细节**:
- AST: `TemplateString{Parts: []string, Exprs: []Expr}`
- 解析：检测 `${...}` 并分割为多个部分
- 转译：转换为 `fmt.Sprintf`

---

### 4. 数组方法 (TypeScript 风格)

#### 扩展方法
```gox
extend int[] {
    public func map(fn: func(int): int): int[] {
        return self
    }
    
    public func filter(fn: func(int): bool): int[] {
        return self
    }
}
```

#### 使用
```gox
let numbers = [1, 2, 3]
let doubled = numbers.map(func(x: int): int => x * 2)
let evens = numbers.filter(func(x: int): bool => x % 2 == 0)
```

**生成的 Go**:
```go
numbers := []interface{}{1, 2, 3}
doubled := numbers.map(func(x int) int { return x * 2 })
```

---

### 5. 高阶函数

```gox
// 函数作为参数
public func map(arr: int[], fn: func(int): int): int[] {
    return arr
}

// 函数作为返回值
public func makeAdder(): func(int): int {
    return func(x: int): int => x + 1
}
```

---

### 6. 自引用关键字

```gox
extend int[] {
    public func map(fn: func(int): int): int[] {
        return self  // → return self
    }
}
```

---

## 📊 测试覆盖

### 单元测试
- **Parser**: 4 个测试 ✅
- **Transformer**: 17 个测试 ✅
- **总计**: 21 个测试全部通过

### 真实运行测试
- ✅ 编译并运行成功
- ✅ 输出正确结果

---

## 🔧 技术实现

### 编译器架构

```
Gox 源代码
    ↓
Lexer (词法分析)
    ↓
Tokens
    ↓
Parser (语法分析)
    ↓
AST
    ↓
Transformer (转译)
    ↓
Go 代码
```

### 关键组件

#### 1. Lexer (`lexer/lexer.go`)
- Token 识别
- 字符串字面量
- 反引号字符串

#### 2. Parser (`parser/parser.go`)
- 递归下降解析
- 表达式优先级
- 模板字符串解析 (`parseTemplateString`)
- 数组字面量解析
- 泛型参数解析

#### 3. Transformer (`transformer/transformer.go`)
- AST 转 Go 代码
- 可见性转换 (public → 大写)
- 模板字符串转 `fmt.Sprintf`
- 数组转 `[]interface{}`

#### 4. AST (`ast/ast.go`)
- `TemplateString` - 模板字符串节点
- `ArrayLit` - 数组字面量
- `FuncType` - 函数类型
- `TypeParam` - 泛型参数

---

## ⏳ 待实现功能

### 高优先级

1. **结构体完整支持**
   ```gox
   public struct User {
       name: string
       age: int
   }
   ```

2. **Kotlin 风格 print/println**
   ```gox
   println("Hello, ${name}!")  // → fmt.Println(fmt.Sprintf(...))
   print("Value: ${x}")        // → fmt.Print(...)
   ```

3. **数组类型推断**
   - 当前：`[]interface{}`
   - 目标：`[]int`, `[]string` 等具体类型

### 中优先级

4. **结构体方法**
   ```gox
   struct User {
       name: string
   }
   
   extend User {
       func greet(): string => "Hello, ${self.name}"
   }
   ```

5. **接口支持**

6. **错误处理**
   ```gox
   try {
       riskyOperation()
   } catch (e) {
       handleError(e)
   }
   ```

### 低优先级

7. **异步/等待**

8. **命名空间/模块**

9. **标准库绑定**

---

## 📝 语法设计原则

### 1. 简洁性
- 类型推断优先
- 可选的类型标注
- 箭头函数简写

### 2. 一致性
- 类似 TypeScript 的语法
- 类似 Kotlin 的模板字符串
- 类似 Go 的底层语义

### 3. 互操作性
- 生成的 Go 代码可直接使用
- 支持调用 Go 标准库
- 保持 Go 的性能

---

## 🎯 示例代码

### 完整示例

```gox
package main

// 泛型函数
public func map[T, U](arr: T[], fn: func(T): U): U[] {
    return arr
}

// 使用
let numbers = [1, 2, 3]
let doubled = map(numbers, func(n: int): int => n * 2)

// 模板字符串
let name = "Alice"
println("Hello, ${name}!")

// 结构体
public struct User {
    name: string
    age: int
}

let user = User{name: "Bob", age: 30}
```

---

## 📈 性能考虑

### 当前实现
- 数组使用 `[]interface{}` - 有装箱开销
- 模板字符串使用 `fmt.Sprintf` - 运行时格式化

### 优化方向
1. 具体类型数组（减少装箱）
2. 编译时字符串拼接优化
3. 内联小函数

---

## 🔗 参考资料

- TypeScript 数组方法
- Kotlin 模板字符串
- Go 泛型 (1.18+)
- Babel/TypeScript 编译器设计

---

## 📅 开发日志

### 2024-03-26
- ✅ 添加模板字符串支持
- ✅ 添加字符串字面量转义处理
- ⏳ 结构体支持（进行中）
- ⏳ print/println 函数（待实现）

### 2024-03-25
- ✅ TypeScript 风格数组方法
- ✅ 数组字面量解析
- ✅ self 关键字支持

### 2024-03-24
- ✅ 泛型支持
- ✅ 高阶函数
- ✅ 类型推断

---

**文档状态**: 草稿  
**维护者**: Gox Team
