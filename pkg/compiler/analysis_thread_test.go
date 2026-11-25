package compiler

import (
	"strings"
	"testing"
)

func diagsOf(t *testing.T, src string) []GXDiagnostic {
	t.Helper()
	res, err := CompileScene("thread.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	return res.Analysis.Diagnostics()
}

func hasCrossThread(diags []GXDiagnostic) bool {
	for _, d := range diags {
		if d.Code == "signal-cross-thread" {
			return true
		}
	}
	return false
}

// 后台 goroutine 裸写 sig：必须报 signal-cross-thread 诊断。
func TestCrossThreadSigWriteDetected(t *testing.T) {
	cases := []struct {
		name, src, wantName string
	}{
		{"赋值", "package main\nfunc main(){ sig count = 0; go func(){ count = 5 }() }\n", "count"},
		{"复合赋值", "package main\nfunc main(){ sig count = 0; go func(){ count += 1 }() }\n", "count"},
		{"自增", "package main\nfunc main(){ sig n = 0; go func(){ n++ }() }\n", "n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ds := diagsOf(t, c.src)
			found := false
			for _, d := range ds {
				if d.Code == "signal-cross-thread" && strings.Contains(d.Message, c.wantName) {
					found = true
					if d.Pos.Line < 1 || d.Pos.Col < 1 {
						t.Errorf("诊断应带 1 基 GX 坐标, got %+v", d.Pos)
					}
					if !strings.Contains(d.Message, "Signal.safe") {
						t.Errorf("诊断应含修法 Signal.safe, got %s", d.Message)
					}
				}
			}
			if !found {
				t.Errorf("应报 signal-cross-thread 含 %s, got %v", c.wantName, ds)
			}
		})
	}
}

// 经 Signal.safe / app.dispatch 投递的 sig 写：放行，不误报。
func TestCrossThreadSigWriteSafeAllowed(t *testing.T) {
	cases := []struct{ name, src string }{
		{"safe", "package main\nfunc main(){ sig count = 0; go func(){ Signal.safe(() => { count = 5 }) }() }\n"},
		{"dispatch", "package main\nfunc main(){ sig count = 0; go func(){ app.dispatch(() => { count = 5 }) }() }\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if ds := diagsOf(t, c.src); hasCrossThread(ds) {
				t.Errorf("safe/dispatch 投递不应误报, got %v", ds)
			}
		})
	}
}

// 非后台写法不得误报：普通函数内写、主线程顶层写、普通变量写。
func TestCrossThreadSigWriteNoFalsePositive(t *testing.T) {
	cases := []struct{ name, src string }{
		{"普通函数", "package main\nfunc bump(){ count = 1 }\nfunc main(){ sig count = 0; bump() }\n"},
		{"顶层写", "package main\nfunc main(){ sig count = 0; count = 5 }\n"},
		{"普通变量", "package main\nfunc main(){ x := 0; go func(){ x = 5 }() }\n"},
		{"go非func", "package main\nfunc main(){ sig n = 0; go print(n) }\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if ds := diagsOf(t, c.src); hasCrossThread(ds) {
				t.Errorf("不应误报 signal-cross-thread, got %v", ds)
			}
		})
	}
}
