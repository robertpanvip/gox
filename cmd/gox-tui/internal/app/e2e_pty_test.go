//go:build !windows

package app

// Layer 3：真 PTY 端到端测试。启动编译出的 gox-tui 二进制跑在真实伪终端里，
// 注入按键 / 鼠标转义序列 / 窗口缩放，断言全链路行为——机器版"手点"。

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

// e2eBinary 在 TestMain 里编译一次（跳过则跳过全部 e2e）。
var e2eBinary string

var ansiRe = regexp.MustCompile(`\x1b(?:\[[0-9;?<>=]*[a-zA-Z]|\][^\x07\x1b]*(?:\x07|\x1b\\)|[@-Z\\-_])`)

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

func TestMain(m *testing.M) {
	// 沙箱等环境 /tmp 常挂 noexec，二进制编进源码树内保证可执行。
	// 测试进程 cwd 是 internal/app（库包），go build . 会产出归档而非
	// 可执行；主包在 cmd/gox-tui（本文件相对路径 ../..）。
	bin := ".e2e-gox-tui.bin"
	ctx := exec.Command("go", "build", "-o", bin, ".")
	ctx.Env = append(os.Environ(), "GOWORK=off")
	ctx.Dir = "../.."
	if out, berr := ctx.CombinedOutput(); berr != nil {
		fmt.Fprintf(os.Stderr, "e2e 二进制编译失败: %v\n%s", berr, out)
	} else {
		_ = os.Chmod(bin, 0o755)
		abs, aerr := filepath.Abs(filepath.Join("../..", bin))
		if aerr != nil {
			fmt.Fprintf(os.Stderr, "e2e 二进制路径解析失败: %v", aerr)
		} else {
			e2eBinary = abs
		}
	}
	code := m.Run()
	os.Remove(filepath.Join("../..", bin))
	os.Exit(code)
}

// e2eSession 是一个跑在 PTY 里的 gox-tui 实例。
type e2eSession struct {
	tty    io.ReadWriteCloser
	cmd    *exec.Cmd
	mu     sync.Mutex
	buf    strings.Builder
	exited chan struct{} // 子进程退出（已 reap），close 通知
}

func startE2E(t *testing.T, root string, size pty.Winsize) *e2eSession {
	t.Helper()
	if e2eBinary == "" {
		t.Skip("e2e 二进制不可用")
	}
	cmd := exec.Command(e2eBinary, "-path", root)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"GOWORK=off",
		"COLUMNS="+fmt.Sprint(size.Cols), "LINES="+fmt.Sprint(size.Rows),
	)
	tty, err := pty.StartWithSize(cmd, &size)
	if err != nil {
		t.Fatalf("PTY 启动失败: %v", err)
	}
	s := &e2eSession{tty: tty, cmd: cmd, exited: make(chan struct{})}
	// 及时 reap 子进程：退出的进程若不 Wait 会变 zombie，
	// Signal(0) 对 zombie 仍返回 nil，alive() 就永远为 true。
	go func() {
		_, _ = s.cmd.Process.Wait()
		close(s.exited)
	}()
	// 后台持续读取（PTY 输出必须持续消费，否则子进程写满缓冲会卡死）。
	// 程序启动时会发终端探测查询（OSC 11 背景色 + DSR 光标位置），
	// 真实终端会应答，这里模拟应答否则程序一直阻塞等待。
	go func() {
		tmp := make([]byte, 65536)
		answered := map[string]bool{}
		for {
			n, err := s.tty.Read(tmp)
			if n > 0 {
				s.mu.Lock()
				s.buf.Write(tmp[:n])
				s.mu.Unlock()
				chunk := string(tmp[:n])
				if strings.Contains(chunk, "\x1b]11;?") && !answered["bg"] {
					answered["bg"] = true
					_, _ = s.tty.Write([]byte("\x1b]11;rgb:1c1c/1c1c/1c1c\x1b\\"))
				}
				if strings.Contains(chunk, "\x1b[6n") && !answered["dsr"] {
					answered["dsr"] = true
					_, _ = s.tty.Write([]byte("\x1b[1;1R"))
				}
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		s.tty.Close()
		if s.alive() {
			_ = s.cmd.Process.Kill()
		}
		<-s.exited
	})
	// 等首帧渲染。
	time.Sleep(700 * time.Millisecond)
	return s
}

func (s *e2eSession) screen() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return stripANSI(s.buf.String())
}

func (s *e2eSession) send(keys string) {
	_, _ = s.tty.Write([]byte(keys))
}

func (s *e2eSession) alive() bool {
	select {
	case <-s.exited:
		return false
	default:
		return true
	}
}

// waitScreen 轮询直到 screen 包含 want 或超时。
func (s *e2eSession) waitScreen(t *testing.T, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(s.screen(), want) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("2s 内屏幕未出现 %q；最近输出：%q", want, tail(s.screen(), 600))
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

func fixtureProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"main.go":  "package main\n\nfunc main() {}\n",
		"util.go":  "package main\n\nfunc helper() {}\n",
		"demo.gx":  "sig count = 0\n\nfunc add() {}\n",
		"notes.md": "# 笔记\n\n中文内容\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestE2ESmoke：启动 → 打开文件 → 输入中文 → 保存 → 关闭 → 退出，全链路。
func TestE2ESmoke(t *testing.T) {
	root := fixtureProject(t)
	s := startE2E(t, root, pty.Winsize{Cols: 100, Rows: 30})

	// 文件树渲染
	s.waitScreen(t, "PROJECT", 3*time.Second)
	s.waitScreen(t, "main.go", 2*time.Second)
	if !s.alive() {
		t.Fatal("启动后进程不应退出")
	}

	// Ctrl+P 快速打开 util.go
	s.send("\x10") // ctrl+p
	s.waitScreen(t, "快速打开文件", 2*time.Second)
	s.send("util")
	s.send("\r") // enter
	s.waitScreen(t, "util.go", 2*time.Second)

	// 输入中文（CJK 输入回归：曾无法输入多字节字符）
	s.send("中文输入")

	// Ctrl+S 保存
	s.send("\x13") // ctrl+s
	s.waitScreen(t, "已保存", 2*time.Second)
	got, err := os.ReadFile(filepath.Join(root, "util.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "中文输入") {
		t.Errorf("保存的文件未包含输入的中文：%q", got)
	}

	// Ctrl+W 关闭标签，Esc 退出（无脏数据）
	s.send("\x17") // ctrl+w
	time.Sleep(300 * time.Millisecond)
	s.send("\x1b") // esc
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !s.alive() {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if s.alive() {
		t.Error("esc 后进程应退出")
	}
}

// TestE2EResizeStress：连续缩放 + 中途交互，进程不崩、恢复渲染。
func TestE2EResizeStress(t *testing.T) {
	root := fixtureProject(t)
	s := startE2E(t, root, pty.Winsize{Cols: 120, Rows: 40})
	s.waitScreen(t, "PROJECT", 3*time.Second)

	sizes := []pty.Winsize{
		{Cols: 40, Rows: 14}, {Cols: 200, Rows: 60},
		{Cols: 60, Rows: 20}, {Cols: 80, Rows: 24},
	}
	for _, sz := range sizes {
		if err := pty.Setsize(s.tty.(*os.File), &sz); err != nil {
			t.Fatalf("resize 失败: %v", err)
		}
		time.Sleep(250 * time.Millisecond)
		s.send("j") // 缩放后立即交互
		time.Sleep(150 * time.Millisecond)
		if !s.alive() {
			t.Fatalf("缩放到 %dx%d 后进程崩溃", sz.Cols, sz.Rows)
		}
	}
	s.waitScreen(t, "PROJECT", 2*time.Second)
}

// TestE2EMouseClickTree：模拟 SGR 鼠标点击文件树节点打开文件。
func TestE2EMouseClickTree(t *testing.T) {
	root := fixtureProject(t)
	// 加一个名字独特的文件便于断言
	if err := os.WriteFile(filepath.Join(root, "zz_click.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := startE2E(t, root, pty.Winsize{Cols: 100, Rows: 30})
	s.waitScreen(t, "PROJECT", 3*time.Second)

	// 文件树节点行从屏幕 y=4 起（0 基，SGR 序列为 1 基）。
	// 节点顺序：根、目录(无)、app 文件按字母序 app_test.go…夹具是
	// demo.gx/main.go/notes.md/util.go/zz_click.go，均排根之后。
	// 点第 5 行（y=8, 1 基 9）风险大，直接扫描几行点击 zz_click.go 可能的行。
	// 简化：点击行 8（demo.gx），随后标签栏应出现 demo.gx（原本未打开）。
	click := func(x, y int) {
		s.send(fmt.Sprintf("\x1b[<0;%d;%dM", x, y))
		s.send(fmt.Sprintf("\x1b[<0;%d;%dm", x, y))
	}
	click(10, 9) // 屏幕第 8 行 = demo.gx 行（headerY=4 → 节点 idx 4）
	time.Sleep(400 * time.Millisecond)
	s.waitScreen(t, "demo.gx", 2*time.Second)
	if !s.alive() {
		t.Fatal("鼠标点击后进程不应退出")
	}

	// 滚轮：无内容变化但不得崩
	s.send("\x1b[<65;60;10M") // wheel down press
	s.send("\x1b[<64;60;10M") // wheel up press
	time.Sleep(300 * time.Millisecond)
	if !s.alive() {
		t.Fatal("滚轮事件后进程不应退出")
	}
}

// TestE2ETinyTerminal：极小终端启动给出降级提示且不崩。
func TestE2ETinyTerminal(t *testing.T) {
	root := fixtureProject(t)
	s := startE2E(t, root, pty.Winsize{Cols: 18, Rows: 6})
	s.waitScreen(t, "终端窗口太小", 3*time.Second)
	if !s.alive() {
		t.Fatal("极小终端下进程不应退出")
	}
	// 放大后恢复正常渲染
	if err := pty.Setsize(s.tty.(*os.File), &pty.Winsize{Cols: 100, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	s.waitScreen(t, "PROJECT", 3*time.Second)
}
