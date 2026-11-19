package compiler

import (
	"sort"
	"strconv"
	"strings"
)

// 联合参数运行时守卫：函数参数声明为联合类型 A|B 时，擦除为 any 会丢失类型约束。
// 为补回契约，在函数入口插入 type switch，动态类型不在成员列表里就 panic：
//
//	// GX                            // 生成的 Go
//	func f(v int | string) {    →    func f(v any) {
//	    ...                              switch v.(type) {
//	}                                    case int, string:
//	                                     default:
//	                                         panic("gx: f: v expects int|string, got " + fmt.Sprintf("%T", v))
//	                                     }
//	                                     ...
//	                                 }
//
// 实现时序（关键）：
//  1. collectUnionGuards 在擦除前扫描原始 token，记录每个联合参数的「位置(Pos)/
//     参数名/成员列表/所属函数体 '{' 的下标」。
//  2. transformUnion 擦除 A|B → any（不改参数名 token 的 Pos）。
//  3. insertUnionGuards 用步骤 1 记录的函数体 '{' 下标插入守卫；因擦除只在类型
//     段内删除 token，函数体 '{' 之后的下标会漂移，故插入按 '{' 下标降序进行，
//     从后往前插入，前面的下标不受影响。
//
// 守卫只加在「函数参数的联合」上；var/type/字段的联合仍纯擦除（没有入口可检查）。

type unionGuard struct {
	param     string   // 参数名
	members   []string // 联合成员类型文本（用于 case 与 panic 消息）
	braceIdx  int      // 函数体 '{' 在原始 token 流中的下标
	funcName  string   // 所属函数名（用于 panic 消息）
	insertPos Pos      // 生成 token 的位置锚点（用 '{' 的位置）
}

// runUnion 执行完整的 union 处理：收集守卫 → 擦除 → 插入守卫。
func runUnion(in []Token) []Token {
	guards := collectUnionGuards(in)
	out := transformUnion(in)
	return insertUnionGuards(out, guards)
}

// collectUnionGuards 在擦除前扫描函数声明，收集联合参数的守卫信息。
func collectUnionGuards(toks []Token) []unionGuard {
	var guards []unionGuard
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("func") {
			continue
		}
		// 找参数列表的 '('，可能先有接收者 (r T)。
		j := nextSig(toks, i+1)
		if j < 0 {
			continue
		}
		if toks[j].Text == "(" {
			recvClose := matchDelim(toks, j, "(", ")")
			after := nextSig(toks, recvClose+1)
			if after >= 0 && toks[after].Kind == Ident {
				// 方法：after 是方法名，再后面是参数的 '('
				j = nextSig(toks, after+1)
			} else {
				// 匿名函数 func(...)：
				j = -1
				// 直接把 j 当作参数括号
				j = recvClose + 1 // 占位，稍后单列处理
			}
		}
		// 定位参数 '('
		var open int
		if toks[j].Text == "(" {
			open = j
		} else if j >= 0 {
			open = nextSig(toks, j+1)
		}
		if open < 0 || open >= len(toks) || toks[open].Text != "(" {
			continue
		}
		close := matchDelim(toks, open, "(", ")")
		if close <= open {
			continue
		}
		// 函数体 '{'：参数 ')' 之后、返回值之后的第一个 '{'
		brace := findFuncBodyBrace(toks, close)
		if brace < 0 {
			continue
		}
		funcName := "?"
		if nameIdx := funcNameAt(toks, i); nameIdx >= 0 {
			funcName = toks[nameIdx].Text
		}

		// 逐参数检查联合类型（按 Go 参数声明规则处理共享类型，如 a, b int|string）
		for _, p := range parseParamBindings(toks, open, close) {
			members := unionMembersAt(toks, p.typeStart, p.typeEnd)
			if len(members) >= 2 {
				guards = append(guards, unionGuard{
					param:     toks[p.nameIdx].Text,
					members:   members,
					braceIdx:  brace,
					funcName:  funcName,
					insertPos: toks[brace].Pos,
				})
			}
		}
	}
	return guards
}

// parseParamSeg 解析单个逗号段：有两个类型起始 token 时第一个是名字、其后是类型；
// 只有一个时视为无名参数（nameIdx=-1）。供 analysis 层的签名索引使用。
// unionguard 不用它——unionguard 需要按 Go 规则处理共享类型，见 parseParamBindings。
func parseParamSeg(toks []Token, start, limit int) paramSegInfo {
	first := nextSig(toks, start)
	if first < 0 || first >= limit || !isTypeStartTok(toks[first]) {
		return paramSegInfo{}
	}
	second := nextSig(toks, first+1)
	if second >= 0 && second < limit && isTypeStartTok(toks[second]) {
		return paramSegInfo{nameIdx: first, typeStart: second, typeEnd: limit}
	}
	return paramSegInfo{nameIdx: -1, typeStart: first, typeEnd: limit}
}

type paramSegInfo struct {
	nameIdx   int
	typeStart int
	typeEnd   int
}

// paramBinding 是一个参数名与其类型的绑定（类型区间为 [typeStart,typeEnd)）。
type paramBinding struct {
	nameIdx   int
	typeStart int
	typeEnd   int
}

// paramSeg 是逗号分割后的一个参数段。
type paramSeg struct {
	names     []int // 该段中的参数名下标（如 "a, b int" 里的 a、b 在拆分前已分到不同段）
	nameIdx   int   // 单一参数名下标，-1 表示没有名字
	typeStart int   // 类型起始，-1 表示本段未写类型（共享后面的类型）
	typeEnd   int
}

// parseParamBindings 解析参数列表，产出每个「参数名 → 类型」绑定。
//
// 关键点：Go 允许 a, b int 这种「多个名字共享一个类型」的写法，也允许
// a, b int | string。按逗号切段后，`a` 段只有名字没类型，`b int|string`
// 段有名字也有类型。这里把「连续的无类型名字段」绑定到其后第一个「有类型段」
// 的类型上，确保共享类型的每个参数都能拿到联合守卫。
func parseParamBindings(toks []Token, open, close int) []paramBinding {
	// 1) 按顶层逗号切段（跳过括号/方括号/花括号内部的逗号）
	var segs [][2]int
	depth := 0
	segStart := open + 1
	for k := open + 1; k <= close; k++ {
		if k == close {
			segs = append(segs, [2]int{segStart, k})
			break
		}
		switch toks[k].Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		case ",":
			if depth == 0 {
				segs = append(segs, [2]int{segStart, k})
				segStart = k + 1
			}
		}
	}

	// 2) 解析每段：找到参数名与类型起点
	parsed := make([]paramSeg, len(segs))
	for i, seg := range segs {
		s, e := seg[0], seg[1]
		first := nextSigIn(toks, s, e)
		if first < 0 || !isTypeStartTok(toks[first]) {
			continue
		}
		// 段内第二个有效 token：若存在且是类型起始，则 first 是名字、其后是类型
		second := nextSigIn(toks, first+1, e)
		if second >= 0 && isTypeStartTok(toks[second]) {
			parsed[i] = paramSeg{nameIdx: first, typeStart: second, typeEnd: e}
		} else {
			// 只有一个标识符：可能是「待共享类型的名字」，也可能是无名参数类型。
			parsed[i] = paramSeg{nameIdx: first, typeStart: -1}
		}
	}

	// 3) 合并共享类型：连续的无类型名字段绑定到其后第一个有类型段。
	//    但若到段尾都没有有类型段，则这些标识符本身是无名参数的类型（跳过）。
	var out []paramBinding
	for i := 0; i < len(parsed); i++ {
		cur := parsed[i]
		if cur.nameIdx < 0 {
			continue
		}
		if cur.typeStart >= 0 {
			out = append(out, paramBinding{cur.nameIdx, cur.typeStart, cur.typeEnd})
			continue
		}
		// 向后找第一个有类型的段，把从 i 开始连续的无类型名字都绑定过去
		j := i
		var names []int
		for j < len(parsed) && parsed[j].nameIdx >= 0 && parsed[j].typeStart < 0 {
			names = append(names, parsed[j].nameIdx)
			j++
		}
		if j < len(parsed) && parsed[j].typeStart >= 0 {
			for _, n := range names {
				out = append(out, paramBinding{n, parsed[j].typeStart, parsed[j].typeEnd})
			}
			// 有类型段本身会在下一轮正常处理，这里只跳过被合并的无类型段
			i = j - 1
		}
		// 若 j 到尾仍无有类型段，这些是无名参数类型，不产出绑定
	}
	return out
}

// nextSigIn 在 [start, limit) 内找第一个非空白/注释 token，越界返回 -1。
func nextSigIn(toks []Token, start, limit int) int {
	for i := start; i < limit && i < len(toks); i++ {
		switch toks[i].Kind {
		case Space, Newline, Comment:
			continue
		}
		return i
	}
	return -1
}

// unionMembersAt 在 [start,end) 内按顶层 '|' 切出联合成员文本；非联合返回 nil。
func unionMembersAt(toks []Token, start, end int) []string {
	if start >= end {
		return nil
	}
	var members []string
	segStart := start
	depth := 0
	for i := start; i < end; i++ {
		t := toks[i]
		switch t.Text {
		case "[", "(":
			depth++
		case "]", ")":
			depth--
		case "|":
			if depth == 0 && t.Kind == Operator {
				m := joinTypeText(toks, segStart, i)
				if m != "" {
					members = append(members, m)
				}
				segStart = i + 1
			}
		}
	}
	if len(members) == 0 {
		return nil
	}
	m := joinTypeText(toks, segStart, end)
	if m != "" {
		members = append(members, m)
	}
	return members
}

func joinTypeText(toks []Token, start, end int) string {
	var b strings.Builder
	for i := start; i < end; i++ {
		t := toks[i]
		if t.Kind == Space || t.Kind == Newline || t.Kind == Comment {
			continue
		}
		b.WriteString(t.Text)
	}
	return b.String()
}

// findFuncBodyBrace 从参数 ')' 的下标之后找函数体的 '{'，跳过返回值类型。
func findFuncBodyBrace(toks []Token, closeParen int) int {
	depth := 0
	for i := closeParen + 1; i < len(toks); i++ {
		switch toks[i].Text {
		case "(", "[":
			depth++
		case ")", "]":
			if depth > 0 {
				depth--
			}
		case "{":
			if depth == 0 {
				return i
			}
			// '{' 在括号中时（如 map[K]struct{}），用配对跳过
			if c := matchDelim(toks, i, "{", "}"); c > i {
				i = c
			}
		}
	}
	return -1
}

// funcNameAt 返回 func 声明的名字下标；方法/匿名函数返回 -1。
func funcNameAt(toks []Token, fi int) int {
	j := nextSig(toks, fi+1)
	if j < 0 {
		return -1
	}
	if toks[j].Text == "(" { // 接收者或匿名函数
		close := matchDelim(toks, j, "(", ")")
		after := nextSig(toks, close+1)
		if after >= 0 && toks[after].Kind == Ident {
			paren := nextSig(toks, after+1)
			if paren >= 0 && toks[paren].Text == "(" { // 方法名
				return after
			}
		}
		return -1
	}
	if toks[j].Kind == Ident {
		return j
	}
	return -1
}

// insertUnionGuards 在擦除后的 token 流上插入守卫。按 '{' 下标降序插入避免漂移。
func insertUnionGuards(in []Token, guards []unionGuard) []Token {
	if len(guards) == 0 {
		return in
	}
	// 按 braceIdx 聚合：同一函数多个联合参数合并到一段守卫块。
	byBrace := map[int][]unionGuard{}
	order := []int{}
	for _, g := range guards {
		if _, ok := byBrace[g.braceIdx]; !ok {
			order = append(order, g.braceIdx)
		}
		byBrace[g.braceIdx] = append(byBrace[g.braceIdx], g)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(order)))

	out := append([]Token(nil), in...)
	for _, brace := range order {
		// 擦除后 '{' 的下标可能已变（前面的类型段被删）；用源位置重新定位。
		idx := findBraceBySourcePos(out, guardsByBrace(byBrace, brace)[0].insertPos, brace)
		if idx < 0 {
			continue
		}
		block := buildGuardBlock(byBrace[brace])
		out = append(out[:idx+1], append(block, out[idx+1:]...)...)
	}
	return out
}

func guardsByBrace(m map[int][]unionGuard, brace int) []unionGuard { return m[brace] }

// findBraceBySourcePos 在擦除后的流里按源 Pos 找 '{'；找不到则回退到原下标（尽力而为）。
func findBraceBySourcePos(toks []Token, p Pos, fallback int) int {
	for i, t := range toks {
		if t.Text == "{" && t.Pos.Line == p.Line && t.Pos.Col == p.Col {
			return i
		}
	}
	if fallback >= 0 && fallback < len(toks) && toks[fallback].Text == "{" {
		return fallback
	}
	return -1
}

// buildGuardBlock 为一个函数体的所有联合参数生成守卫 token 序列。
func buildGuardBlock(gs []unionGuard) []Token {
	p := gs[0].insertPos
	var out []Token
	out = append(out, tokNewline(p))
	for _, g := range gs {
		// switch <param>.(type) {
		out = append(out,
			tokIdent("switch", p), tokSpace(" ", p),
			tokIdent(g.param, p), tokOp(".", p), tokOp("(", p), tokIdent("type", p), tokOp(")", p), tokSpace(" ", p),
			tokOp("{", p), tokNewline(p),
		)
		// case A, B, C:
		out = append(out, tokSpace("    ", p), tokIdent("case", p), tokSpace(" ", p))
		for i, m := range g.members {
			if i > 0 {
				out = append(out, tokOp(",", p), tokSpace(" ", p))
			}
			out = append(out, tokTypeText(m, p)...)
		}
		out = append(out, tokOp(":", p), tokNewline(p))
		// default: panic("gx: <func>: <param> expects <A|B>, got non-matching type")
		// 不打印实际类型：避免引入 fmt 依赖（源程序可能未 import fmt）。
		msg := "gx: " + g.funcName + ": " + g.param + " expects " + strings.Join(g.members, "|") + ", got a non-matching type"
		out = append(out,
			tokSpace("    ", p), tokIdent("default", p), tokOp(":", p), tokNewline(p),
			tokSpace("        ", p), tokIdent("panic", p), tokOp("(", p),
			tokString(strconv.Quote(msg), p), tokOp(")", p), tokNewline(p),
		)
		out = append(out, tokSpace(" ", p), tokOp("}", p), tokNewline(p))
	}
	return out
}

// tokTypeText 把成员类型文本作为若干 token 还原，保留符号结构便于阅读。
// 成员文本可能含 '.', '*', '[', ']' 等，逐字符切成 Operator + Ident 组合。
func tokTypeText(s string, p Pos) []Token {
	var out []Token
	var ident strings.Builder
	flush := func() {
		if ident.Len() > 0 {
			out = append(out, tokIdent(ident.String(), p))
			ident.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r >= 0x80:
			ident.WriteRune(r)
		default:
			flush()
			out = append(out, tokOp(string(r), p))
		}
	}
	flush()
	return out
}
