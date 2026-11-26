package lsp

import (
	"io"
	"runtime"
	"strings"
	"testing"
)

func TestReferencesDesktop(t *testing.T) {
	if runtime.GOROOT() == "" {
		t.Skip("无 GOROOT")
	}
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	src := "package main\n\nimport \"fmt\"\n\nfunc main() {\n\ts := fmt.Sprintf(\"%d\", 1)\n\t_ = s\n}\n"
	s.docs["file:///r.gx"] = &document{Text: src}
	res, e := s.references([]byte(`{"textDocument":{"uri":"file:///r.gx"},"position":{"line":5,"character":16},"context":{"includeDeclaration":true}}`))
	if e != nil {
		t.Fatal(e.Message)
	}
	locs, ok := res.([]Location)
	if !ok || len(locs) < 2 {
		t.Fatalf("引用数: %+v (%T)", res, res)
	}
	files, uses := 0, 0
	for _, l := range locs {
		if strings.Contains(l.URI, "/src/fmt/") {
			files++
		}
		if l.URI == "file:///r.gx" {
			uses++
		}
	}
	if files == 0 || uses == 0 {
		t.Fatalf("声明/使用分布异常: %+v", locs)
	}
}

func TestDocumentHighlightDesktop(t *testing.T) {
	if runtime.GOROOT() == "" {
		t.Skip("无 GOROOT")
	}
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	src := "package main\n\nfunc main() {\n\tn := 1\n\tn = n + 1\n}\n"
	s.docs["file:///h.gx"] = &document{Text: src}
	res, e := s.documentHighlight([]byte(`{"textDocument":{"uri":"file:///h.gx"},"position":{"line":4,"character":6}}`))
	if e != nil {
		t.Fatal(e.Message)
	}
	its, ok := res.([]DocumentHighlight)
	if !ok || len(its) < 2 {
		t.Fatalf("高亮数量: %+v (%T)", res, res)
	}
}

func TestCodeActionDesktop(t *testing.T) {
	if runtime.GOROOT() == "" {
		t.Skip("无 GOROOT")
	}
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	src := "package main\n\nfunc main() {\n\ts := fmt.Sprintf(\"%d\", 1)\n\t_ = s\n}\n"
	s.docs["file:///c.gx"] = &document{Text: src}
	res, e := s.codeAction([]byte(`{"textDocument":{"uri":"file:///c.gx"},"range":{"start":{"line":3,"character":10},"end":{"line":3,"character":10}},"context":{"diagnostics":[{"range":{"start":{"line":3,"character":7},"end":{"line":3,"character":7}},"message":"undefined: fmt","source":"gox"}]}}`))
	if e != nil {
		t.Fatal(e.Message)
	}
	acts, ok := res.([]CodeAction)
	if !ok || len(acts) == 0 {
		t.Fatalf("修复数量: %+v (%T)", res, res)
	}
	if !strings.Contains(acts[0].Title, "fmt") || len(acts[0].Edit.Changes["file:///c.gx"]) == 0 {
		t.Fatalf("修复内容: %+v", acts[0])
	}
	if !strings.Contains(acts[0].Edit.Changes["file:///c.gx"][0].NewText, `import "fmt"`) {
		t.Fatalf("NewText: %+v", acts[0].Edit.Changes["file:///c.gx"][0])
	}
}

func TestWorkspaceSymbolDesktop(t *testing.T) {
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	src := "package main\n\nfunc Hello() {}\n\nfunc main() { Hello() }\n"
	s.docs["file:///w.gx"] = &document{Text: src}
	res, e := s.workspaceSymbol([]byte(`{"query":"ello"}`))
	if e != nil {
		t.Fatal(e.Message)
	}
	syms, ok := res.([]WorkspaceSymbol)
	if !ok {
		t.Fatalf("类型: %T", res)
	}
	for _, sy := range syms {
		if sy.Name == "Hello" {
			return
		}
	}
	t.Fatalf("未找到 Hello: %+v", syms)
}
