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
- **完整的终端屏幕模拟器**（`internal/screen`）：这是支持 vim/htop 的关键。维护一块二维字符网格（每格有字符 + 前景/背景色 + 属性），而非简单地把输出逐行追加。正确处理：
  - **光标寻址**（`ESC[row;colH`）、局部擦除（`ESC[K`/`ESC[J`）、插入/删除行列、滚动区
  - **交替屏幕**（`?1049`）——vim/htop 退出后能干净恢复进入前的画面
  - **SGR 颜色**（8/16 色、256 色、24-bit 真彩）、反显、粗体、下划线等属性
  - **UTF-8 / CJK 宽字符**、延迟换行、滚动历史
  - 因此光标移到屏幕中间"擦掉重写"能正确还原，不再乱套。
- **会话隔离**：每个标签一个独立 PTY + 屏幕模拟器，输出与历史互不串扰。
- **独立 module**：bubbletea / lipgloss / creack/pty 依赖留在 `cmd/gox-term` 子模块，不污染 gox 主库依赖树。

## 测试

```bash
go test ./cmd/gox-term/...
```

- `internal/screen`：光标寻址、局部擦除、滚动与历史、SGR 颜色、反显、交替屏幕、CJK 宽字符、vim 典型会话序列、CJK 回滚等单测。
- `internal/app`：可见宽度（含 CJK）、ANSI 安全截断、按键编码等。

实测（xvfb + xterm）：`echo`/`ls`/`git`/ANSI 彩色输出、vim 全屏编辑并保存退出、htop 实时刷新与功能键栏、交替屏幕退出恢复、明暗双主题渲染均正常。
