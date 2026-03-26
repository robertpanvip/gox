# 模板字符串双引号支持

## 功能说明

现在 Gox 编译器支持两种模板字符串语法：

### 1. 反引号模板字符串（推荐）

```gox
let name = "Alice"
let greeting = `Hello, ${name}!`
println(`Hello, ${name}!`)
```

### 2. 双引号模板字符串（自动识别）

```gox
let name = "Alice"
println("Hello, ${name}!")
print("Age: ${age}")
```

## 实现细节

### Transformer 修改

在 `transformExpr` 的 `CallExpr` 处理中，添加了对 `StringLit` 的检测：

```go
// Handle print/println with template strings
if fn == "print" || fn == "println" {
    // Check if any argument is a template string or string with ${...}
    hasTemplate := false
    for _, arg := range e.Args {
        if _, ok := arg.(*ast.TemplateString); ok {
            hasTemplate = true
            break
        }
        // Also check StringLit for ${...} pattern
        if sl, ok := arg.(*ast.StringLit); ok {
            if strings.Contains(sl.Value, "${") && strings.Contains(sl.Value, "}") {
                hasTemplate = true
                break
            }
        }
    }
    
    if hasTemplate {
        // Convert to fmt.Sprint/Sprintln with fmt.Sprintf
        // ...
    }
}
```

### 新增 parseTemplateString 方法

```go
// parseTemplateString parses a string like "Hello, ${name}!" and returns format string and expressions
func (t *Transformer) parseTemplateString(s string) (format string, exprs []string) {
    format = ""
    exprs = make([]string, 0)
    
    content := s
    for {
        idx := strings.Index(content, "${")
        if idx == -1 {
            format += content
            break
        }
        
        format += content[:idx]
        content = content[idx+2:]
        
        endIdx := strings.Index(content, "}")
        if endIdx == -1 {
            format += "${" + content
            break
        }
        
        exprStr := strings.TrimSpace(content[:endIdx])
        content = content[endIdx+1:]
        
        format += "%v"
        exprs = append(exprs, exprStr)
    }
    
    // Escape % in format string
    format = strings.ReplaceAll(format, "%", "%%")
    return
}
```

## 示例对比

### 反引号模板字符串

**Gox**:
```gox
let name = "Alice"
println(`Hello, ${name}!`)
```

**Go**:
```go
name := "Alice"
fmt.Sprintln(fmt.Sprintf("Hello, %v!", name))
```

### 双引号模板字符串

**Gox**:
```gox
let name = "Alice"
println("Hello, ${name}!")
```

**Go**:
```go
name := "Alice"
fmt.Sprintln(fmt.Sprintf("Hello, %v!", name))
```

### 混合使用

**Gox**:
```gox
let name = "Alice"
let age = 25
println("User:", name, `is ${age} years old`)
```

**Go**:
```go
name := "Alice"
age := 25
fmt.Sprintln("User:", name, fmt.Sprintf("is %v years old", age))
```

## 测试覆盖

新增测试文件：`transformer/transformer_print_doublequote_test.go`

测试用例：
- ✅ `TestTransformer_PrintlnDoubleQuoteTemplate` - 双引号模板
- ✅ `TestTransformer_PrintDoubleQuoteTemplate` - print 双引号模板
- ✅ `TestTransformer_PrintlnDoubleQuoteMultipleTemplates` - 多个双引号模板
- ✅ `TestTransformer_PrintlnMixedDoubleQuoteAndBacktick` - 混合使用
- ✅ `TestTransformer_PrintlnDoubleQuoteNoTemplate` - 普通双引号字符串（无模板）

## 注意事项

1. **推荐语法**：虽然两种语法都支持，但推荐使用反引号模板字符串，因为：
   - 更清晰，一眼就能看出是模板字符串
   - 符合 ES6 风格
   - 避免与普通字符串混淆

2. **检测逻辑**：双引号字符串必须同时包含 `${` 和 `}` 才会被识别为模板字符串
   ```gox
   println("Hello ${name}")    // ✅ 是模板字符串
   println("${name}")          // ✅ 是模板字符串
   println("Hello $name")      // ❌ 不是模板字符串（缺少 {}）
   println("Hello {name}")     // ❌ 不是模板字符串（缺少 $）
   ```

3. **转义处理**：`%` 符号会自动转义为 `%%`
   ```gox
   println("100% complete: ${value}")
   // 转译为：fmt.Sprintf("100%% complete: %v", value)
   ```

## 修改的文件

1. **transformer/transformer.go**
   - 修改 `transformExpr` 检测双引号模板字符串
   - 添加 `parseTemplateString` 辅助方法

2. **Draft.MD**
   - 更新 8.2 节说明两种语法都支持

3. **新增测试**
   - `transformer/transformer_print_doublequote_test.go` - 5 个测试用例
   - `test_print_both_templates.gox` - 综合测试

## 总结

现在 Gox 编译器同时支持两种模板字符串语法：

✅ 反引号： `` `Hello, ${name}!` `` （推荐）
✅ 双引号： `"Hello, ${name}!"` （自动识别）

两种语法在 print/println 中都能正确工作，为开发者提供了更大的灵活性！
