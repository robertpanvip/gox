# goxrun 快速参考

## 一行命令运行 .gox 文件

```powershell
# 方式 1: 直接使用编译好的 goxrun
.\cmd\goxrun\goxrun.exe test\your_file.gox

# 方式 2: 使用 PowerShell 脚本（推荐）
.\goxrun.ps1 test\your_file.gox
```

## 对比

| 方式 | 命令长度 | 步骤 |
|------|---------|------|
| **goxrun** | `.\goxrun.ps1 test\app.gox` | 1 步 ✅ |
| 传统 | `.\gox.exe -o test\app.go test\app.gox` + `cd test` + `go mod tidy` + `go build app.go` + `.\app.exe` | 5 步 ❌ |

## 常用场景

### 快速测试
```powershell
.\goxrun.ps1 test\simple_test.gox
```

### 运行示例
```powershell
.\goxrun.ps1 test\demo_counter.gox
```

### 运行 FX 组件（需要 transformer 支持）
```powershell
.\goxrun.ps1 test\fx_component.gox
```

## 输出示例

```
🔍 Parsing simple_test.gox...
✓ Parsing successful
✓ Code generation successful
✓ Temporary file: simple_test_gox_temp_1774783587493323500.go
📦 Running go mod tidy...
🔨 Building...
✓ Build successful
🚀 Running...
Hello from Gox!
x = 42

✓ Execution completed
```

## 提示

- ✅ 临时文件会自动清理
- ✅ 自动处理依赖（go mod tidy）
- ✅ 使用项目自带的 Go 环境（.\runtime\go\bin\go.exe）
- ✅ 支持传递命令行参数

## 故障排除

如果遇到问题：

```powershell
# 1. 手动运行 gox 查看生成的代码
.\gox.exe -o test\debug.go test\problem.gox
Get-Content test\debug.go

# 2. 检查 test 目录的 go.mod
cd test
..\runtime\go\bin\go.exe mod tidy

# 3. 重新编译 goxrun
.\runtime\go\bin\go.exe build -o cmd\goxrun\goxrun.exe cmd\goxrun\main.go
```
