package compiler

import (
	"fmt"
	"go/token"
	"go/types"
	"regexp"
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
		gen, end, _ := s.genEnum(enumIdx, isPub)
		out = append(out, gen...)
		i = end // 跳过 enum ... '}'
	}
	// strconv import 由 ReconcileImports 按实际使用情况统一注入。
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

	// duplicateValue 标记「值与此前某成员相同」的别名成员。别名仍有常量定义，
	// 但不参与 String()/Parse() 的 case 生成——否则 Go 编译器会报重复 case。
	// 别名语义：第一个出现的成员是规范名，String() 返回它，Parse() 也解析到它。
	duplicateValue := computeDuplicateValues(info, memberNames)

	// String()
	if info.underlying == "string" {
		fmt.Fprintf(&b, "func (x %s) String() string { return string(x) }\n", t)
	} else {
		fmt.Fprintf(&b, "func (x %s) String() string {\n\tswitch x {\n", t)
		for i, m := range info.members {
			cn := p + upperFirst(m.name)
			if duplicateValue[i] {
				continue // 值与其他成员相同（别名）：跳过重复 case，规范名取首个
			}
			// 每个成员（含位标志 read=1<<iota 及其省略值的后续成员、乃至组合值
			// rw=read|write）都是独立的具名常量，值互异时都应有自己的 case。
			fmt.Fprintf(&b, "\tcase %s:\n\t\treturn %s\n", cn, strconv.Quote(memberNames[i]))
		}
		b.WriteString("\t}\n\treturn strconv.Itoa(int(x))\n}\n")
	}

	// Parse()
	parseName := "Parse" + p
	fmt.Fprintf(&b, "func %s(s string) (%s, bool) {\n\tswitch s {\n", parseName, t)
	seenKeys := map[string]bool{}
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
		if seenKeys[key] {
			continue // 重复 key：保留第一个 case
		}
		seenKeys[key] = true
		fmt.Fprintf(&b, "\tcase %s:\n\t\treturn %s, true\n", key, cn)
	}
	zero := "0"
	if info.underlying == "string" {
		zero = `""`
	}
	fmt.Fprintf(&b, "\t}\n\treturn %s, false\n}\n", zero)

	return b.String()
}

// iotaIdent 匹配常量表达式里的独立 iota 标识符（不误伤 myiota 等）。
var iotaIdent = regexp.MustCompile(`\biota\b`)

// evalIntConst 求值一个整数常量表达式（iota 已替换为字面量），返回规范值串；
// 求值失败返回空串。用规范值而非表达式文本作键，才能识别 "1<<1" 与 "2" 同值、
// 以及 "=1 后省略成员" 与前面成员同值——这类同值成员若都生成 String() case
// 会导致 Go 编译器报 duplicate case。
func evalIntConst(expr string) string {
	tv, err := types.Eval(token.NewFileSet(), nil, token.NoPos, expr)
	if err != nil || tv.Value == nil {
		return ""
	}
	return tv.Value.String()
}

// computeDuplicateValues 返回每个成员是否与更早成员「值相同」。
//
// 整数枚举：按 Go 的 const 规则计算每个成员的实际值——显式值更新 lastExpr，
// 省略值重复 lastExpr；表达式中的 iota 用成员在 const 组内的行号（从 0）代入，
// 再用 go/types 求值为规范常量串作键。值相同者视为别名，String() 只保留首个 case。
// 字符串枚举：以显式字符串值（含引号）为键；省略值用成员名引号形式。
func computeDuplicateValues(info enumInfo, memberNames []string) []bool {
	out := make([]bool, len(info.members))
	seen := map[string]bool{}
	lastExpr := "iota"
	for i, m := range info.members {
		var key string
		val := strings.TrimSpace(valueCode(m.value))
		if info.underlying == "string" {
			if val == "" {
				val = strconv.Quote(m.name)
			}
			key = val
		} else {
			if val != "" {
				lastExpr = val
			}
			// iota 为 const 组内从 0 开始的行序号；本生成器每成员占一行，故用 i。
			resolved := iotaIdent.ReplaceAllString(lastExpr, strconv.Itoa(i))
			if norm := evalIntConst(resolved); norm != "" {
				key = norm
			} else {
				// 无法求值（含非 Go 常量表达式）时退化为解析后的文本，保守不合并。
				key = "\x00expr#" + resolved
			}
		}
		if seen[key] {
			out[i] = true
		} else {
			seen[key] = true
		}
	}
	return out
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
