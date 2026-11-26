package syntax

import (
	"strings"
	"testing"
)

func TestHighlightRoundTrip(t *testing.T) {
	cases := []string{
		"",
		"sig count = 0",
		"func main() { return \"hello\" }",
		"// 注释 with 中文",
		"<View className=\"x\">text</View>",
		"for i in range(10) { x = x + 1 }",
		"let n = 123 // number",
		"func add(a int, b int) int { return a + b }",
		"enum Color { Red, Green, Blue }",
		"s = strings.NewReplacer(\"a\", \"x\")",
		"var buf []byte = make([]byte, 10)",
	}
	for _, line := range cases {
		var sb strings.Builder
		for _, tk := range Highlight(line) {
			sb.WriteString(tk.Text)
		}
		if sb.String() != line {
			t.Errorf("拼回原文不一致: in=%q out=%q", line, sb.String())
		}
	}
}

func TestHighlightColors(t *testing.T) {
	check := func(line, text, color string) {
		t.Helper()
		for _, tk := range Highlight(line) {
			if tk.Text == text {
				if tk.Color != color {
					t.Errorf("token %q: want color %q, got %q", text, color, tk.Color)
				}
				return
			}
		}
		t.Errorf("在 %q 中没找到 token %q", line, text)
	}
	check("func main()", "func", "keyword")
	check("let x = 0", "let", "keyword")
	check("x = 123", "123", "number")
	check("s = \"hi\"", "\"hi\"", "str")
	check("// note", "// note", "comment")
}

func TestHighlightSemanticKinds(t *testing.T) {
	// 断言某行里首次出现的指定文本 token 的颜色。
	colorOf := func(line, text string) string {
		t.Helper()
		for _, tk := range Highlight(line) {
			if tk.Text == text {
				return tk.Color
			}
		}
		t.Fatalf("在 %q 中没找到 token %q", line, text)
		return ""
	}

	// 函数：定义名、调用名、方法名。
	if got := colorOf("func add(a int) {}", "add"); got != "fn" {
		t.Errorf("函数定义名 add: want fn, got %s", got)
	}
	if got := colorOf("x := compute(1)", "compute"); got != "fn" {
		t.Errorf("函数调用 compute: want fn, got %s", got)
	}
	if got := colorOf("s := strings.NewReplacer(\"a\",\"x\")", "NewReplacer"); got != "fn" {
		t.Errorf("方法调用 NewReplacer: want fn, got %s", got)
	}
	// 类型/枚举：enum 后的名字、内置类型、大写类型名。
	if got := colorOf("enum Color { Red }", "Color"); got != "type" {
		t.Errorf("枚举名 Color: want type, got %s", got)
	}
	if got := colorOf("type Node struct {}", "Node"); got != "type" {
		t.Errorf("类型名 Node: want type, got %s", got)
	}
	if got := colorOf("var n int = 1", "int"); got != "type" {
		t.Errorf("内置类型 int: want type, got %s", got)
	}
	// 参数：括号内的参数名。
	if got := colorOf("func f(count int) {}", "count"); got != "param" {
		t.Errorf("参数名 count: want param, got %s", got)
	}
	// 括号：与 punct 区分。
	br := Highlight("f(a)")
	seen := map[string]string{}
	for _, tk := range br {
		if tk.Text == "(" || tk.Text == ")" {
			seen[tk.Text] = tk.Color
		}
	}
	if seen["("] != "bracket" || seen[")"] != "bracket" {
		t.Errorf("括号应为 bracket: %+v", seen)
	}
}

func TestHighlightBracketWide(t *testing.T) {
	// 方括号、花括号同样是 bracket。
	for _, tc := range []struct{ text, want string }{
		{"[", "bracket"}, {"]", "bracket"}, {"{", "bracket"}, {"}", "bracket"},
	} {
		found := false
		for _, tk := range Highlight(tc.text + "x") {
			if tk.Text == tc.text {
				if tk.Color != tc.want {
					t.Errorf("token %q: want %s, got %s", tc.text, tc.want, tk.Color)
				}
				found = true
			}
		}
		if !found {
			t.Errorf("没找到 token %q", tc.text)
		}
	}
}
