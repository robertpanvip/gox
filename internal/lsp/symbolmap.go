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
	EnumType string // 成员所属枚举的常量前缀（如 "Color"）；空表示非枚举成员
}

// IsEnumMember 报告该符号是否为枚举成员。
func (s Symbol) IsEnumMember() bool { return s.EnumType != "" }

// SymbolMap 保存一份 gx 源码的符号表。
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
	// T4.3：sig 声明 sig name [Type] = expr；Type 可省略（从 expr 推断）。
	reSignal = regexp.MustCompile(`^\s*sig\s+([a-zA-Z_][\w]*)(\s+[\w\[\]]+)?\s*=`)
)

// inferLitType 从简单字面量/构造式推断类型文本，供 sig hover 显示 Signal[T]（T4.3）。
// 只做高置信推断，推不出返回 ""（hover 显示 Signal 即可，不瞎猜）。
func inferLitType(expr string) string {
	e := strings.TrimSpace(expr)
	switch {
	case e == "true" || e == "false":
		return "bool"
	case strings.HasPrefix(e, `"`) || strings.HasPrefix(e, "`"):
		return "string"
	case strings.HasPrefix(e, "[]"):
		return e[:strings.IndexByte(e, '{')] // []int / []Row
	case strings.HasPrefix(e, "map["):
		if i := strings.IndexByte(e, ']'); i > 0 {
			return e[:i+1] // map[K]
		}
	case regexp.MustCompile(`^-?\d+\.\d+$`).MatchString(e):
		return "float64"
	case regexp.MustCompile(`^-?\d+$`).MatchString(e):
		return "int"
	}
	return ""
}

// BuildSymbolMap 从 gx 源码抽取符号定义（含枚举成员的三拼写索引）。轻量扫描，不求语法精确。
func BuildSymbolMap(src string) *SymbolMap {
	m := &SymbolMap{byName: map[string][]Symbol{}}
	add := func(name string, kind int, line, col int, detail, doc string) Symbol {
		s := Symbol{Name: name, Kind: kind, Line: line, Col: col, Detail: detail, Doc: doc}
		m.Symbols = append(m.Symbols, s)
		m.byName[name] = append(m.byName[name], s)
		return s
	}
	indexAlias := func(alias string, s Symbol) {
		if alias != "" && alias != s.Name {
			m.byName[alias] = append(m.byName[alias], s)
		}
	}

	lines := strings.Split(src, "\n")
	enumPrefix := ""    // 非空表示正处于 enum 块中，值为常量前缀
	enumMemberBase := 0 // enum 声明所在行
	var enumSeg strings.Builder
	addMember := func(seg string, line int) {
		for _, part := range strings.Split(seg, ",") {
			part = strings.TrimSpace(part)
			if part == "" || strings.HasPrefix(part, "//") {
				continue
			}
			if r := reMember.FindStringSubmatch(part); r != nil {
				short := r[1]
				if isGoKeywordLocal(short) {
					continue
				}
				sym := add(short, 20, line, strings.Index(lines[line], short),
					"enum member", "枚举成员 "+short)
				sym.EnumType = enumPrefix
				m.Symbols[len(m.Symbols)-1] = sym
				m.byName[short][len(m.byName[short])-1] = sym
				full := enumPrefix + upperFirstLocal(short)
				indexAlias(full, sym)                  // ColorGreen
				indexAlias(lowerFirstLocal(full), sym) // colorGreen（跨包）
			}
		}
	}
	for i, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		trimmed := strings.TrimLeft(line, " \t")
		col := len(line) - len(trimmed)

		if enumPrefix != "" {
			enumSeg.WriteString(" ")
			enumSeg.WriteString(trimmed)
			if ci := strings.Index(enumSeg.String(), "}"); ci >= 0 {
				// 首行已把 '{' 之后的内容写入 enumSeg，这里直接取 '}' 之前的部分。
				// 若同一行还含 '{'（如 enum x { a, b } 跨到该行才闭合），再剥掉它。
				seg := enumSeg.String()[:ci]
				if oi := strings.Index(seg, "{"); oi >= 0 {
					seg = seg[oi+1:]
				}
				addMember(seg, enumMemberBase)
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
				typeName = upperFirstLocal(name)
			}
			prefix := upperFirstLocal(typeName)
			add(name, 10, i, col+strings.Index(line, name), "enum", "枚举 "+name)
			// 单行 enum { ... }：当场解析成员；多行 enum { ：进入累积状态
			if oi := strings.Index(line, "{"); oi >= 0 {
				if ci := strings.Index(line, "}"); ci > oi {
					memberLine := i
					seg := line[oi+1 : ci]
					enumPrefix = prefix
					enumMemberBase = memberLine
					addMember(seg, memberLine)
					enumPrefix = ""
				} else {
					// 有 '{' 无 '}'：多行 enum，记录已出现的成员文本
					enumPrefix = prefix
					enumMemberBase = i
					enumSeg.Reset()
					enumSeg.WriteString(line[oi+1:])
				}
			} else {
				// 无 '{'（罕见的宽松写法）：同样进入累积状态
				enumPrefix = prefix
				enumMemberBase = i
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
		// T4.3：sig 声明——Detail 带 Signal[T]，hover 直接展示响应式类型。
		if r := reSignal.FindStringSubmatch(line); r != nil {
			name := r[1]
			typ := strings.TrimSpace(r[2])
			if typ == "" {
				// 从 = 右侧表达式推断元素类型。
				if eq := strings.IndexByte(line, '='); eq >= 0 {
					typ = inferLitType(line[eq+1:])
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
		}
		if r := reAssign.FindStringSubmatch(trimmed); r != nil {
			// T4.1：从 := 右侧字面量推断类型填 Detail，hover 展示推导类型。
			typ := ""
			if eq := strings.Index(trimmed, ":="); eq >= 0 && eq+2 < len(trimmed) {
				typ = inferLitType(trimmed[eq+2:])
			}
			detail := ":="
			if typ != "" {
				detail = typ + " (推导)"
			}
			add(r[1], 13, i, col+strings.Index(trimmed, r[1]), detail, "局部变量 "+r[1])
		}
	}
	return m
}

// Lookup 返回名为 name 的所有定义（含枚举成员的三种拼写）。
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

// splitLines 按 \n 切分源码行（保留行内容，去掉 \r 由调用方处理）。
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

// Names 返回所有唯一符号名与枚举成员的三种拼写（供 completion）。
func (m *SymbolMap) Names() []string {
	seen := map[string]bool{}
	var out []string
	for name := range m.byName {
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// ---- 本地小工具（避免符号表依赖 compiler 包）----

func upperFirstLocal(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}

func lowerFirstLocal(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	if r[0] >= 'A' && r[0] <= 'Z' {
		r[0] += 'a' - 'A'
	}
	return string(r)
}

func isGoKeywordLocal(s string) bool {
	switch s {
	case "break", "default", "func", "interface", "select", "case", "defer", "go",
		"map", "struct", "chan", "else", "goto", "package", "switch", "const",
		"fallthrough", "if", "range", "type", "continue", "for", "import",
		"return", "var", "pub", "enum":
		return true
	}
	return false
}
