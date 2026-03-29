# 后置自增/自减运算符 - 修复对比

## 📊 修复前后对比

### 场景 1: `count++` 在 FX 组件事件处理器中

#### 输入代码
```typescript
fx func Counter() {
    let count = 0
    
    return <button onClick={func() {
        count++
    }} />
}
```

#### AST 结构
```
ExprStmt
└─ UnaryExpr
   ├─ Op: INC (token.INC)
   ├─ X: Ident("count")
   └─ Post: true
```

#### 修复前 ❌
```go
// transformStmtWithStatePrefix
if unary.Post {
    sb.WriteString(fmt.Sprintf("    %s%s++\n", prefix, ident.Name))
    //                                  ^^^ 硬编码
}
```

**输出**: `c.Count++` ✅ （碰巧正确）

#### 修复后 ✅
```go
// transformStmtWithStatePrefix
op := t.mapOp(unary.Op)  // op = "++"
if unary.Post {
    sb.WriteString(fmt.Sprintf("    %s%s%s\n", prefix, ident.Name, op))
}
```

**输出**: `c.Count++` ✅ （正确）

---

### 场景 2: `count--` 在 FX 组件事件处理器中

#### 输入代码
```typescript
fx func Counter() {
    let count = 0
    
    return <button onClick={func() {
        count--
    }} />
}
```

#### AST 结构
```
ExprStmt
└─ UnaryExpr
   ├─ Op: DEC (token.DEC)  ← 关键：这里是 DEC
   ├─ X: Ident("count")
   └─ Post: true
```

#### 修复前 ❌
```go
// transformStmtWithStatePrefix
if unary.Post {
    sb.WriteString(fmt.Sprintf("    %s%s++\n", prefix, ident.Name))
    //                                  ^^^ 硬编码为 "++"
}
```

**输出**: `c.Count++` ❌ **错误！应该是 `c.Count--`**

#### 修复后 ✅
```go
// transformStmtWithStatePrefix
op := t.mapOp(unary.Op)  // op = "--"
if unary.Post {
    sb.WriteString(fmt.Sprintf("    %s%s%s\n", prefix, ident.Name, op))
}
```

**输出**: `c.Count--` ✅ **正确！**

---

## 🔍 详细代码对比

### 修复位置 1: `transformStmtWithStatePrefix` 函数

```diff
case *ast.ExprStmt:
    if unary, ok := s.X.(*ast.UnaryExpr); ok {
        if ident, ok := unary.X.(*ast.Ident); ok {
            if containsStateVar(stateVars, ident.Name) {
-               if unary.Post {
-                   sb.WriteString(fmt.Sprintf("    %s%s++\n", prefix, ident.Name))
-               } else {
-                   sb.WriteString(fmt.Sprintf("    ++%s%s\n", prefix, ident.Name))
-               }
+               op := t.mapOp(unary.Op)
+               if unary.Post {
+                   sb.WriteString(fmt.Sprintf("    %s%s%s\n", prefix, ident.Name, op))
+               } else {
+                   sb.WriteString(fmt.Sprintf("    %s%s%s\n", prefix, op, ident.Name))
+               }
            }
        }
    }
```

### 修复位置 2: `transformExprWithStatePrefix` 函数

```diff
case *ast.UnaryExpr:
    x := t.transformExprWithStatePrefix(e.X, stateVars, prefix)
+   op := t.mapOp(e.Op)
    if e.Post {
-       return x + "++"
+       return x + op
    }
-   return t.mapOp(e.Op) + x
+   return op + x
```

---

## 🎯 为什么需要修复？

### 问题根源

1. **AST 中保存了正确的运算符类型**
   ```go
   &ast.UnaryExpr{
       Op: token.DEC,  // ← 明确标记为 DEC
       Post: true,
   }
   ```

2. **但 Transformer 忽略了它**
   ```go
   // ❌ 错误：硬编码为 "++"
   return x + "++"
   
   // ✅ 正确：使用 AST 中的运算符
   op := t.mapOp(e.Op)  // 从 AST 读取
   return x + op
   ```

3. **`mapOp` 函数的作用**
   ```go
   func (t *Transformer) mapOp(op token.TokenKind) string {
       switch op {
       case token.INC:
           return "++"  // 自增
       case token.DEC:
           return "--"  // 自减
       // ...
       }
   }
   ```

---

## 📝 完整的代码生成示例

### 输入
```typescript
import "github.com/gox-lang/gox/gui"

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

### 期望输出（修复后）
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
                c.Count++              // ✅ 正确的后置自增
                c.RequestUpdate()
            },
        }),
        gui.NewButton(gui.ButtonProps{
            Text: "Decrement",
            OnClick: func() {
                c.Count--              // ✅ 正确的后置自减
                c.RequestUpdate()
            },
        }),
    )
    
    // ... 动态部分 ...
    
    return c
}
```

---

## 🧪 测试验证

### 测试用例

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
    
    p := parser.New(src)
    prog := p.ParseProgram()
    
    if len(p.Errors()) > 0 {
        t.Fatalf("parser errors: %v", p.Errors())
    }
    
    tfm := New()
    result := tfm.Transform(prog)
    
    // 验证两个运算符都正确生成
    if !strings.Contains(result, "c.Count++") {
        t.Error("expected 'c.Count++' in output")
    }
    if !strings.Contains(result, "c.Count--") {
        t.Error("expected 'c.Count--' in output")
    }
}
```

---

## 📈 修复影响

| 方面 | 修复前 | 修复后 |
|------|--------|--------|
| `count++` 转换 | ✅ 碰巧正确 | ✅ 正确 |
| `count--` 转换 | ❌ 错误为 `++` | ✅ 正确为 `--` |
| 代码可维护性 | ❌ 硬编码 | ✅ 动态映射 |
| 代码一致性 | ❌ 与其他函数不一致 | ✅ 保持一致 |
| 测试覆盖 | ❌ 缺少测试 | ✅ 完整测试 |

---

## ✅ 总结

### 修复的关键点

1. **识别问题**: Transformer 硬编码运算符，忽略了 AST 中的实际值
2. **定位原因**: 两处代码都硬编码了 `"++"`
3. **实施修复**: 使用 `t.mapOp(e.Op)` 动态获取运算符
4. **验证修复**: 添加测试用例覆盖 `++` 和 `--`

### 学到的教训

- ✅ 永远不要硬编码可以从 AST 中读取的值
- ✅ 保持相似函数的实现一致性
- ✅ 测试应该覆盖所有变体（不仅仅是常见的）
- ✅ 代码审查时要检查"看似正确"的代码

---

**修复完成日期**: 2026-03-29  
**测试状态**: ✅ 已添加测试用例  
**文档状态**: ✅ 已完成
