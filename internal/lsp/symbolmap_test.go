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

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}
