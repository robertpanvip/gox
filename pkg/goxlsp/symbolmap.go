package goxlsp

import (
	"regexp"
	"strings"
)

// Symbol 是一个源码符号定义（0 基行/列）。
type Symbol struct {
	Name     string
	Kind     int // 12 func, 5 type, 10 enum, 13 var/sig, 20 enum member
	Line     int
	Col      int
	Detail   string
	Doc      string
	EnumType string
}

// SymbolMap 是单份源码的轻量符号表，供补全 / hover / 跳转使用。
type SymbolMap struct {
	Symbols []Symbol
	byName  map[string][]Symbol
}

var (
	reFunc   = regexp.MustCompile(`\bpub\s+func\s+([a-zA-Z_][\w]*)|^func\s+([a-zA-Z_][\w]*)`)
	reType   = regexp.MustCompile(`\bpub\s+type\s+([a-zA-Z_][\w]*)|^type\s+([a-zA-Z_][\w]*)`)
	reEnum   = regexp.MustCompile(`^\s*(?:pub\s+)?enum\s+([a-zA-Z_][\w]*)(?:\s+(int|string))?`)
	reVar    = regexp.MustCompile(`^\s*(?:pub\s+)?(?:var|const)\s+([a-zA-Z_][\w]*)`)
	reAssign = regexp.MustCompile(`^\s*([a-zA-Z_][\w]*)\s*:=`)
	reMember = regexp.MustCompile(`^\s*([a-zA-Z_][\w]*)`)
	reSig    = regexp.MustCompile(`^\s*sig\s+([a-zA-Z_][\w]*)(\s+[\w\[\]]+)?\s*=`)
	reFloat  = regexp.MustCompile(`^-?\d+\.\d+$`)
	reInt    = regexp.MustCompile(`^-?\d+$`)
)

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func inferType(expr string) string {
	e := strings.TrimSpace(expr)
	switch {
	case e == "true" || e == "false":
		return "bool"
	case strings.HasPrefix(e, `"`) || strings.HasPrefix(e, "`"):
		return "string"
	case strings.HasPrefix(e, "[]"):
		if i := strings.IndexByte(e, '{'); i > 0 {
			return e[:i]
		}
		return e
	case strings.HasPrefix(e, "map["):
		if i := strings.IndexByte(e, ']'); i > 0 {
			return e[:i+1]
		}
	case reFloat.MatchString(e):
		return "float64"
	case reInt.MatchString(e):
		return "int"
	}
	return ""
}

// BuildSymbolMap 从 GX 源码抽取符号定义。轻量扫描，语义精确性够 IDE 用。
func BuildSymbolMap(src string) *SymbolMap {
	m := &SymbolMap{byName: map[string][]Symbol{}}
	add := func(name string, kind int, line, col int, detail, doc string) Symbol {
		s := Symbol{Name: name, Kind: kind, Line: line, Col: col, Detail: detail, Doc: doc}
		m.Symbols = append(m.Symbols, s)
		m.byName[name] = append(m.byName[name], s)
		return s
	}
	alias := func(name string, s Symbol) {
		if name != "" && name != s.Name {
			m.byName[name] = append(m.byName[name], s)
		}
	}

	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	enumPrefix := ""
	enumBase := 0
	var enumSeg strings.Builder
	addMembers := func(seg string, line int) {
		for _, part := range strings.Split(seg, ",") {
			part = strings.TrimSpace(part)
			if part == "" || strings.HasPrefix(part, "//") {
				continue
			}
			r := reMember.FindStringSubmatch(part)
			if r == nil {
				continue
			}
			short := r[1]
			s := add(short, 20, line, strings.Index(lines[line], short), "enum member", "枚举成员 "+short)
			s.EnumType = enumPrefix
			m.Symbols[len(m.Symbols)-1] = s
			m.byName[short][len(m.byName[short])-1] = s
			full := enumPrefix + upperFirst(short)
			alias(full, s)
			alias(lowerFirst(full), s)
		}
	}

	for i, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimLeft(line, " \t")
		col := len(line) - len(trimmed)

		if enumPrefix != "" {
			enumSeg.WriteString(" ")
			enumSeg.WriteString(trimmed)
			if ci := strings.Index(enumSeg.String(), "}"); ci >= 0 {
				seg := enumSeg.String()[:ci]
				if oi := strings.Index(seg, "{"); oi >= 0 {
					seg = seg[oi+1:]
				}
				addMembers(seg, enumBase)
				enumSeg.Reset()
				enumPrefix = ""
			}
			continue
		}

		if r := reEnum.FindStringSubmatch(line); r != nil {
			name := r[1]
			pub := strings.Contains(line[:strings.Index(line, "enum")], "pub")
			typeName := name
			if pub {
				typeName = upperFirst(name)
			}
			prefix := upperFirst(typeName)
			add(name, 10, i, col+strings.Index(line, name), "enum", "枚举 "+name)
			if oi := strings.Index(line, "{"); oi >= 0 {
				if ci := strings.Index(line, "}"); ci > oi {
					addMembers(line[oi+1:ci], i)
				} else {
					enumPrefix = prefix
					enumBase = i
					enumSeg.Reset()
					enumSeg.WriteString(line[oi+1:])
				}
			} else {
				enumPrefix = prefix
				enumBase = i
				enumSeg.Reset()
			}
			continue
		}
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
		if r := reSig.FindStringSubmatch(line); r != nil {
			name := r[1]
			typ := strings.TrimSpace(r[2])
			if typ == "" {
				if eq := strings.IndexByte(line, '='); eq >= 0 {
					typ = inferType(line[eq+1:])
				}
			}
			detail := "Signal"
			if typ != "" {
				detail = "Signal[" + typ + "]"
			}
			add(name, 13, i, col+strings.Index(line, name), detail, "sig "+name)
			continue
		}
		if r := reVar.FindStringSubmatch(trimmed); r != nil {
			add(r[1], 13, i, col+strings.Index(trimmed, r[1]), "var", "变量 "+r[1])
			continue
		}
		if r := reAssign.FindStringSubmatch(trimmed); r != nil {
			detail := ":="
			if eq := strings.Index(trimmed, ":="); eq >= 0 && eq+2 < len(trimmed) {
				if typ := inferType(trimmed[eq+2:]); typ != "" {
					detail = typ + " (推导)"
				}
			}
			add(r[1], 13, i, col+strings.Index(trimmed, r[1]), detail, "局部变量 "+r[1])
		}
	}
	return m
}

// At 返回恰好覆盖 line:col 的符号。
func (m *SymbolMap) At(lines []string, line, col int) *Symbol {
	if line < 0 || line >= len(lines) || col < 0 {
		return nil
	}
	s := lines[line]
	if col > len(s) {
		return nil
	}
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
	syms := m.byName[s[start:end]]
	if len(syms) == 0 {
		return nil
	}
	sym := syms[0]
	return &sym
}

func isIdentByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9' || c >= 0x80
}
