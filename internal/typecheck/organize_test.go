package typecheck

import (
	"strings"
	"testing"
)

// OrganizeImports 红测试：合并连续单行、块内 std/非 std 分组排序、别名保留；
// 已有序的返回 ok=false（幂等，不乱改）。

func TestOrganizeMergeSingles(t *testing.T) {
	src := "package main\n\nimport \"strings\"\nimport \"fmt\"\nimport \"os/exec\"\n\nfunc main() {}\n"
	edits, ok := OrganizeImports(src)
	if !ok || len(edits) != 1 {
		t.Fatalf("应产出一条合并编辑: ok=%v edits=%+v", ok, edits)
	}
	e := edits[0]
	if e.StartLine != 3 || e.EndLine != 5 {
		t.Fatalf("区域: %+v", e)
	}
	want := "import (\n\t\"fmt\"\n\t\"os/exec\"\n\t\"strings\"\n)"
	if strings.TrimRight(e.Text, "\n") != want {
		t.Fatalf("文本:\n%s", e.Text)
	}
}

func TestOrganizeBlockGroups(t *testing.T) {
	src := "package main\n\nimport (\n\t\"github.com/x/y\"\n\tgxscene \"github.com/robertpanvip/gox/runtime/scene\"\n\t\"strings\"\n)\n\nfunc main() {}\n"
	edits, ok := OrganizeImports(src)
	if !ok || len(edits) != 1 {
		t.Fatalf("块整理应命中: ok=%v %+v", ok, edits)
	}
	got := strings.TrimRight(edits[0].Text, "\n")
	want := "import (\n\t\"strings\"\n\n\tgxscene \"github.com/robertpanvip/gox/runtime/scene\"\n\t\"github.com/x/y\"\n)"
	if got != want {
		t.Fatalf("分组排序:\n%s\n----want----\n%s", got, want)
	}
}

func TestOrganizeIdempotent(t *testing.T) {
	src := "package main\n\nimport (\n\t\"fmt\"\n\n\t\"github.com/x/y\"\n)\n\nfunc main() {}\n"
	if _, ok := OrganizeImports(src); ok {
		t.Fatal("已有序不得变更")
	}
	src2 := "package main\n\nimport \"fmt\"\n\nfunc main() {}\n"
	if _, ok := OrganizeImports(src2); ok {
		t.Fatal("单条独立 import 不动")
	}
	src3 := "package main\n\nimport \"fmt\"\n\nimport \"strings\"\n\nfunc main() {}\n"
	if _, ok := OrganizeImports(src3); ok {
		t.Fatal("空行隔开的两组不跨组合并")
	}
}
