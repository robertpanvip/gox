package compiler

import (
	"fmt"
	"strconv"
	"strings"
)

// enum 转换：把 GX 的 enum 声明展开为原生 Go 的具名类型 + const 组 + String()/Parse()。
//
// 命名规则（前缀式，与 http.MethodGet 一致）：
//   类型 Color + 成员 red  ->  常量 ColorRed、String() 返回 "red"
//   常量前缀一律 upperFirst(类型名)，与 enum 是否 pub 无关
//   pub enum 时类型名也大写；非 pub 时类型名保持小写
//   Parse 函数名 = "Parse" + upperFirst(类型名)，始终导出
//
// 支持三种底层类型：
//   enum X        -> int，无值成员用 iota（首个）/ 省略（后续，自动重复并递增）
//   enum X int    -> 同上
//   enum X string -> 字符串枚举，成员须显式给值（如 get = "GET"）
//   位标志：成员写 read = 1 << iota，后续省略自动重复

type enumMember struct {
	name  string
	pos   Pos
	value []Token
}

type enumInfo struct {
	typeName   string // 生成 Go 的类型名（pub 时大写）
	constName  string // upperFirst(typeName)，常量/Parse 前缀
	underlying string // "int" 或 "string"
	members    []enumMember
	p          Pos
}

func transformEnum(in []Token, names map[string]string) []Token {
	s := &stream{toks: in, pubNames: names}
	out := make([]Token, 0, len(in))
	needStrconv := false
	for i := 0; i < len(s.toks); i++ {
		t := s.toks[i]
		isPub := false
		enumIdx := -1
		if t.is("pub") {
			k := s.ns(i + 1)
			if k < len(s.toks) && s.toks[k].is("enum") {
				isPub = true
				enumIdx = k
			}
		} else if t.is("enum") {
			enumIdx = i
		}
		if enumIdx < 0 {
			out = append(out, t)
			continue
		}
		gen, end, isInt := s.genEnum(enumIdx, isPub)
		out = append(out, gen...)
		if isInt {
			needStrconv = true
		}
		i = end // 跳过 enum ... '}'
	}
	if needStrconv {
		out = ensureImport(out, "strconv")
	}
	return out
}

// genEnum 生成一个 enum 的完整 Go 代码，返回 (tokens, '}' 下标, 是否整数枚举)。
func (s *stream) genEnum(enumIdx int, isPub bool) ([]Token, int, bool) {
	p := s.toks[enumIdx].Pos
	nameIdx := s.ns(enumIdx + 1)
	name := "enum"
	if nameIdx < len(s.toks) && s.toks[nameIdx].Kind == Ident {
		name = s.toks[nameIdx].Text
	}

	// 底层类型：enum X [int|string] { ... }
	underlying := "int"
	braceIdx := s.ns(nameIdx + 1)
	if braceIdx < len(s.toks) && s.toks[braceIdx].Kind == Ident &&
		(s.toks[braceIdx].Text == "int" || s.toks[braceIdx].Text == "string") {
		underlying = s.toks[braceIdx].Text
		braceIdx = s.ns(braceIdx + 1)
	}
	closeIdx := s.findBraceClose(braceIdx)
	members := s.collectMembers(braceIdx, closeIdx)

	typeName := name
	if isPub {
		typeName = upperFirst(name)
		s.recordName(name, typeName) // 裸类型引用还原
	}
	constPrefix := upperFirst(typeName)
	parseName := "Parse" + constPrefix
	// Parse 的裸小写引用也要还原
	s.recordName(lowerFirst(parseName), parseName)

	info := enumInfo{
		typeName: typeName, constName: constPrefix, underlying: underlying,
		members: members, p: p,
	}

	code := genEnumCode(info, namesOf(members))
	// 成员常量的裸引用还原：colorGreen / colorRed -> ColorGreen / ColorRed
	for _, m := range members {
		cn := constPrefix + upperFirst(m.name)
		s.recordName(lowerFirst(cn), cn)
	}
	return genGoCode(code, p), closeIdx, underlying == "int"
}

// namesOf 提取成员名列表（供 String/Parse 的 case 生成）。
func namesOf(ms []enumMember) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.name
	}
	return out
}

// genEnumCode 生成完整 enum 的 Go 源码文本。
func genEnumCode(info enumInfo, memberNames []string) string {
	var b strings.Builder
	p := info.constName
	t := info.typeName

	fmt.Fprintf(&b, "type %s %s\n", t, info.underlying)
	b.WriteString("const (\n")
	// Go 要求带类型的 const spec 显式写初始化表达式；无值成员重复上一表达式
	// （iota / 1 << iota），iota 每行自增。
	lastExpr := "iota"
	for _, m := range info.members {
		cn := p + upperFirst(m.name)
		fmt.Fprintf(&b, "\t%s %s", cn, t)
		val := strings.TrimSpace(valueCode(m.value))
		if info.underlying == "string" {
			if val == "" {
				val = strconv.Quote("")
			}
			fmt.Fprintf(&b, " = %s\n", val)
			continue
		}
		if val != "" {
			lastExpr = val
		}
		fmt.Fprintf(&b, " = %s\n", lastExpr)
	}
	b.WriteString(")\n")

	// String()
	if info.underlying == "string" {
		fmt.Fprintf(&b, "func (x %s) String() string { return string(x) }\n", t)
	} else {
		fmt.Fprintf(&b, "func (x %s) String() string {\n\tswitch x {\n", t)
		for i, m := range info.members {
			cn := p + upperFirst(m.name)
			// 位标志（值含 <<）的组合不单独列 case，交给 default 返回数字
			if strings.Contains(valueCode(m.value), "<<") {
				continue
			}
			fmt.Fprintf(&b, "\tcase %s:\n\t\treturn %s\n", cn, strconv.Quote(memberNames[i]))
		}
		b.WriteString("\t}\n\treturn strconv.Itoa(int(x))\n}\n")
	}

	// Parse()
	parseName := "Parse" + p
	fmt.Fprintf(&b, "func %s(s string) (%s, bool) {\n\tswitch s {\n", parseName, t)
	for _, m := range info.members {
		cn := p + upperFirst(m.name)
		// 字符串枚举用成员的显式值匹配；整数枚举用 String() 返回的名字匹配
		var key string
		if info.underlying == "string" {
			v := strings.TrimSpace(valueCode(m.value))
			if v != "" {
				key = v
			} else {
				key = strconv.Quote(m.name)
			}
		} else {
			key = strconv.Quote(m.name)
		}
		fmt.Fprintf(&b, "\tcase %s:\n\t\treturn %s, true\n", key, cn)
	}
	zero := "0"
	if info.underlying == "string" {
		zero = `""`
	}
	fmt.Fprintf(&b, "\t}\n\treturn %s, false\n}\n", zero)

	return b.String()
}

// valueCode 把值表达式 token 拼回源码文本。
func valueCode(ts []Token) string {
	var b strings.Builder
	for _, t := range ts {
		if t.Kind == Comment {
			continue
		}
		b.WriteString(t.Text)
	}
	return b.String()
}

// genGoCode 把一段 Go 源码文本 lex 成 token，并把位置重定位到 p，
// 使生成代码的 sourcemap 映射回 .gx 的 enum 声明处。
func genGoCode(code string, p Pos) []Token {
	toks := New(code).AllTokens()
	out := make([]Token, 0, len(toks))
	for _, t := range toks {
		if t.Kind == EOF {
			break
		}
		t.Pos = p
		t.IsGen = true
		out = append(out, t)
	}
	return out
}

// ensureImport 在 package 子句之后插入一个带别名的 import（若路径已存在则不重复）。
func ensureImport(toks []Token, path string) []Token {
	quoted := strconv.Quote(path)
	for _, t := range toks {
		if t.Kind == String && t.Text == quoted {
			return toks
		}
	}
	insert := 0
	for i, t := range toks {
		if t.is("package") {
			for j := i + 1; j < len(toks); j++ {
				if toks[j].Kind == Newline {
					insert = j + 1
					break
				}
			}
			break
		}
	}
	p := toks[0].Pos
	imp := []Token{
		tokNewline(p),
		tokIdent("import", p), tokSpace(" ", p), tokString(quoted, p), tokNewline(p),
	}
	out := append([]Token{}, toks[:insert]...)
	out = append(out, imp...)
	out = append(out, toks[insert:]...)
	return out
}

func (s *stream) findBraceClose(open int) int {
	depth := 0
	for i := open; i < len(s.toks); i++ {
		switch s.toks[i].Text {
		case "{":
			depth++
		case "}":
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(s.toks) - 1
}

func (s *stream) collectMembers(open, close int) []enumMember {
	var members []enumMember
	var seg []Token
	depth := 0
	flush := func() {
		idx := 0
		for idx < len(seg) {
			k := seg[idx].Kind
			if k == Space || k == Newline || k == Comment {
				idx++
				continue
			}
			break
		}
		if idx >= len(seg) || seg[idx].Kind != Ident {
			seg = nil
			return
		}
		m := enumMember{name: seg[idx].Text, pos: seg[idx].Pos}
		k := idx + 1
		for k < len(seg) && (seg[k].Kind == Space || seg[k].Kind == Comment) {
			k++
		}
		if k < len(seg) && seg[k].Text == "=" {
			k++
			for k < len(seg) && (seg[k].Kind == Space || seg[k].Kind == Comment || seg[k].Kind == Newline) {
				k++
			}
			m.value = append([]Token{}, seg[k:]...)
		}
		members = append(members, m)
		seg = nil
	}
	for i := open + 1; i < close; i++ {
		t := s.toks[i]
		switch t.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
		if depth == 0 && (t.Text == "," || t.Kind == Newline) {
			flush()
			continue
		}
		seg = append(seg, t)
	}
	flush()
	return members
}
