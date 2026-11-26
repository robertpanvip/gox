// gox-tui —— 终端里全屏运行的 macOS 风格 IDE。
//
// 命令行一键启动，左侧文件树 + 顶部多标签 + 语法高亮编辑 + 搜索 + 明暗主题。
// 文本引擎复用 gox 的 Rope（大文件 O(log N) 编辑），视觉用 lipgloss 实现
// 圆角、柔和配色、红黄绿窗口点缀的苹果风格。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/robertpanvip/gox-tui/internal/app"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	var (
		themeFlag string
		pathFlag  string
		showVer   bool
	)
	flag.StringVar(&themeFlag, "theme", "dark", "主题：dark 或 light")
	flag.StringVar(&pathFlag, "path", "", "起始目录或文件（默认当前目录）")
	flag.BoolVar(&showVer, "version", false, "显示版本")
	flag.Parse()

	if showVer {
		fmt.Printf("gox-tui %s (%s)\n", version, commit)
		return
	}

	// 无 TERM（管道化 pty、极简环境）时 lipgloss 会降级到 ASCII——全终端
	// 无色彩、无边框、近似黑屏。注入安全默认值，仅影响本进程；真实终端里
	// xterm-256color 与 lipgloss 的颜色探测行为一致，不改变既有渲染。
	if os.Getenv("TERM") == "" {
		_ = os.Setenv("TERM", "xterm-256color")
	}

	dark := strings.ToLower(themeFlag) != "light"

	// 解析起始路径：文件则打开它并以其目录为根；目录则作为项目根。
	root := pathFlag
	var files []string
	if pathFlag == "" {
		if wd, err := os.Getwd(); err == nil {
			root = wd
		}
	} else if info, err := os.Stat(pathFlag); err == nil && !info.IsDir() {
		abs, _ := filepath.Abs(pathFlag)
		root = filepath.Dir(abs)
		files = append(files, abs)
	} else if abs, err := filepath.Abs(pathFlag); err == nil {
		root = abs
	}

	m := app.New(root, dark, files)
	p := tea.NewProgram(m,
		tea.WithAltScreen(),      // 全屏备用缓冲区，退出后还原终端
		tea.WithMouseAllMotion(), // 全鼠标移动：Ctrl+ hovering 单词即可显示下划线
	)
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "gox-tui 启动失败:", err)
		os.Exit(1)
	}
}
