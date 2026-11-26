# gox-term

终端里运行的 **macOS 风格终端面板**。命令行一键启动，直接进入一个真实 shell（`$SHELL`，可指定），带红黄绿窗口点缀、圆角边框、多会话标签——执行的是真命令，不是模拟。视觉配色与 [gox-tui](../gox-tui/README.md) 同源。

不同于把终端嵌进 IDE，这是一个**独立的小程序**：只做一件事——在一个好看的苹果风格窗口里跑 shell。

## 运行

```bash
go run ./cmd/gox-term                          # 默认 $SHELL，深色
go run ./cmd/gox-term --theme light            # 浅色
go run ./cmd/gox-term --shell /bin/bash        # 指定 shell
go run ./cmd/gox-term --version

# 或构建成单个二进制
go build -o gox-term ./cmd/gox-term
./gox-term
```

> **终端要求**：建议 256 色/真彩终端（iTerm2、macOS Terminal、Windows Terminal、Kitty 等）。
> 16 色终端下浅色主题的边框角可能颜色降级，`TERM=xterm-256color` 即可。

## 智能补全 ✨

输入命令时**边打字边弹候选**——像 IDE 的 LSP 补全那样，输入前缀即弹出，上下键或鼠标点选、回车/Tab 采纳，不用死记命令。

```
$ git st
┌ 补全 1/3   ↑↓ 选择 · Tab/Enter 采纳 · Esc 关闭 ┐
│ ◆ status   git · 查看工作区状态  │   ← 光标选中
│ ◆ stash    git · 暂存改动        │
│ ◆ reset    git · 重置            │
└──────────────────────────────────┘
```

**四类互补的补全来源**（可插拔 Provider）：

| 来源 | 装了什么 | 弹什么 |
|---|---|---|
| 上下文子命令 | 内置（git / go / docker / npm / kubectl / cargo） | `git st` → `status`/`stash`；`go te` → `test`/`vet` |
| 全局命令 PATH | 系统/全局装了任意 CLI | 扫 `$PATH` 可执行文件，`g` → `git`/`go`/`grep` |
| LSP 代码符号 | PATH 里有 `gopls` / `gox-lsp` | 项目里的函数、类型、方法（异步预热，没装则静默降级） |
| 项目内符号 + 文件 | 打开一个项目即可 | 当前目录的文件名、目录、源码里声明的符号 |
| 关键字/片段 | 内置 | `func`/`for` 等 GX 关键字与常用命令片段 |

设计上补全**不接管 shell 的行编辑**：按键照常进 PTY、shell 负责回显与历史；引擎从 VT 屏幕网格读出当前光标行做查询，采纳时回发等长退格 + 候选文本，行编辑始终由 shell 掌控。因此 **vim/htop 等全屏 TUI（交替屏幕）下补全自动禁用**，绝不会糊到 TUI 上。

## 复制与粘贴

- **复制**：在终端区**按住鼠标左键拖拽**选中文本，松开即复制到系统剪贴板（反显高亮选区）。双通道写入：本地剪贴板工具（`pbcopy`/`wl-copy`/`xclip`/`xsel`，供其它 App 用）+ OSC 52（供终端自身用），不依赖任何外部工具也能工作。
- **粘贴**：系统原生粘贴（`Cmd+V` / `Ctrl+Shift+V`，由终端发 bracketed-paste）会把整段文本一次性写入 shell，vim 里也不会逐字符执行。若终端不支持 bracketed paste，可用 `Alt+V` 从本地剪贴板读取粘贴。

> 多行复制/粘贴按流式语义处理（起点行从锚点、终点行到光标），CJK 宽字符正确对齐，行尾空白自动去除。

## 鼠标操作

| 操作 | 功能 |
|---|---|
| 左键拖拽终端区 | 选中文本，松开复制到剪贴板 |
| 点击标签 | 切换会话；点 `+` 新建会话 |
| 点击补全候选 | 直接采纳该候选并填入命令行 |
| 滚轮 ↑ / ↓ | 回滚 / 回到实时输出 |
| `Alt+V` | 从本地剪贴板粘贴 |

## 快捷键

| 按键 | 功能 |
|---|---|
| `Ctrl+T` | 新建 shell 会话（每个标签独立 shell） |
| `Ctrl+N` | 切换到下一个会话 |
| `Ctrl+W` | 关闭当前会话 |
| `Ctrl+Shift+↑ / ↓` | 向上/下滚动输出 |
| 补全时 `↑ / ↓` | 在候选间移动 |
| 补全时 `Tab / Enter / Ctrl+Y` | 采纳当前候选 |
| 补全时 `1`–`9` | 数字快选对应候选 |
| 补全时 `Esc` | 关闭补全浮层 |
| `Alt+V` | 从剪贴板粘贴（bracketed-paste 整体写入） |
| 普通按键 | 直接转发给 shell（命令、Ctrl+C 中断、方向键历史等全部可用） |

> `Ctrl+Tab` 也可用作切换，但 xterm 等终端会把它退化成普通 Tab，故推荐 `Ctrl+N`。

## 实现要点

- **真 PTY**：`creack/pty` 启动 shell，`$TERM=xterm-256color`；键盘字节写入 PTY，输出经 `tea.Program.Send` 回投 UI，窗口尺寸变化时 `Setsize` 同步（保证 `ls` 分列正确）。
- **完整的终端屏幕模拟器**（`internal/screen`）：这是支持 vim/htop 的关键。维护一块二维字符网格（每格有字符 + 前景/背景色 + 属性），而非简单地把输出逐行追加。正确处理：
  - **光标寻址**（`ESC[row;colH`）、局部擦除（`ESC[K`/`ESC[J`）、插入/删除行列、滚动区
  - **交替屏幕**（`?1049`）——vim/htop 退出后能干净恢复进入前的画面
  - **SGR 颜色**（8/16 色、256 色、24-bit 真彩）、反显、粗体、下划线等属性
  - **UTF-8 / CJK 宽字符**、延迟换行、滚动历史
  - 因此光标移到屏幕中间"擦掉重写"能正确还原，不再乱套。
- **会话隔离**：每个标签一个独立 PTY + 屏幕模拟器，输出与历史互不串扰。
- **旁路补全引擎**（`internal/complete`）：纯标准库、可插拔 Provider 架构。统一打分（前缀 > 子串 > 子序列）、去重、排序、限流；PATH 与项目符号带磁盘缓存，LSP 走异步预热 + 内存缓存，`Provide` 永不阻塞 UI。上下文子命令（`git st` → `status`）命中时自动屏蔽同前缀的系统命令噪声。
- **补全与 TUI 共存**：浮层只在常规屏幕且未回滚时渲染；`Screen.LineLeftOfCursor()` 提供光标左侧文本作查询；采纳时按字符数回发退格，与 shell readline 保持同步。
- **文本选区与剪贴板**（`internal/clipboard` + `screen.selection`）：鼠标拖拽在 VT 网格上形成选区，渲染层用反显高亮；`SelectedText()` 按流式语义提取（跳过 CJK 占位格、去行尾空白）。复制走"本地工具 + OSC 52"双写；粘贴复用 bubbletea 的 bracketed paste，`wrapBracketedPaste` 整体包裹避免 shell 逐字符处理。
- **独立 module**：bubbletea / lipgloss / creack/pty 依赖留在 `cmd/gox-term` 子模块，不污染 gox 主库依赖树。

## 测试

```bash
go test ./cmd/gox-term/...
```

- `internal/screen`：光标寻址、局部擦除、滚动与历史、SGR 颜色、反显、交替屏幕、CJK 宽字符、vim 典型会话序列、CJK 回滚、`LineLeftOfCursor` 等单测。
- `internal/complete`：打分/去重/排序、空查询、`TokenBefore`/`BeforeTokens`、关键字与项目符号抽取、上下文子命令等单测。
- `internal/app`：布局单行、标签命中、浮层整行宽度（含 CJK）、选中环绕、鼠标点选候选/标签/滚轮、拖拽选区坐标、bracketed-paste 包裹与粘贴消息等单测。
- `internal/clipboard`：OSC 52 序列格式与 base64 编解码、写出等单测。

实测（xvfb + xterm）：`echo`/`ls`/`git`/ANSI 彩色输出、vim 全屏编辑并保存退出、htop 实时刷新与功能键栏、交替屏幕退出恢复、明暗双主题渲染均正常；`git`/`git st` 补全浮层弹出、Tab 采纳退格对齐正确、vim 插入模式下不误弹补全、鼠标拖拽选区复制到 xclip 剪贴板、bracketed-paste 整体粘贴。
