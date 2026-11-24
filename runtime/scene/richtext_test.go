package gxscene

import (
	"strings"
	"testing"
)

func TestRichTextPlain(t *testing.T) {
	rt := RichText{{Text: "func", Color: "#fff"}, {Text: " f() {}", Color: ""}}
	if got := rt.Plain(); got != "func f() {}" {
		t.Fatalf("Plain 拼接错误: %q", got)
	}
}

func TestRichTextHTMLEscapes(t *testing.T) {
	rt := RichText{
		{Text: "if a < b && c > d", Color: "#c586c0"},
		{Text: `"q"`, Color: ""},
	}
	h := rt.HTML()
	if !strings.Contains(h, "a &lt; b &amp;&amp; c &gt; d") {
		t.Errorf("HTML 应转义 < > &: %q", h)
	}
	if !strings.Contains(h, `style="color:#c586c0"`) {
		t.Errorf("应含内联颜色样式: %q", h)
	}
	// Color 为空的分段不应产生 span 标签
	if strings.Contains(h, `style="color:"`) {
		t.Errorf("空颜色不应写 style: %q", h)
	}
}

// 细粒度分段在序列化时合并为更少 span，且拼接文本不变。
func TestRichTextHTMLMergesAdjacentSameColor(t *testing.T) {
	rt := RichText{
		{Text: "  ", Color: "#d4d4d4"},
		{Text: "foo", Color: "#d4d4d4"},
		{Text: "(", Color: "#808080"},
		{Text: ")", Color: "#808080"},
		{Text: " ", Color: "#d4d4d4"},
	}
	h := rt.HTML()
	if n := strings.Count(h, "<span"); n != 3 {
		t.Fatalf("应合并为 3 个 span，实际 %d: %q", n, h)
	}
	if !strings.Contains(h, "  foo") {
		t.Errorf("同色分段应写进同一个 span: %q", h)
	}
	if rt.Plain() != "  foo() " {
		t.Errorf("Plain 不应受合并影响: %q", rt.Plain())
	}
}

func TestRichTextFromProps(t *testing.T) {
	rt := RichText{{Text: "x", Color: "#fff"}}
	if got, ok := RichTextFromProps(map[string]any{PropRichText: rt}); !ok || got.Plain() != "x" {
		t.Fatalf("FromProps 失败: %v %v", got, ok)
	}
	if _, ok := RichTextFromProps(nil); ok {
		t.Fatal("nil props 应返回 false")
	}
	if _, ok := RichTextFromProps(map[string]any{PropRichText: "not richtext"}); ok {
		t.Fatal("类型错误应返回 false")
	}
}
