package lsp

import (
	"io"
	"strings"
	"testing"
)

func TestTypeDefinitionDesktop(t *testing.T) {
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	src := "package main\n\ntype Box struct{ N int }\n\nfunc main() {\n\tvar b Box\n\t_ = b\n}\n"
	s.docs["file:///td.gx"] = &document{Text: src}
	res, e := s.typeDefinition([]byte(`{"textDocument":{"uri":"file:///td.gx"},"position":{"line":5,"character":10}}`))
	if e != nil {
		t.Fatal(e.Message)
	}
	loc, ok := res.(Location)
	if !ok {
		t.Fatalf("类型: %T", res)
	}
	if loc.URI != "file:///td.gx" || loc.Range.Start.Line != 2 {
		t.Fatalf("应指向 Box 声明(0基第2行): %+v", loc)
	}
}

func TestImplementationDesktop(t *testing.T) {
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	src := "package main\n\ntype Speaker interface{ Speak() string }\n\ntype Dog struct{}\n\nfunc (d Dog) Speak() string { return \"w\" }\n"
	s.docs["file:///im.gx"] = &document{Text: src}
	res, e := s.implementation([]byte(`{"textDocument":{"uri":"file:///im.gx"},"position":{"line":2,"character":7}}`))
	if e != nil {
		t.Fatal(e.Message)
	}
	locs, ok := res.([]Location)
	if !ok || len(locs) != 1 {
		t.Fatalf("应有 1 个实现: %+v (%T)", res, res)
	}
	if locs[0].Range.Start.Line != 4 {
		t.Fatalf("Dog 声明在第 4 行(0基): %+v", locs[0])
	}
}

func TestRenameCrossFileDesktop(t *testing.T) {
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	s.docs["file:///a.gx"] = &document{Text: "package main\n\nfunc helper() int { return 1 }\n"}
	s.docs["file:///b.gx"] = &document{Text: "package main\n\nfunc use() int { return helper() }\n"}
	res, e := s.rename([]byte(`{"textDocument":{"uri":"file:///a.gx"},"position":{"line":2,"character":7},"newName":"helpAll"}`))
	if e != nil {
		t.Fatal(e.Message)
	}
	we, ok := res.(WorkspaceEdit)
	if !ok {
		t.Fatalf("类型: %T", res)
	}
	if len(we.Changes["file:///a.gx"]) != 1 {
		t.Fatalf("a.gx 编辑: %+v", we.Changes["file:///a.gx"])
	}
	if len(we.Changes["file:///b.gx"]) != 1 {
		t.Fatalf("跨文件应波及 b.gx: %+v", we.Changes)
	}
	if we.Changes["file:///b.gx"][0].Range.Start.Line != 2 {
		t.Fatalf("b.gx 行号: %+v", we.Changes["file:///b.gx"][0])
	}
}
