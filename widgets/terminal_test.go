package widgets

import (
	"fmt"
	"strings"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// 内置终端面板（Console）回归：输出缓冲、环形裁剪、输出钩子、清屏与视图渲染。

func TestTerminalBufferBasics(t *testing.T) {
	st := NewTerminal()
	st.Println("hello")
	st.Print("no-newline")
	st.Printf(" %d=%s", 42, "answer")
	want := "hello\nno-newline 42=answer"
	if st.Text() != want {
		t.Fatalf("缓冲内容不符:\n got %q\nwant %q", st.Text(), want)
	}
	if st.LineCount() != 2 {
		t.Errorf("LineCount=%d, want 2", st.LineCount())
	}
	st.Clear()
	if st.Text() != "" || st.LineCount() != 0 {
		t.Errorf("Clear 后应清空, got %q", st.Text())
	}
}

func TestTerminalRingTrim(t *testing.T) {
	st := NewTerminal()
	st.OnOutput(nil) // no-op：钩子可为 nil
	for i := 0; i < 1200; i++ {
		st.Println("line-" + fmt.Sprintf("%d", i))
	}
	if st.LineCount() > 500 {
		t.Errorf("缓冲应裁剪到 ≤500 行, got %d", st.LineCount())
	}
	// 最早的行被裁掉，最近的行保留。
	if strings.Contains(st.Text(), "line-0\n") {
		t.Error("最早的行应被裁剪")
	}
	if !strings.Contains(st.Text(), "line-1199\n") {
		t.Error("最近的行必须保留")
	}
}

func TestTerminalOutputHook(t *testing.T) {
	st := NewTerminal()
	var hooked []string
	st.OnOutput(func(s string) { hooked = append(hooked, s) })
	st.Println("a")
	st.Print("b")
	if len(hooked) != 2 || hooked[0] != "a\n" || hooked[1] != "b" {
		t.Fatalf("钩子应收到每份输出: %q", hooked)
	}
	st.OnOutput(nil) // 解除
	st.Println("c")
	if len(hooked) != 2 {
		t.Errorf("解除后不应再回调: %q", hooked)
	}
}

func TestTerminalRunMark(t *testing.T) {
	st := NewTerminal()
	st.RunMark("Run #1")
	if !strings.HasPrefix(st.Text(), "── Run #1 ──\n") {
		t.Errorf("分隔行格式不符: %q", st.Text())
	}
}

func TestTerminalViewRenders(t *testing.T) {
	st := NewTerminal()
	st.Println("hello gx")
	inst := Terminal(gxscene.Props{"state": st})
	if inst == nil {
		t.Fatal("Terminal 应返回实例")
	}
	out := (gxscene.TextBackend{}).Paint(inst.Root())
	if !strings.Contains(out, "hello gx") || !strings.Contains(out, "Console") {
		t.Errorf("视图应包含标题与输出内容:\n%s", out)
	}
}
