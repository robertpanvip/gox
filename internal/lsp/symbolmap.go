package lsp

import (
	"regexp"
	"strings"
)

// Symbol 是一个源码符号定义（位置为 0 基行/列）。
type Symbol struct {
	Name     string
	Kind     int // LSP SymbolKind
	Line     int
	Col      int
	Detail   string
	Doc      string
}

// SymbolMap 保存一份 gx 源码的符号表。
type SymbolMap struct {
	Symbols []Symbol
	byName  map[string][]Symbol
}

var (
	reFunc   = regexp.MustCompile(`\bpub\s+func\s+([a-zA-Z_][\w]*)|^func\s+([a-zA-Z_][\w]*)`)
	reType   = regexp.MustCompile(`\bpub\s+type\s+([a-zA-Z_][\w]*)|^type\s+([a-zA-Z_][\w]*)`)
	reEnum   = regexp.MustCompile(`\b(?:pub\s+)?enum\s+([a-zA-Z_][\w]*)`)
	reVar    = regexp.MustCompile(`^\s*(?:pub\s+)?(?:var|const)\s+([a-zA-Z_][\w]*)`)
	reAssign = regexp.MustCompile(`^\s*([a-zA-Z_][\w]*)\s*:=`)
)

// BuildSymbolMap 从 gx 源码抽取符号定义。轻量扫描，不求语法精确。
func BuildSymbolMap(src string) *SymbolMap {
	m := &SymbolMap{byName: map[string][]Symbol{}}
	add := func(name string, kind int, line, col int, detail, doc string) {
		if name == "" || name == "pub" || name == "func" || name == "type" {
			return
		}
		s := Symbol{Name: name, Kind: kind, Line: line, Col: col, Detail: detail, Doc: doc}
		m.Symbols = append(m.Symbols, s)
		m.byName[name] = append(m.byName[name], s)
	}
	for i, rawLine := range strings.Split(src, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		trimmed := strings.TrimLeft(line, " \t")
		col := len(line) - len(trimmed)
		if r := reFunc.FindStringSubmatch(trimmed); r != nil {
			name := r[1]
			if name == "" {
				name = r[2]
			}
			add(name, 12, i, col+strings.Index(trimmed, name), "func", "函数 "+name)
			continue
		}
		if r := reType.FindStringSubmatch(trimmed); r != nil {
			name := r[1]
			if name == "" {
				name = r[2]
			}
			add(name, 5, i, col+strings.Index(trimmed, name), "type", "类型 "+name)
			continue
		}
		if r := reEnum.FindStringSubmatch(trimmed); r != nil {
			add(r[1], 10, i, col+strings.Index(trimmed, r[1]), "enum", "枚举 "+r[1])
			continue
		}
		if r := reVar.FindStringSubmatch(trimmed); r != nil {
			add(r[1], 13, i, col+strings.Index(trimmed, r[1]), "var", "变量 "+r[1])
		}
		if r := reAssign.FindStringSubmatch(trimmed); r != nil {
			add(r[1], 13, i, col+strings.Index(trimmed, r[1]), ":=", "局部变量 "+r[1])
		}
	}
	return m
}

// Lookup 返回名为 name 的所有定义。
func (m *SymbolMap) Lookup(name string) []Symbol {
	return m.byName[name]
}

// At 返回恰好覆盖 line:col 的符号（标识符粒度近似：按名字长度匹配）。
func (m *SymbolMap) At(lines []string, line, col int) *Symbol {
	if line < 0 || line >= len(lines) || col < 0 {
		return nil
	}
	s := lines[line]
	if col > len(s) {
		return nil
	}
	// 取 col 所在位置的连续标识符
	start := col
	for start > 0 && isIdentByte(s[start-1]) {
		start--
	}
	end := col
	for end < len(s) && isIdentByte(s[end]) {
		end++
	}
	if start == end {
		return nil
	}
	word := s[start:end]
	if syms := m.byName[word]; len(syms) > 0 {
		sym := syms[0]
		return &sym
	}
	return nil
}

func isIdentByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}

// Names 返回所有唯一符号名（供 completion）。
func (m *SymbolMap) Names() []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range m.Symbols {
		if !seen[s.Name] {
			seen[s.Name] = true
			out = append(out, s.Name)
		}
	}
	return out
}
