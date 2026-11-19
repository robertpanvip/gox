package compiler

// defaults.go — 函数参数默认值（TS 语义）。
//
// GX 写法：func greet(name string = "world", times int = 1) { ... }
// 调用 greet()          → greet("world", 1)
// 调用 greet("gx")      → greet("gx", 1)
// 调用 greet("gx", 3)   → 原样
//
// 核心：Go 没有默认值，GX 在「调用点补全缺失的尾部实参」。
//  1) collectDefaults：扫描 func 声明，记录每个参数位是否有默认值及其表达式 token，
//     并把声明参数列表里的 `= expr` 标记 Skip（使生成的 Go 签名合法）。
//  2) expandDefaults：扫描调用点 f(...)，数顶层实参；若实参数 < 形参数，且缺失的
//     尾部参数位均有默认值，则在 ')' 前插入对应默认值表达式。
//
// 补全发生在管线早期（analyze 之后、union 之前），因此默认值表达式里的 GX 语法
// （外部符号小写调用 / JSX / 模板 / 集合链式）会被后续所有 transform 处理。
//
// 限制（弱类型按名字匹配所固有，与 external 层一致）：
//   - 只补全「尾部连续」的缺失参数；中间无默认值的参数不补（保持原样）；
//   - 方法调用 obj.f(...) 不补全（避免同名方法误伤），仅顶层函数调用 f(...)；
//   - 同名局部遮蔽、函数值间接调用不补全。

// paramDefault 是一个参数位的默认值信息。
type paramDefault struct {
	hasDefault bool
	def        []Token // 默认值表达式 token（Skip 已清除的副本）
}

// funcDefault 是一个带默认值函数的签名信息。
type funcDefault struct {
	name   string
	params []paramDefault
}

// collectDefaults 扫描所有 func 声明：记录默认值并把声明里的 `= expr` 标记 Skip。
// 返回「函数名 → 签名信息」（仅收集至少有一个默认值的函数）。
func collectDefaults(in []Token) map[string]*funcDefault {
	out := map[string]*funcDefault{}
	for i := 0; i < len(in); i++ {
		if !in[i].is("func") {
			continue
		}
		nameIdx := funcNameAt(in, i)
		if nameIdx < 0 {
			continue // 匿名函数/复杂接收者
		}
		open, close, ok := funcParamParens(in, i)
		if !ok {
			continue
		}
		segs := splitTopLevelSegs(in, open+1, close)
		fd := &funcDefault{name: in[nameIdx].Text}
		changed := false
		for _, seg := range segs {
			eq := findDefaultEq(in, seg[0], seg[1])
			pd := paramDefault{}
			if eq >= 0 {
				def := cleanDefaultTokens(in, eq+1, seg[1])
				if len(def) > 0 {
					pd.hasDefault = true
					pd.def = def
					changed = true
					// 把 `=` 到段尾（含前导空白）标记 Skip，声明只留参数名与类型。
					markSkipFrom(in, eq, seg[1])
				}
			}
			fd.params = append(fd.params, pd)
		}
		if changed {
			out[fd.name] = fd
		}
	}
	return out
}

// splitTopLevelSegs 按顶层逗号把 [start,limit) 切成若干 [segStart,segEnd) 段，
// 跳过括号/方括号/花括号内部的逗号。空参数列表返回空切片。
func splitTopLevelSegs(toks []Token, start, limit int) [][2]int {
	var segs [][2]int
	depth := 0
	segStart := start
	any := false
	for k := start; k < limit; k++ {
		switch toks[k].Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth > 0 {
				depth--
			}
		case ",":
			if depth == 0 {
				segs = append(segs, [2]int{segStart, k})
				segStart = k + 1
				any = false
			}
		default:
			if toks[k].Kind != Space && toks[k].Kind != Newline && toks[k].Kind != Comment {
				any = true
			}
		}
	}
	if segStart < limit || any {
		segs = append(segs, [2]int{segStart, limit})
	}
	return segs
}

// findDefaultEq 在段 [start,limit) 内找顶层的默认值赋值符 '='（深度 0 且非 ==/=> 等）。
// 返回 -1 表示该段没有默认值。
func findDefaultEq(toks []Token, start, limit int) int {
	depth := 0
	for k := start; k < limit; k++ {
		t := toks[k]
		switch t.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth > 0 {
				depth--
			}
		case "=":
			if depth == 0 && t.Kind == Operator {
				return k
			}
		}
	}
	return -1
}

// cleanDefaultTokens 取 [start,limit) 内的有效 token 作为默认值表达式副本：
// 去掉首尾空白/换行/注释与尾随逗号，清除 Skip 标志。
func cleanDefaultTokens(toks []Token, start, limit int) []Token {
	first, last := -1, -1
	for k := start; k < limit; k++ {
		switch toks[k].Kind {
		case Space, Newline, Comment:
			continue
		}
		if toks[k].Text == "," {
			continue
		}
		if first < 0 {
			first = k
		}
		last = k
	}
	if first < 0 {
		return nil
	}
	out := make([]Token, 0, last-first+1)
	for k := first; k <= last; k++ {
		switch toks[k].Kind {
		case Space, Newline, Comment:
			continue
		}
		if toks[k].Text == "," {
			continue
		}
		t := toks[k]
		t.Skip = false
		out = append(out, t)
	}
	return out
}

// markSkipFrom 把 [start,limit) 内所有 token 标记 Skip（从 start 的 '=' 开始）。
func markSkipFrom(toks []Token, start, limit int) {
	for k := start; k < limit && k < len(toks); k++ {
		toks[k].Skip = true
	}
}

// expandDefaults 扫描调用点，按签名表补全缺失的尾部实参。
func expandDefaults(in []Token, defs map[string]*funcDefault) []Token {
	if len(defs) == 0 {
		return in
	}
	type insertion struct {
		at    int // 在 in[at] 之前插入
		toks  []Token
	}
	var ins []insertion
	for i := 0; i < len(in); i++ {
		t := in[i]
		if t.Kind != Ident || t.IsGen {
			continue
		}
		fd, ok := defs[t.Text]
		if !ok {
			continue
		}
		// 排除声明本身：前一个有效 token 是 func。
		if p := prevSig(in, i-1); p >= 0 && in[p].is("func") {
			continue
		}
		// 排除 selector 调用 obj.f(...) 与方法调用，避免同名误伤。
		if p := prevSig(in, i-1); p >= 0 && in[p].Text == "." {
			continue
		}
		paren := nextSig(in, i+1)
		if paren < 0 || in[paren].Text != "(" {
			continue
		}
		close := matchDelimSkipSpace(in, paren, "(", ")")
		if close <= paren {
			continue
		}
		argc := len(splitTopLevelSegs(in, paren+1, close))
		want := len(fd.params)
		if argc >= want {
			continue
		}
		// 需要补 want-argc 个，取最后 want-argc 个参数位；必须全部有默认值。
		var add []Token
		ok2 := true
		for pos := argc; pos < want; pos++ {
			pd := fd.params[pos]
			if !pd.hasDefault {
				ok2 = false
				break
			}
			if len(add) > 0 || argc > 0 {
				add = append(add, tokOp(",", in[close].Pos))
			}
			add = append(add, pd.def...)
		}
		if !ok2 || len(add) == 0 {
			continue
		}
		ins = append(ins, insertion{at: close, toks: add})
	}
	if len(ins) == 0 {
		return in
	}
	// 从后往前插入，避免下标漂移。
	out := append([]Token(nil), in...)
	for k := len(ins) - 1; k >= 0; k-- {
		x := ins[k]
		out = append(out[:x.at], append(x.toks, out[x.at:]...)...)
	}
	return out
}

// stripDefaults 收集默认值并返回「声明处已物理删除默认值」的干净 token 流。
// 必须在 Analyze 之前调用：否则 Analyze 的参数类型扫描会把 `= default`
// 误当作类型的一部分（如 int|string=5）。默认值表达式本身记录在 defs 中，
// 稍后由 expandDefaults 插回调用点，仍会流经后续所有 transform。
//
// 这里真正删除 token（而非仅标记 Skip）：Analyze/typeEnd 等扫描器不检查 Skip，
// 只有 emit 检查，故必须物理移除才能让分析层看到干净的参数列表。
func stripDefaults(in []Token) ([]Token, map[string]*funcDefault) {
	marked := append([]Token(nil), in...)
	defs := collectDefaults(marked)
	clean := make([]Token, 0, len(marked))
	for _, t := range marked {
		if t.Skip {
			continue
		}
		clean = append(clean, t)
	}
	return clean, defs
}

// transformDefaults 是默认值变换的入口：先收集/剥离声明，再补全调用点。
// 供 CompileStage 等无需提前 Analyze 的场景使用。
func transformDefaults(in []Token) []Token {
	out, defs := stripDefaults(in)
	return expandDefaults(out, defs)
}
