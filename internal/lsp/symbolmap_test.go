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
