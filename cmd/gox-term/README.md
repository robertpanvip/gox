# gox-term

终端里运行的 **macOS 风格终端面板**。命令行一键启动，直接进入一个真实 shell（`$SHELL`，可指定），带红黄绿窗口点缀、圆角边框、多会话标签——执行的是真命令，不是模拟。视觉配色与 [gox-ide](../gox-ide/README.md) 同源。

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

## 快捷键

| 按键 | 功能 |
|---|---|
| `Ctrl+T` | 新建 shell 会话（每个标签独立 shell） |
| `Ctrl+N` | 切换到下一个会话 |
| `Ctrl+W` | 关闭当前会话 |
| `Ctrl+Shift+↑ / ↓` | 向上/下滚动输出 |
| 普通按键 | 直接转发给 shell（命令、Tab 补全、Ctrl+C 中断、方向键历史等全部可用） |

> `Ctrl+Tab` 也可用作切换，但 xterm 等终端会把它退化成普通 Tab，故推荐 `Ctrl+N`。

## 实现要点

- **真 PTY**：`creack/pty` 启动 shell，`$TERM=xterm-256color`；键盘字节写入 PTY，输出经 `tea.Program.Send` 回投 UI，窗口尺寸变化时 `Setsize` 同步（保证 `ls` 分列正确）。
- **ANSI 保留**：PTY 输出里的 SGR 颜色序列（红/绿/蓝、粗体等）保留并渲染；光标移动、清屏、OSC 标题等控制序列做简化处理（`ansi.go`）。对 `echo`/`ls --color`/`git`/`pwd` 等常规命令效果完整；全屏 TUI 程序（vim 等）的复杂重绘是已知边界。
- **会话隔离**：每个标签一个独立 PTY + 进程，输出缓冲独立；切换标签互不串扰。
- **独立 module**：bubbletea / lipgloss / creack/pty 依赖留在 `cmd/gox-term` 子模块，不污染 gox 主库依赖树。

## 测试

```bash
go test ./cmd/gox-term/...
```

覆盖 ANSI 序列解析（SGR 保留、光标/清屏/OSC 丢弃、退格）、可见宽度（含 CJK）、ANSI 安全截断、按键编码等。
