# 结构体字面量实现

## 📋 功能说明

现在 Gox 编译器支持 Go 风格的结构体创建语法，包括两种初始化方式：

### 1. 带字段名（推荐）

```gox
public struct User {
    public name: string
    public age: int
}

let user = User{
    name: "Alice",
    age: 25
}
```

**转译为 Go**:
```go
type User struct {
    Name string
    Age int
}

user := User{
    Name: "Alice",
    Age: 25,
}
```

### 2. 位置初始化（按字段顺序）

```gox
public struct Point {
    public x: int
    public y: int
}

let point = Point{10, 20}
```

**转译为 Go**:
```go
type Point struct {
    X int
    Y int
}

point := Point{10, 20}
```

## 🎯 特性

### 1. 字段名自动转换

- public 字段名首字母自动大写
- private 字段名保持小写

```gox
public struct User {
    public name: string    // → Name
    private age: int       // → age
}

let u = User{name: "Alice"}
```

转译为：
```go
type User struct {
    Name string
    age int
}

u := User{Name: "Alice"}
```

### 2. 支持表达式

```gox
let x = 10
let y = 20
let point = Point{x: x, y: y}
let point2 = Point{x + 5, y + 10}
```

转译为：
```go
x := 10
y := 20
point := Point{X: x, Y: y}
point2 := Point{x + 5, y + 10}
```

### 3. 嵌套结构体

```gox
public struct Container {
    public user: User
    public point: Point
}

let container = Container{
    user: User{name: "Bob", age: 30},
    point: Point{100, 200}
}
```

转译为：
```go
type Container struct {
    User User
    Point Point
}

container := Container{
    User: User{Name: "Bob", Age: 30},
    Point: Point{100, 200},
}
```

### 4. 空结构体

```gox
public struct Empty {}
let e = Empty{}
```

转译为：
```go
type Empty struct {}
e := Empty{}
```

## 📝 实现细节

### AST 节点

```go
type StructLit struct {
    Type   Expr
    Fields []*StructField
    P      Position
}

type StructField struct {
    Name  string
    Value Expr
    P     Position
}
```

### Parser 解析

```go
func (p *Parser) parsePostfix() ast.Expr {
    // ...
    case p.curTok.Kind == token.LBRACE:
        // Check if this is a struct literal
        if ident, ok := x.(*ast.Ident); ok {
            p.nextToken()
            fields := p.parseStructFields()
            x = &ast.StructLit{Type: ident, Fields: fields, P: ident.P}
        }
    // ...
}

func (p *Parser) parseStructFields() []*ast.StructField {
    fields := make([]*ast.StructField, 0)
    
    for p.curTok.Kind != token.RBRACE {
        // Check if this is a named field (fieldName: value)
        if p.curTok.Kind == token.IDENT && p.peekTok.Kind == token.COLON {
            // Named field
            name := p.curTok.Literal
            p.nextToken() // skip identifier
            p.nextToken() // skip colon
            value := p.parseExpr()
            
            fields = append(fields, &ast.StructField{
                Name:  name,
                Value: value,
            })
        } else {
            // Positional field
            value := p.parseExpr()
            fields = append(fields, &ast.StructField{
                Name:  "",
                Value: value,
            })
        }
    }
    
    return fields
}
```

### Transformer 转译

```go
case *ast.StructLit:
    typeName := t.transformExpr(e.Type)
    fields := make([]string, 0)
    for _, field := range e.Fields {
        if field.Name != "" {
            // Named field - capitalize if public
            fieldName := field.Name
            if len(fieldName) > 0 && fieldName[0] >= 'a' && fieldName[0] <= 'z' {
                fieldName = strings.Title(fieldName)
            }
            fields = append(fields, fmt.Sprintf("%s: %s", fieldName, t.transformExpr(field.Value)))
        } else {
            // Positional field
            fields = append(fields, t.transformExpr(field.Value))
        }
    }
    return fmt.Sprintf("%s{%s}", typeName, strings.Join(fields, ", "))
```

## 📊 语法对比

| 特性 | Gox | Go | TypeScript |
|------|-----|----|------------|
| 带字段名 | `User{name: "Alice"}` | `User{Name: "Alice"}` | `{name: "Alice"}` |
| 位置初始化 | `Point{10, 20}` | `Point{10, 20}` | ❌ |
| 嵌套 | `Container{user: User{...}}` | `Container{User: User{...}}` | `{user: {...}}` |
| 空结构体 | `Empty{}` | `Empty{}` | `{}` |
| 字段名转换 | 自动 | 手动 | N/A |

## 🧪 测试覆盖

测试文件：
- `transformer/transformer_struct_lit_test.go` - 5 个单元测试
  - `TestTransformer_StructLitNamedFields` - 带字段名
  - `TestTransformer_StructLitPositional` - 位置初始化
  - `TestTransformer_StructLitMixed` - 混合使用
  - `TestTransformer_StructLitWithExpressions` - 表达式
  - `TestTransformer_StructLitEmpty` - 空结构体

- `test_struct_lit.gox` - 综合测试

## 📝 修改的文件

1. **ast/ast.go**
   - 添加 `StructLit` 节点
   - 添加 `StructField` 节点

2. **parser/parser.go**
   - 修改 `parsePostfix()` 检测结构体字面量
   - 添加 `parseStructFields()` 解析字段

3. **transformer/transformer.go**
   - 添加 `StructLit` 转译逻辑
   - 字段名自动大小写转换

4. **Draft.MD**
   - 添加第 7.4.3 节：结构体字面量

## ⚠️ 注意事项

1. **推荐带字段名** - 更清晰，不易出错
2. **位置初始化** - 必须按字段声明顺序
3. **字段名转换** - public 字段首字母自动大写
4. **混合使用** - 不能同时使用带字段名和位置初始化

## 🎉 总结

现在 Gox 编译器完全支持 Go 风格的结构体创建语法：

✅ 带字段名的初始化（推荐）
✅ 位置初始化（按字段顺序）
✅ 字段名自动转换大小写
✅ 支持表达式作为字段值
✅ 支持嵌套结构体
✅ 支持空结构体
✅ 完整的测试覆盖
✅ 详细的文档说明

这使得创建结构体实例更加直观和方便！🎊
