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

type typeGuard struct {
	start    int    // if 的下标
	end      int    // 块 '{' 的下标（含）
	recv     string // 被守卫标识符（x）
	recvPos  Pos
	typeToks []Token // 目标类型 T 的原始 token（保留位置）
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
	recvIdx := p

	// 接受者后必须是 `is`
	q := nextSig(toks, recvIdx+1)
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

	// 类型之后必须紧跟 ')'（带括号）或 '{'（不带括号），否则视为复合条件，放弃
	after := nextSig(toks, te)
	if after < 0 {
		return typeGuard{}, false
	}
	var brace int
	if parenthesized {
		if toks[after].Text != ")" {
			return typeGuard{}, false
		}
		brace = nextSig(toks, after+1)
		if brace < 0 || toks[brace].Text != "{" {
			return typeGuard{}, false
		}
	} else {
		if toks[after].Text != "{" {
			return typeGuard{}, false
		}
		brace = after
	}

	return typeGuard{
		start:    i,
		end:      brace,
		recv:     toks[recvIdx].Text,
		recvPos:  toks[recvIdx].Pos,
		typeToks: append([]Token(nil), toks[ts:te]...),
	}, true
}

// transformTypeGuard 消费 Analysis，把每个已识别的 is 守卫改写为 comma-ok 断言。
// 守卫的识别与语义（recv / 目标类型）完全来自 an；本函数只负责生成 token。
func transformTypeGuard(in []Token, an *Analysis) []Token {
	out := make([]Token, 0, len(in))
	for i := 0; i < len(in); i++ {
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

		// comma-ok 的第一个返回值：then 块用到 recv 才绑定具名变量并注入 shadow，
		// 否则用 '_' 丢弃，避免 Go 报「declared and not used」。
		bindName := vName
		if !g.ThenUsesRecv {
			bindName = "_"
		}

		// if <bind>, <ok> := <x>.(<T>); <ok> { [x := <bind>;]
		out = append(out,
			tokIdent("if", p), tokSpace(" ", p),
			tokIdent(bindName, p), tokOp(",", p), tokSpace(" ", p),
			tokIdent(okName, p), tokSpace(" ", p), tokOp(":=", p), tokSpace(" ", p),
			tokIdent(g.Recv, g.RecvPos), tokOp(".", p), tokOp("(", p),
		)
		out = append(out, g.TypeToks...)
		out = append(out,
			tokOp(")", p), tokOp(";", p), tokSpace(" ", p),
			tokIdent(okName, p), tokSpace(" ", p), tokOp("{", p),
		)
		if g.ThenUsesRecv {
			out = append(out,
				tokSpace(" ", p),
				tokIdent(g.Recv, g.RecvPos), tokSpace(" ", p), tokOp(":=", p), tokSpace(" ", p),
				tokIdent(vName, p), tokOp(";", p),
			)
		}

		// 跳过原 `if ... {` 区间：用 EndPos 定位块 '{'（不受 runUnion 的下标漂移影响）。
		j := i + 1
		for j < len(in) && !(in[j].Text == "{" && in[j].Pos.Line == g.EndPos.Line && in[j].Pos.Col == g.EndPos.Col) {
			j++
		}
		if j > i {
			i = j // 循环末尾 i++ 后从块内第一条继续
		}
	}
	return out
}
