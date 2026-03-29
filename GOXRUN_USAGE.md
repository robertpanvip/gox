# goxrun - 直接运行 .gox 文件

## 概述

`goxrun` 是一个命令行工具，让你可以像 `go run` 一样直接运行 `.gox` 文件，无需手动编译步骤。

## 安装

工具已经编译好，位于：
```
.\cmd\goxrun\goxrun.exe
```

## 使用方法

### 基本用法

```powershell
# 直接运行 .gox 文件
.\cmd\goxrun\goxrun.exe test\simple_test.gox
```

### 使用 PowerShell 脚本

```powershell
# 使用提供的 PowerShell 脚本（会自动编译 goxrun）
.\goxrun.ps1 test\simple_test.gox
```

### 运行带参数的程序

```powershell
# 传递参数给 Gox 程序
.\cmd\goxrun\goxrun.exe test\myapp.gox arg1 arg2
```

## 工作流程

当你运行 `goxrun` 时，它会自动执行以下步骤：

1. **解析** - 读取 `.gox` 文件并进行词法分析和语法分析
2. **转换** - 将 Gox 代码转换为 Go 代码
3. **生成临时文件** - 在源文件目录生成临时 `.go` 文件
4. **整理依赖** - 运行 `go mod tidy` 确保依赖完整
5. **编译** - 使用 Go 编译器编译生成的代码
6. **运行** - 执行编译后的程序
7. **清理** - 删除临时文件

## 示例

### 示例 1: 运行简单的 Gox 程序

创建文件 `test\hello.gox`:
```gox
func Main() {
    println("Hello from Gox!")
}
```

运行：
```powershell
.\cmd\goxrun\goxrun.exe test\hello.gox
```

输出：
```
🔍 Parsing hello.gox...
✓ Parsing successful
✓ Code generation successful
✓ Temporary file: hello_gox_temp_1234567890.go
📦 Running go mod tidy...
🔨 Building...
✓ Build successful
🚀 Running...
Hello from Gox!

✓ Execution completed
```

### 示例 2: 运行计数器程序

```powershell
.\cmd\goxrun\goxrun.exe test\demo_counter.gox
```

## 环境变量

可以通过环境变量自定义 Go 可执行文件路径：

```powershell
$env:GOX_GO_EXECUTABLE = "C:\Path\To\Your\go.exe"
.\cmd\goxrun\goxrun.exe test\app.gox
```

## 系统要求

- Windows 10/11
- PowerShell 5.1+
- Go 1.26+ (项目自带 `.\runtime\go\bin\go.exe`)

## 与手动编译的对比

### 传统方式（多步骤）
```powershell
# 1. 生成 Go 文件
.\gox.exe -o test\output.go test\app.gox

# 2. 进入 test 目录
cd test

# 3. 整理依赖
..\runtime\go\bin\go.exe mod tidy

# 4. 编译
..\runtime\go\bin\go.exe build output.go

# 5. 运行
.\output.exe
```

### 使用 goxrun（一键完成）
```powershell
.\cmd\goxrun\goxrun.exe test\app.gox
```

## 故障排除

### 问题 1: 找不到 Go 可执行文件

确保 `.\runtime\go\bin\go.exe` 存在，或设置 `GOX_GO_EXECUTABLE` 环境变量。

### 问题 2: 构建失败

检查生成的 Go 代码是否有语法错误。可以先用 `-o` 参数生成 Go 文件查看：
```powershell
.\gox.exe -o test\debug.go test\problem.gox
Get-Content test\debug.go
```

### 问题 3: 依赖问题

`goxrun` 会自动运行 `go mod tidy`。如果仍有问题，可以手动运行：
```powershell
cd test
..\runtime\go\bin\go.exe mod tidy
```

## 文件说明

- `cmd\goxrun\main.go` - goxrun 源代码
- `cmd\goxrun\goxrun.exe` - 编译后的可执行文件
- `goxrun.ps1` - PowerShell 辅助脚本（自动编译和运行）

## 开发说明

### 重新编译 goxrun

```powershell
.\runtime\go\bin\go.exe build -o cmd\goxrun\goxrun.exe cmd\goxrun\main.go
```

### 调试模式

修改源代码，添加更多调试输出：
```go
fmt.Printf("DEBUG: tempFile = %s\n", tempFile)
fmt.Printf("DEBUG: srcDir = %s\n", srcDir)
```

## 限制

- 目前只支持 Windows 平台
- 需要源文件目录存在 `go.mod` 文件
- 对于包含 FX 组件的代码，需要确保 transformer 生成的代码正确

## 更新日志

### v0.1.0 (2026-03-29)
- ✅ 初始版本
- ✅ 支持直接运行 .gox 文件
- ✅ 自动管理临时文件
- ✅ 自动运行 go mod tidy
- ✅ 支持 PowerShell 脚本
