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
