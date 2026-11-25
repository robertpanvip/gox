package typecheck

import (
	"runtime"
	"strings"
	"testing"
)

func stdLookup(t *testing.T) Lookup {
	t.Helper()
	gr := runtime.GOROOT()
	if gr == "" || !dirExists(gr+"/src/strings") {
		t.Skip("无 GOROOT/src/strings")
	}
	return DiskLookup(gr+"/src", runtime.GOOS, runtime.GOARCH)
}

func names(cs []Candidate) string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Name+":"+c.Kind)
	}
	return strings.Join(out, ",")
}

func TestCompletePackageQualifier(t *testing.T) {
	imp := NewImporter(stdLookup(t))
	src := "package main\n\nimport \"strings\"\n\nfunc f() {\n\tstrings.\n}\n"
	off := strings.Index(src, "strings.") + len("strings.")
	cs := imp.Complete(src, off)
	if !strings.Contains(names(cs), "NewReplacer") {
		t.Fatalf("strings. 应含 NewReplacer, got %s", names(cs))
	}
}

func TestCompleteMethodOnVar(t *testing.T) {
	imp := NewImporter(stdLookup(t))
	src := "package main\n\nimport \"strings\"\n\nfunc f() {\n\tb := strings.NewReplacer()\n\tb.\n\t_ = b\n}\n"
	off := strings.Index(src, "\n\tb.") + len("\n\tb.")
	cs := imp.Complete(src, off)
	n := names(cs)
	if !strings.Contains(n, "Replace") {
		t.Fatalf("b.(Replacer) 应含 Replace, got %s", n)
	}
	if !strings.Contains(n, ":method") {
		t.Fatalf("Replace 应为 method, got %s", n)
	}
}

func TestCompleteNotAfterDot(t *testing.T) {
	imp := NewImporter(stdLookup(t))
	cs := imp.Complete("package main\n\nfunc f() { x := 1 }\n", 20)
	if cs != nil {
		t.Fatalf("非点号上下文应返回 nil: %v", cs)
	}
}

// TestCompleteExcludesUnexportedForeign 跨包场景（b 是 *strings.Replacer）不得
// 出现其它包的未导出字段/方法（once/r/oldnew/build 这类内部实现）。
func TestCompleteExcludesUnexportedForeign(t *testing.T) {
	imp := NewImporter(stdLookup(t))
	src := "package main\n\nimport \"strings\"\n\nfunc f() {\n\tb := strings.NewReplacer()\n\tb.\n\t_ = b\n}\n"
	off := strings.Index(src, "\n\tb.") + len("\n\tb.")
	for _, c := range imp.Complete(src, off) {
		first := c.Name[0]
		if !(first >= 'A' && first <= 'Z') {
			t.Fatalf("跨包补全泄漏未导出成员: %s (%s)", c.Name, c.Kind)
		}
	}
}
