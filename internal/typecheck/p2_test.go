package typecheck

import (
	"strings"
	"testing"
)

// typeDefinition / implementation / MultiRename 红测试。

func p2Lookup() Lookup {
	return func(path string) ([]File, bool) {
		switch path {
		case "p":
			return []File{{Name: "box.go", Src: "package p\n\n// Box 是测试类型。\ntype Box struct{ Name string }\n\nfunc (b Box) Hello() string { return b.Name }\n"}}, true
		}
		return nil, false
	}
}

func TestTypeDeclViaPointer(t *testing.T) {
	src := "package main\n\nimport \"p\"\n\nfunc main() {\n\tvar b *p.Box\n\t_ = b\n}\n"
	// 光标在 "_ = b" 的 b 之后：b 的类型 *p.Box → 声明应指向 p/box.go 的 type Box（第 4 行）
	d, ok := TypeDecl(src, strings.Index(src, "_ = b")+5, p2Lookup())
	if !ok {
		t.Fatal("指针类型应解引用命中")
	}
	if d.Pkg != "p" || d.File != "box.go" || d.Line != 4 {
		t.Fatalf("声明位置: %+v", d)
	}
}

func TestTypeDeclLocal(t *testing.T) {
	src := "package main\n\ntype Cat struct{ Name string }\n\nfunc main() {\n\tvar c Cat\n\t_ = c\n}\n"
	d, ok := TypeDecl(src, strings.Index(src, "_ = c")+5, p2Lookup())
	if !ok || d.Pkg != "" || d.Line != 3 {
		t.Fatalf("本地类型声明: %+v ok=%v", d, ok)
	}
}

func TestImplementations(t *testing.T) {
	src := "package main\n\ntype Speaker interface{ Speak() string }\n\ntype Dog struct{}\n\nfunc (d Dog) Speak() string { return \"w\" }\n\ntype Cat struct{}\n\nfunc (c Cat) Speak() string { return \"m\" }\n"
	// 光标在接口类型名 Speaker 上
	decls, ok := Implementations(src, strings.Index(src, "Speaker")+3, p2Lookup())
	if !ok || len(decls) != 2 {
		t.Fatalf("应有 Dog/Cat 两个实现: %+v ok=%v", decls, ok)
	}
	if decls[0].Name != "Dog" || decls[1].Name != "Cat" {
		t.Fatalf("实现名: %+v", decls)
	}
	// 光标在接口方法 Speak 上：同一接口
	decls2, ok := Implementations(src, strings.Index(src, "Speak()")+3, p2Lookup())
	if !ok || len(decls2) != 2 {
		t.Fatalf("方法上下文应命中同一接口: %+v", decls2)
	}
}

func TestMultiRenameAcrossFiles(t *testing.T) {
	files := []File{
		{Name: "a.go", Src: "package main\n\nfunc helper() int { return 1 }\n"},
		{Name: "b.go", Src: "package main\n\nfunc use() int { return helper() }\n"},
	}
	name, edits, ok := MultiRename(files, "a.go", strings.Index("package main\n\nfunc helper() int { return 1 }\n", "helper")+3)
	if !ok || name != "helper" {
		t.Fatalf("MultiRename: name=%q ok=%v", name, ok)
	}
	if len(edits["a.go"]) != 1 || len(edits["b.go"]) != 1 {
		t.Fatalf("每文件应各 1 处: %+v", edits)
	}
	if edits["b.go"][0].Line != 3 || edits["b.go"][0].Col != 25 {
		t.Fatalf("b.go 定位: %+v", edits["b.go"])
	}
}

func TestMultiRenameNoCrossTalk(t *testing.T) {
	// 两个包里同名不互扰：局部声明遮蔽时，只改自身
	files := []File{
		{Name: "a.go", Src: "package main\n\nfunc v() int { return 1 }\n"},
		{Name: "b.go", Src: "package main\n\nfunc w() int { v := 2; return v }\n"},
	}
	name, edits, ok := MultiRename(files, "a.go", strings.Index(files[0].Src, "v()")+1)
	if !ok || name != "v" {
		t.Fatalf("应命中顶层 v: name=%q ok=%v", name, ok)
	}
	if len(edits["a.go"]) != 1 || len(edits["b.go"]) != 0 {
		t.Fatalf("b.go 局部 v 不得被牵连: %+v", edits)
	}
}
