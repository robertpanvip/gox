package compiler

import "strings"

// lambda.go — GX Lambda 表达式（最简箭头）降级。
//
// GX 写法：x => x+1            → func(x int) int { return x+1 }
//         (x, y int) => x+y   → func(x int, y int) int { return x+y }
//         (x int|string) => x → func(x any) any { return x }（配合 is 守卫）
//         () => 42            → func() int { return 42 }
//
// 最简箭头缺少参数/返回类型，需要推断。类型来源（优先级从高到低）：
//  1. 参数显式标注 `(x int, y string)` —— 直接用；
//  2. 上下文函数类型 `var f func(int) int = x => ...` / 函数实参 `apply(x => ...)`；
//  3. 集合回调元素类型 `xs.map(x => ...)`；
//  4. 表达式反推 `x => x+1`（数值字面量→int，`len(x)`→int/string 等）。
//
// 返回类型无法从 body 反推时降级 any；参数类型无法推断时降级 any 并给出诊断。
// 生成的 func 字面量 token 流经后续所有 transform（联合擦除 / 外部符号 / 集合）。

type lambdaExpr struct {
	at         int    // '=>' token 下标
	params     []lambdaParam
	bodyFrom   int    // body 起始 token 下标
	bodyTo     int    // body 结束（开区间）
	endIdx     int    // 整个 lambda 在 token 流中的结束下标（bodyTo-1)
	paramOpen  int    // 参数区起始下标（单参数时即参数本身）
	paramClose int    // 参数区结束下标
	bodyResult string // 推断出的返回类型
	posZero    Pos    // lambda 位置
}

type lambdaParam struct {
	name     string
	typeText string // 显式类型（可能为空）
	pos      Pos
}

// detectLambdas 扫描 token 流里的 '=>'，识别每个 lambda 的参数与 body 区间。
func detectLambdas(toks []Token) []lambdaExpr {
	var out []lambdaExpr
	for i := 0; i < len(toks); i++ {
		if toks[i].Text != "=>" || toks[i].Kind != Operator {
			continue
		}
		l := parseLambdaAt(toks, i)
		if l != nil {
			out = append(out, *l)
		}
	}
	return out
}

// parseLambdaAt 解析位于 arrowIdx 的 lambda。返回 nil 表示不是合法 lambda 形态。
func parseLambdaAt(toks []Token, arrowIdx int) *lambdaExpr {
	l := &lambdaExpr{at: arrowIdx, posZero: toks[arrowIdx].Pos}
	// 1) 参数区：箭头左侧
	p := prevSig(toks, arrowIdx-1)
	if p < 0 {
		return nil
	}
	if toks[p].Text == ")" {
		// 括号参数：(x, y int) => ...
		open := matchDelimBack(toks, p, ")")
		if open < 0 {
			return nil
		}
		params, ok := parseLambdaParams(toks, open+1, p)
		if !ok {
			return nil
		}
		l.params = params
		l.paramOpen, l.paramClose = open, p
	} else if toks[p].Kind == Ident && !isGoKeyword(toks[p].Text) {
		// 单参数：x => ...
		l.params = []lambdaParam{{name: toks[p].Text, pos: toks[p].Pos}}
		l.paramOpen, l.paramClose = p, p
	} else {
		return nil
	}

	// 2) body 区：箭头右侧到表达式边界
	bodyFrom := nextSig(toks, arrowIdx+1)
	if bodyFrom < 0 {
		return nil
	}
	bodyTo := lambdaBodyEnd(toks, bodyFrom)
	if bodyTo <= bodyFrom {
		return nil
	}
	l.bodyFrom, l.bodyTo = bodyFrom, bodyTo
	l.endIdx = bodyTo - 1
	return l
}

// lambdaBodyEnd 返回 lambda body 的结束下标（开区间）。
// 从 start 起按括号/花括号深度扫描，遇到深度 0 的顶层分隔符停止：
//   - 逗号（函数实参列表中的多个实参）
//   - 右圆/方/花括号（lambda 处于某个括号内）
//   - 分号 / 换行（语句结束）
// 注意：body 内的函数调用参数逗号因深度 >0 被跳过。
func lambdaBodyEnd(toks []Token, start int) int {
	depth := 0
	for i := start; i < len(toks); i++ {
		t := toks[i]
		switch t.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth == 0 {
				return i
			}
			depth--
		case ",":
			if depth == 0 {
				return i
			}
		case ";":
			if depth == 0 {
				return i
			}
		}
		if (t.Kind == Newline || t.Kind == Comment) && depth == 0 {
			return i
		}
		if t.Kind == EOF {
			return i
		}
	}
	return len(toks)
}

// parseLambdaParams 解析括号内的参数列表 [open+1, close)。
// 支持 `x`, `x, y`, `x int, y string`, `x int|string`。
func parseLambdaParams(toks []Token, start, close int) ([]lambdaParam, bool) {
	var out []lambdaParam
	for _, seg := range splitTopLevelSegs(toks, start, close) {
		first := nextSigIn(toks, seg[0], seg[1])
		if first < 0 {
			continue
		}
		second := nextSigIn(toks, first+1, seg[1])
		if second >= 0 && isTypeStartTok(toks[second]) {
			// 名字 + 类型
			te := typeRangeEnd(toks, second)
			if te > seg[1] {
				te = seg[1]
			}
			out = append(out, lambdaParam{
				name:     toks[first].Text,
				typeText: exprText(toks, second, te),
				pos:      toks[first].Pos,
			})
		} else {
			// 只有名字
			out = append(out, lambdaParam{name: toks[first].Text, pos: toks[first].Pos})
		}
	}
	return out, len(out) > 0 || start == close
}

// matchDelimBack 从右括号 close 向左找配对左括号。
func matchDelimBack(toks []Token, close int, r string) int {
	open := map[string]string{")": "(", "]": "[", "}": "{"}[r]
	d := 0
	for i := close; i >= 0; i-- {
		switch toks[i].Text {
		case r:
			d++
		case open:
			d--
			if d == 0 {
				return i
			}
		}
	}
	return -1
}

// inferLambdaTypes 为一个 lambda 推断参数类型与返回类型。
// an 可为 nil（无 Analysis 时只用显式标注 + 表达式反推）。
func inferLambdaTypes(toks []Token, l *lambdaExpr, an *Analysis) {
	// 上下文函数类型优先：拿到参数类型列表与返回类型。
	ctxParams, ctxResult := inferContextFuncType(toks, l, an)

	for i := range l.params {
		p := &l.params[i]
		if p.typeText != "" {
			continue // 显式标注
		}
		if i < len(ctxParams) && ctxParams[i] != "" {
			p.typeText = ctxParams[i]
			continue
		}
		// 表达式反推
		p.typeText = inferParamTypeFromBody(toks, l, p.name)
		if p.typeText == "" {
			p.typeText = "any"
		}
	}

	if l.bodyResult != "" {
		return
	}
	// 无返回值 body（如 () => println(count) / effect 回调）必须先判，
	// 否则会生成非法的 `return println(...)`（println 无返回值）。
	if bodyIsVoid(toks, l) {
		l.bodyResult = "void"
		return
	}
	if ctxResult != "" {
		l.bodyResult = ctxResult
		return
	}
	l.bodyResult = inferExprType(toks, l.bodyFrom, l.bodyTo, l.params)
	if l.bodyResult == "" {
		l.bodyResult = "any"
	}
}

// voidBuiltins 是调用后不返回任何值的 Go 内建函数。
// 作为 lambda body 时必须生成 func(){...} 而非 return <call>（后者非法）。
var voidBuiltins = map[string]bool{
	"print": true, "println": true, "panic": true,
	"delete": true, "close": true,
}

// voidCallbackNames 是期望无返回值回调（func()/func(T)）的上下文调用名。
var voidCallbackNames = map[string]bool{
	"effect": true, "Effect": true,
}

// bodyIsVoid 判断 lambda body 是否应生成无返回值的 func(){...}。
// 1) body 顶层是已知无返回值调用（println / 本文件声明的无返回值函数）；
// 2) lambda 处于 effect 这类 void 回调上下文中。
func bodyIsVoid(toks []Token, l *lambdaExpr) bool {
	first := nextSigIn(toks, l.bodyFrom, l.bodyTo)
	if first >= 0 && toks[first].Kind == Ident {
		callee := toks[first].Text
		n := nextSigIn(toks, first+1, l.bodyTo)
		// selector 调用 a.b()：callee 取 b
		if n >= 0 && toks[n].Text == "." {
			if m := nextSigIn(toks, n+1, l.bodyTo); m >= 0 && toks[m].Kind == Ident {
				callee = toks[m].Text
				n = nextSigIn(toks, m+1, l.bodyTo)
			}
		}
		if n >= 0 && toks[n].Text == "(" {
			if voidBuiltins[callee] || declaredVoidFunc(toks, callee) {
				return true
			}
		}
	}
	return inVoidCallbackContext(toks, l)
}

// declaredVoidFunc 扫描本文件是否声明了名为 name 的无返回值函数 func name(...) {...}。
func declaredVoidFunc(toks []Token, name string) bool {
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("func") {
			continue
		}
		j := nextSig(toks, i+1)
		if j < 0 || toks[j].Text != name {
			continue
		}
		op := nextSig(toks, j+1)
		if op < 0 || toks[op].Text != "(" {
			continue
		}
		cl := matchDelim(toks, op, "(", ")")
		if cl <= op {
			continue
		}
		if k := nextSig(toks, cl+1); k >= 0 && toks[k].Text == "{" {
			return true
		}
	}
	return false
}

// inVoidCallbackContext 判断 lambda 是否直接位于某个 void 回调调用的参数位置。
func inVoidCallbackContext(toks []Token, l *lambdaExpr) bool {
	// () => ... : paramOpen 即 '('，其前是调用名（如 effect）
	p := prevSig(toks, l.paramOpen)
	if p >= 0 && toks[p].Kind == Ident && voidCallbackNames[toks[p].Text] {
		return true
	}
	// (x) => ... / x => ... : 调用名在外层 '(' 之前
	q := l.paramOpen
	if toks[q].Text == "(" {
		q = prevSig(toks, q - 1)
	} else {
		q = prevSig(toks, q - 1)
	}
	if q >= 0 && toks[q].Text == "(" {
		if m := prevSig(toks, q-1); m >= 0 && toks[m].Kind == Ident && voidCallbackNames[toks[m].Text] {
			return true
		}
	}
	return false
}

// inferContextFuncType 尝试从上下文拿到 lambda 的参数类型与返回类型。
// 支持：集合回调 xs.map(x => ...)、函数实参 apply(x => ...)、
// 赋值/初始化 `var f func(int) int = x => ...`。
func inferContextFuncType(toks []Token, l *lambdaExpr, an *Analysis) ([]string, string) {
	// 集合回调：向前找 ".map"/".filter"/".reduce" 等方法名，再找接收者元素类型。
	if p := prevSig(toks, l.paramOpen-1); p >= 0 && toks[p].Text == "(" {
		method := prevSig(toks, p-1)
		if method >= 0 && toks[method].Kind == Ident {
			dot := prevSig(toks, method-1)
			if dot >= 0 && toks[dot].Text == "." {
				recv := prevSig(toks, dot-1)
				if recv >= 0 && toks[recv].Kind == Ident {
					if et := inferCollectionElemType(toks, recv, an); et != "" {
						params := make([]string, len(l.params))
						for i := range params {
							params[i] = et
						}
						// reduce 的第一参数是累加器（初值类型），第二参数才是元素
						if toks[method].Text == "reduce" || toks[method].Text == "Reduce" {
							if len(params) >= 2 {
								params[0] = inferExprType(toks, l.bodyFrom, l.bodyFrom+1, l.params)
							}
						}
						return params, "" // 返回类型交给 body 反推
					}
				}
			}
		}
	}
	return nil, ""
}

// inferCollectionElemType 从接收者变量的类型标注/初始化推断元素类型。
func inferCollectionElemType(toks []Token, recvIdx int, an *Analysis) string {
	name := toks[recvIdx].Text
	// 找 `name := ...` 或 `name []T` 或 `var name []T`
	for i := 0; i < len(toks); i++ {
		if toks[i].Kind != Ident || toks[i].Text != name {
			continue
		}
		j := nextSig(toks, i+1)
		if j < 0 {
			continue
		}
		// name := []T{...} / name = []T{...}
		if toks[j].Text == ":=" || toks[j].Text == "=" {
			k := nextSig(toks, j+1)
			if k >= 0 && toks[k].Text == "[" {
				close := matchDelim(toks, k, "[", "]")
				if close > k && close == k+1 { // 空方括号 []T（切片）
					te := typeEnd(toks, close+1)
					if te > close+1 {
						return exprText(toks, close+1, te)
					}
				}
			}
		}
	}
	return ""
}

// inferParamTypeFromBody 从 body 中该参数的使用方式反推类型。
func inferParamTypeFromBody(toks []Token, l *lambdaExpr, name string) string {
	for i := l.bodyFrom; i < l.bodyTo; i++ {
		t := toks[i]
		if t.Kind != Ident || t.Text != name {
			continue
		}
		// len(x) / len(x) > 0
		p := prevSig(toks, i-1)
		n := nextSig(toks, i+1)
		if p >= 0 && toks[p].Text == "(" && n >= 0 && toks[n].Text == ")" {
			callName := prevSig(toks, p-1)
			if callName >= 0 && toks[callName].is("len") {
				return "string" // 无法确定是 string 还是 slice，默认 string
			}
		}
		// x + <literal> / <literal> + x 等二元运算：取另一操作数类型
		if n >= 0 && n < l.bodyTo {
			switch toks[n].Text {
			case "+", "-", "*", "/", "%", "<", ">", "<=", ">=", "==", "!=":
				other := nextSig(toks, n+1)
				if other >= 0 && other < l.bodyTo {
					if lt := literalType(toks[other]); lt != "" && lt != "bool" {
						return lt
					}
				}
			}
		}
		if p >= 0 && p >= l.bodyFrom {
			switch toks[p].Text {
			case "+", "-", "*", "/", "%":
				other := prevSig(toks, p-1)
				if other >= 0 && other >= l.bodyFrom {
					if lt := literalType(toks[other]); lt != "" && lt != "bool" {
						return lt
					}
				}
			}
		}
	}
	return ""
}

// inferExprType 推断 [start,end) 表达式的结果类型。
func inferExprType(toks []Token, start, end int, params []lambdaParam) string {
	if start >= end {
		return ""
	}
	// 顶层二元比较 → bool
	depth := 0
	for i := start; i < end; i++ {
		t := toks[i]
		switch t.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		case "<", ">", "<=", ">=", "==", "!=", "&&", "||":
			if depth == 0 {
				return "bool"
			}
		}
	}
	// 顶层算术：取数值字面量类型
	depth = 0
	for i := start; i < end; i++ {
		t := toks[i]
		switch t.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		case "+", "-", "*", "/", "%":
			if depth == 0 {
				// 先扫描两侧字面量
				r := inferExprTypeSide(toks, i+1, end)
				if r != "" {
					return r
				}
				l := inferExprTypeSide(toks, start, i)
				if l != "" {
					return l
				}
				// 再扫描两侧已知参数的类型（如 a + b，a/b 都是 int）
				if rt := paramTypeInRange(toks, i+1, end, params); rt != "" {
					return rt
				}
				if lt := paramTypeInRange(toks, start, i, params); lt != "" {
					return lt
				}
			}
		}
	}
	// 单 token：字面量 / 参数 / 调用
	if end-start == 1 {
		if lt := literalType(toks[start]); lt != "" {
			return lt
		}
		if toks[start].Kind == Ident {
			for _, p := range params {
				if p.name == toks[start].Text && p.typeText != "" {
					return p.typeText
				}
			}
		}
	}
	// len(...) → int
	if toks[start].is("len") {
		return "int"
	}
	// 字符串拼接：含任意字符串字面量 → string
	for i := start; i < end; i++ {
		if toks[i].Kind == String || toks[i].Kind == Template {
			return "string"
		}
	}
	// 第一个字面量兜底
	for i := start; i < end; i++ {
		if lt := literalType(toks[i]); lt != "" {
			return lt
		}
	}
	return ""
}

func inferExprTypeSide(toks []Token, start, end int) string {
	for i := start; i < end; i++ {
		if lt := literalType(toks[i]); lt != "" {
			return lt
		}
	}
	return ""
}

// paramTypeInRange 在 [start,end) 内找第一个已知类型的参数并返回其类型。
func paramTypeInRange(toks []Token, start, end int, params []lambdaParam) string {
	for i := start; i < end; i++ {
		if toks[i].Kind != Ident {
			continue
		}
		for _, p := range params {
			if p.name == toks[i].Text && p.typeText != "" && p.typeText != "any" {
				return p.typeText
			}
		}
	}
	return ""
}

// literalType 返回单个字面量 token 的 Go 类型名。
func literalType(t Token) string {
	switch t.Kind {
	case Number:
		if strings.ContainsAny(t.Text, ".eEpP") {
			return "float64"
		}
		return "int"
	case String, Template:
		return "string"
	case Rune:
		return "rune"
	}
	if t.is("true") || t.is("false") {
		return "bool"
	}
	return ""
}

// transformLambdas 把所有 lambda 改写为 func 字面量。
// 必须在 Analyze 之后（需要 sigs），union/typeguard 之前（body 里的联合/is 仍能被处理）。
func transformLambdas(in []Token, an *Analysis) []Token {
	lambdas := detectLambdas(in)
	if len(lambdas) == 0 {
		return in
	}
	for i := range lambdas {
		inferLambdaTypes(in, &lambdas[i], an)
	}

	// 生成替换 token：从后往前替换，避免下标漂移。
	type repl struct{ from, to int; toks []Token }
	var reps []repl
	for _, l := range lambdas {
		p := in[l.at].Pos
		var parts []Token
		parts = append(parts, tokIdent("func", p), tokOp("(", p))
		for i, param := range l.params {
			if i > 0 {
				parts = append(parts, tokOp(",", p))
			}
			parts = append(parts, tokIdent(param.name, p), tokSpace(" ", p))
			parts = append(parts, tokenizeTypeText(param.typeText, p)...)
		}
		parts = append(parts, tokOp(")", p))
		isVoid := l.bodyResult == "void"
		if !isVoid && l.bodyResult != "" {
			parts = append(parts, tokSpace(" ", p))
			parts = append(parts, tokenizeTypeText(l.bodyResult, p)...)
		}
		parts = append(parts, tokSpace(" ", p), tokOp("{", p))
		if !isVoid {
			parts = append(parts, tokIdent("return", p), tokSpace(" ", p))
		}
		for k := l.bodyFrom; k < l.bodyTo; k++ {
			parts = append(parts, in[k])
		}
		parts = append(parts, tokOp("}", p))

		from := l.paramOpen
		to := l.bodyTo
		reps = append(reps, repl{from: from, to: to, toks: parts})
	}
	// 按 from 降序替换
	for k := len(reps) - 1; k >= 0; k-- {
		r := reps[k]
		out := append([]Token(nil), in[:r.from]...)
		out = append(out, r.toks...)
		out = append(out, in[r.to:]...)
		in = out
	}
	return in
}

// tokenizeTypeText 把类型文本（可能是联合 A|B）切成 token。
// 联合类型降级为 any（与 union 擦除一致）。
func tokenizeTypeText(text string, pos Pos) []Token {
	text = strings.TrimSpace(text)
	if text == "" {
		return []Token{tokIdent("any", pos)}
	}
	if strings.Contains(text, "|") {
		return []Token{tokIdent("any", pos)}
	}
	return New(text).AllTokens()
}
