package lsp

import (
	"io"
	"strings"
	"testing"
)

// pull diagnostics（3.17）+ organizeImports codeAction 红测试。

func TestPullDiagnosticDesktop(t *testing.T) {
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	src := "package main\n\nfunc main() {\n\ts := 1\n\tvar g string = s\n\t_ = g\n}\n"
	s.docs["file:///pd.gx"] = &document{Text: src}
	res, e := s.pullDiagnostic([]byte(`{"textDocument":{"uri":"file:///pd.gx"}}`))
	if e != nil {
		t.Fatal(e.Message)
	}
	rep, ok := res.(DiagnosticReport)
	if !ok {
		t.Fatalf("类型: %T", res)
	}
	if rep.Kind != "full" || len(rep.Items) == 0 {
		t.Fatalf("报告: %+v", rep)
	}
	found := false
	for _, it := range rep.Items {
		if strings.Contains(it.Message, "cannot use") {
			found = true
		}
	}
	if !found {
		t.Fatalf("应含 cannot use 诊断: %+v", rep.Items)
	}
}

func TestCodeActionOrganizeDesktop(t *testing.T) {
	s := NewServer(strings.NewReader(""), io.Discard, nil)
	src := "package main\n\nimport \"strings\"\nimport \"fmt\"\n\nfunc main() { _, _ = strings.Builder{}, fmt.Println }\n"
	s.docs["file:///og.gx"] = &document{Text: src}
	res, e := s.codeAction([]byte(`{"textDocument":{"uri":"file:///og.gx"},"range":{"start":{"line":2,"character":0},"end":{"line":2,"character":0}},"context":{"diagnostics":[]}}`))
	if e != nil {
		t.Fatal(e.Message)
	}
	acts, ok := res.([]CodeAction)
	if !ok {
		t.Fatalf("类型: %T", res)
	}
	for _, a := range acts {
		if a.Kind == "source.organizeImports" {
			ed := a.Edit.Changes["file:///og.gx"][0]
			if strings.Contains(ed.NewText, `"fmt"`) && strings.Contains(ed.NewText, `"strings"`) {
				return
			}
		}
	}
	t.Fatalf("缺 organize 动作: %+v", acts)
}
