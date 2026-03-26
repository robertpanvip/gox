# Gox 编译器测试用例

本文档记录所有测试用例及其期望输出，用于验证编译器功能。

## 使用方法

```bash
# 运行所有测试
go test ./parser/... ./transformer/... -v

# 运行单个测试
go test ./parser/... -run TestParser_VarDecl -v
```

## 测试用例

### 1. 变量声明 (VarDecl)

**输入:**
```gox
let a: int = 10
```

**期望输出:**
```go
a := 10
```

**测试:** `TestTransformer_VarDecl`

---

### 2. 函数定义 (Func)

**输入:**
```gox
public func add(x: int, y: int): int { return x + y }
```

**期望输出:**
```go
func Add(x int, y int) int {
    return x + y
}
```

**测试:** `TestTransformer_Func`

---

### 3. 结构体 (Struct)

**输入:**
```gox
struct User { name: string age: int }
```

**期望行为:** 成功解析

**测试:** `TestParser_SimpleStruct`

---

### 4. 扩展方法 (Extend)

**输入:**
```gox
extend string { func hello(): string { return "hello" } }
```

**期望输出:**
```go
func stringStringHello(self string) string {
    return "hello"
}
```

**测试:** `TestTransformer_Extend`

---

### 5. 闭包 (Closure) - TODO

**输入:**
```gox
let add = func(a: int, b: int): int { return a + b }
```

**期望输出:**
```go
add := func(a int, b int) int {
    return a + b
}
```

**状态:** 需要修复解析器

---

### 6. 箭头函数 (Arrow Function) - TODO

**输入:**
```gox
let add = func(a: int, b: int): int => a + b
```

**期望输出:**
```go
add := func(a int, b int) int { return a + b }
```

**状态:** 需要修复解析器

---

## 运行真实测试

使用 `test_runner` 工具获取真实输出：

```bash
go run ./cmd/test_runner/main.go
```

## 完整示例

### test.gox
```gox
package main

let a: int = 10
let b: int = 20

public func add(x: int, y: int): int {
    return x + y
}

let result: int = add(a, b)
```

### 生成的 Go 代码
```go
package main

a := 10
b := 20
func Add(x int, y int) int {
    return x + y
}

result := add(a, b)
```

### 运行结果
```bash
$ go run output.go
result = 30
```
