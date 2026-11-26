package lsp

import (
	"io"
	"runtime"
	"strings"
	"testing"
)

// 桌面 semanticTokens / completionItem/resolve / inlayHints 红测试。

func TestSemanticTokensDesktop(t *testing.T) {
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(1)\n}\n"
	s.docs["file:///t.gx"] = &document{Text: src}
	res, e := s.semanticTokens([]byte(`{"textDocument":{"uri":"file:///t.gx"}}`))
	if e != nil {
		t.Fatal(e.Message)
	}
	out, ok := res.(SemanticTokensResult)
	if !ok {
		t.Fatalf("类型: %T", res)
	}
	if len(out.Data) < 15 || len(out.Data)%5 != 0 {
		t.Fatalf("data=%d", len(out.Data))
	}
}

func TestCompletionResolveDesktop(t *testing.T) {
	if runtime.GOROOT() == "" {
		t.Skip("无 GOROOT")
	}
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	res, e := s.completionResolve([]byte(`{"label":"Sprintf","data":"fmt.Sprintf"}`))
	if e != nil {
		t.Fatal(e.Message)
	}
	it, ok := res.(CompletionItem)
	if !ok {
		t.Fatalf("类型: %T", res)
	}
	if !strings.Contains(it.Documentation, "formats according") {
		t.Fatalf("documentation=%q", it.Documentation)
	}
}

func TestInlayHintsDesktop(t *testing.T) {
	if runtime.GOROOT() == "" {
		t.Skip("无 GOROOT")
	}
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Sprintf(\"%d\", 1)\n}\n"
	s.docs["file:///i.gx"] = &document{Text: src}
	res, e := s.inlayHints([]byte(`{"textDocument":{"uri":"file:///i.gx"},"range":{"start":{"line":0,"character":0},"end":{"line":6,"character":0}}}`))
	if e != nil {
		t.Fatal(e.Message)
	}
	hs, ok := res.([]InlayHint)
	if !ok {
		t.Fatalf("类型: %T", res)
	}
	for _, h := range hs {
		if h.Label == "format:" && h.Position.Line == 5 {
			return
		}
	}
	t.Fatalf("缺 format: 提示: %+v", hs)
}
