# Gox 项目快速编译指南

## 🚀 快速开始

### 方法 1：预编译（推荐）

第一次使用时运行预编译脚本：

```powershell
.\precompile.ps1
```

这会：
- 预编译 GUI 库到缓存
- 编译 gox 工具

### 方法 2：使用构建脚本

```powershell
.\build_and_run.ps1 simple_button.gox
```

### 方法 3：手动编译（最灵活）

```powershell
# 1. 生成 Go 代码
.\gox.exe -o test\simple_button.go test\simple_button.gox

# 2. 编译为 exe（第一次较慢，之后会使用缓存）
cd test
..\runtime\go\bin\go.exe build -o simple_button.exe simple_button.go

# 3. 运行（瞬间启动）
.\simple_button.exe
```

## ⚡ 优化技巧

### 1. 使用 Go 编译缓存

Go 会自动缓存编译结果，确保不要禁用缓存：

```powershell
# ✅ 好的做法
go build -o app.exe app.go

# ❌ 避免使用 -a 参数（每次都强制重新编译）
go build -a -o app.exe app.go
```

### 2. 分离编译和运行

```powershell
# ✅ 先编译，再运行
go build -o app.exe app.go
.\app.exe

# ❌ 避免使用 go run（每次都重新编译）
go run app.go
```

### 3. 使用 VS Code 任务

在 `.vscode/tasks.json` 中添加：

```json
{
    "label": "Build and Run Gox",
    "type": "shell",
    "command": ".\\build_and_run.ps1 ${file}",
    "group": "build"
}
```

然后按 `Ctrl+Shift+B` 快速构建运行。

## 📊 编译速度对比

| 方法 | 首次编译 | 后续编译 | 推荐场景 |
|------|---------|---------|---------|
| `go run` | 30-60 秒 | 30-60 秒 | ❌ 不推荐 |
| `go build` | 30-60 秒 | 2-5 秒 | ✅ 推荐 |
| 预编译后 `go build` | 5-10 秒 | 1-2 秒 | ✅✅ 最佳 |

## 🔧 故障排除

### 编译很慢

1. 检查是否使用了 `-a` 参数（会禁用缓存）
2. 运行预编译脚本：`.\precompile.ps1`
3. 清理缓存后重试：`go clean -cache`

### 窗口不显示

1. 检查是否有编译错误
2. 确保 Button 设置了 Text 属性
3. 检查是否有红色边框（调试用）

## 📝 最佳实践

1. **开发阶段**：使用 `go build` 编译为 exe，然后反复运行测试
2. **调试阶段**：修改代码后重新编译，利用 Go 的增量编译
3. **发布阶段**：使用 `go build -ldflags="-s -w"` 减小 exe 体积

```powershell
# 发布构建（更小的 exe）
go build -ldflags="-s -w" -o app.exe app.go
```
