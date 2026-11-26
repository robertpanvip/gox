# gox-ide

终端里全屏运行的 **macOS 风格 IDE**。命令行一键启动，左侧文件树、顶部多标签、语法高亮编辑、搜索、快速打开、明暗双主题——观感走苹果的柔和配色、圆角边框、红黄绿窗口点缀，而不是传统终端的朴素黑白。

文本引擎**直接复用 gox 的 Rope**（与 playground 编辑器同源，大文件编辑/行列换算 O(log N)）；视觉层用 lipgloss 实现，UI 代码独立在 `cmd/gox-ide` 子 module，**不把 bubbletea 依赖带进 gox 主库**。

## 运行

```bash
# 从仓库根目录
go run ./cmd/gox-ide                          # 打开当前目录，深色主题
go run ./cmd/gox-ide --path /path/to/project  # 指定项目目录
go run ./cmd/gox-ide --path main.go           # 直接打开文件
go run ./cmd/gox-ide --theme light            # 浅色主题
go run ./cmd/gox-ide --version

# 或构建成单个二进制
go build -o gox-ide ./cmd/gox-ide
./gox-ide --path .
```

> **终端要求**：建议使用支持 256 色/真彩的终端（iTerm2、macOS Terminal、Windows Terminal、Alacritty、Kitty 等），
> 圆角、渐变与语法色的观感最佳。某些仅 16 色的终端（如默认 TERM 的 xterm）下，浅色主题的边框角可能出现颜色降级，
> 用 `TERM=xterm-256color` 即可正常。

## 快捷键

| 按键 | 功能 |
|---|---|
| `Ctrl+P` | 快速打开文件（按文件名子串过滤） |
| `Ctrl+F` | 在当前文件搜索（列出命中行，回车跳转） |
| `Ctrl+T` | 切换深色/浅色主题 |
| `Ctrl+S` | 保存当前文件 |
| `Ctrl+W` | 关闭当前标签 |
| `Ctrl+Tab` | 切换到下一个标签 |
| `Tab` | 在文件树 / 编辑区之间切换焦点 |
| `↑ ↓ ← →` | 移动光标（文件树中展开/折叠与选择） |
| `Home / End` | 跳到行首 / 行尾 |
| `PgUp / PgDn` | 上下翻页 |
| `Ctrl+← / →` | 按词移动 |
| `↑ ↓`（补全时） | 在候选间移动 |
| `Tab / Enter`（补全时） | 采纳当前候选（替换正在输入的 token） |
| `Esc` | 关闭补全 / 退出浮层 / 文件树焦点时回到编辑区 / 退出程序 |

## 代码补全 ✨

在编辑区输入标识符时**光标旁弹出候选**，无需记 API：

- **类型感知成员补全**（`obj.` / `pkg.`）：输入点号即弹成员，走真实 **go/types** —— 结构体字段、方法集、包导出符号，详情显示完整函数签名。例如 `b := strings.NewReplacer()` 后输 `b.R` 弹 `Replace · func(s string) string`。占位符注入 + 持久 Importer 缓存类型图，空前缀（`obj.`）也触发。
- **GX/Go 关键字与片段**：`sig`/`func`/`view`/`for`… 及常用代码块。
- **本文件 / 项目符号**：从当前文档与项目源码里抽取 `func`/`type`/`sig`/`var` 等声明。
- **LSP 代码符号**：PATH 里有 `gopls`/`gox-lsp` 时异步拉取 `workspace/symbol`，装了全局语言服务器就自动多一路候选（没装则静默降级）。

五类来源走可插拔 Provider，统一打分（前缀 > 子串 > 子序列）、去重、限流。点号上下文**只走成员补全**，避免关键字/符号噪声淹没成员；其余上下文跳过成员 provider。成员/LSP 异步预热，`Provide` 永不阻塞 UI。采纳时按字符数回退当前 token 再插入候选，光标与 Rope 文本保持同步。

> 成员补全由主仓库公共包 `pkg/gxcomplete` 提供（gox-ide 是独立 module，无法 import `internal/typecheck`，故经此门面复用），依赖 GOROOT 源码；无 GOROOT 时静默降级到符号补全。

## 架构

```
cmd/gox-ide/                  独立 Go module（replace gox => ../..）
├── main.go                   CLI 入口（flag 解析、tea.NewProgram）
├── internal/
│   ├── theme/theme.go        macOS 双主题色板与全部 lipgloss 样式（构建时一次成型）
│   ├── syntax/highlight.go   轻量语法分词（移植自 widgets/editor.go，语义 token 名同源）
│   ├── editor/buffer.go      文本缓冲（复用 gox/runtime/rope 作为文本权威）
│   ├── complete/             代码补全引擎（纯标准库，可插拔 Provider）
│   │   ├── complete.go       引擎：打分/去重/排序、token 提取
│   │   ├── keyword.go        GX/Go 关键字与片段
│   │   ├── buffer.go         本文件符号
│   │   ├── project.go        项目符号扫描（带缓存）
│   │   ├── symbols.go        声明符号抽取
│   │   ├── member.go         类型感知点号成员补全（经 gox/pkg/gxcomplete）
│   │   └── lsp.go            可选 LSP workspace/symbol（异步预热）
│   └── app/                  bubbletea 主 Model
│       ├── model.go          状态、Update、全局快捷键
│       ├── filetree.go       可折叠文件树
│       ├── overlay.go        搜索 / 快速打开浮层逻辑
│       ├── completion.go     补全触发/采纳状态机
│       └── view.go           分层渲染（标题栏/主体/状态栏/浮层/补全框）
```

### 设计取舍

- **复用与隔离**：Rope 与分词算法复用 gox 的既有实现，保证编辑手感与大文件性能一致；但 UI/主题层独立，避免 `widgets` 包把 skia/cgo 拖进终端程序。
- **零重复分配**：主题所有 `lipgloss.Style` 在 `theme.New` 时一次构建并缓存；渲染热路径只做 `.Render`。
- **虚拟滚动**：编辑区只渲染视口内可见行，20000 行文件首屏与滚动开销恒定。
- **视觉分层**：标题栏（红黄绿点 + 标签）、主体（文件树 │ 竖线 │ 代码）、状态栏分层渲染，浮层覆盖在最上层。

## 测试

```bash
go test ./cmd/gox-ide/...
```

覆盖高亮"拼回原文不变"契约、关键字/字符串/注释着色、Rope 插入删除/跨行退格/中文光标、20000 行定位等；补全引擎的打分/去重/排序、token 与成员前缀提取、符号抽取、关键字过滤；补全市集的 token 替换、选中环绕、状态清理等。

类型感知成员补全的核心断言在 `gox/pkg/gxcomplete`（空/非空前缀、局部变量、GX sig 声明、非点号回退、CJK 行列换算），gox-ide 侧另有真实 go/types 集成断言、点号分流与 Tab 采纳（补全打开时 Tab 优先采纳而非切焦点）测试。

实测（xvfb + xterm）：打开含 `import "strings"` 的 GX 文件，输入 `b.R` 弹出 `Replace`/`WriteString` 两个 Replacer 成员（含完整函数签名），Tab 采纳后成为 `b.Replace`，浮层关闭、无重复插入。
