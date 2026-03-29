# test_fx_simple2 测试 - 快速运行指南

## 🚀 最简单的运行方式

### PowerShell 脚本（推荐）

```powershell
.\test_fx_simple2_runner.ps1
```

这个脚本会：
1. 自动查找项目中的 gox 编译器
2. 运行 test_fx_simple2.gox 文件
3. 显示解析和生成的代码
4. 验证修复是否生效

## 📂 测试文件

- **源代码**: `test/test_fx_simple2.gox`
- **测试脚本**: `test_fx_simple2_runner.ps1`
- **测试程序**: `cmd/run_test_fx_simple2/main.go`

## 🔍 测试内容

```gox
import "github.com/gox-lang/gox/gui"

fx func Counter() {
    let count = 0
    
    return <button text="Click" onClick={func() {
        count++  // ← 测试后置自增运算符
    }} />
}
```

## ✅ 验证项目

测试会检查以下内容：

1. ✅ **后置自增运算符** - `Count++`
2. ✅ **状态变量前缀** - `c.Count++`
3. ✅ **自动更新机制** - `RequestUpdate()`

## 📊 预期输出

```
╔════════════════════════════════════════════════════════╗
║        运行 test_fx_simple2.gox 测试                  ║
╚════════════════════════════════════════════════════════╝

【测试文件】
  test/test_fx_simple2.gox

【使用编译器】
  E:\Soft\JetBrains\WebStorm WorkSpace\go-ts\test\gox.exe~

【运行编译和测试】
=== 源代码 ===
import "github.com/gox-lang/gox/gui"

fx func Counter() {
    let count = 0
    
    return <button text="Click" onClick={func() {
        count++
    }} />
}

=== 解析结果 ===
  ✅ 解析成功！

=== 生成的 Go 代码 ===
...

=== 验证结果 ===
  ✅ 后置自增运算符    : Count++
  ✅ 状态变量前缀      : c.Count++
  ✅ 自动更新机制      : RequestUpdate()

╔════════════════════════════════════════════════════════╗
║  🎉 成功：所有检查通过！修复已生效！                   ║
╚════════════════════════════════════════════════════════╝
```

## 🛠️ 其他运行方法

### 方法 2: 直接使用 gox 编译器

```bash
.\test\gox.exe~ .\test\test_fx_simple2.gox
```

### 方法 3: 使用 go run

```bash
go run cmd\run_test_fx_simple2\main.go
```

### 方法 4: 运行单元测试

```bash
go test ./transformer -run TestTransformer_FxPostIncrementAndDecrement -v
```

## 📖 相关文档

- **完整指南**: `test/RUN_FX_SIMPLE2_GUIDE.md`
- **测试报告**: `test/TEST_FX_SIMPLE2_REPORT.md`
- **修复总结**: `FX_SIMPLE2_TEST_COMPLETE.md`
- **技术分析**: `doc/POST_INCREMENT_ANALYSIS.md`

## 🔧 故障排除

### 问题 1: 找不到 gox.exe~

**解决方案**:
1. 检查 `test\gox.exe~` 是否存在
2. 或者使用 `gox_new.exe~`
3. 或者使用系统 Go 运行测试程序

### 问题 2: PowerShell 执行策略限制

**解决方案**:
```powershell
Set-ExecutionPolicy -Scope Process -ExecutionPolicy Bypass
.\test_fx_simple2_runner.ps1
```

### 问题 3: 测试失败

**解决方案**:
1. 确认 `transformer/transformer_fx.go` 已正确修复
2. 查看生成的代码，检查是否包含 `c.Count++`
3. 参考 `doc/POST_INCREMENT_FIX_SUMMARY.md` 了解修复详情

## 📝 修复摘要

**问题**: Transformer 硬编码了 `"++"` 运算符

**修复**: 使用 `t.mapOp(e.Op)` 动态获取正确的运算符

**文件**: `transformer/transformer_fx.go`

**影响**: 
- ✅ `count++` 现在正确转换为 `c.Count++`
- ✅ `count--` 现在正确转换为 `c.Count--`

---

**创建日期**: 2026-03-29  
**最后更新**: 2026-03-29  
**状态**: ✅ 测试通过
