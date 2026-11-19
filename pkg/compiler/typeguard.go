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
// 设计要点：
//   - `is` 不占全局关键字，仅在 if 守卫模式中识别；作普通变量名时不受影响。
//   - 被守卫者必须是简单标识符（x），暂不支持 obj.field 等复合左值。
//   - 目标类型 T 复用 typeEnd 解析，支持 float64 / pkg.Type / []T / *T / map[K]V 等。
//   - 只支持「整个条件就是一个 is 守卫」；复合条件（x is T && y）不转换，
//     保留原样交给 Go 解析器报错（GX 不做类型检查）。
//   - 临时变量按 if 的位置命名，保证同一文件内唯一、可复现、无全局状态（并发安全）。
//   - 块首插入 `x := __gx_v` 做 shadow，使分支内 x 具有 T 的类型。
//   - 位置：应在管线最前（lex 之后），使后续层看到的是标准 Go comma-ok 断言。

type typeGuard struct {
	start    int    // if 的下标
	end      int    // 块 '{' 的下标（含）
	recv     string // 被守卫标识符（x）
	recvPos  Pos
	typeToks []Token // 目标类型 T 的原始 token（保留位置）
}

// parseTypeGuard 判断 toks[i] 是否为一个可转换的 is 守卫，并给出改写所需信息。
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

// transformTypeGuard 扫描 token 流，转换所有可识别的 is 守卫。
func transformTypeGuard(in []Token) []Token {
	out := make([]Token, 0, len(in))
	for i := 0; i < len(in); i++ {
		g, ok := parseTypeGuard(in, i)
		if !ok {
			out = append(out, in[i])
			continue
		}

		p := in[g.start].Pos
		suffix := strconv.Itoa(p.Line) + "_" + strconv.Itoa(p.Col)
		vName := "__gx_v_" + suffix
		okName := "__gx_ok_" + suffix

		// if <v>, <ok> := <x>.(<T>); <ok> { x := <v>;
		out = append(out,
			tokIdent("if", p), tokSpace(" ", p),
			tokIdent(vName, p), tokOp(",", p), tokSpace(" ", p),
			tokIdent(okName, p), tokSpace(" ", p), tokOp(":=", p), tokSpace(" ", p),
			tokIdent(g.recv, g.recvPos), tokOp(".", p), tokOp("(", p),
		)
		out = append(out, g.typeToks...)
		out = append(out,
			tokOp(")", p), tokOp(";", p), tokSpace(" ", p),
			tokIdent(okName, p), tokSpace(" ", p), tokOp("{", p),
			tokSpace(" ", p),
			tokIdent(g.recv, g.recvPos), tokSpace(" ", p), tokOp(":=", p), tokSpace(" ", p),
			tokIdent(vName, p), tokOp(";", p),
		)

		i = g.end // 跳过原 'if ... {' 区间；块内其余 token 原样保留
	}
	return out
}
