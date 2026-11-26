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
	at         int // '=>' token 下标
	params     []lambdaParam
	bodyFrom   int    // body 起始 token 下标
	bodyTo     int    // body 结束（开区间）
	endIdx     int    // 整个 lambda 在 token 流中的结束下标（bodyTo-1)
	paramOpen  int    // 参数区起始下标（单参数时即参数本身）
	paramClose int    // 参数区结束下标
	bodyResult string // 推断出的返回类型
	blockBody  bool   // body 是花括号语句块 `() => { ... }`（SolidJS 风格副作用回调）
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
	// 花括号 body `() => { stmt; stmt }`：lambdaBodyEnd 的深度扫描会把整块
	// { ... } 纳入 body（内部的 ) ; 换行因深度>0 不终止）。标记后由
	// buildLambdaParts 生成 `func(){ ... }`（不再包裹 return）。
	// 例外（B8）：`Signal.computed(() => { x: a, y: b })` 的 body 是对象字面量
	// 而非语句块——`x: a` 不是合法 Go 语句。此时不标 blockBody，让它走
	// `return {...}` 并由 objectlit 阶段转成匿名 struct。
	if toks[bodyFrom].Text == "{" && !isObjectLiteralBody(toks, bodyFrom, bodyTo) {
		l.blockBody = true
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
//
// 注意：body 内的函数调用参数逗号因深度 >0 被跳过。
// isObjectLiteralBody 判断 lambda 的花括号 body 是否为对象字面量 `{ k: v, ... }`
// 而非语句块。对象字面量的特征：花括号内首个有效 token 是标识符且紧跟 ':'
// （如 `{ x: a, y: b }`）；语句块首 token 多为关键字 / 声明 / 调用，或含 ';'、':='、
// return 等语句元素。用于 B8：computed(() => { x: a, y: b }) 应生成返回对象的 lambda。
func isObjectLiteralBody(toks []Token, from, to int) bool {
	if from >= to || toks[from].Text != "{" {
		return false
	}
	i := nextSigIn(toks, from+1, to)
	if i < 0 || toks[i].Kind != Ident || isGoKeyword(toks[i].Text) {
		return false
	}
	// 首个标识符后必须紧跟 ':'（标签语句 `L: for` 也满足，但后面是关键字，下面排除）。
	j := nextSigIn(toks, i+1, to)
	if j < 0 || toks[j].Text != ":" {
		return false
	}
	// 含语句分隔符 / 短声明 / return 的不是对象字面量。
	for k := from + 1; k < to && k < len(toks); k++ {
		switch toks[k].Text {
		case ";", ":=", "return", "if", "for", "switch", "var", "const", "defer", "go":
			return false
		}
	}
	// 标签语句 `L: for` / `L: {` 形态：':' 后是控制关键字或 '{'，排除。
	if k := nextSigIn(toks, j+1, to); k >= 0 {
		if isGoKeyword(toks[k].Text) || toks[k].Text == "{" {
			return false
		}
	}
	return true
}

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
func inferLambdaTypes(toks []Token, l *lambdaExpr, an *Analysis, b *Bindings, sigTypes map[string]string) {
	// 上下文函数类型优先：拿到参数类型列表与返回类型。
	ctxParams, ctxResult := inferContextFuncType(toks, l, an, sigTypes)

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

	// 花括号语句块 body：整体作为 func body 原样输出，不返回值。
	// 参数类型仍按上下文/显式标注推断（如 createRoot(o => { ... }) 的 o）。
	if l.blockBody {
		l.bodyResult = "void"
		return
	}

	if l.bodyResult != "" {
		return
	}
	// 无返回值 body（如 () => println(count) / effect 回调）必须先判，
	// 否则会生成非法的 `return println(...)`（println 无返回值）。
	if bodyIsVoid(toks, l, b) {
		l.bodyResult = "void"
		return
	}
	if ctxResult != "" {
		l.bodyResult = ctxResult
		return
	}
	l.bodyResult = inferExprType(toks, l.bodyFrom, l.bodyTo, l.params, sigTypes, b)
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
func bodyIsVoid(toks []Token, l *lambdaExpr, b *Bindings) bool {
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
			if voidBuiltins[callee] || declaredVoidFunc(toks, callee, b) {
				return true
			}
		}
	}
	return inVoidCallbackContext(toks, l)
}

// declaredVoidFunc 扫描本文件是否声明了名为 name 的无返回值函数 func name(...) {...}。
func declaredVoidFunc(toks []Token, name string, b *Bindings) bool {
	// 优先绑定表的原始文本签名索引（含 IsVoid）；未收录时回退旧扫描。
	if isVoid, ok := b.FuncIsVoid(name); ok {
		return isVoid
	}
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
		q = prevSig(toks, q-1)
	} else {
		q = prevSig(toks, q-1)
	}
	if q >= 0 && toks[q].Text == "(" {
		if m := prevSig(toks, q-1); m >= 0 && toks[m].Kind == Ident && voidCallbackNames[toks[m].Text] {
			return true
		}
	}
	return false
}

// inferContextFuncType 尝试从上下文拿到 lambda 的参数类型与返回类型。
// 支持三种来源（优先级从高到低）：
//  1. 集合回调 xs.map(x => ...)（元素类型来自接收者）；
//  2. 函数实参 apply(x => ...)（类型来自被调函数签名的对应参数位）；
//  3. 赋值/初始化 `var f func(int) int = x => ...`（类型来自符号表的声明类型）。
//
// 后两种拿到的是函数类型文本（如 "func(int)int"），用 parseFuncTypeString 拆成
// 参数类型列表与返回类型；联合参数（含 |）在 tokenizeTypeText 处统一擦除为 any。
func inferContextFuncType(toks []Token, l *lambdaExpr, an *Analysis, sigTypes map[string]string) ([]string, string) {
	// 1) 集合回调：向前找 ".map"/".filter"/".reduce" 等方法名，再找接收者元素类型。
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
								params[0] = inferExprType(toks, l.bodyFrom, l.bodyFrom+1, l.params, sigTypes)
							}
						}
						return params, "" // 返回类型交给 body 反推
					}
				}
			}
		}
	}

	if an == nil {
		return nil, ""
	}

	// 2) 函数实参：lambda 参数起点之前是调用的 '('，其前是被调函数名。
	if callOpen := prevSig(toks, l.paramOpen-1); callOpen >= 0 && toks[callOpen].Text == "(" {
		if callee := prevSig(toks, callOpen-1); callee >= 0 && toks[callee].Kind == Ident {
			if sig, ok := an.FuncSig(toks[callee].Text); ok {
				// 数出 lambda 是第几个实参（顶层逗号计数，跳过 lambda 自身参数里的逗号）。
				argIdx, depth := 0, 0
				for i := callOpen + 1; i < l.paramOpen && i < len(toks); i++ {
					switch toks[i].Text {
					case "(", "[", "{":
						depth++
					case ")", "]", "}":
						depth--
					case ",":
						if depth == 0 {
							argIdx++
						}
					}
				}
				if argIdx < len(sig.Params) {
					if p, r, ok := parseFuncTypeString(sig.Params[argIdx].String()); ok {
						return p, r
					}
				}
			}
		}
	}

	// 3) 显式声明赋值：`var f func(int) int = x => ...`。
	// 只认独立 '='（':=' 左边无显式类型，不能提供上下文），且 '=' 之前必须
	// 存在显式 func 类型标注——避免把 `isBig := x => x > 10` 这类表达式反推
	// 的错误类型从邻近符号"借"过来。
	eqIdx := -1
	for i := l.paramOpen - 1; i >= 0; i-- {
		t := toks[i]
		if t.Kind == Space || t.Kind == Newline || t.Kind == Comment {
			continue
		}
		if t.Text == ";" || t.Text == "{" || t.Text == "}" || t.Text == "(" || t.Text == ")" {
			break
		}
		if t.Text == "=" { // 仅独立 '='；':=' 不匹配
			eqIdx = i
			break
		}
		if t.Text == ":=" {
			return nil, "" // 短变量声明无显式类型，交给表达式反推
		}
	}
	if eqIdx >= 0 {
		// 从 '=' 向左找 func 类型标注起点，取其到 '=' 的完整类型文本。
		funcTok := -1
		for i := eqIdx - 1; i >= 0; i-- {
			t := toks[i]
			if t.Kind == Space || t.Kind == Newline || t.Kind == Comment {
				continue
			}
			if t.is("func") {
				funcTok = i
			}
			if t.is("var") || t.Text == ";" || t.Text == "{" || t.Text == "}" {
				break
			}
		}
		if funcTok >= 0 {
			if p, r, ok := parseFuncTypeString(exprText(toks, funcTok, eqIdx)); ok {
				return p, r
			}
		}
	}
	return nil, ""
}

// parseFuncTypeString 把函数类型文本（如 "func(int)int"、"func(a int, b string) error"、
// "func(int,string)(bool,error)"）拆成参数类型列表与返回类型文本。
// 多返回值只取首个作为 lambda 结果（GX lambda 目前只支持单值 body）。
// 不是可识别的函数类型时 ok=false。
func parseFuncTypeString(s string) (params []string, result string, ok bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "func") {
		return nil, "", false
	}
	toks := New(s).AllTokens()
	i := nextSig(toks, 0)
	if i < 0 || toks[i].Text != "func" {
		return nil, "", false
	}
	open := nextSig(toks, i+1)
	if open < 0 || toks[open].Text != "(" {
		return nil, "", false
	}
	close := matchDelim(toks, open, "(", ")")
	if close <= open {
		return nil, "", false
	}
	for _, seg := range splitTopLevelSegs(toks, open+1, close) {
		first := nextSigIn(toks, seg[0], seg[1])
		if first < 0 || toks[first].Kind == EOF {
			continue
		}
		second := nextSigIn(toks, first+1, seg[1])
		typeStart := first
		if second >= 0 && toks[second].Kind != EOF && isTypeStartTok(toks[second]) {
			typeStart = second // 具名参数 `a int`：类型从第二段起
		}
		te := typeRangeEnd(toks, typeStart)
		if te > seg[1] {
			te = seg[1]
		}
		if typ := exprText(toks, typeStart, te); typ != "" {
			params = append(params, typ)
		}
	}
	// 返回类型：')' 之后的首个类型；多返回括号组只取第一段。
	r := nextSig(toks, close+1)
	if r >= 0 && toks[r].Kind != EOF {
		if toks[r].Text == "(" {
			// 括号返回值组 (int, error)：取段内第一个有效 token 到逗号为止。
			rc := matchDelim(toks, r, "(", ")")
			if rc > r {
				firstRet := nextSig(toks, r+1)
				if firstRet >= 0 && firstRet < rc {
					te := typeRangeEnd(toks, firstRet)
					if te > rc {
						te = rc
					}
					result = exprText(toks, firstRet, te)
				}
			}
		} else {
			result = exprText(toks, r, typeRangeEnd(toks, r))
		}
	}
	return params, result, true
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

// signalGetterResultType 判断 [start,end) 是否为「sig getter 单点调用」形态
// （即 body 恰好是 `name()`，例如 `() => count()` 的 body），若是则返回该
// sig 的声明类型（如 count → int）。这是 SolidJS 风格 `computed(() => count())`
// 正确推断返回类型的关键：改写后 count 的读是 count()，单点调用时 inferExprType
// 的通用规则（字面量/比较/算术）都不命中，只能靠改写前采集的 sigTypes 反查。
//
// sigTypes 为 nil 或名字不在表内时返回空，交给其它推断路径。
func signalGetterResultType(toks []Token, start, end int, params []lambdaParam, sigTypes map[string]string) string {
	if len(sigTypes) == 0 || start >= end {
		return ""
	}
	// 去掉首尾空白，要求恰好是 ident ( ) 三个有效 token。
	first := nextSigIn(toks, start, end)
	if first < 0 || toks[first].Kind != Ident {
		return ""
	}
	name := toks[first].Text
	open := nextSigIn(toks, first+1, end)
	if open < 0 || toks[open].Text != "(" {
		return ""
	}
	close := nextSigIn(toks, open+1, end)
	if close < 0 || toks[close].Text != ")" {
		return ""
	}
	if nextSigIn(toks, close+1, end) >= 0 {
		return "" // ')' 后还有 token，不是单点调用
	}
	// 不能是 lambda 参数名（参数类型另有来源），只查 sig 声明类型表。
	for _, p := range params {
		if p.name == name {
			return ""
		}
	}
	return sigTypes[name]
}

// inferExprType 推断 [start,end) 表达式的结果类型。
// b 可选（可变参数）：提供时额外识别「本文件函数调用」「struct 字段访问」
// 两类本可静态确定的 body，避免 lambda 返回类型无谓降级 any。
func inferExprType(toks []Token, start, end int, params []lambdaParam, sigTypes map[string]string, b ...*Bindings) string {
	if start >= end {
		return ""
	}
	// 单点 sig getter 调用（computed(() => count())）优先：从改写前采集的
	// 声明类型表反查，避免降级 any。
	if t := signalGetterResultType(toks, start, end, params, sigTypes); t != "" {
		return t
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
	// 字符串拼接：含任意顶层（depth==0）字符串字面量 → string。
	// 深度追踪：`f(x, "s")` 里的 "s" 在括号内，不能据此断定整个表达式是 string。
	depth = 0
	for i := start; i < end; i++ {
		t := toks[i]
		switch t.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
		if depth <= 0 && (t.Kind == String || t.Kind == Template) {
			return "string"
		}
	}
	// 第一个字面量兜底（仅限顶层 token）：`users[0].name` 的下标 0 在括号内，
	// 不得把整个表达式误判成 int（遗留缺陷——lambda 返回类型误推成下标类型）。
	depth = 0
	for i := start; i < end; i++ {
		t := toks[i]
		switch t.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
		if depth > 0 {
			continue
		}
		if lt := literalType(t); lt != "" {
			return lt
		}
	}
	// 绑定辅助推断：本文件函数调用 / struct 字段访问。
	// 这两类 body 在 GX 组件里极常见（x => makeCell(x)、p => p.Name），
	// 类型其实完全静态可知；此前一律降级 any，导致 lambda 结果类型失真。
	if len(b) > 0 && b[0] != nil {
		if t := inferExprTypeFromBindings(toks, start, end, params, b[0]); t != "" {
			return t
		}
	}
	return ""
}

// inferExprTypeFromBindings 从函数签名表与 struct 字段声明推断表达式类型。
// 只认确定形态，跨包/未知一律放弃（交回调用方降级 any，不误报）：
//  1. `ident(args...)` —— 本文件具名函数调用，取首个返回类型；
//  2. `recv.Field`     —— 已知类型的 lambda 参数/绑定变量的字段访问，取字段声明类型。
//     形态 2 覆盖索引链 `users[0].name`、多级链 `u.inner.name` 与指针 `*T` 解引用：
//     基础类型沿 `[...]`（切片/数组解包）与 `.字段`（struct 字段传播）逐段推进。
func inferExprTypeFromBindings(toks []Token, start, end int, params []lambdaParam, b *Bindings) string {
	first := nextSigIn(toks, start, end)
	if first < 0 || toks[first].Kind != Ident || isGoKeyword(toks[first].Text) {
		return ""
	}

	// 形态 1：函数调用 ident(...)。calledFuncResult 只认起始调用且返回类型已知。
	if t := calledFuncResult(toks, first, end, b); t != "" {
		return t
	}

	// 形态 2：链式访问 recv.field / recv[i].field / recv.a.b。基础类型来源：
	// lambda 参数注解优先，其次 Bindings 里的绑定变量类型（如 users := []User{...}）。
	recvName := toks[first].Text
	base := ""
	for _, p := range params {
		if p.name == recvName && p.typeText != "" {
			base = p.typeText
			break
		}
	}
	if base == "" && b != nil {
		if bd := b.ResolveAt(recvName, toks[first].Pos); bd != nil && bd.Type != "" {
			base = bd.Type
		}
	}
	if base == "" {
		return ""
	}

	// 沿链推进：`[` 跳到匹配的 `]`（切片/数组解包一层），`.field` 取 struct 字段类型。
	i := nextSigIn(toks, first+1, end)
	for i >= 0 {
		switch toks[i].Text {
		case "[":
			// 索引：解包切片/数组元素类型。要求闭合的 ] 且无函数调用形态（`[` 后
			// 出现 `(` 说明是泛型实例化或其他，放弃推断）。
			depth := 0
			close := -1
			for k := i; k < end; k++ {
				switch toks[k].Text {
				case "[":
					depth++
				case "]":
					depth--
					if depth == 0 {
						close = k
					}
				}
				if close >= 0 {
					break
				}
			}
			if close < 0 {
				return ""
			}
			base = sliceElemTypeText(base)
			if base == "" {
				return ""
			}
			i = nextSigIn(toks, close+1, end)
		case ".":
			field := nextSigIn(toks, i+1, end)
			if field < 0 || toks[field].Kind != Ident {
				return ""
			}
			base = strings.TrimSpace(strings.TrimPrefix(base, "*"))
			if base == "" || base == "any" {
				return ""
			}
			ft := structFieldTypeFromToks(toks, base, toks[field].Text)
			if ft == "" {
				return ""
			}
			base = ft
			// 字段必须仍是链的延续（后面还有 . 或 [）或恰好是 body 末尾；
			// 中间出现其他 token（如 `p.Name + 1`）说明不是纯链式访问，放弃——
			// 但这层防御交给循环尾部判断：非 . / [ 的后续 token 直接返回 ""。
			i = nextSigIn(toks, field+1, end)
		default:
			// 链中间出现其他 token（复合表达式）：不是纯链式访问，放弃推断。
			return ""
		}
	}
	// i == -1 表示链已走到 body 末尾（或起点就不是链）。若 recv 后没有 `.`
	// 也没有 `[`（单变量 body），返回 "" 交给单 token 路径。
	if i == -1 && first+1 >= end {
		return ""
	}
	return base
}

// sliceElemTypeText 从 `[]T` / `[N]T` / `[][]T` / `*[]T` 提取元素类型文本。
// 非 slice/array/指向它们的指针返回 ""。
func sliceElemTypeText(t string) string {
	t = strings.TrimSpace(t)
	t = strings.TrimSpace(strings.TrimPrefix(t, "*"))
	if !strings.HasPrefix(t, "[") {
		return ""
	}
	depth := 0
	for i := 0; i < len(t); i++ {
		switch t[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return strings.TrimSpace(t[i+1:])
			}
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
//
// 采用迭代式替换：每轮只替换 paramOpen 最大（最靠右/最内层）的一个 lambda，
// 然后重新 detect。这样下标始终基于最新 token 流，且内层先被转成 func 字面量后，
// 外层 body 里嵌的就是已转换的结果——避免嵌套 lambda 用旧下标替换导致区间错位、
// 内层 => 残留的 bug。
// sigTypes 为可选参数（sig 声明类型表，来自 collectSignalTypes），用于在 lambda
// body 是单点 sig getter 调用时推断返回类型（SolidJS 风格 computed(() => count())）。
// 省略时该推断路径关闭，其它规则照常工作——保持既有调用点兼容。
func transformLambdas(in []Token, an *Analysis, b *Bindings, sigTypes ...map[string]string) []Token {
	var st map[string]string
	if len(sigTypes) > 0 {
		st = sigTypes[0]
	}
	for {
		lambdas := detectLambdas(in)
		if len(lambdas) == 0 {
			return in
		}
		sel := 0
		for i := 1; i < len(lambdas); i++ {
			if lambdas[i].paramOpen > lambdas[sel].paramOpen {
				sel = i
			}
		}
		l := &lambdas[sel]
		inferLambdaTypes(in, l, an, b, st)
		parts := buildLambdaParts(in, l)
		out := make([]Token, 0, len(in)+len(parts))
		out = append(out, in[:l.paramOpen]...)
		out = append(out, parts...)
		rest := l.bodyTo
		// JSX 跨行缩进的排版换行：当 lambda body 末尾是 JSX（如 `() => <div>\n...\n</div>`），
		// lambdaBodyEnd 在 JSX 后的换行处停止，该换行落在生成的 `}` 与宿主闭括号之间，
		// 形成 `f(func() any {...}\n)`——Go 要求参数列表换行前有 trailing comma，否则报
		// "missing ',' before newline in argument list"。这段空白只是 JSX 的视觉缩进：
		// 当且仅当换行后紧跟宿主调用闭合符号（`)` / `]` / `,`）时吞掉；后接标识符或
		// `}`（函数体/语句块结束）说明是真正的语句结束换行，保留——否则会把 return
		// 语句与函数闭合括到同一行、破坏 golden 形态。
		// 第十三轮 P3-D：lambdaBodyEnd 遇到 Comment 即截断，bodyTo 可能停在
		// `</div> // 尾注` 的空格/注释处，而非紧邻 JSX。往前跳过排版空白/注释
		// 确认 body 末尾确实是 JSX，再决定是否吞掉换行——否则带尾注的跨行形态
		// 会把注释留在 `}` 与 `)` 之间，生成 `}// 尾注\n)` 报 missing ','。
		lastBody := l.bodyTo - 1
		for lastBody >= l.bodyFrom &&
			(in[lastBody].Kind == Space || in[lastBody].Kind == Newline || in[lastBody].Kind == Comment) {
			lastBody--
		}
		if !l.blockBody && l.bodyTo > l.bodyFrom && lastBody >= l.bodyFrom && in[lastBody].Kind == JSX {
			j := l.bodyTo
			hasNL := false
			// 第十三轮 P3-D：JSX 后排版区可能夹行注释（`</div> // 尾注` 再换行接
			// `)`）。Comment 一并吞掉，否则它会把随后的 `)` 注释掉、丢失闭合括号。
			for j < len(in) && (in[j].Kind == Space || in[j].Kind == Newline || in[j].Kind == Comment) {
				if in[j].Kind == Newline {
					hasNL = true
				}
				j++
			}
			if hasNL && j < len(in) {
				switch in[j].Text {
				case ")", "]", ",":
					rest = j
				}
			}
		}
		out = append(out, in[rest:]...)
		in = out
	}
}

// buildLambdaParts 生成一个 lambda 对应的 func 字面量 token 序列，
// body 从当前 in 的 [bodyFrom, bodyTo) 拷贝（此时内层 lambda 可能已被转换）。
func buildLambdaParts(in []Token, l *lambdaExpr) []Token {
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

	// 花括号语句块 body：body tokens 自带 { ... }，直接拼在参数后，
	// 不再额外包裹 `{ return ... }`——生成 `func(x T) { stmt; stmt }`。
	if l.blockBody {
		parts = append(parts, tokSpace(" ", p))
		for k := l.bodyFrom; k < l.bodyTo; k++ {
			parts = append(parts, in[k])
		}
		return parts
	}

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
	return parts
}

// tokenizeTypeText 把类型文本（可能是联合 A|B）切成 token。
// 联合类型降级为 any（与 union 擦除一致）。
//
// 注意：New(text).AllTokens() 末尾会带一个 EOF token，且所有 token 的 Pos
// 是「片段局部坐标」（从 1:1 起）。若把这些 token 直接插回主 token 流：
//   - EOF 出现在流中间，会让 lambdaBodyEnd / nextSig 等扫描误判提前终止（H-1 嵌套 lambda 结构错乱）；
//   - 局部坐标会让诊断 sourcemap 回投到第 1 行（M-1）。
//
// 因此这里过滤掉 EOF/Newline，并把每个 token 的 Pos 重设为 lambda 位置。
func tokenizeTypeText(text string, pos Pos) []Token {
	text = strings.TrimSpace(text)
	if text == "" || strings.Contains(text, "|") {
		return []Token{tokIdent("any", pos)}
	}
	out := make([]Token, 0, len(text))
	for _, t := range New(text).AllTokens() {
		if t.Kind == EOF || t.Kind == Newline {
			continue
		}
		t.Pos = pos
		out = append(out, t)
	}
	if len(out) == 0 {
		return []Token{tokIdent("any", pos)}
	}
	return out
}
