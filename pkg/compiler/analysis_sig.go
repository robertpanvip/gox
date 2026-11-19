package compiler

// =============================================================================
// 函数签名索引
//
// 收集每个函数/方法声明的参数与返回值类型（保留联合，因为此时还没擦除），
// 供 AST 侧的类型检查规则使用：
//   - union-call-arg：调用已知函数时校验实参是否落在联合参数成员内；
//   - union-return  ：函数声明返回联合时校验 return 值是否在成员内。
//
// desugar 后所有联合都变成 any，AST 上无从得知契约；签名索引是唯一的真值来源。
// =============================================================================

// FuncSig 是一个函数声明的签名记录。
type FuncSig struct {
	Name     string
	Params   []Type // 参数类型（按位置；无名参数以 unknown 占位）
	Results  []Type // 返回值类型
	BodyFrom int    // 函数体 token 区间（开区间），用于定位 return 归属
	BodyTo   int
}

// FuncSig 返回名为 name 的函数签名（首个匹配）。
func (a *Analysis) FuncSig(name string) (FuncSig, bool) {
	if a == nil {
		return FuncSig{}, false
	}
	for _, s := range a.sigs {
		if s.Name == name {
			return s, true
		}
	}
	return FuncSig{}, false
}

// FuncSigAtGx 返回包含 GX 坐标 (line,col) 的函数签名——用于 ReturnStmt
// 定位「这条 return 属于哪个函数」。取包含该位置且函数体区间最小的签名。
func (a *Analysis) FuncSigAtGx(line, col int) (FuncSig, bool) {
	if a == nil {
		return FuncSig{}, false
	}
	idx := a.idxNearGx(line, col)
	if idx < 0 {
		return FuncSig{}, false
	}
	best := -1
	bestSpan := a.n + 1
	for i, s := range a.sigs {
		if idx > s.BodyFrom && idx < s.BodyTo {
			if span := s.BodyTo - s.BodyFrom; span < bestSpan {
				bestSpan = span
				best = i
			}
		}
	}
	if best < 0 {
		return FuncSig{}, false
	}
	return a.sigs[best], true
}

// collectSigs 扫描所有 func 声明，建立函数签名索引。
func (a *Analysis) collectSigs(toks []Token) {
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("func") {
			continue
		}
		nameIdx := funcNameAt(toks, i)
		if nameIdx < 0 {
			continue // 匿名函数 / 复杂接收者，跳过（调用侧无法按名字解析）
		}
		open, close, ok := funcParamParens(toks, i)
		if !ok {
			continue
		}
		brace := findFuncBodyBrace(toks, close)
		if brace < 0 {
			continue
		}
		bodyEnd := matchDelim(toks, brace, "{", "}")
		if bodyEnd <= brace {
			continue
		}
		sig := FuncSig{
			Name:     toks[nameIdx].Text,
			Params:   parseSigParams(toks, open, close),
			Results:  parseSigResults(toks, close+1, brace),
			BodyFrom: brace,
			BodyTo:   bodyEnd,
		}
		a.sigs = append(a.sigs, sig)
	}
}

// funcParamParens 定位函数参数列表的 '(' ')'。
// 处理普通函数、方法（首个 '(' 是接收者）两种情况。
func funcParamParens(toks []Token, fi int) (open, close int, ok bool) {
	j := nextSig(toks, fi+1)
	if j < 0 {
		return 0, 0, false
	}
	if toks[j].Text == "(" { // 接收者，或匿名函数参数
		rc := matchDelim(toks, j, "(", ")")
		if rc <= j {
			return 0, 0, false
		}
		after := nextSig(toks, rc+1)
		if after >= 0 && toks[after].Kind == Ident {
			paren := nextSig(toks, after+1)
			if paren >= 0 && toks[paren].Text == "(" { // 方法：after 是方法名，paren 是参数
				close = matchDelim(toks, paren, "(", ")")
				return paren, close, close > paren
			}
		}
		return j, rc, true // 匿名函数：首个 '(' 就是参数
	}
	if toks[j].Kind != Ident {
		return 0, 0, false
	}
	open = nextSig(toks, j+1)
	if open < 0 || toks[open].Text != "(" {
		return 0, 0, false
	}
	close = matchDelim(toks, open, "(", ")")
	return open, close, close > open
}

// parseSigParams 解析参数段为类型列表（联合完整保留）。
func parseSigParams(toks []Token, open, close int) []Type {
	var out []Type
	segStart := open + 1
	for k := open + 1; k <= close; k++ {
		if k == close || toks[k].Text == "," {
			seg := parseParamSeg(toks, segStart, k)
			if seg.typeStart >= 0 {
				te := typeRangeEnd(toks, seg.typeStart)
				if te > k {
					te = k
				}
				out = append(out, typeFromTokens(toks, seg.typeStart, te))
			} else {
				out = append(out, Type{Name: "unknown"})
			}
			segStart = k + 1
		}
	}
	return out
}

// parseSigResults 解析返回值段 [from,to) 为类型列表。
// 支持：单类型（含联合）、括号分组 (int, error)、命名返回 (x int|string)。
func parseSigResults(toks []Token, from, to int) []Type {
	// 去掉尾部的空白/换行
	for to > from && (toks[to-1].Kind == Space || toks[to-1].Kind == Newline || toks[to-1].Kind == Comment) {
		to--
	}
	j := nextSig(toks, from)
	if j < 0 || j >= to {
		return nil
	}
	if toks[j].Text == "(" {
		rc := matchDelim(toks, j, "(", ")")
		if rc > j && rc <= to {
			return parseResultGroup(toks, j+1, rc)
		}
	}
	// 单类型（可能联合）：用 typeRangeEnd 吃完整
	te := typeRangeEnd(toks, j)
	if te > to {
		te = to
	}
	return []Type{typeFromTokens(toks, j, te)}
}

// parseResultGroup 解析括号内返回值列表，逐段取类型（跳过命名返回的名字）。
func parseResultGroup(toks []Token, from, to int) []Type {
	var out []Type
	segStart := from
	for k := from; k <= to; k++ {
		if k == to || toks[k].Text == "," {
			seg := parseParamSeg(toks, segStart, k)
			if seg.typeStart >= 0 {
				te := typeRangeEnd(toks, seg.typeStart)
				if te > k {
					te = k
				}
				out = append(out, typeFromTokens(toks, seg.typeStart, te))
			} else if nameIdx := nextSig(toks, segStart); nameIdx >= 0 && nameIdx < k {
				// 兜底：整段当一个类型名
				out = append(out, typeFromTokens(toks, nameIdx, k))
			}
			segStart = k + 1
		}
	}
	return out
}
