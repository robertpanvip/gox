package compiler

import (
	"strings"
	"testing"
)

// C2：signal 持 slice 时 `rows[0].name = "b"` 编译得过、值也变了，但没调用
// signal 写函数、依赖它的 slot 不刷新（静默错值）。改为带坐标+带修法的诊断。
func TestC2SignalSliceElemDeepWriteDiag(t *testing.T) {
	src := `package main
type Row struct { name string }
func main() {
	signal rows = []Row{{name: "a"}}
	rows[0].name = "b"
	_, _ = <div>{rows[0].name}</div>
}
`
	res, err := CompileScene("c2.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range res.Analysis.Diagnostics() {
		if d.Code == "signal-deep-write" && strings.Contains(d.Message, "rows()[0].name") {
			found = true
			if !strings.Contains(d.Message, "新切片") {
				t.Errorf("诊断应含修法提示, got %s", d.Message)
			}
			if d.Pos.Line < 1 || d.Pos.Col < 1 {
				t.Errorf("诊断应带 1 基坐标, got %+v", d.Pos)
			}
		}
	}
	if !found {
		t.Errorf("应报 signal 切片元素深写诊断, got %v", res.Analysis.Diagnostics())
	}
}

// 守护：signal slice 的元素「读」不报深写（rows()[0].name 出现在插值里）。
func TestC2ReadNoDeepWriteDiag(t *testing.T) {
	src := `package main
type Row struct { name string }
func main() {
	signal rows = []Row{{name: "a"}}
	_, _ = <div>{rows[0].name}</div>
}
`
	res, err := CompileScene("c2-read.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range res.Analysis.Diagnostics() {
		if d.Code == "signal-deep-write" {
			t.Errorf("只读不应报 signal-deep-write: %s", d.Message)
		}
	}
}

// 守护：普通 slice（非 signal）的元素字段赋值是合法 Go，不报深写。
func TestC2PlainSliceElemWriteNoDiag(t *testing.T) {
	src := `package main
type Row struct { name string }
func main() {
	rows := []Row{{name: "a"}}
	rows[0].name = "b"
	_ = rows
}
`
	res, err := CompileScene("c2-plain.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range res.Analysis.Diagnostics() {
		if d.Code == "signal-deep-write" {
			t.Errorf("普通 slice 元素赋值不应报 signal-deep-write: %s", d.Message)
		}
	}
}
