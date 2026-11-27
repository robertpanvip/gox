package ui

import "testing"

// TestProjectIconToneMapping WebStorm 风格文件图标按扩展名语义分色。
func TestProjectIconToneMapping(t *testing.T) {
	cases := []struct{ name, want string }{
		{"main.go", "cyan"},
		{"main_test.go", "green"},
		{"README.md", "green"},
		{"package.json", "yellow"},
		{"go.mod", "faint"},
		{"go.sum", "faint"},
		{"app.gx", "purple"},
		{"logo.svg", "orange"},
		{"index.html", "orange"},
		{"LICENSE", "muted"},
	}
	for _, c := range cases {
		if got := projectIconTone(c.name); got != c.want {
			t.Errorf("%s: tone=%q want %q", c.name, got, c.want)
		}
	}
}

// TestProjectIconToneNoPrefixCollision 精确后缀：.md 不得误匹配 .mod 系。
func TestProjectIconToneNoPrefixCollision(t *testing.T) {
	if projectIconTone("x.md") != "green" || projectIconTone("x.mod") != "faint" {
		t.Error(".md 与 .mod 分色不得互相误撞")
	}
}
