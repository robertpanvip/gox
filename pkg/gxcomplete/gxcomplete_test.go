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

// markLineCol 返回 src 中 '|' 的 0 基行、0 基 rune 列，并去掉标记。
func markLineCol(src string) (string, int, int) {
	i := strings.IndexByte(src, '|')
	if i < 0 {
		return src, -1, -1
	}
	before := src[:i]
	line := strings.Count(before, "\n")
	lastNL := strings.LastIndexByte(before, '\n')
	col := len([]rune(before[lastNL+1:]))
	return src[:i] + src[i+1:], line, col
}

func TestDefinitionLocal(t *testing.T) {
	stdAvailable(t)
	// 光标放在使用处 "loc|al" 中间，定义是本文件第 4 行（0 基）的局部变量。
	src, line, col := markLineCol(`package main

func f() {
    local := 1
    x := loc|al
    _ = x
}
`)
	s := New()
	d, ok := s.Definition(src, line, col)
	if !ok {
		t.Fatal("本地符号应能求定义")
	}
	if !d.Local {
		t.Fatalf("应识别为本地符号, got %+v", d)
	}
	if d.GxLine != 3 {
		t.Errorf("应跳到 local 声明行(0基 3), got %d", d.GxLine)
	}
}

func TestDefinitionStd(t *testing.T) {
	stdAvailable(t)
	// 光标在 strings.NewReplacer 的 Replacer 末尾。
	src, line, col := markLineCol(`package main

import "strings"

func f() {
    b := strings.NewRepla|cer("a", "x")
    _ = b
}
`)
	s := New()
	d, ok := s.Definition(src, line, col)
	if !ok {
		t.Fatal("std 符号应能求定义")
	}
	if d.Local || d.Pkg != "strings" || d.File == "" || d.Line <= 0 {
		t.Fatalf("应解析为 strings 包声明, got %+v", d)
	}
	if d.Snippet == "" {
		t.Error("std 定义应带声明片段")
	}
}

func TestHoverSignature(t *testing.T) {
	stdAvailable(t)
	src, line, col := markLineCol(`package main

import "strings"

func f() {
    b := strings.NewReplacer("a", "x")
    r := b.Replace|("x", "y")
    _ = r
}
`)
	s := New()
	h, ok := s.Hover(src, line, col)
	if !ok {
		t.Fatal("应能 hover 到签名")
	}
	if h.Expr != "Replace" {
		t.Errorf("标识符应为 Replace, got %q", h.Expr)
	}
	if !strings.Contains(h.Sig, "func(") || !strings.Contains(h.Sig, "string") {
		t.Errorf("签名应是函数签名, got %q", h.Sig)
	}
}

func TestHoverNonIdent(t *testing.T) {
	stdAvailable(t)
	// 光标落在空格之后（非标识符），应返回 false。
	src := "package main\n\nfunc f() { x := 1 } \n"
	// 第 2 行末尾空格后：col = rune 数（含末尾空格），前一字符是空格
	lines := strings.Split(src, "\n")
	col := len([]rune(lines[2]))
	if _, ok := New().Hover(src, 2, col); ok {
		t.Error("空格后不应有 hover")
	}
}

func TestStdSource(t *testing.T) {
	stdAvailable(t)
	s := New()
	d, ok := s.Definition(`package main

import "strings"

func f() {
    _ = strings.NewReplac|er()
}
`, 5, len([]rune("    _ = strings.NewReplacer()"))-len("er()")+2)
	_ = d
	_ = ok
	// 直接按已知包/文件取全文更稳：strings 包必有 strings.go
	src, ok := s.StdSource("strings", "strings.go")
	if !ok || len(src) < 100 {
		t.Fatalf("StdSource(strings,strings.go) 应返回源码全文")
	}
}
