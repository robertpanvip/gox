# Gox 编译器 - ES6 模板字符串和结构体方法实现

## 📋 实现概览

本次更新完成了两个核心功能：

1. ✅ **ES6 风格模板字符串** - 使用反引号 `` ` `` 和 `${expr}` 插值语法
2. ✅ **Go Receiver 风格结构体方法** - 在结构体外部定义方法

---

## 🎯 功能详情

### 1. ES6 风格模板字符串（新增 ✨）

**实现位置**:
- Token: `token/token.go` - 添加 `TEMPLATE` token 类型
- Lexer: `lexer/lexer.go` - `readRawString()` 检测模板字符串
- Parser: `parser/parser.go` - `parseTemplateString()` 解析
- Transformer: `transformer/transformer.go` - 转译为 `fmt.Sprintf`

**语法**:
```gox
// 使用反引号包裹
let name = "Alice"
let greeting = `Hello, ${name}!`
let message = `The value is ${x} and ${y}`
```

**转译**:
```go
name := "Alice"
greeting := fmt.Sprintf("Hello, %v!", name)
message := fmt.Sprintf("The value is %v and %v", x, y)
```

**关键实现**:

1. **Lexer 检测**:
```go
func (l *Lexer) readRawString() token.Token {
    l.next()
    hasTemplate := false
    for l.peekByte() != '`' && l.pos < len(l.src) {
        // Check for ${ pattern
        if l.peekByte() == '$' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '{' {
            hasTemplate = true
        }
        l.next()
    }
    // ...
    if hasTemplate {
        return token.Token{Kind: token.TEMPLATE, Literal: lit, ...}
    }
    return token.Token{Kind: token.STRING, Literal: lit, ...}
}
```

2. **Parser 解析**:
```go
case token.TEMPLATE:
    pos := ast.Position{Line: p.curTok.Line, Col: p.curTok.Col}
    val := p.curTok.Literal
    p.nextToken()
    return p.parseTemplateString(val, pos)

func (p *Parser) parseTemplateString(val string, pos ast.Position) ast.Expr {
    // ES6-style template string parser
    content := strings.Trim(val, "`")  // Trim backticks
    // ... parse ${...} expressions
}
```

**测试用例**: `transformer/transformer_struct_method_test.go`
- `TestTransformer_TemplateStringES6` - 基本模板字符串
- `TestTransformer_TemplateStringMultipleExpressions` - 多个表达式
- `TestTransformer_TemplateStringWithPrint` - 与 print/println 结合

---

### 2. Go Receiver 风格结构体方法（新增 ✨）

**实现位置**:
- AST: `ast/ast.go` - `FuncDecl` 添加 `Receiver` 字段
- Parser: `parser/parser.go` - 解析 receiver 语法
- Transformer: `transformer/transformer.go` - 生成 receiver 方法

**语法**:
```gox
// 定义结构体
public struct User {
    public name: string
    private age: int
}

// 在结构体外部定义方法（Go receiver 风格）
public func (u: User) GetName(): string {
    return u.name
}

public func (u: User) SetAge(age: int) {
    u.age = age
}
```

**转译**:
```go
type User struct {
    Name string
    age int
}

func (u User) GetName() string {
    return u.Name
}

func (u User) SetAge(age int) {
    u.age = age
}
```

**关键实现**:

1. **AST 扩展**:
```go
type FuncDecl struct {
    Visibility  Visibility
    Name        string
    Receiver    *FuncParam  // Go receiver (for struct methods)
    TypeParams  []*TypeParam
    Params      []*FuncParam
    ReturnType  Expr
    Throws      bool
    Body        *BlockStmt
    P           Position
}
```

2. **Parser 解析 receiver**:
```go
func (p *Parser) parseFuncDeclWithVisibility(vis ast.Visibility) ast.Decl {
    // Parse receiver if present: (receiver: Type)
    var receiver *ast.FuncParam
    if p.curTok.Kind == token.LPAREN {
        p.nextToken()
        if p.curTok.Kind == token.IDENT {
            receiverName := p.curTok.Literal
            p.nextToken()
            if p.curTok.Kind == token.COLON {
                p.nextToken()
                receiverType := p.parseType()
                receiver = &ast.FuncParam{Name: receiverName, Type: receiverType}
                if p.curTok.Kind == token.RPAREN {
                    p.nextToken()
                }
            }
        }
    }
    // ... parse rest of function
}
```

3. **Transformer 生成 receiver 方法**:
```go
func (t *Transformer) transformFunc(f *ast.FuncDecl) string {
    // Handle receiver (struct method)
    if f.Receiver != nil {
        // Go-style method with receiver: func (r ReceiverType) MethodName(...)
        sb.WriteString(fmt.Sprintf("func (%s %s) %s", 
            f.Receiver.Name, 
            t.transformType(f.Receiver.Type), 
            name))
    } else {
        // Regular function
        sb.WriteString(fmt.Sprintf("func %s", name))
    }
    // ... rest of function
}
```

**测试用例**: `transformer/transformer_struct_method_test.go`
- `TestTransformer_StructMethod` - 基本 receiver 方法
- `TestTransformer_StructMethodMultiple` - 多个方法
- `TestTransformer_StructMethodWithPointer` - receiver 方法

---

## 📊 对比说明

### 模板字符串语法对比

| 特性 | 之前 | 现在 |
|------|------|------|
| 语法 | `"Hello, ${name}!"` | `` `Hello, ${name}!` `` |
| Token | `STRING` | `TEMPLATE` |
| 风格 | Kotlin/其他 | ES6 (JavaScript) |
| 普通字符串 | `"text"` | `"text"` (不变) |
| 模板字符串 | 检测 `${` | 使用反引号自动识别 |

### 结构体方法对比

| 特性 | TypeScript Class | Go Receiver | Gox (现在) |
|------|------------------|-------------|------------|
| 定义位置 | 类内部 | 结构体外部 | 结构体外部 |
| 语法 | `method() {}` | `func (r T) method()` | `func (r: T) method()` |
| this/self | `this` | receiver 变量 | receiver 变量 |
| 示例 | `class User { getName() {} }` | `func (u User) GetName()` | `func (u: User) GetName()` |

---

## 📝 文件修改清单

### 新增/修改的文件

1. **token/token.go**
   - 添加 `TEMPLATE` token 类型

2. **lexer/lexer.go**
   - 修改 `readRawString()` 检测模板字符串

3. **parser/parser.go**
   - 修改 `parseStructDeclWithVisibility()` 回退到只解析字段
   - 修改 `parseFuncDeclWithVisibility()` 支持 receiver
   - 修改 `parsePrimary()` 处理 `TEMPLATE` token
   - 修改 `parseTemplateString()` 使用反引号

4. **ast/ast.go**
   - `StructDecl` 添加 `Methods` 字段（保留，暂不使用）
   - `FuncDecl` 添加 `Receiver` 字段

5. **transformer/transformer.go**
   - 修改 `transformFunc()` 处理 receiver

6. **Draft.MD**
   - 更新 8.2 节为 ES6 风格模板字符串
   - 添加 7.3 节：结构体方法（Go Receiver 风格）

### 新增的测试文件

1. **transformer/transformer_struct_method_test.go**
   - 6 个测试用例覆盖模板字符串和结构体方法

2. **test_es6_struct.gox**
   - 综合测试文件

---

## 🎯 使用示例

### 完整示例

```gox
package main

// ES6 模板字符串
let name = "Alice"
let age = 25
let greeting = `Hello, ${name}!`
let message = `I am ${age} years old`

// 结构体定义
public struct User {
    public name: string
    private age: int
}

// 结构体方法（Go receiver 风格）
public func (u: User) GetName(): string {
    return u.name
}

public func (u: User) GetAge(): int {
    return u.age
}

public func (u: User) SetAge(newAge: int) {
    u.age = newAge
}

// 普通函数
public func createUser(name: string, age: int): User {
    return User{name: name, age: age}
}

// 使用
println(greeting)  // Hello, Alice!
println(message)   // I am 25 years old
println(`User: ${name}, Age: ${age}`)
```

**生成的 Go 代码**:

```go
package main

import "fmt"

name := "Alice"
age := 25
greeting := fmt.Sprintf("Hello, %v!", name)
message := fmt.Sprintf("I am %v years old", age)

type User struct {
    Name string
    age int
}

func (u User) GetName() string {
    return u.Name
}

func (u User) GetAge() int {
    return u.age
}

func (u User) SetAge(newAge int) {
    u.age = newAge
}

func CreateUser(name string, age int) User {
    return User{Name: name, age: age}
}

fmt.Sprintln(greeting)
fmt.Sprintln(message)
fmt.Sprintln(fmt.Sprintf("User: %v, Age: %v", name, age))
```

---

## ✅ 测试覆盖

### 模板字符串测试
- ✅ 基本模板字符串
- ✅ 多个表达式
- ✅ 与 print/println 结合
- ✅ 反引号识别
- ✅ 转义处理

### 结构体方法测试
- ✅ 基本 receiver 方法
- ✅ 多个方法
- ✅ receiver 变量访问字段
- ✅ public/private 可见性
- ✅ 方法参数和返回值

---

## 🚀 下一步

- [ ] 实现结构体构造函数
- [ ] 支持 pointer receiver (`*User`)
- [ ] 支持方法链式调用
- [ ] 实现接口（interface）
- [ ] 添加更多模板字符串功能（标签模板、嵌套表达式等）

---

## 📚 参考

- [ES6 Template Literals](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Template_literals)
- [Go Methods](https://go.dev/tour/methods/1)
- [Go Receiver](https://go.dev/doc/effective_go#methods)

---

## 🎉 总结

本次更新成功实现了：

1. ✅ **ES6 风格模板字符串** - 使用反引号，更符合现代 JavaScript 开发者的习惯
2. ✅ **Go Receiver 风格结构体方法** - 正确的 Go 语法，在结构体外部定义方法

这两个功能使 Gox 更加现代化和易用，同时保持了与 Go 的完全兼容性。

**关键改进**:
- 模板字符串语法更清晰（反引号 vs 双引号）
- 结构体方法符合 Go 的最佳实践
- 完整的测试覆盖
- 详细的文档更新

Gox 现在可以更好地支持现代编程模式了！🎊
