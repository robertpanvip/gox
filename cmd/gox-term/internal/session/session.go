// Package session 封装一个 PTY + shell 子进程，作为终端面板里的一个"标签"。
//
// 每个 Session：
//   - 用 creack/pty 起一个伪终端，spawn shell（默认 $SHELL，可覆盖）；
//   - 一个读 goroutine 持续把 PTY 输出通过 tea.Program.Send 投递给 UI；
//   - UI 把键盘字节 Write 回 PTY，实现真正的交互式命令；
//   - 窗口尺寸变化时 Setsize，保证 shell 里程序按当前宽度排版。
package session

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
)

// OutputMsg 携带一次 PTY 读到的输出（可能含 ANSI 转义序列）。
type OutputMsg struct {
	ID   int
	Data []byte
}

// ExitedMsg 表示 shell 退出。
type ExitedMsg struct {
	ID     int
	Reason string
}

// Session 是一个 PTY shell 会话。
type Session struct {
	ID   int
	Name string

	ptmx *os.File
	cmd  *exec.Cmd
	prog *tea.Program

	mu     sync.Mutex
	buf    []byte // 滚动输出缓冲（保留最近若干字节，供重绘/回滚）
	exited bool
}

// New 启动一个 shell 会话并挂到 prog。id 用于区分多标签。
func New(id int, prog *tea.Program, shell string, cols, rows uint16) (*Session, error) {
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}
	_ = pty.Setsize(ptmx, &pty.Winsize{Cols: cols, Rows: rows})

	s := &Session{ID: id, Name: shellName(shell), ptmx: ptmx, cmd: cmd, prog: prog}
	go s.readLoop()
	go s.reapLoop()
	return s, nil
}

func shellName(shell string) string {
	if i := len(shell) - 1; i >= 0 && shell[i] == '/' {
		return shell[i+1:]
	}
	return shell
}

// NewStub 构造一个不启动进程的桩会话，供 UI 层单元测试注入。
func NewStub(id int, name string) *Session {
	return &Session{ID: id, Name: name}
}

// readLoop 把 PTY 输出持续投递给 UI；shell 退出（EOF/错误）时发一次 ExitedMsg。
func (s *Session) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			data := append([]byte(nil), buf[:n]...)
			s.mu.Lock()
			s.buf = append(s.buf, data...)
			// 只保留最近 512KB，避免长会话无限增长。
			if len(s.buf) > 512*1024 {
				s.buf = s.buf[len(s.buf)-512*1024:]
			}
			s.mu.Unlock()
			s.prog.Send(OutputMsg{ID: s.ID, Data: data})
		}
		if err != nil {
			reason := ""
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				reason = err.Error()
			}
			s.mu.Lock()
			s.exited = true
			s.mu.Unlock()
			s.prog.Send(ExitedMsg{ID: s.ID, Reason: reason})
			return
		}
	}
}

// reapLoop 回收子进程，避免僵尸（不重复发消息，退出信号由 readLoop 负责）。
func (s *Session) reapLoop() {
	_ = s.cmd.Wait()
}

// Write 把按键字节写入 PTY。
func (s *Session) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exited {
		return 0, io.EOF
	}
	if s.ptmx == nil { // 桩会话（测试用），吞掉写入
		return len(p), nil
	}
	return s.ptmx.Write(p)
}

// Resize 同步 PTY 窗口大小。
func (s *Session) Resize(cols, rows uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exited {
		return
	}
	_ = pty.Setsize(s.ptmx, &pty.Winsize{Cols: cols, Rows: rows})
}

// Snapshot 返回当前输出缓冲的拷贝（供滚动画面用）。
func (s *Session) Snapshot() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.buf...)
}

// Exited 返回会话是否已退出。
func (s *Session) Exited() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited
}

// CWD 返回 shell 进程当前工作目录（Linux 读 /proc/<pid>/cwd，macOS 回退到启动目录）。
// 用于补全引擎按当前目录扫描项目文件与符号；失败返回空串由上层兜底。
func (s *Session) CWD() string {
	if s.cmd == nil || s.cmd.Process == nil {
		return ""
	}
	if p, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", s.cmd.Process.Pid)); err == nil {
		return p
	}
	if s.cmd.Dir != "" {
		return s.cmd.Dir
	}
	wd, _ := os.Getwd()
	return wd
}

// Close 结束 shell 并关闭 PTY。
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.exited {
		return
	}
	_ = s.ptmx.Close()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
}
