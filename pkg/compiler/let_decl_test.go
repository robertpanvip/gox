package compiler

import (
	"strings"
	"testing"
)

// let 是语言规范 §11 的正式语法（let 即短声明，无新语义）。遗留缺陷回归：
// 旧实现把 let 降级耦合在 transformSignals 的 HasSignal() 短路后面，导致
// 不含任何 sig 的文件里 `let x = 42` 原样透传成非法 Go，被 InvalidGo 闸门拦截。
func TestLetDeclWithoutSignal(t *testing.T) {
	res, err := CompileStage("package main\n\nfunc main() {\n\tlet x = 42\n\tlet s = \"hi\"\n\tprintln(x, s)\n}\n", "all")
	if err != nil {
		t.Fatalf("纯 let 文件应编译通过: %v", err)
	}
	got := res
	if !strings.Contains(got, "x := 42") || !strings.Contains(got, `s := "hi"`) {
		t.Fatalf("let 应降级为短声明，产物缺 x := 42 / s := \"hi\":\n%s", got)
	}
	if strings.Contains(got, "let ") {
		t.Fatalf("产物不应残留 let 关键字:\n%s", got)
	}
}

// 带类型标注的 let：let x int = 42 → var x int = 42。
func TestLetDeclTyped(t *testing.T) {
	res, err := CompileStage("package main\n\nfunc main() {\n\tlet x int = 42\n\tprintln(x)\n}\n", "all")
	if err != nil {
		t.Fatalf("带类型标注的 let 应编译通过: %v", err)
	}
	got := res
	if !strings.Contains(got, "var x int = 42") {
		t.Fatalf("let x int = 42 应降级为 var x int = 42:\n%s", got)
	}
}

// sig 与 let 混用时两条降级路径互不干扰。
func TestLetDeclMixedWithSig(t *testing.T) {
	res, err := CompileStage("package main\n\nsig count = 1\n\nfunc main() {\n\tlet x = 42\n\tcount = count + x\n\tprintln(x)\n}\n", "all")
	if err != nil {
		t.Fatalf("sig + let 混用应编译通过: %v", err)
	}
	got := res
	if !strings.Contains(got, "x := 42") {
		t.Fatalf("let 应降级为短声明:\n%s", got)
	}
	if strings.Contains(got, "let ") {
		t.Fatalf("产物不应残留 let 关键字:\n%s", got)
	}
}

// 非声明形态的 let（无 `=`）不应吞掉后续语句。
func TestLetDeclIncompletePassThrough(t *testing.T) {
	// `let` 后无标识符：原样透传（下游自然报非法 Go，而不是静默吞掉别的语句）。
	_, err := CompileStage("package main\n\nfunc main() {\n\tlet\n\tprintln(1)\n}\n", "all")
	// 不硬性要求报错或通过，只要求不 panic；真实失败形态由闸门兜底。
	if err != nil && !strings.Contains(err.Error(), "let") && !strings.Contains(err.Error(), "语法无效") {
		t.Fatalf("意外错误形态: %v", err)
	}
}
