package gxweb

import "testing"

func TestCSSValueBoxShorthand(t *testing.T) {
	tests := []struct {
		key  string
		in   string
		want string
	}{
		{"padding", "8", "8px"},
		{"padding", "7 14", "7px 14px"},
		{"padding", "0 16 12 16", "0px 16px 12px 16px"},
		{"margin", "4 8", "4px 8px"},
		{"border-radius", "6 10", "6px 10px"},
		{"gap", "2 6", "2px 6px"},
		{"padding", "auto", "auto"}, // 非数值原样透传
		{"opacity", "0.5", "0.5"},   // 无量纲
		{"width", "100", "100px"},   // 普通长度
		{"color", "#fff", "#fff"},   // 颜色
	}
	for _, tc := range tests {
		if got := cssValue(tc.key, tc.in); got != tc.want {
			t.Errorf("cssValue(%q, %q) = %q, want %q", tc.key, tc.in, got, tc.want)
		}
	}
}
