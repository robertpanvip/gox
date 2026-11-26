// Package clipboard 提供终端剪贴板访问：OSC 52 序列 + 本地剪贴板工具回退。
//
// OSC 52 是终端标准扩展（xterm/iTerm2/Windows Terminal/Kitty 等广泛支持），
// 通过一段控制序列把文本写进“操作系统剪贴板”，无需任何外部命令。
// 在本程序里，OSC52 序列由 UI 层在渲染循环外直接写到 stdout——它不占屏幕格子，
// 不会被 bubbletea 的下一帧重绘覆盖。
package clipboard

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
)

// OSC52 生成把 text 写入系统剪贴板的 OSC 52 序列（BEL 结尾）。
func OSC52(text string) string {
	enc := base64.StdEncoding.EncodeToString([]byte(text))
	return "\x1b]52;c;" + enc + "\x07"
}

// Available 返回本机可用的本地剪贴板写入方式："pbcopy"/"xclip"/"xsel"/"wl-copy"，
// 都没有则返回空串（此时只能靠 OSC 52）。
func Available() string {
	for _, c := range []struct {
		bin  string
		args []string
	}{
		{"pbcopy", nil},
		{"wl-copy", nil},
		{"xclip", []string{"-selection", "clipboard"}},
		{"xsel", []string{"--clipboard", "--input"}},
	} {
		if _, err := exec.LookPath(c.bin); err == nil {
			return c.bin
		}
	}
	return ""
}

// WriteLocal 用本地剪贴板工具写入 text（best-effort）。返回使用的工具名，
// 失败返回空串。失败时调用方仍可回退到 OSC 52。
func WriteLocal(text string) string {
	for _, c := range []struct {
		bin  string
		args []string
	}{
		{"pbcopy", nil},
		{"wl-copy", nil},
		{"xclip", []string{"-selection", "clipboard"}},
		{"xsel", []string{"--clipboard", "--input"}},
	} {
		path, err := exec.LookPath(c.bin)
		if err != nil {
			continue
		}
		cmd := exec.Command(path, c.args...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return c.bin
		}
	}
	return ""
}

// ReadLocal 从本地剪贴板工具读取文本（best-effort），失败返回空串。
func ReadLocal() string {
	for _, c := range []struct {
		bin  string
		args []string
	}{
		{"pbpaste", nil},
		{"wl-paste", []string{"--no-newline"}},
		{"xclip", []string{"-selection", "clipboard", "-o"}},
		{"xsel", []string{"--clipboard", "--output"}},
	} {
		path, err := exec.LookPath(c.bin)
		if err != nil {
			continue
		}
		out, err := exec.Command(path, c.args...).Output()
		if err == nil {
			return string(out)
		}
	}
	return ""
}

// WriteOSC52 把 OSC 52 序列写到指定 writer（通常是 os.Stdout）。
func WriteOSC52(w interface{ Write([]byte) (int, error) }, text string) error {
	_, err := fmt.Fprint(w, OSC52(text))
	return err
}
