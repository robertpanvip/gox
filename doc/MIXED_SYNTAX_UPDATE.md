# Mixed 语法更新

## 修改内容

将 `mixed` 关键字从结构体内部移到结构体声明处，类似继承语法。

### 之前的语法（❌ 不再支持）

```gox
public struct Derived {
    mixed Base
    public name: string
}
```

### 新的语法（✅ 现在支持）

```gox
public struct Derived mixed Base {
    public name: string
}

// 支持多个嵌入
public struct C mixed A mixed B {
    public c: float64
}
```

## 修改的文件

1. **parser/parser.go** - 修改 `parseStructDeclWithVisibility()` 在结构体名称后解析 `mixed`
2. **transformer/transformer_mixed_interface_test.go** - 更新测试用例使用新语法
3. **test_mixed_interface.gox** - 更新综合测试
4. **Draft.MD** - 更新文档示例
5. **IMPLEMENTATION_MIXED_INTERFACE.md** - 更新实现文档

## 实现细节

### Parser 修改

```go
func (p *Parser) parseStructDeclWithVisibility(vis ast.Visibility) ast.Decl {
    pos := ast.Position{Line: p.curTok.Line, Col: p.curTok.Col}
    p.nextToken()

    name := p.expect(token.IDENT).Literal

    // Check for mixed keyword after struct name
    mixed := make([]*ast.BaseType, 0)
    if p.check(token.MIXED) {
        p.nextToken()
        baseType := p.parseBaseType()
        if bt, ok := baseType.(*ast.BaseType); ok {
            mixed = append(mixed, bt)
        }
    }

    p.expect(token.LBRACE)
    // ... 解析字段
}
```

### 生成的 Go 代码

```go
// Gox
public struct Derived mixed Base {
    public name: string
}

// Go
type Derived struct {
    Base
    Name string
}
```

## 优势

1. ✅ 语法更清晰 - 类似继承，一眼就能看出是组合关系
2. ✅ 更符合直觉 - `struct Derived mixed Base` 表示 Derived 混合了 Base
3. ✅ 支持多个 mixed - `struct C mixed A mixed B`
4. ✅ 与 Go 的 embedding 语义一致

## 测试覆盖

- ✅ 基本 mixed 语法
- ✅ 多个 mixed
- ✅ mixed 带方法的 struct
- ✅  visibility 正确处理

## 示例对比

### TypeScript 继承风格
```typescript
class Base {
    value: number
}

class Derived extends Base {
    name: string
}
```

### Gox mixed 风格
```gox
public struct Base {
    public value: int
}

public struct Derived mixed Base {
    public name: string
}
```

### Go embedding
```go
type Base struct {
    Value int
}

type Derived struct {
    Base
    Name string
}
```

Gox 的 `mixed` 关键字提供了 TypeScript 风格的声明语法，同时保持 Go 的 embedding 语义！
