package skia

// Skia 后端的原生文件对话框实现。
//
// 它实现 widgets.FileDialog 契约，由装配层注册到 widgets.RegisterFileDialog。
// 采用调用平台原生对话框工具（而非把对话框画进场景树）——这与 Window 的
// "平台能力归后端" 分层一致：GX 核心与组件库零平台依赖，能力各自实现。
//
// 支持：
//   - Linux: zenity --file-selection / kdialog（按需探测）
//   - macOS: osascript（AppleScript 系统对话框）
//   - Windows: PowerShell 调 .NET OpenFileDialog
//
// 任何环节失败都返回 error，由 FilePicker 组件的 onCancel 路径处理，绝不 panic。
import (
	"os/exec"
	"runtime"
	"strings"

	"github.com/robertpanvip/gox-core/widgets"
)

// NativeFileDialog 是 Skia 后端的 FileDialog 实现。
type NativeFileDialog struct{}

var _ widgets.FileDialog = (*NativeFileDialog)(nil)

// NewNativeFileDialog 返回本后端的原生对话框实现。
func NewNativeFileDialog() *NativeFileDialog { return &NativeFileDialog{} }

// RegisterFileDialog 把本后端实现注册到组件库（装配层在启动时调用一次）。
func RegisterFileDialog() { widgets.RegisterFileDialog(&NativeFileDialog{}) }

func (d *NativeFileDialog) OpenFile(opts widgets.FileOptions) ([]string, error) {
	switch runtime.GOOS {
	case "linux":
		return openLinux(opts)
	case "darwin":
		return openDarwin(opts)
	case "windows":
		return openWindows(opts)
	default:
		return nil, &exec.Error{Name: "filedialog", Err: exec.ErrNotFound}
	}
}

func openLinux(opts widgets.FileOptions) ([]string, error) {
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
	if opts.DefaultPath != "" {
		args = append(args, "--filename="+opts.DefaultPath)
	}
	for _, f := range opts.Filters {
		args = append(args, "--file-filter=*"+f)
	}
	out, err := exec.Command("zenity", args...).Output()
	if err != nil {
		// 回退 kdialog
		return openKDialog(opts)
	}
	return splitPaths(string(out)), nil
}

func openKDialog(opts widgets.FileOptions) ([]string, error) {
	args := []string{"--getopenfilename"}
	if opts.Directory {
		args = []string{"--getexistingdirectory"}
	}
	start := opts.DefaultPath
	if start == "" {
		start = "/"
	}
	args = append(args, start)
	if opts.Title != "" {
		args = append(args, "--title", opts.Title)
	}
	out, err := exec.Command("kdialog", args...).Output()
	if err != nil {
		return nil, err
	}
	return splitPaths(string(out)), nil
}

func openDarwin(opts widgets.FileOptions) ([]string, error) {
	// AppleScript：tell application "System Events" → choose file / folder。
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
	return splitPaths(string(out)), nil
}

func openWindows(opts widgets.FileOptions) ([]string, error) {
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
	return splitPaths(string(out)), nil
}

// splitPaths 按行拆分对话框输出，去空白与 AppleScript 的 ", " 分隔。
func splitPaths(s string) []string {
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
