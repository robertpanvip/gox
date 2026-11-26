package typecheck

import (
	"strings"
	"testing"
)

// References：引用查找走同一持久类型图——声明点 + 用户文件内全部使用点。

func TestReferencesLocal(t *testing.T) {
	refs, ok := References(defSrc, strings.Index(defSrc, "s :=")+1, fmtLookup())
	if !ok || len(refs) != 2 {
		t.Fatalf("局部变量应有 2 处引用: ok=%v %+v", ok, refs)
	}
	if !refs[0].IsDef || refs[0].Line != 6 || refs[0].Pkg != "" {
		t.Fatalf("首个应为声明点(第6行): %+v", refs[0])
	}
	if refs[1].IsDef || refs[1].Line != 7 {
		t.Fatalf("第二项应为使用点(第7行): %+v", refs[1])
	}
	if refs[0].Name != "s" || refs[1].Name != "s" {
		t.Fatalf("Name=%q/%q", refs[0].Name, refs[1].Name)
	}
}

func TestReferencesExternal(t *testing.T) {
	refs, ok := References(defSrc, curSprintf(defSrc), fmtLookup())
	if !ok || len(refs) != 2 {
		t.Fatalf("Sprintf 应有 声明+使用 2 处: ok=%v %+v", ok, refs)
	}
	if !refs[0].IsDef || refs[0].Pkg != "fmt" || refs[0].File != "print.go" || refs[0].Line != 4 {
		t.Fatalf("外部声明点定位错误: %+v", refs[0])
	}
	if refs[1].IsDef || refs[1].Pkg != "" || refs[1].Line != 6 {
		t.Fatalf("使用点定位错误: %+v", refs[1])
	}
}

func TestReferencesMiss(t *testing.T) {
	src := "package main\n\nfunc main() {\n\tx := 2\n\t_ = x\n}\n"
	if _, ok := References(src, strings.Index(src, "2"), fmtLookup()); ok {
		t.Fatal("字面量上应返回 false")
	}
}
