package compiler

import "strings"

// prevSigIn 在 [from, limit) 区间内从 end 向前找第一个有效 token（跳过空白/注释）。
func prevSigIn(toks []Token, end, from int) int {
	for i := end; i >= from; i-- {
		if toks[i].Kind != Space && toks[i].Kind != Newline && toks[i].Kind != Comment {
			return i
		}
	}
	return -1
}

// typeinfer.go — 从 token 流反推标识符/表达式的类型文本。
//
// 供对象字面量字段类型推断（{ a: i } → struct{ a int }）与模板字符串整数
// 插值特化（`${i}` → strconv.Itoa(i)）共用。
//
// 全部查询都基于「当前 token 流」向前扫描声明，不依赖 Analysis 的旧下标
// （desugar 层会改写 token 流，Analysis 的下标基于改写前的快照，会漂移）。

// lookupIdentType 是 A 类词法类型查询的统一入口（取代散落的 inferIdentType）。
// 优先走绑定表 TypeAtIdx（Pos 稳定、O(1)、天然处理遮蔽）；绑定表未推断出类型时
// 回退旧扫描逻辑，保证迁移期间零行为差异。所有 transform 经此入口查询。
func lookupIdentType(b *Bindings, toks []Token, name string, useIdx int) string {
	if b != nil {
		if t := b.TypeAtIdx(toks, name, useIdx); t != "" {
			return t
		}
	}
	return inferIdentType(toks, name, useIdx)
}

// inferIdentType 推断标识符 name 在 useIdx 处的类型文本。
// 返回 "" 表示无法推断（调用方降级为 any / fmt.Sprint）。
// 优先级：函数形参 → var 显式类型 → := 右侧表达式 → range 元素。
// 保留为 lookupIdentType 的回退路径；绑定表覆盖全部场景后可在对拍确认下移除。
func inferIdentType(toks []Token, name string, useIdx int) string {
	// 1) 函数形参：在所有 func 声明里找名为 name 的参数，取最近的一个。
	if t := inferIdentFromParams(toks, name, useIdx); t != "" {
		return t
	}
	// 2) var 显式类型 / := 右侧字面量：找 useIdx 之前最近的 `name T` 或 `name := expr`。
	if t := inferIdentFromDecl(toks, name, useIdx); t != "" {
		return t
	}
	// 3) for name := range xs：从集合元素类型推断。
	if t := inferIdentFromRange(toks, name, useIdx); t != "" {
		return t
	}
	return ""
}

// inferIdentFromParams 扫描 func 声明的参数列表，找名为 name 的形参类型。
func inferIdentFromParams(toks []Token, name string, useIdx int) string {
	best := ""
	bestDecl := -1
	for i := 0; i < useIdx && i < len(toks); i++ {
		if !toks[i].is("func") {
			continue
		}
		open, close, ok := funcParamParens(toks, i)
		if !ok || close > useIdx {
			continue
		}
		for _, seg := range splitTopLevelSegs(toks, open+1, close) {
			ps := parseParamSeg(toks, seg[0], seg[1])
			if ps.nameIdx < 0 || toks[ps.nameIdx].Text != name || ps.nameIdx >= useIdx {
				continue
			}
			te := typeRangeEnd(toks, ps.typeStart)
			if te > seg[1] {
				te = seg[1]
			}
			if te > ps.typeStart && ps.nameIdx > bestDecl {
				best = exprText(toks, ps.typeStart, te)
				bestDecl = ps.nameIdx
			}
		}
	}
	return best
}

// inferIdentFromDecl 找 useIdx 之前最近的 `name := expr` / `var name T` 声明，
// 从右侧表达式或显式类型推断 name 的类型。
func inferIdentFromDecl(toks []Token, name string, useIdx int) string {
	bestDecl := -1
	bestType := ""
	for i := 0; i < useIdx && i < len(toks); i++ {
		if toks[i].Kind != Ident || toks[i].Text != name {
			continue
		}
		// var name T [= ...]
		if p := prevSig(toks, i-1); p >= 0 && toks[p].is("var") {
			k := nextSig(toks, i+1)
			if k < 0 || k >= useIdx {
				continue
			}
			if toks[k].Text == "=" {
				continue // var name = expr：从 expr 推断，走下面的 := 分支逻辑
			}
			te := typeRangeEnd(toks, k)
			if te > k && i > bestDecl {
				bestDecl = i
				bestType = exprText(toks, k, te)
			}
			continue
		}
		// name := expr 或 name = expr：从右侧首个表达式推断
		n := nextSig(toks, i+1)
		if n < 0 || n >= useIdx || (toks[n].Text != ":=" && toks[n].Text != "=") {
			continue
		}
		exprFrom := nextSig(toks, n+1)
		if exprFrom < 0 || exprFrom >= useIdx {
			continue
		}
		// 表达式到语句结束（分号 / 换行 / 顶层逗号）
		exprTo := exprEndInStmt(toks, exprFrom, useIdx)
		t := inferExprLitType(toks, exprFrom, exprTo)
		if t != "" && i > bestDecl {
			bestDecl = i
			bestType = t
		}
	}
	return bestType
}

// inferIdentFromRange 处理 `for name := range xs` / `for _, name := range xs`：
// 从 xs 推断元素类型。支持 []T / 数组 / string（rune）。
func inferIdentFromRange(toks []Token, name string, useIdx int) string {
	for i := 0; i < useIdx && i < len(toks); i++ {
		if !toks[i].is("range") {
			continue
		}
		// 找 `for ... := range xs` 里的 name（在 := 左边）
		colonEq := prevSig(toks, i-1)
		if colonEq < 0 || toks[colonEq].Text != ":=" {
			continue
		}
		// := 左边可能有 `k, name`，确认 name 在其中
		foundName := false
		for j := colonEq - 1; j >= 0 && toks[j].Text != "for"; j-- {
			if toks[j].Kind == Ident && toks[j].Text == name {
				foundName = true
				break
			}
		}
		if !foundName {
			continue
		}
		xsIdx := nextSig(toks, i+1)
		if xsIdx < 0 || xsIdx >= useIdx || toks[xsIdx].Kind != Ident {
			continue
		}
		if t := inferIdentType(toks, toks[xsIdx].Text, xsIdx); t != "" {
			// []T / [N]T / string → 元素类型
			return sliceElemType(t)
		}
	}
	return ""
}

// exprEndInStmt 返回表达式 [from,end) 的结束下标：到分号、换行、顶层逗号或 useIdx。
func exprEndInStmt(toks []Token, from, limit int) int {
	depth := 0
	for i := from; i < limit; i++ {
		t := toks[i]
		switch t.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth > 0 {
				depth--
			}
		case ",", ";":
			if depth == 0 {
				return i
			}
		}
		if t.Kind == Newline && depth == 0 {
			return i
		}
	}
	return limit
}

// inferExprLitType 从字面量表达式推断类型：数字/字符串/rune/bool 及其简单运算。
// 只处理编译期可静态判定的字面量，复杂表达式返回 ""。
func inferExprLitType(toks []Token, from, to int) string {
	if from >= to {
		return ""
	}
	// 具名复合字面量 TypeName{...}（含 &TypeName{...}）→ 返回类型名。
	// 这是 struct signal 字段读写解包判定 receiver 类型的关键依据：
	// `p := CardProps{...}` 必须推断出 p 的类型是 CardProps。
	// 注意排除 map[K]V{...} / []T{...} 等：它们的起点是 `map`/`[` 而非裸 Ident。
	nameIdx := from
	if toks[nameIdx].Text == "&" {
		nameIdx = nextSigIn(toks, from+1, to)
	}
	if nameIdx >= 0 && toks[nameIdx].Kind == Ident && !isGoKeyword(toks[nameIdx].Text) {
		if brace := nextSigIn(toks, nameIdx+1, to); brace >= 0 && toks[brace].Text == "{" {
			// 确认这个 '{' 确实是复合字面量体（有匹配的 '}'，或至少是起始），
			// 而不是 `if x {` 之类——但此处 from 是表达式起点，前面不会出现控制关键字，
			// 且调用方（:= 右侧）确保是值上下文，故可直接认定。
			return toks[nameIdx].Text
		}
	}
	// 单个标识符：再查一次它的声明（支持链式 i := j; j := 0）
	if to-from == 1 && toks[from].Kind == Ident {
		if toks[from].is("true") || toks[from].is("false") {
			return "bool"
		}
		if toks[from].is("nil") {
			return ""
		}
		return inferIdentType(toks, toks[from].Text, from)
	}
	// 泛型调用 Get[T](...) / pkg.Get[T](...) → 从显式类型实参提取 T（B1）。
	// 必须早于「含字符串 → string」：Get[[]Row](props, "rows") 的参数含字符串
	// 字面量，旧扫描会把 rows 误推成 string，进而 <For> 元素推成 rune。
	// 该形态很窄（'(' 前是类型实参的 ']'），不会误伤索引 rows[0] 或普通调用。
	if t := genericCallTypeArg(toks, from, to); t != "" {
		return t
	}
	// 函数字面量 func(...) T { ... }（B3）：rows := func() []Row {...} 时，
	// GX 把 rows 当 getter（读时 ()），绑定类型应记其返回值 T（[]Row），
	// 供 <For each={rows}> 推断元素 Row。也必须早于字符串扫描——函数体里的
	// 字符串字面量会让 rows 误推成 string → rune。
	if t := funcLitResultType(toks, from, to); t != "" {
		return t
	}
	// 切片/数组复合字面量：[]T{...} / [N]T{...} → []T / [N]T。
	// 必须早于「含字符串 → string」：元素里可能带字符串字面量（如 []User{{1,"Tom"}}）。
	if typ := compositeLitType(toks, from, to); typ != "" {
		return typ
	}
	// bool 字面量（表达式含 true/false 且是简单比较）
	for i := from; i < to; i++ {
		if toks[i].is("true") || toks[i].is("false") {
			return "bool"
		}
	}
	// 含字符串字面量 → string
	for i := from; i < to; i++ {
		if toks[i].Kind == String || toks[i].Kind == Template {
			return "string"
		}
	}
	// 浮点 → float64；整数 → int
	for i := from; i < to; i++ {
		if toks[i].Kind == Number && strings.ContainsAny(toks[i].Text, ".eEpP") {
			return "float64"
		}
	}
	for i := from; i < to; i++ {
		if toks[i].Kind == Number {
			return "int"
		}
	}
	// rune 字面量
	if to-from == 1 && toks[from].Kind == Rune {
		return "rune"
	}
	return ""
}

// funcLitResultType 从函数字面量 `func(...) T { ... }` 提取返回类型文本 T（B3）。
// 用于 `rows := func() []Row {...}`：rows 是 getter，绑定类型记返回值 []Row，
// <For each={rows}> 才能推断元素 Row（而非函数体字符串导致的 string/rune）。
// 只认单个返回类型（无括号多返回值），且用 go/parser 校验为合法 Go 类型。
func funcLitResultType(toks []Token, from, to int) string {
	if from >= to || toks[from].Text != "func" {
		return ""
	}
	lp := nextSigIn(toks, from+1, to)
	if lp < 0 || toks[lp].Text != "(" {
		return ""
	}
	rp := matchDelim(toks, lp, "(", ")")
	if rp < 0 || rp >= to {
		return ""
	}
	// 返回类型：')' 之后到 '{' 之间的 token；多返回值（以 '(' 开头）暂不处理。
	rstart := nextSigIn(toks, rp+1, to)
	if rstart < 0 || toks[rstart].Text == "{" || toks[rstart].Text == "(" {
		return ""
	}
	rend := rstart
	depth := 0
	for rend < to {
		txt := toks[rend].Text
		if txt == "{" && depth == 0 {
			break // 函数体起始
		}
		switch txt {
		case "[", "(":
			depth++
		case "]":
			depth--
		case ")":
			if depth > 0 {
				depth--
			}
		}
		rend++
	}
	typ := exprText(toks, rstart, rend)
	if !isValidGoTypeExpr(typ) {
		return ""
	}
	return typ
}

// genericCallTypeArg 从泛型调用 `Get[T](args...)` / `pkg.Get[T](args...)` 的显式
// 类型实参提取 T 的文本（如 []Row、int、map[string]int）。用于 B1：
// `rows := gxscene.Get[[]Row](...)` 的参数含字符串字面量，字符串扫描会把 rows
// 误推成 string，进而让 <For> 元素类型退化成 rune。
//
// 判据（足够窄，不误伤索引 rows[0] 或普通调用 makeP("x")）：
//   - 在 [from,to) 内找第一个 '('（调用参数列表起点）；
//   - '(' 的前一个非空白 token 必须是 ']'（类型实参的闭合括号）；
//   - 用 matchDelimBack 反查配对的 '['，其内容用 go/parser 校验为合法 Go 类型。
// 校验失败或非该形态返回 ""，交由调用方走原有推断路径。
func genericCallTypeArg(toks []Token, from, to int) string {
	lp := -1
	for i := from; i < to && i < len(toks); i++ {
		if toks[i].Text == "(" {
			lp = i
			break
		}
	}
	if lp <= from {
		return ""
	}
	closeBracket := prevSigIn(toks, lp-1, from)
	if closeBracket < from || toks[closeBracket].Text != "]" {
		return ""
	}
	openBracket := matchDelimBack(toks, closeBracket, "]")
	if openBracket < from || openBracket >= closeBracket {
		return ""
	}
	arg := exprText(toks, openBracket+1, closeBracket)
	if !isValidGoTypeExpr(arg) {
		return ""
	}
	return arg
}

// compositeLitType 从复合字面量 `[]T{...}` / `[N]T{...}` / `map[K]V{...}` 提取
// 完整集合类型文本。用 Go parser 校验，确保只返回合法切片/map 类型；
// 非复合字面量或无法判定返回 ""。调用方可再经 sliceElemType/containerElemTypeOnce
// 取元素类型。
//
// 第十二轮 P2-B：原实现只认 `[]`/`[N]` 切片，map[K]V{...} 落到「含字符串字面量
// → string」兜底，把 m := map[string]P{...} 的 TypeAt 推成 key 类型 string，
// 逼得索引链家族靠 150 行文本回扫擦屁股。这里补 map 分支，让类型从源头正确。
func compositeLitType(toks []Token, from, to int) string {
	if from >= to {
		return ""
	}
	isMap := toks[from].is("map")
	if !isMap && toks[from].Text != "[" {
		return ""
	}
	// 找顶层第一个 `{`（值序列起点），其左侧即类型片段 `[]T` / `[N]T`。
	depth := 0
	brace := -1
	for i := from; i < to; i++ {
		switch toks[i].Text {
		case "[", "(":
			depth++
		case "]", ")":
			if depth > 0 {
				depth--
			}
		case "{":
			if depth == 0 {
				brace = i
			}
		}
		if brace >= 0 {
			break
		}
	}
	if brace <= from {
		return ""
	}
	text := exprText(toks, from, brace)
	if isMap {
		// map[K]V：必须是合法 Go 类型且以 map[ 开头。

		if strings.HasPrefix(text, "map[") && isValidGoTypeExpr(text) {
			return text
		}
		return ""
	}
	if !isSliceGoTypeExpr(text) {
		return ""
	}
	return text
}

// sliceElemType 从切片/数组类型文本提取元素类型文本。
// []int → int, [3]string → string, string → rune（range 语义），其他 → ""。
func sliceElemType(typeText string) string {
	t := strings.TrimSpace(typeText)
	if t == "string" {
		return "rune" // range string 得到 rune/byte；保守用 rune（由 Go 决定具体）
	}
	// 第十二轮 P3-F：非切片且非 string 的类型（如 map[K]V）不是列表，
	// 旧实现直接 return t 会把 map 当元素类型，生成 []map[K]V 的误导产物。
	// For 需要列表，这类类型应由调用方给出明确诊断。
	if !strings.HasPrefix(t, "[") {
		return ""
	}
	for strings.HasPrefix(t, "[") {
		close := -1
		depth := 0
		for i, c := range t {
			switch c {
			case '[':
				depth++
			case ']':
				depth--
				if depth == 0 {
					close = i
				}
			}
			if close >= 0 {
				break
			}
		}
		if close < 0 {
			return ""
		}
		t = strings.TrimSpace(t[close+1:])
	}
	return t
}
