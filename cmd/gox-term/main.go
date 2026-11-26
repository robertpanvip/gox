// gox-term —— 终端里运行的 macOS 风格终端面板。
//
// 命令行启动即进入一个真实 shell（默认 $SHELL），带红黄绿点缀、圆角边框、
// 多会话标签。键盘输入转发到 PTY、shell 输出渲染回界面——执行的是真命令，
// 不是模拟。视觉用 lipgloss 实现苹果风格，与 gox-ide 同源配色。
package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/robertpanvip/gox-term/internal/app"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	var (
		themeFlag string
		shellFlag string
		showVer   bool
	)
	flag.StringVar(&themeFlag, "theme", "dark", "主题：dark 或 light")
	flag.StringVar(&shellFlag, "shell", "", "指定 shell（默认 $SHELL，缺省 /bin/sh）")
	flag.BoolVar(&showVer, "version", false, "显示版本")
	flag.Parse()

	if showVer {
		fmt.Printf("gox-term %s (%s)\n", version, commit)
		return
	}

	dark := themeFlag != "light"
	m := app.New(dark, shellFlag)
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseAllMotion())
	m.BindProgram(p)

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gox-term 启动失败:", err)
		os.Exit(1)
	}
}
