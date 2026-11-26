package gxcomplete

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func stdAvailable(t *testing.T) {
	t.Helper()
	gr := runtime.GOROOT()
	if gr == "" {
		t.Skip("无 GOROOT")
	}
	if st, err := os.Stat(filepath.Join(gr, "src", "strings")); err != nil || !st.IsDir() {
		t.Skip("无 GOROOT/src/strings")
	}
}

func hasName(cs []Candidate, name string) bool {
	for _, c := range cs {
		if c.Name == name {
			return true
		}
	}
	return false
}

// markOff 返回 src 中 '|' 的字节偏移并去掉该标记。
func markOff(src string) (string, int) {
	i := strings.IndexByte(src, '|')
	if i < 0 {
		return src, -1
	}
	return src[:i] + src[i+1:], i
}

func TestMembersPackageQualifier(t *testing.T) {
	stdAvailable(t)
	src, off := markOff(`package main

import "strings"

func f() {
    s := strings.|
    _ = s
}
`)
	s := New()
	cs := s.Members(src, off)
	if !hasName(cs, "NewReplacer") {
		t.Fatalf("strings. 应含 NewReplacer, got %d 项", len(cs))
	}
}

func TestMembersWithPrefix(t *testing.T) {
	stdAvailable(t)
	src, off := markOff(`package main

import "strings"

func f() {
    s := strings.New|
    _ = s
}
`)
	s := New()
	cs := s.Members(src, off)
	if !hasName(cs, "NewReplacer") || !hasName(cs, "NewReader") {
		t.Fatalf("strings.New 应含 NewReplacer/NewReader, got %d 项", len(cs))
	}
}

func TestMembersLocalVar(t *testing.T) {
	stdAvailable(t)
	src, off := markOff(`package main

import "strings"

func f() {
    b := strings.NewReplacer()
    b.|
    _ = b
}
`)
	s := New()
	cs := s.Members(src, off)
	if !hasName(cs, "Replace") {
		t.Fatalf("b.(Replacer) 应含 Replace, got %d 项", len(cs))
	}
	for _, c := range cs {
		if c.Kind != "method" {
			t.Errorf("Replacer 成员应为 method: %s=%s", c.Name, c.Kind)
		}
	}
}

func TestMembersGxSig(t *testing.T) {
	stdAvailable(t)
	src, off := markOff(`package main

import "strings"

sig name = "x"

func f() {
    b := strings.NewReplacer("a", name)
    b.|
    _ = b
}
`)
	s := New()
	cs := s.Members(src, off)
	if !hasName(cs, "Replace") {
		t.Fatalf("GX sig 声明下 b. 应含 Replace, got %d 项", len(cs))
	}
}

func TestMembersNotAfterDot(t *testing.T) {
	stdAvailable(t)
	// 标记在标识符中间（非点号上下文）→ nil。
	src, off := markOff(`package main

func f() {
    abc|
}
`)
	if cs := New().Members(src, off); cs != nil {
		t.Fatalf("非点号上下文应返回 nil, got %v", cs)
	}
}

func TestMembersAtRuneColumn(t *testing.T) {
	stdAvailable(t)
	// 用行列定位替代字节偏移：第 5 行（0 基），光标在 "    s := strings." 末尾。
	lines := []string{
		"package main",
		"",
		`import "strings"`,
		"",
		"func f() {",
		"    s := strings.",
		"    _ = s",
		"}",
	}
	src := strings.Join(lines, "\n")
	col := len([]rune(lines[5])) // 光标在该行末尾
	cs := New().MembersAt(src, 5, col)
	if !hasName(cs, "NewReplacer") {
		t.Fatalf("MembersAt 应含 NewReplacer, got %d 项", len(cs))
	}
}

func TestRuneOffsetCJK(t *testing.T) {
	src := "第一行\n中文abc def\n第三行"
	// 第 1 行 "中文abc def"，第 6 个 rune 之后（中=0 文=1 a=2 b=3 c=4 space=5）→ 指向 'd'
	off := runeOffset(src, 1, 6)
	if off < 0 || off >= len(src) || src[off] != 'd' {
		t.Fatalf("中文 rune 列换算应指向 'd', off=%d byte=%q", off, src[off])
	}
}
