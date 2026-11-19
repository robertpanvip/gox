package lsp

import "testing"

const sample = `package main

import "fmt"

pub enum color { red, green }

pub type point struct { x int }

pub func greet(name string) {
    fmt.println(name)
}

func main() { greet("gx") }
`

func TestBuildSymbolMap(t *testing.T) {
	sm := BuildSymbolMap(sample)
	want := map[string]bool{"color": false, "point": false, "greet": false, "main": false}
	for _, s := range sm.Symbols {
		if _, ok := want[s.Name]; ok {
			want[s.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("未收集到符号 %q", name)
		}
	}
}

func TestSymbolAt(t *testing.T) {
	sm := BuildSymbolMap(sample)
	lines := splitLines(sample)
	// "greet" 出现在第 9 行（0 基 8） func 声明处与第 12 行（0 基 11）调用处
	sym := sm.At(lines, 8, 10)
	if sym == nil || sym.Name != "greet" {
		t.Fatalf("At(8,10) = %+v, want greet", sym)
	}
	sym2 := sm.At(lines, 12, 14)
	if sym2 == nil || sym2.Name != "greet" {
		t.Fatalf("At(12,14) = %+v, want greet", sym2)
	}
}

func TestNamesAndLookup(t *testing.T) {
	sm := BuildSymbolMap(sample)
	names := sm.Names()
	if len(names) == 0 {
		t.Fatal("Names 为空")
	}
	if len(sm.Lookup("greet")) == 0 {
		t.Fatal("Lookup(greet) 为空")
	}
}

func TestEnumMemberThreeSpellings(t *testing.T) {
	sm := BuildSymbolMap(sample) // pub enum color { red, green }
	for _, spelling := range []string{"green", "ColorGreen", "colorGreen"} {
		syms := sm.Lookup(spelling)
		if len(syms) == 0 {
			t.Errorf("拼写 %q 未命中任何符号", spelling)
			continue
		}
		if !syms[0].IsEnumMember() || syms[0].EnumType != "Color" {
			t.Errorf("拼写 %q 命中的不是 Color 枚举成员: %+v", spelling, syms[0])
		}
	}
}

func TestEnumMemberPrivatePrefix(t *testing.T) {
	src := "package main\nenum status { pending, done }\n"
	sm := BuildSymbolMap(src)
	for _, spelling := range []string{"pending", "StatusPending", "statusPending"} {
		if len(sm.Lookup(spelling)) == 0 {
			t.Errorf("私有 enum 成员拼写 %q 未命中", spelling)
		}
	}
}

func TestFindIdentifierEditsEnumRename(t *testing.T) {
	// 模拟：成员 green 重命名为 yellow -> ColorGreen/ColorYellow, colorGreen/colorYellow
	repl := map[string]string{
		"green":      "yellow",
		"ColorGreen": "ColorYellow",
		"colorGreen": "colorYellow",
	}
	lines := []string{
		"pub enum color { red, green, blue }",
		"func f(c Color) bool { return c == ColorGreen }",
		"import pkg; x := pkg.colorGreen",
	}
	edits := findIdentifierEdits(lines, repl)
	if len(edits) != 3 {
		t.Fatalf("期望 3 处编辑，实际 %d: %+v", len(edits), edits)
	}
	byNew := map[string]int{}
	for _, e := range edits {
		byNew[e.NewText]++
	}
	if byNew["yellow"] != 1 || byNew["ColorYellow"] != 1 || byNew["colorYellow"] != 1 {
		t.Errorf("派生替换不正确: %+v", edits)
	}
}

func TestBuildSymbolMapEmpty(t *testing.T) {
	sm := BuildSymbolMap("")
	if len(sm.Symbols) != 0 {
		t.Errorf("空源码符号表应为空, got %+v", sm.Symbols)
	}
	if len(sm.Names()) != 0 {
		t.Errorf("空源码 Names 应为空, got %v", sm.Names())
	}
}

func TestAtOutOfBounds(t *testing.T) {
	sm := BuildSymbolMap(sample)
	lines := splitLines(sample)
	if sm.At(lines, -1, 0) != nil {
		t.Error("负行号应返回 nil")
	}
	if sm.At(lines, 999, 0) != nil {
		t.Error("越界行号应返回 nil")
	}
	if sm.At(lines, 0, -1) != nil {
		t.Error("负列号应返回 nil")
	}
	// 空白位置无符号
	if sm.At(lines, 1, 0) != nil {
		t.Error("空行应返回 nil")
	}
}

func TestAtUnknownIdentifier(t *testing.T) {
	sm := BuildSymbolMap(sample)
	lines := splitLines(sample)
	// 第 11 行 "import "fmt"" 中的 fmt 非定义符号
	if sym := sm.At(lines, 2, 8); sym != nil {
		t.Errorf("未知标识符不应命中符号, got %+v", sym)
	}
}

func TestSymbolKinds(t *testing.T) {
	sm := BuildSymbolMap(sample)
	want := map[string]int{
		"color": 10, // enum
		"point": 5,  // type
		"greet": 12, // func
		"main":  12, // func
	}
	for name, kind := range want {
		syms := sm.Lookup(name)
		if len(syms) == 0 {
			t.Errorf("未找到符号 %q", name)
			continue
		}
		if syms[0].Kind != kind {
			t.Errorf("%q Kind = %d, want %d", name, syms[0].Kind, kind)
		}
	}
}

func TestNamesIncludeAllSpelling(t *testing.T) {
	sm := BuildSymbolMap(sample)
	names := map[string]bool{}
	for _, n := range sm.Names() {
		names[n] = true
	}
	for _, n := range []string{"green", "ColorGreen", "colorGreen", "red", "ColorRed", "colorRed"} {
		if !names[n] {
			t.Errorf("Names 应包含 %q", n)
		}
	}
}

func TestMultiLineEnumMembers(t *testing.T) {
	src := "package main\npub enum color {\n  red,\n  green,\n  blue,\n}\n"
	sm := BuildSymbolMap(src)
	for _, spelling := range []string{"red", "ColorRed", "colorRed", "blue", "ColorBlue"} {
		if len(sm.Lookup(spelling)) == 0 {
			t.Errorf("多行 enum 成员拼写 %q 未命中", spelling)
		}
	}
}

func TestCRLFSource(t *testing.T) {
	src := "package main\r\npub func greet() {}\r\nfunc main() { greet() }\r\n"
	sm := BuildSymbolMap(src)
	if len(sm.Lookup("greet")) == 0 {
		t.Error("CRLF 源码应正确识别函数符号")
	}
}
