package app

import (
	"strings"
	"testing"
)

func TestAnsiToLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"plain", "hello\nworld", []string{"hello", "world"}},
		{"crlf", "a\r\nb", []string{"a", "b"}},
		{"sgr kept", "\x1b[31mred\x1b[0m", []string{"\x1b[31mred\x1b[0m"}},
		{"cursor dropped", "a\x1b[1;1Hb\x1b[KC", []string{"abC"}},
		{"clear screen", "a\x1b[2Jb", []string{"b"}},
		{"osc dropped", "\x1b]0;title\x07hello", []string{"hello"}},
		{"backspace", "abc\b\bZ", []string{"aZ"}},
	}
	for _, c := range cases {
		got := ansiToLines([]byte(c.in))
		if len(got) != len(c.want) {
			t.Errorf("%s: lines = %d, want %d (%q)", c.name, len(got), len(c.want), c.in)
			continue
		}
		for i := range got {
			if got[i].text != c.want[i] {
				t.Errorf("%s line%d = %q, want %q", c.name, i, got[i].text, c.want[i])
			}
		}
	}
}

func TestVisibleLen(t *testing.T) {
	if got := visibleLen("\x1b[31mred\x1b[0m"); got != 3 {
		t.Errorf("red len = %d, want 3", got)
	}
	if got := visibleLen("你好"); got != 4 {
		t.Errorf("中文 len = %d, want 4", got)
	}
}

func TestTruncateANSI(t *testing.T) {
	s := "\x1b[32mabcdef\x1b[0m"
	got := truncateANSI(s, 3)
	if !strings.HasPrefix(got, "\x1b[32mabc") {
		t.Errorf("截断后前缀不对: %q", got)
	}
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("截断后未重置 SGR: %q", got)
	}
}

func TestKeyBytes(t *testing.T) {
	tests := []struct {
		key  string
		want []byte
	}{
		{"enter", []byte{'\r'}},
		{"up", []byte{0x1b, '[', 'A'}},
		{"ctrl+c", []byte{0x03}},
	}
	for _, tt := range tests {
		got := keyBytesFromString(tt.key)
		if string(got) != string(tt.want) {
			t.Errorf("key %s = %v, want %v", tt.key, got, tt.want)
		}
	}
}
