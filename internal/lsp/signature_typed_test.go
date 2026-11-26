package lsp

import (
	"io"
	"runtime"
	"strings"
	"testing"
)

// TestSignatureHelpTyped 桌面 textDocument/signatureHelp 走类型层：
// fmt.Sprintf 逗号后 active=1，标签含真实参数名。依赖本机 GOROOT。
func TestSignatureHelpTyped(t *testing.T) {
	gr := runtime.GOROOT()
	if gr == "" {
		t.Skip("无 GOROOT")
	}
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	src := "package main\n\nimport \"fmt\"\n\nfunc f() {\n\tfmt.Sprintf(\"%d\", \"x\")\n\t_ = 0\n}\n"
	s.docs["file:///s.gx"] = &document{Text: src}
	res, rpcErr := s.signatureHelp([]byte(`{"textDocument":{"uri":"file:///s.gx"},"position":{"line":5,"character":19}}`))
	if rpcErr != nil {
		t.Fatal(rpcErr.Message)
	}
	h, ok := res.(SignatureHelp)
	if !ok {
		t.Fatalf("返回类型错误: %T", res)
	}
	if len(h.Signatures) == 0 || h.ActiveParameter != 1 {
		t.Fatalf("签名结果不符: %+v", h)
	}
	if !strings.Contains(h.Signatures[0].Label, "format string") {
		t.Fatalf("标签缺少参数名: %q", h.Signatures[0].Label)
	}
}
