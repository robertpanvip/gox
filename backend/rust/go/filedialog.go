package rustbackend

// Rust 后端的原生文件对话框实现。
//
// Rust 渲染栈（winit/wgpu）负责窗口与绘制，文件对话框属于平台能力，走与 Skia
// 后端相同的"平台原生工具"策略，但实现放在各自包里——两端互不 import，符合
// "Window/能力各自实现" 的分层约定。装配层注册到 widgets.RegisterFileDialog。
//
// 若要用纯 Rust 对话框（rfd crate），可在 rust/ 侧新增 gx_file_dialog C ABI，
// 并在本文件的 OpenFile 中改调 cFileDialog；接口形态不变。
import (
	"os/exec"
	"runtime"
	"strings"

	"github.com/robertpanvip/gox/widgets"
)

// NativeFileDialog 是 Rust 后端的 FileDialog 实现。
type NativeFileDialog struct{}

var _ widgets.FileDialog = (*NativeFileDialog)(nil)

func NewNativeFileDialog() *NativeFileDialog { return &NativeFileDialog{} }

// RegisterFileDialog 把本后端实现注册到组件库。
func RegisterFileDialog() { widgets.RegisterFileDialog(&NativeFileDialog{}) }

func (d *NativeFileDialog) OpenFile(opts widgets.FileOptions) ([]string, error) {
	switch runtime.GOOS {
	case "linux":
		return openWithZenity(opts)
	case "darwin":
		return openWithOsascript(opts)
	case "windows":
		return openWithPowershell(opts)
	default:
		return nil, &exec.Error{Name: "filedialog", Err: exec.ErrNotFound}
	}
}

func openWithZenity(opts widgets.FileOptions) ([]string, error) {
	args := []string{"--file-selection"}
	if opts.Multiple {
		args = append(args, "--multiple", "--separator=\n")
	}
	if opts.Directory {
		args = append(args, "--directory")
	}
	if opts.Title != "" {
		args = append(args, "--title="+opts.Title)
	}
	for _, f := range opts.Filters {
		args = append(args, "--file-filter=*"+f)
	}
	out, err := exec.Command("zenity", args...).Output()
	if err != nil {
		return nil, err
	}
	return splitDialogPaths(string(out)), nil
}

func openWithOsascript(opts widgets.FileOptions) ([]string, error) {
	many := "false"
	if opts.Multiple {
		many = "true"
	}
	kind := "choose file"
	if opts.Directory {
		kind = "choose folder"
	}
	script := "tell application \"System Events\" to " + kind + " with multiple selections allowed " + many
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return nil, err
	}
	return splitDialogPaths(string(out)), nil
}

func openWithPowershell(opts widgets.FileOptions) ([]string, error) {
	multi := "$false"
	if opts.Multiple {
		multi = "$true"
	}
	filter := "All Files|*.*"
	if len(opts.Filters) > 0 {
		var pats []string
		for _, f := range opts.Filters {
			pats = append(pats, "*"+f)
		}
		filter = "Supported|" + strings.Join(pats, ";")
	}
	ps := "Add-Type -AssemblyName System.Windows.Forms; " +
		"$d = New-Object System.Windows.Forms.OpenFileDialog; " +
		"$d.Multiselect = " + multi + "; " +
		"$d.Filter = '" + filter + "'; " +
		"if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { $d.FileNames }"
	out, err := exec.Command("powershell", "-NoProfile", "-Command", ps).Output()
	if err != nil {
		return nil, err
	}
	return splitDialogPaths(string(out)), nil
}

func splitDialogPaths(s string) []string {
	s = strings.ReplaceAll(s, ", ", "\n")
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
