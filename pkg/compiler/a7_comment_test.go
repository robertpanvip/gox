package compiler

import (
	"strings"
	"testing"
)

// A7：JSX 内的行注释（// ...）必须被忽略，不能变成可见文本节点或假属性。
// 三种位置：开始标签行尾、子节点行尾、属性行尾。
func TestJSXLineCommentsIgnored(t *testing.T) {
	cases := []struct {
		name string
		jsx  string
	}{
		{"开始标签行尾", "<div> // 头注\n  <span>x</span>\n</div>"},
		{"子节点行尾", "<div><span>x</span> // 尾注\n</div>"},
		{"属性行尾", "<div\n  class=\"a\" // 类名\n  id=\"b\">x</div>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := CompileScene("a7.gx", "package main\nfunc main() { _ = "+c.jsx+" }\n")
			if err != nil {
				t.Fatal(err)
			}
			g := res.GoSource
			for _, bad := range []string{"头注", "尾注", "类名", `// `} {
				if strings.Contains(g, bad) {
					t.Errorf("产物不应含注释内容 %q:\n%s", bad, g)
				}
			}
			if !strings.Contains(g, `Arg:"x"`) {
				t.Errorf("正常文本 x 应保留:\n%s", g)
			}
		})
	}
}

// A7 守护：文本节点和字符串属性值里的 '//'（URL / 路径）不是注释，必须保留。
func TestJSXSlashSlashInTextNotComment(t *testing.T) {
	src := "package main\nfunc main() { _ = <div><a href=\"http://example.com\">go//x</a></div> }\n"
	res, err := CompileScene("a7-guard.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	g := res.GoSource
	if !strings.Contains(g, "http://example.com") {
		t.Errorf("属性值里的 URL 应保留:\n%s", g)
	}
	if !strings.Contains(g, `go//x`) {
		t.Errorf("文本节点里的 go//x 应保留:\n%s", g)
	}
}

// A7 守护：注释里含 <tag> / {x} 等特殊字符时，Lexer 的 JSX 原文扫描不应被破坏。
func TestJSXCommentWithBrackets(t *testing.T) {
	src := "package main\nfunc main() { _ = <div> // 注释里有 <tag> 和 {x}\n  <span>ok</span>\n</div> }\n"
	res, err := CompileScene("a7-lex.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	g := res.GoSource
	if strings.Contains(g, "注释") {
		t.Errorf("注释未忽略:\n%s", g)
	}
	if !strings.Contains(g, `Arg:"ok"`) {
		t.Errorf("ok 文本应保留:\n%s", g)
	}
}
