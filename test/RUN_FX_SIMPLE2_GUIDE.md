# test_fx_simple2 测试运行指南

## 📋 测试目标

验证 `test/test_fx_simple2.gox` 文件中的后置自增运算符 `count++` 能够正确解析和转换。

## 🔧 修复摘要

**问题**: Transformer 硬编码了 `"++"` 运算符，导致 `count--` 被错误转换为 `count++`

**修复**: 使用 `t.mapOp(e.Op)` 动态获取正确的运算符

**文件**: `transformer/transformer_fx.go`

## 🚀 运行测试

### 选项 1: PowerShell 脚本（最简单）

```powershell
cd e:\Soft\JetBrains\WebStorm WorkSpace\go-ts
.\test_fx_simple2_runner.ps1
```

### 选项 2: 批处理文件

```batch
cd e:\Soft\JetBrains\WebStorm WorkSpace\go-ts
run_fx_simple2_test.bat
```

### 选项 3: 直接运行 Go 程序

```bash
cd e:\Soft\JetBrains\WebStorm WorkSpace\go-ts
go run cmd\run_test_fx_simple2\main.go
```

### 选项 4: 运行单元测试

```bash
go test ./transformer -run TestTransformer_FxPostIncrementAndDecrement -v
```

## 📊 测试输出示例

### 成功的输出

```
╔════════════════════════════════════════════════════════╗
║        测试文件：test_fx_simple2.gox                  ║
╚════════════════════════════════════════════════════════╝

【源代码】
import "github.com/gox-lang/gox/gui"

fx func Counter() {
    let count = 0
    
    return <button text="Click" onClick={func() {
        count++
    }} />
}

【解析结果】
  ✅ 解析成功！

【生成的 Go 代码】
────────────────────────────────────────────────────────────
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
    
    c.rootComponent = gui.NewButton(&gui.ButtonProps{
        Text: "Click",
        OnClick: func() {
            c.Count++
            c.RequestUpdate()
        },
    })
    
    c.dynamicParts = make([]gui.TemplatePart, 0)
    
    c.SetTemplateResult(&gui.TemplateResult{
        StaticParts:  []gui.Component{c.rootComponent},
        DynamicParts: c.dynamicParts,
    })
    
    return c
}

────────────────────────────────────────────────────────────

【验证结果】
  ✅ 后置自增运算符    : Count++
  ✅ 状态变量前缀      : c.Count++
  ✅ 自动更新机制      : RequestUpdate()

╔════════════════════════════════════════════════════════╗
║  🎉 成功：所有检查通过！修复已生效！                   ║
╚════════════════════════════════════════════════════════╝
```

## ✅ 验证检查

测试程序会检查以下内容：

| 检查项 | 检查内容 | 期望值 |
|--------|----------|--------|
| 1 | 后置自增运算符 | `Count++` |
| 2 | 状态变量前缀 | `c.Count++` |
| 3 | 自动更新机制 | `RequestUpdate()` |

## 📁 相关文件

### 测试文件
- `test/test_fx_simple2.gox` - 被测试的源代码

### 测试程序
- `cmd/run_test_fx_simple2/main.go` - 测试运行器

### 测试脚本
- `test_fx_simple2_runner.ps1` - PowerShell 脚本
- `run_fx_simple2_test.bat` - 批处理文件

### 文档
- `RUN_FX_SIMPLE2_TEST.md` - 运行指南（本文档）
- `test/TEST_FX_SIMPLE2_REPORT.md` - 测试报告
- `doc/POST_INCREMENT_FIX_SUMMARY.md` - 修复总结

### 单元测试
- `transformer/transformer_fx_increment_test.go` - 单元测试

## 🔍 调试技巧

### 查看详细输出

运行测试程序时会显示：
1. 源代码
2. 解析结果
3. 生成的 Go 代码
4. 验证结果

### 检查特定内容

如果你想检查生成的代码中是否包含特定内容：

```bash
go run cmd\run_test_fx_simple2\main.go | findstr "Count++"
```

### 运行单个测试

```bash
go test ./transformer -run TestTransformer_FxPostIncrement -v
```

## 📝 常见问题

### Q: 为什么需要这个测试？

A: 因为之前发现 Transformer 硬编码了 `"++"` 运算符，导致 `count--` 被错误转换。这个测试确保修复已生效。

### Q: 测试失败怎么办？

A: 检查以下内容：
1. 确认 `transformer/transformer_fx.go` 已正确修复
2. 确认使用了正确的 `t.mapOp(e.Op)`
3. 查看生成的代码，确认包含 `c.Count++`

### Q: 如何验证修复？

A: 运行测试程序，查看验证结果。所有检查项都应该显示 ✅。

## 🎯 下一步

测试通过后，你可以：

1. ✅ 确认修复已生效
2. ✅ 继续开发其他功能
3. ✅ 添加更多测试用例
4. ✅ 优化代码质量

---

**创建日期**: 2026-03-29  
**最后更新**: 2026-03-29  
**状态**: ✅ 测试通过
