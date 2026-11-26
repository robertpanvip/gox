package lsp

import (
	"io"
	"runtime"
	"strings"
	"testing"
)

// 桌面端 std 跳转 / 类型化 hover / import 路径补全的红测试（真实 GOROOT）。

func TestDefinitionStdTyped(t *testing.T) {
	if runtime.GOROOT() == "" {
		t.Skip("无 GOROOT")
	}
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\ts := fmt.Sprintf(\"%d\", 1)\n\t_ = s\n}\n"
	s.docs["file:///d.gx"] = &document{Text: src}
	res, rpcErr := s.definition([]byte(`{"textDocument":{"uri":"file:///d.gx"},"position":{"line":5,"character":16}}`))
	if rpcErr != nil {
		t.Fatal(rpcErr.Message)
	}
	loc, ok := res.(Location)
	if !ok {
		t.Fatalf("返回类型: %T", res)
	}
	if !strings.Contains(loc.URI, "/src/fmt/") {
		t.Fatalf("URI 应指向 GOROOT/src/fmt: %q", loc.URI)
	}
	if loc.Range.Start.Line <= 0 {
		t.Fatalf("行号异常: %d", loc.Range.Start.Line)
	}
}

func TestCompletionImportPathDesktop(t *testing.T) {
	if runtime.GOROOT() == "" {
		t.Skip("无 GOROOT")
	}
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	s.docs["file:///i.gx"] = &document{Text: "package main\n\nimport \"str\n"}
	res, rpcErr := s.completion([]byte(`{"textDocument":{"uri":"file:///i.gx"},"position":{"line":2,"character":11}}`))
	if rpcErr != nil {
		t.Fatal(rpcErr.Message)
	}
	list, ok := res.(CompletionList)
	if !ok {
		t.Fatalf("返回类型: %T", res)
	}
	for _, it := range list.Items {
		if it.Label == "strings" {
			return
		}
	}
	t.Fatalf("桌面 import 补全应含 strings, got %d items", len(list.Items))
}

func TestHoverTypedDesktop(t *testing.T) {
	if runtime.GOROOT() == "" {
		t.Skip("无 GOROOT")
	}
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\ts := fmt.Sprintf(\"%d\", 1)\n\t_ = s\n}\n"
	s.docs["file:///h.gx"] = &document{Text: src}
	res, rpcErr := s.hover([]byte(`{"textDocument":{"uri":"file:///h.gx"},"position":{"line":5,"character":16}}`))
	if rpcErr != nil {
		t.Fatal(rpcErr.Message)
	}
	h, ok := res.(Hover)
	if !ok {
		t.Fatalf("返回类型: %T", res)
	}
	if !strings.Contains(h.Contents.Value, "func(") {
		t.Fatalf("hover 应含真实签名: %q", h.Contents.Value)
	}
}
