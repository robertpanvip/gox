# 运行 test_fx_simple2 测试

## 快速开始

### 方法 1: 使用 PowerShell（推荐）

```powershell
.\test_fx_simple2_runner.ps1
```

### 方法 2: 使用批处理文件

```batch
run_fx_simple2_test.bat
```

### 方法 3: 使用 Go 命令

```bash
go run cmd\run_test_fx_simple2\main.go
```

## 测试内容

测试文件：`test/test_fx_simple2.gox`

```gox
import "github.com/gox-lang/gox/gui"

fx func Counter() {
    let count = 0
    
    return <button text="Click" onClick={func() {
        count++
    }} />
}
```

## 验证项目

测试程序会验证以下内容：

1. ✅ **后置自增运算符** - 检查生成的代码包含 `Count++`
2. ✅ **状态变量前缀** - 检查生成的代码包含 `c.Count++`
3. ✅ **自动更新机制** - 检查生成的代码包含 `RequestUpdate()`

## 预期结果

如果所有检查都通过，你会看到：

```
╔════════════════════════════════════════════════════════╗
║  🎉 成功：所有检查通过！修复已生效！                   ║
╚════════════════════════════════════════════════════════╝
```

## 相关文件

- **测试程序**: `cmd/run_test_fx_simple2/main.go`
- **测试报告**: `test/TEST_FX_SIMPLE2_REPORT.md`
- **修复文档**: `doc/POST_INCREMENT_FIX_SUMMARY.md`

## 故障排除

### 问题：找不到 go 命令

**解决方案**: 确保 Go 已安装并添加到系统 PATH

### 问题：编译错误

**解决方案**: 确保所有依赖已正确安装

```bash
go mod tidy
```

## 其他测试

### 测试后置自减

```bash
go test ./transformer -run TestTransformer_FxPostDecrement -v
```

### 测试所有 FX 相关

```bash
go test ./transformer -run "Fx.*" -v
```
