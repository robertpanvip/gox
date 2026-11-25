package lsp

import (
	"io"
	"runtime"
	"strings"
	"testing"
)

// TestCompletionTypedMember 桌面 LSP 点号补全走类型层：b.（*strings.Replacer）
// 应含 Replace。依赖本机 GOROOT 源码（与 embed 无关，桌面天然可用）。
func TestCompletionTypedMember(t *testing.T) {
	gr := runtime.GOROOT()
	if gr == "" {
		t.Skip("无 GOROOT")
	}
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	src := "package main\n\nimport \"strings\"\n\nfunc f() {\n\tb := strings.NewReplacer()\n\tb.\n\t_ = b\n}\n"
	s.docs["file:///t.gx"] = &document{Text: src}
	res, rpcErr := s.completion([]byte(`{"textDocument":{"uri":"file:///t.gx"},"position":{"line":6,"character":3}}`))
	if rpcErr != nil {
		t.Fatal(rpcErr.Message)
	}
	list, ok := res.(CompletionList)
	if !ok {
		t.Fatalf("返回类型错误: %T", res)
	}
	for _, it := range list.Items {
		if it.Label == "Replace" {
			return
		}
	}
	t.Fatalf("桌面 b. 应含 Replace, got %d items", len(list.Items))
}
