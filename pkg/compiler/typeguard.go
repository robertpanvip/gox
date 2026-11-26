package compiler

import "strconv"

// typeguard 转换：把 GX 的 `is` 类型守卫转译为 Go 的标准 comma-ok 类型断言。
//
//	// GX 输入                          // 生成的 Go
//	if (x is float64) {           →      if __gx_v_3_5, __gx_ok_3_5 := x.(float64); __gx_ok_3_5 {
//	    fmt.println(x + 1)                  x := __gx_v_3_5
//	}                                        fmt.println(x + 1)
//	                                     }
//
// 职责边界（M2 起）：
//   - 「什么是一个 is 守卫」「守卫目标类型是什么」由 GX 语义层 Analyze 判定，
//     结果存在 Analysis 里；本文件只按 Analysis.GuardAt(pos) 的语义生成代码。
//   - parseTypeGuard 退化为分析层内部的「语法形态识别」，不再被 desugar 直接调用。
//   - 临时变量按 if 位置命名，保证同文件唯一、可复现、无全局状态（并发安全）。
//   - 块首插入 `x := __gx_v` 做 shadow，使 then 分支内 x 具有收窄后的类型。
//   - else 分支：若分析层记录了排除法 shadow（ElseShadowAt），在 else 块首插入
//     `x := x.(<剩余类型>)`，使 x 拥有「联合减去已守卫成员」后的真实类型。
//   - 本阶段应在 runUnion 之后：union 擦除与守卫插入不改变 if / '{' 的原始 Pos，
//     故 GuardAt(pos) / ElseShadowAt(pos) 仍能精确命中（用 Pos 定位，不受下标漂移影响）。
//
// 收窄覆盖面（遗留缺陷修复）：
//   - 复合条件：守卫位于条件首位且后续只有 `&&` 链时支持——
//     `if v is int && len(v) > 0 {` → `if __gx_v, ok := v.(int); ok && len(__gx_v) > 0 {`
//     尾部条件与 then 块内的 recv 读都替换为收窄临时变量。`||` 会破坏收窄语义
//     （守卫为假时另一分支仍可能为真），不支持、保持报错引导拆分。
//   - selector 守卫：`if b.val is int {` —— 断言目标是链表达式；then 块内的
//     `b.val` 读替换为收窄临时变量（写目标 `b.val = ...` 保持原样：字段赋值
//     本身合法，语义是写回字段而非临时变量）。
type typeGuard struct {
	start    int     // if 的下标
	end      int     // 块 '{' 的下标（含）
	recv     string  // 被守卫者文本（selector 链为 "b.val"）
	recvToks []Token // 被守卫者原始 token（单标识符或 selector 链）
	recvPos  Pos
	typeToks []Token // 目标类型 T 的原始 token（保留位置）
	tailToks []Token // 复合条件 `&&` 尾部原始 token（nil 表示简单守卫）
}

// parseSelectorChain 从 toks[p] 起解析 ident(.ident)+ selector 链，返回链尾下标。
// 只接受纯链（不含索引/调用）；`is` 或其他 token 终止链。
func parseSelectorChain(toks []Token, p int, limit int) int {
	last := p
	for {
		d := nextSig(toks, last+1)
		if d < 0 || d >= limit || toks[d].Text != "." {
			return last
		}
		f := nextSig(toks, d+1)
		if f < 0 || f >= limit || toks[f].Kind != Ident || isGoKeyword(toks[f].Text) {
			return last
		}
		last = f
	}
}

// parseTypeGuard 判断 toks[i] 是否为一个语法上可转换的 is 守卫。
// 仅做 token 形态识别，不做类型判定；语义由 analysis.go 的 parseGuardAt 补全。
func parseTypeGuard(toks []Token, i int) (typeGuard, bool) {
	if !toks[i].is("if") {
		return typeGuard{}, false
	}

	p := nextSig(toks, i+1)
	if p < 0 {
		return typeGuard{}, false
	}

	// 可选括号：if (x is T) 或 if x is T
	parenthesized := false
	if toks[p].Text == "(" {
		parenthesized = true
		p = nextSig(toks, p+1)
	}
	if p < 0 || toks[p].Kind != Ident || isGoKeyword(toks[p].Text) || toks[p].Text == "_" {
		return typeGuard{}, false
	}
	// selector 守卫：b.val / o.inner.n —— 纯 ident(.ident)+ 链。
	recvEnd := parseSelectorChain(toks, p, len(toks))

	// 接受者后必须是 `is`
	q := nextSig(toks, recvEnd+1)
	if q < 0 || !toks[q].is("is") {
		return typeGuard{}, false
	}

	// is 后必须是类型表达式
	ts := nextSig(toks, q+1)
	if ts < 0 {
		return typeGuard{}, false
	}
	te := typeEnd(toks, ts)
	if te <= ts {
		return typeGuard{}, false
	}

	// 类型之后：')'（带括号）或 '{'（简单守卫）或 `&&`（复合条件），否则放弃。
	after := nextSig(toks, te)
	if after < 0 {
		return typeGuard{}, false
	}
	var brace int
	tailStart := -1
	if parenthesized {
		if toks[after].Text != ")" {
			return typeGuard{}, false
		}
		after = nextSig(toks, after+1)
		if after < 0 {
			return typeGuard{}, false
		}
	}
	if toks[after].Text == "{" {
		brace = after
	} else if toks[after].Text == "&&" {
		// 复合条件：`&&` 尾部直到块 '{'。尾部出现 `||`（破坏收窄语义）时放弃。
		tailStart = after
		depth := 0
		brace = -1
		for k := after; k < len(toks); k++ {
			switch toks[k].Text {
			case "(", "[":
				depth++
			case ")", "]":
				depth--
			case "||":
				if depth == 0 {
					return typeGuard{}, false
				}
			case "{":
				if depth == 0 {
					brace = k
				}
			}
			if brace >= 0 {
				break
			}
		}
		if brace < 0 {
			return typeGuard{}, false
		}
	} else {
		return typeGuard{}, false
	}

	recvToks := append([]Token(nil), toks[p:recvEnd+1]...)
	recvText := ""
	for k, rt := range recvToks {
		if rt.Kind != Ident {
			continue
		}
		if k > 0 {
			recvText += "."
		}
		recvText += rt.Text
	}
	if recvText == "" {
		return typeGuard{}, false
	}

	var tail []Token
	if tailStart >= 0 {
		tail = append([]Token(nil), toks[tailStart:brace]...)
	}

	return typeGuard{
		start:    i,
		end:      brace,
		recv:     recvText,
		recvToks: recvToks,
		recvPos:  toks[p].Pos,
		typeToks: append([]Token(nil), toks[ts:te]...),
		tailToks: tail,
	}, true
}

// transformTypeGuard 消费 Analysis，把每个已识别的 is 守卫改写为 comma-ok 断言。
// 守卫的识别与语义（recv / 目标类型）完全来自 an；本函数只负责生成 token。
func transformTypeGuard(in []Token, an *Analysis) []Token {
	out := make([]Token, 0, len(in))
	// selector 守卫的 then 块内收窄改写规则：区间内的 recv 链读替换为收窄临时
	// 变量（写目标保持原样）。区间到块 '}' 为止，嵌套守卫各自登记、互不干扰。
	type selRewrite struct {
		from, to int
		recv     []Token
		vName    string
	}
	var pending []selRewrite
	expire := func(i int) {
		keep := pending[:0]
		for _, r := range pending {
			if i <= r.to {
				keep = append(keep, r)
			}
		}
		pending = keep
	}
	// matchChainAt 判断 in[i:] 是否恰好以 recv 链开头（忽略空白），返回链尾下标。
	matchChainAt := func(i int, recv []Token) int {
		k := i
		for _, rt := range recv {
			for k < len(in) && in[k].Kind == Space {
				k++
			}
			if k >= len(in) || in[k].Text != rt.Text {
				return -1
			}
			k++
		}
		return k - 1
	}
	for i := 0; i < len(in); i++ {
		expire(i)
		// selector 收窄改写：命中 recv 链且其后不是赋值/自增目标时，替换为临时变量。
		// 只从有效 token 起匹配——从空白 token 起匹配会吞掉链前的排版空格。
		replaced := false
		if in[i].Kind != Space && in[i].Kind != Newline && in[i].Kind != Comment {
			for _, r := range pending {
				if i < r.from {
					continue
				}
				last := matchChainAt(i, r.recv)
				if last < 0 {
					continue
				}
				n := nextSig(in, last+1)
				if n >= 0 && (isAssignOp(in[n].Text) || compoundAssignOps[in[n].Text] ||
					in[n].Text == "++" || in[n].Text == "--") {
					continue // 写目标：字段赋值本身合法，保持原样
				}
				out = append(out, tokIdent(r.vName, in[i].Pos))
				i = last
				replaced = true
				break
			}
		}
		if replaced {
			continue
		}
		// else 块首的排除法 shadow：if x is T {...} else {...} 中，
		// else 内 x 必为「联合减去已守卫成员」的剩余类型，注入
		// `x := x.(<Narrow>);` 让 x 拥有真实类型（而非擦除后的 any）。
		if in[i].Text == "{" {
			if es, ok := an.ElseShadowAt(in[i].Pos); ok {
				p := in[i].Pos
				out = append(out, in[i]) // 先保留原始的 '{'
				out = append(out,
					tokSpace(" ", p),
					tokIdent(es.Recv, es.RecvPos), tokSpace(" ", p), tokOp(":=", p), tokSpace(" ", p),
					tokIdent(es.Recv, es.RecvPos), tokOp(".", p), tokOp("(", p),
				)
				out = append(out, tokTypeText(es.Narrow.String(), p)...)
				out = append(out, tokOp(")", p), tokOp(";", p))
				continue
			}
		}
		if !in[i].is("if") {
			out = append(out, in[i])
			continue
		}
		g, ok := an.GuardAt(in[i].Pos)
		if !ok {
			out = append(out, in[i])
			continue
		}

		p := g.Pos
		suffix := strconv.Itoa(p.Line) + "_" + strconv.Itoa(p.Col)
		vName := "__gx_v_" + suffix
		okName := "__gx_ok_" + suffix

		// comma-ok 的第一个返回值：条件整体（then 块或 && 尾部）用到 recv 才绑定
		// 具名变量，否则用 '_' 丢弃，避免 Go 报「declared and not used」。
		bindName := vName
		if !g.CondUsesRecv {
			bindName = "_"
		}

		// if <bind>, <ok> := <recv链>.(<T>); ok [&& tail] {
		out = append(out,
			tokIdent("if", p), tokSpace(" ", p),
			tokIdent(bindName, p), tokOp(",", p), tokSpace(" ", p),
			tokIdent(okName, p), tokSpace(" ", p), tokOp(":=", p), tokSpace(" ", p),
		)
		for _, rt := range g.RecvToks {
			out = append(out, rt)
		}
		out = append(out, tokOp(".", p), tokOp("(", p))
		out = append(out, g.TypeToks...)
		out = append(out, tokOp(")", p), tokOp(";", p), tokSpace(" ", p))
		if len(g.TailToks) > 0 {
			// 复合条件：`ok && <tail>`，tail 内的 recv 读替换为收窄临时变量。
			out = append(out, tokIdent(okName, p), tokSpace(" ", p))
			out = append(out, rewriteTailRecv(g.TailToks, g.RecvToks, vName)...)
			out = append(out, tokOp("{", p))
		} else {
			out = append(out, tokIdent(okName, p), tokSpace(" ", p), tokOp("{", p))
		}
		if g.ThenUsesRecv && len(g.RecvToks) == 1 {
			// 单标识符守卫：块首注入 `recv := __gx_v;` shadow。
			out = append(out,
				tokSpace(" ", p),
				tokIdent(g.Recv, g.RecvPos), tokSpace(" ", p), tokOp(":=", p), tokSpace(" ", p),
				tokIdent(vName, p), tokOp(";", p),
			)
		}
		if g.ThenUsesRecv && len(g.RecvToks) > 1 {
			// selector 守卫：then 块内的 recv 链读改走 pending 规则替换。
			brace := jFromEndPos(in, i, g.EndPos)
			if brace > i {
				if close := matchDelim(in, brace, "{", "}"); close > brace {
					pending = append(pending, selRewrite{from: brace + 1, to: close, recv: g.RecvToks, vName: vName})
				}
			}
		}

		// 跳过原 `if ... {` 区间：用 EndPos 定位块 '{'（不受 runUnion 的下标漂移影响）。
		j := jFromEndPos(in, i, g.EndPos)
		if j > i {
			i = j // 循环末尾 i++ 后从块内第一条继续
		}
	}
	return out
}

// jFromEndPos 在 in 中从 i 向后定位块 '{'（按 EndPos 的行/列匹配）。
func jFromEndPos(in []Token, i int, end Pos) int {
	for j := i + 1; j < len(in); j++ {
		if in[j].Text == "{" && in[j].Pos.Line == end.Line && in[j].Pos.Col == end.Col {
			return j
		}
	}
	return i
}

// rewriteTailRecv 把复合条件尾部 token 里的 recv 链读替换为收窄临时变量。
// 写目标（recv 后跟赋值/自增）不替换——守卫分支内的条件尾不会是写目标，但保守处理。
func rewriteTailRecv(tail []Token, recv []Token, vName string) []Token {
	out := make([]Token, 0, len(tail)+8)
	for i := 0; i < len(tail); i++ {
		// 链匹配（只从有效 token 起，忽略空白；从空白起匹配会吞链前空格）
		if tail[i].Kind == Space || tail[i].Kind == Newline || tail[i].Kind == Comment {
			out = append(out, tail[i])
			continue
		}
		k := i
		matched := true
		for _, rt := range recv {
			for k < len(tail) && tail[k].Kind == Space {
				k++
			}
			if k >= len(tail) || tail[k].Text != rt.Text {
				matched = false
				break
			}
			k++
		}
		if matched && len(recv) > 0 {
			n := k
			for n < len(tail) && tail[n].Kind == Space {
				n++
			}
			if n >= len(tail) || !(isAssignOp(tail[n].Text) || compoundAssignOps[tail[n].Text] ||
				tail[n].Text == "++" || tail[n].Text == "--") {
				out = append(out, tokIdent(vName, tail[i].Pos))
				i = k - 1
				continue
			}
		}
		out = append(out, tail[i])
	}
	return out
}
