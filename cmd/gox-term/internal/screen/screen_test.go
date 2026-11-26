package screen

import (
	"strings"
	"testing"
)

// visible 把带 SGR 的行还原成纯可见文本，便于断言。
func visible(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	// 末尾补的空格在断言时不关心，TrimRight。
	return strings.TrimRight(b.String(), " ")
}

func cellsText(s *Screen, row int) string {
	var b strings.Builder
	for _, c := range s.buf[row] {
		if c.Cont {
			continue
		}
		if c.Ch == 0 || c.Ch < 0x20 {
			b.WriteByte(' ')
		} else {
			b.WriteRune(c.Ch)
		}
	}
	return strings.TrimRight(b.String(), " ")
}

func TestPlainText(t *testing.T) {
	s := New(10, 3)
	s.Write([]byte("hello"))
	if got := cellsText(s, 0); got != "hello" {
		t.Fatalf("row0 = %q, want hello", got)
	}
	s.Write([]byte("\r\nworld"))
	if got := cellsText(s, 1); got != "world" {
		t.Fatalf("row1 = %q, want world", got)
	}
}

func TestCursorAddressing(t *testing.T) {
	// vim 的典型手法：先清屏归位，跳到第 3 行第 5 列写 X。
	s := New(20, 6)
	s.Write([]byte("\x1b[2J\x1b[H")) // 清屏 + 归位
	s.Write([]byte("\x1b[3;5HX"))    // 移到 (行3,列5) 写 X
	if got := cellsText(s, 2); got != "    X" {
		t.Fatalf("row2 = %q, want '    X'", got)
	}
	// 再跳到第 1 行第 1 列覆盖写 A。
	s.Write([]byte("\x1b[1;1HA"))
	if got := cellsText(s, 0); got != "A" {
		t.Fatalf("row0 = %q, want A", got)
	}
}

func TestEraseLineAndDisplay(t *testing.T) {
	s := New(10, 3)
	s.Write([]byte("abcdefghij"))
	s.Write([]byte("\x1b[5G")) // 移到第 5 列
	s.Write([]byte("\x1b[K"))  // 从光标擦到行尾
	if got := cellsText(s, 0); got != "abcd" {
		t.Fatalf("after EL0 = %q, want abcd", got)
	}
	s.Write([]byte("XYZ"))
	if got := cellsText(s, 0); got != "abcdXYZ" {
		t.Fatalf("after rewrite = %q, want abcdXYZ", got)
	}
	s.Write([]byte("\x1b[2J"))
	if got := cellsText(s, 0); got != "" {
		t.Fatalf("after ED2 = %q, want empty", got)
	}
}

func TestScrollAndHistory(t *testing.T) {
	s := New(5, 2)
	s.Write([]byte("line1\r\nline2\r\n"))
	if s.HistoryCount() != 1 {
		t.Fatalf("history = %d, want 1", s.HistoryCount())
	}
	// 当前屏幕应是 line2 + 空行。
	if got := cellsText(s, 0); got != "line2" {
		t.Fatalf("row0 = %q, want line2", got)
	}
	// 回滚应能看到 line1。
	win := s.RenderWindow(1, 2)
	if got := visible(win[0]); got != "line1" {
		t.Fatalf("scrollback row0 = %q, want line1", got)
	}
}

func TestSGRColors(t *testing.T) {
	s := New(10, 1)
	s.Write([]byte("\x1b[31mR\x1b[0m\x1b[42mG\x1b[0m"))
	line := s.RenderWindow(0, 1)[0]
	if !strings.Contains(line, "38;5;1") {
		t.Fatalf("缺少红色前景 SGR: %q", line)
	}
	if !strings.Contains(line, "48;5;2") {
		t.Fatalf("缺少绿色背景 SGR: %q", line)
	}
	if !strings.Contains(line, "R") || !strings.Contains(line, "G") {
		t.Fatalf("缺少文本: %q", line)
	}
}

func TestReverseVideo(t *testing.T) {
	s := New(4, 1)
	s.Write([]byte("\x1b[7mAB\x1b[0m"))
	line := s.RenderWindow(0, 1)[0]
	if !strings.Contains(line, "7m") {
		t.Fatalf("缺少反显 SGR: %q", line)
	}
}

func TestAltScreen(t *testing.T) {
	s := New(10, 2)
	s.Write([]byte("normal"))
	if s.InAlt() {
		t.Fatal("初始不应在交替屏幕")
	}
	// 进入 alt-screen：应先存光标，再切缓冲。
	s.Write([]byte("\x1b[s\x1b[?1049h"))
	if !s.InAlt() {
		t.Fatal("应进入交替屏幕")
	}
	s.Write([]byte("\x1b[2J\x1b[HALTFULL")) // vim 全屏重绘
	if got := cellsText(s, 0); got != "ALTFULL" {
		t.Fatalf("alt row0 = %q, want ALTFULL", got)
	}
	// 退出 alt-screen：恢复常规缓冲，应还能看到 normal。
	s.Write([]byte("\x1b[?1049l"))
	if s.InAlt() {
		t.Fatal("应退出交替屏幕")
	}
	if got := cellsText(s, 0); got != "normal" {
		t.Fatalf("恢复后 row0 = %q, want normal", got)
	}
}

func TestWideRune(t *testing.T) {
	s := New(6, 1)
	s.Write([]byte("你AB"))
	if got := cellsText(s, 0); got != "你AB" {
		t.Fatalf("row0 = %q, want 你AB", got)
	}
}

func TestVimLikeSession(t *testing.T) {
	// 模拟 vim 启动的一系列操作：进入 alt、清屏画两行、移动光标覆盖、退出。
	s := New(40, 5)
	s.Write([]byte("\x1b[?1049h\x1b[?25l")) // 进 alt，隐藏光标
	s.Write([]byte("\x1b[2J\x1b[H"))
	s.Write([]byte("package main\r\n"))
	s.Write([]byte("func main() {\r\n"))
	// 跳到第 2 行第 6 列，把 "main" 改成 "test"
	s.Write([]byte("\x1b[2;6Htest"))
	if got := cellsText(s, 1); got != "func test() {" {
		t.Fatalf("vim row1 = %q, want 'func test() {'", got)
	}
	// 状态栏反显（htop/vim 常用）
	s.Write([]byte("\x1b[5;1H\x1b[7m-- INSERT --\x1b[0m"))
	line := s.RenderWindow(0, 5)[4]
	if !strings.Contains(line, "7m") {
		t.Fatalf("状态栏应有反显: %q", line)
	}
	s.Write([]byte("\x1b[?1049l\x1b[?25h"))
	if s.InAlt() {
		t.Fatal("应退出 alt")
	}
}

func TestCJKScrollback(t *testing.T) {
	s := New(6, 2)
	s.Write([]byte("中文\r\n下一行\r\n"))
	win := s.RenderWindow(1, 2)
	if got := visible(win[0]); got != "中文" {
		t.Fatalf("scrollback = %q, want 中文", got)
	}
}

func TestLineLeftOfCursor(t *testing.T) {
	s := New(40, 3)
	s.Write([]byte("user@host:~$ git sta"))
	if got := s.LineLeftOfCursor(); got != "user@host:~$ git sta" {
		t.Fatalf("LineLeftOfCursor = %q", got)
	}
	// 退格后应变短
	s.Write([]byte{'\b', '\b'})
	if got := s.LineLeftOfCursor(); got != "user@host:~$ git s" {
		t.Fatalf("退格后 = %q", got)
	}
	// 光标定位到中间：只读左侧
	s.Write([]byte("\x1b[1;6H"))
	if got := s.LineLeftOfCursor(); got != "user@" {
		t.Fatalf("定位后左侧 = %q", got)
	}
}

func TestSelectedText(t *testing.T) {
	s := New(20, 3)
	s.Write([]byte("hello world\r\n中文测试\r\nthird line"))
	// 选第一行 "hello world" 的一部分：列 0..4 → "hello"
	s.SetSelection(true, 0, 0, 4, 0)
	if got := s.SelectedText(); got != "hello" {
		t.Fatalf("SelectedText = %q, want hello", got)
	}
	// 跨行第 0 行到 1 行
	s.SetSelection(true, 6, 0, 3, 1)
	if got := s.SelectedText(); got != "world\n中文" {
		t.Fatalf("跨行 SelectedText = %q, want 'world\\n中文'", got)
	}
	// 反向拖拽（head 在左上）
	s.SetSelection(true, 4, 0, 0, 0)
	if got := s.SelectedText(); got != "hello" {
		t.Fatalf("反向选区 = %q, want hello", got)
	}
	s.SetSelection(false, 0, 0, 0, 0)
	if got := s.SelectedText(); got != "" {
		t.Fatalf("清除后应为空，得到 %q", got)
	}
}

func TestSelectionHighlight(t *testing.T) {
	s := New(10, 1)
	s.Write([]byte("abcdefghij"))
	s.SetSelection(true, 2, 0, 4, 0)
	line := s.RenderWindow(0, 1)[0]
	// 选区通过反显(ESC[7m)标记：整行应含反显序列，且不止光标处。
	if !strings.Contains(line, "7m") {
		t.Fatalf("选区行应含反显序列: %q", line)
	}
}

func TestNoBackgroundBlockForPlainText(t *testing.T) {
	// 普通文本行（默认前景/背景）不应携带背景色序列 48;——这是"文字带背景块"的根因。
	s := New(10, 1)
	s.SetDefaultColors("#e8e8ea", "") // 默认背景留空（透明）
	s.Write([]byte("abc"))
	s.showCursor = false
	line := s.RenderWindow(0, 1)[0]
	if strings.Contains(line, "48;") {
		t.Fatalf("默认文本行不应含背景色序列 48;: %q", line)
	}
}

func TestCursorUsesUnderlineNotReverse(t *testing.T) {
	s := New(10, 1)
	s.Write([]byte("abc")) // 光标停在 c 之后（x=3）
	s.showCursor = true
	line := s.RenderWindow(0, 1)[0]
	// 光标应以下划线(4m)标记，不再是反显块(7m)。
	if !strings.Contains(line, "4m") {
		t.Fatalf("光标行应含下划线序列: %q", line)
	}
	// 无选区时不应出现反显。
	if strings.Contains(line, "7m") {
		t.Fatalf("光标行不应含反显序列 7m: %q", line)
	}
}
