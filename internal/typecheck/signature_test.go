package typecheck

import (
	"strings"
	"testing"
)

// 签名提示三层红测试：typecheck 层直接验证 CallSig；goxlsp 层验证 .gx 坐标接线；
// internal/lsp 层验证桌面 RPC。任何一层缺席都必须在浏览器/编辑器里如实降级。

func sigFS() Lookup {
	return mapFS(map[string]string{
		"fmt": "package fmt\n\nfunc Sprintf(format string, a ...any) string { return format }\n",
	})
}

// TestSignatureFirstArg 光标紧跟左括号：应给出被调真实签名，active 参数为 0。
func TestSignatureFirstArg(t *testing.T) {
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\ts := fmt.Sprintf(\"%d\", \"x\")\n\t_ = s\n}\n"
	cur := strings.Index(src, "Sprintf(") + len("Sprintf(")
	cs, ok := Signature(src, cur, sigFS())
	if !ok {
		t.Fatal("括号内应命中签名")
	}
	if !strings.Contains(cs.Label, "Sprintf(format string, a ...any) string") {
		t.Fatalf("签名文本不符: %q", cs.Label)
	}
	if cs.Active != 0 {
		t.Fatalf("active=%d want 0", cs.Active)
	}
	if len(cs.Params) != 2 {
		t.Fatalf("params=%v want 2 段", cs.Params)
	}
	for _, r := range cs.Params {
		if r[0] >= r[1] || r[1] > len(cs.Label) {
			t.Fatalf("参数区间越界: %v label=%q", r, cs.Label)
		}
	}
}

// TestSignatureSecondArg 光标在第一个逗号之后：active=1（可变参数前）。
func TestSignatureSecondArg(t *testing.T) {
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\ts := fmt.Sprintf(\"%d\", \"x\")\n\t_ = s\n}\n"
	cur := strings.Index(src, "\", \"") + 3
	cs, ok := Signature(src, cur, sigFS())
	if !ok || cs.Active != 1 {
		t.Fatalf("ok=%v active=%d want true/1 label=%q", ok, cs.Active, cs.Label)
	}
}

// TestSignatureVariadicClamp 可变参数第 N 个逗号：active 钳到最后一个参数序号。
func TestSignatureVariadicClamp(t *testing.T) {
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Sprintf(\"%d\", \"a\", \"b\", \"c\")\n}\n"
	cur := strings.LastIndex(src, ",") + 1
	cs, ok := Signature(src, cur, sigFS())
	if !ok || cs.Active != 1 {
		t.Fatalf("ok=%v active=%d want true/1（...any 钳位）", ok, cs.Active)
	}
}

// TestSignatureMidTyping 编辑中的真实形态：括号未闭合（无右括号）也必须命中。
func TestSignatureMidTyping(t *testing.T) {
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Sprintf(\"%d\", \n}\n"
	cur := len(src) - len("\n}\n")
	cs, ok := Signature(src, cur, sigFS())
	if !ok || cs.Active != 1 {
		t.Fatalf("ok=%v active=%d want true/1（未闭合括号）", ok, cs.Active)
	}
}

// TestSignatureNoContext 不在调用括号内：必须优雅返回 false（不得误报）。
func TestSignatureNoContext(t *testing.T) {
	src := "package main\n\nfunc main() {\n\tx := 1\n\t_ = x\n}\n"
	if _, ok := Signature(src, strings.Index(src, "x := 1")+6, sigFS()); ok {
		t.Fatal("非调用上下文应返回 false")
	}
}
