# Gox 项目规范

## 开发环境

### 操作系统
- **主要开发平台**: Windows 10/11
- **Shell**: PowerShell 5.1+
- **IDE**: WebStorm / Trae IDE

### Go 运行环境

**Go 编译器位置**: `.\runtime\go\bin\go.exe`

**编译项目**:
```powershell
.\runtime\go\bin\go.exe build -o gox.exe cmd/gox/main.go
```

**运行测试**:
```powershell
# 编译 gox 编译器
.\runtime\go\bin\go.exe build -o gox.exe cmd/gox/main.go

# 复制 gox.exe 到 test 目录
Copy-Item gox.exe test\ -Force

# 运行 .gox 文件
.\gox.exe test\demo_counter.gox

# 或生成 Go 文件
.\gox.exe -o test\output.go test\demo_counter.gox
```

**运行 GUI 程序**:
```powershell
# 方式 1: 使用 run_tsx_gui.go 脚本
.\runtime\go\bin\go.exe run run_tsx_gui.go

# 方式 2: 手动编译并运行
.\gox.exe -o test\tsx_gui_demo.go test\tsx_gui_demo.gox
cd test
..\runtime\go\bin\go.exe build tsx_gui_demo.go
.\tsx_gui_demo.exe
```

**批处理脚本**:
```powershell
# 使用提供的批处理脚本
.\run_fx_simple2_test.bat
```

### 快速开始示例

**1. 编译 Gox 编译器**:
```powershell
cd e:\Soft\JetBrains\WebStorm WorkSpace\go-ts
.\runtime\go\bin\go.exe build -o gox.exe cmd/gox/main.go
```

**2. 运行简单的 Gox 程序**:
```powershell
# 直接运行（输出到控制台）
.\gox.exe test\demo_counter.gox

# 生成 Go 文件
.\gox.exe -o test\demo_counter.go test\demo_counter.gox

# 查看生成的 Go 代码
Get-Content test\demo_counter.go
```

**3. 运行 GUI 示例（显示窗口）**:

**方式 1: 使用自动化脚本（推荐）**:
```powershell
# 一键运行：解析 -> 转换 -> 编译 -> 运行 GUI
.\runtime\go\bin\go.exe run run_tsx_gui.go
```

**方式 2: 手动步骤**:
```powershell
# Step 1: 转换 Gox 为 Go
.\gox.exe -o test\tsx_gui_demo.go test\tsx_gui_demo.gox

# Step 2: 编译 Go 程序
cd test
..\runtime\go\bin\go.exe build tsx_gui_demo.go

# Step 3: 运行 GUI 程序（会弹出窗口）
.\tsx_gui_demo.exe
```

**4. 运行测试套件**:
```powershell
# 编译 gox 编译器
.\runtime\go\bin\go.exe build -o gox.exe cmd/gox/main.go
Copy-Item gox.exe test\ -Force

# 运行各个测试用例
.\gox.exe test\test_basic.gox
.\gox.exe test\test_fx_assignment.gox
.\gox.exe test\tsx_fx_component.gox
```

### 完整测试用例运行流程

#### 步骤 1: 准备环境
```powershell
# 1. 进入项目目录
cd e:\Soft\JetBrains\WebStorm WorkSpace\go-ts

# 2. 编译 gox 编译器
.\runtime\go\bin\go.exe build -o gox.exe cmd/gox/main.go

# 3. 复制 gox.exe 到 test 目录
Copy-Item gox.exe test\ -Force

# 4. 进入 test 目录
cd test
```

#### 步骤 2: 运行测试用例（查看输出）
```powershell
# 运行测试用例（会输出 token 序列、AST、生成的 Go 代码）
.\gox.exe test_fx_assignment.gox

# 或运行完整的 FX 组件示例
.\gox.exe demo_counter.gox
```

**输出说明**：
- `=== Tokens ===` - 词法分析结果（每个 token 一行）
- `=== AST ===` - 抽象语法树（显示解析后的结构）
- `=== Generated Go Code ===` - 生成的 Go 代码

#### 步骤 3: 生成 Go 文件
```powershell
# 使用 -o 参数生成 Go 文件
.\gox.exe -o demo_counter.go demo_counter.gox

# 查看生成的代码
Get-Content demo_counter.go

# 或保存到文本文件
.\gox.exe test_fx_assignment.gox > output.txt
notepad output.txt
```

#### 步骤 4: 编译生成的 Go 代码
```powershell
# 编译 Go 程序
..\runtime\go\bin\go.exe build demo_counter.go

# Windows 会生成 .exe 文件
# demo_counter.exe
```

#### 步骤 5: 运行程序
```powershell
# 运行编译后的程序
.\demo_counter.exe

# 如果是 GUI 程序，会弹出窗口显示界面
```

### 快速运行脚本

**一键运行测试（批处理）**:
```powershell
# 使用批处理脚本（如果存在）
.\run_fx_simple2_test.bat
```

**一键运行 GUI 程序**:
```powershell
# 在项目根目录运行
cd e:\Soft\JetBrains\WebStorm WorkSpace\go-ts
.\runtime\go\bin\go.exe run run_tsx_gui.go
```

### 实际运行示例

**示例 1: 运行简单的 FX 组件测试**
```powershell
# 完整流程
cd e:\Soft\JetBrains\WebStorm WorkSpace\go-ts
.\runtime\go\bin\go.exe build -o gox.exe cmd/gox/main.go
Copy-Item gox.exe test\ -Force
cd test
.\gox.exe test_fx_assignment.gox
```

**示例 2: 运行完整的计数器应用**
```powershell
# 完整流程
cd e:\Soft\JetBrains\WebStorm WorkSpace\go-ts
.\runtime\go\bin\go.exe build -o gox.exe cmd/gox/main.go
Copy-Item gox.exe test\ -Force
cd test
.\gox.exe -o demo_counter.go demo_counter.gox
..\runtime\go\bin\go.exe build demo_counter.go
.\demo_counter.exe
```

**示例 3: 查看生成的代码**
```powershell
# 只生成代码，不运行
cd e:\Soft\JetBrains\WebStorm WorkSpace\go-ts\test
.\gox.exe -o output.go demo_counter.gox
Get-Content output.go
```

### GUI 测试用例运行示例

**运行 TSX GUI 演示**:
```powershell
# 方式 1: 自动运行（推荐）
cd e:\Soft\JetBrains\WebStorm WorkSpace\go-ts
.\runtime\go\bin\go.exe run run_tsx_gui.go

# 方式 2: 手动运行
cd test
..\gox.exe -o tsx_gui_demo.go tsx_gui_demo.gox
..\runtime\go\bin\go.exe build tsx_gui_demo.go
.\tsx_gui_demo.exe
```

**运行 FX 组件示例**:
```powershell
# 编译 FX 组件
cd test
..\gox.exe -o demo_counter.go demo_counter.gox

# 编译并运行（需要 GUI 库支持）
..\runtime\go\bin\go.exe build demo_counter.go
.\demo_counter.exe
```

### 常见问题

**Q: 找不到 go.exe？**
A: 确保 Go 运行环境在 `.\runtime\go\bin\` 目录下，或者使用系统安装的 Go：
```powershell
go build -o gox.exe cmd/gox/main.go
```

**Q: GUI 程序无法启动？**
A: 检查是否安装了 GUI 依赖（gg, glfw 等），并确保在 Windows 环境下运行。

**Q: 如何调试 Parser 错误？**
A: 运行 gox 时会输出详细的 token 序列和 AST 信息：
```powershell
.\gox.exe test\your_test.gox 2>&1 | Select-String -Pattern "Parser Errors" -Context 5
```

## 文件组织准则

### 文件长度限制

**规则**: 如果单个源文件超过 800 行，应该考虑将文件拆分为多个模块。

**拆分策略**:

1. 按功能模块拆分 - 将相关功能组织到独立文件
2. 按类型拆分 - 不同类型的代码（如 parser、transformer、lexer）应分离
3. 按职责拆分 - 单一职责原则，每个文件专注于一个功能领域

### 代码组织最佳实践

1. **单一职责**: 每个文件应该有明确的单一职责
2. **清晰命名**: 文件名应该清晰反映其内容
3. **合理分组**: 相关功能应该组织在一起
4. **依赖管理**: 减少循环依赖，保持清晰的依赖层次

## 语法规范

## 测试准则

### 测试用例设计原则

**重要**: 测试用例不应该为了适应当前实现而被简化！

1. **测试驱动开发**: 测试用例应该反映期望的完整功能，而不是当前实现
2. **不妥协原则**: 如果测试失败，应该完善实现（parser/transformer），而不是简化测试
3. **完整性**: 测试应该覆盖各种边界情况和复杂场景
4. **真实性**: 测试用例应该反映真实的用户使用场景

### 示例

❌ **错误做法**:

- 因为 parser 不支持 `{id: "app"}` 简写，就改成 `ViewProps{id: "app"}`
- 因为闭包解析有问题，就只用箭头函数

✅ **正确做法**:

- 完善 parser 支持结构体字面量简写
- 修复闭包解析，支持 `func` 关键字

## 测试覆盖

所有核心功能都应该有对应的测试用例：

- Parser 测试
- Transformer 测试
- 集成测试

## 代码质量

1. 保持函数简洁（建议 < 50 行）
2. 使用有意义的变量名
3. 添加必要的注释
4. 遵循 Go 语言规范

## 代码提交

1、在每次对话末尾应该把本次修改提交到git仓库
