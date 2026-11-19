package compiler

import (
	"fmt"
	"strings"
)

// objectlit.go — GX 对象字面量 → Go 匿名结构体 / 期望类型补全。
//
// GX 写法：{ name: "x", age: 1 }
//
// 两种情况：
//  1. 无期望类型（x := { ... }）：从值反推字段类型，生成匿名结构体：
//     x := struct{ name string; age int }{ name: "x", age: 1 }
//  2. 有期望类型（var p point = { ... } / f({ ... }) / return { ... } / p = { ... }）：
//     省略结构体类型名，用期望类型补全：point{ name: "x", age: 1 }，并校验字段名与类型。
//
// 期望类型来自「明确标注了参数类型或返回类型」的上下文：
//   - 变量声明：var p point = {...} / var p = point{...}（后者已是 Go 原生，不处理）
//   - 函数实参：f({...})，f 形参类型已知（具名 struct 或匿名 struct）
//   - 赋值：p = {...}，p 声明类型已知
//   - return：return {...}，函数返回类型已知
//
// 校验失败（未知字段 / 缺字段 / 类型不兼容）→ 编译报错，位置为对象字面量的 GX 坐标。

// structInfo 是一个具名 struct 类型的字段结构。
type structInfo struct {
	name   string
	fields map[string]string // 字段名 → 类型文本
	order  []string          // 字段声明顺序
}

// objectLit 是一个对象字面量的 token 区间与解析出的字段。
type objectLit struct {
	open      int // '{' 下标
	close     int // '}' 下标
	pos       Pos
	fields    []objField
	expectTyp string // 推断出的期望类型名（空表示无期望类型）
}

type objField struct {
	name     string
	pos      Pos
	valFrom  int // 值表达式 token 区间 [valFrom, valTo)
	valTo    int
	valType  string // 反推的值类型（延迟填充）
}

// collectStructTypes 扫描所有 `type X struct { ... }` 声明，建立 struct 字段结构表。
// 同时记录匿名 struct 类型文本 → 字段表（用于 `var p struct{...} = {...}`）。
func collectStructTypes(toks []Token) map[string]*structInfo {
	out := map[string]*structInfo{}
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("type") {
			continue
		}
		j := nextSig(toks, i+1)
		if j < 0 || toks[j].Kind != Ident {
			continue
		}
		name := toks[j].Text
		k := nextSig(toks, j+1)
		if k < 0 || !toks[k].is("struct") {
			continue
		}
		open := nextSig(toks, k+1)
		if open < 0 || toks[open].Text != "{" {
			continue
		}
		close := matchDelimSkipSpace(toks, open, "{", "}")
		if close <= open {
			continue
		}
		info := &structInfo{name: name, fields: map[string]string{}}
		// 逐行解析字段：name type
		for _, seg := range splitStructFields(toks, open+1, close) {
			fname, ftype := parseStructField(toks, seg[0], seg[1])
			if fname == "" || ftype == "" {
				continue
			}
			if _, exists := info.fields[fname]; !exists {
				info.order = append(info.order, fname)
			}
			info.fields[fname] = ftype
		}
		out[name] = info
	}
	return out
}

// splitStructFields 按换行或分号把 struct 体切成字段段。
func splitStructFields(toks []Token, start, end int) [][2]int {
	var segs [][2]int
	depth := 0
	segStart := start
	for i := start; i < end; i++ {
		t := toks[i]
		switch t.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth > 0 {
				depth--
			}
		case ";":
			if depth == 0 {
				segs = append(segs, [2]int{segStart, i})
				segStart = i + 1
			}
		}
		if t.Kind == Newline && depth == 0 {
			if segStart < i {
				segs = append(segs, [2]int{segStart, i})
			}
			segStart = i + 1
		}
	}
	if segStart < end {
		segs = append(segs, [2]int{segStart, end})
	}
	return segs
}

// parseStructField 从字段段 [start,end) 解析出「字段名 类型」。
// 支持 name type、name1, name2 type（取第一个名字）。
func parseStructField(toks []Token, start, end int) (string, string) {
	first := nextSigIn(toks, start, end)
	if first < 0 || toks[first].Kind != Ident {
		return "", ""
	}
	second := nextSigIn(toks, first+1, end)
	if second < 0 || !isTypeStartTok(toks[second]) {
		return "", ""
	}
	te := typeRangeEnd(toks, second)
	if te > end {
		te = end
	}
	return toks[first].Text, exprText(toks, second, te)
}

// detectObjectLits 扫描 token 流里的对象字面量。
// 消歧：'{' 出现在表达式起始位置，且 '{' 后第一个有效 token 是 Ident 后跟 ':'。
func detectObjectLits(toks []Token) []objectLit {
	var out []objectLit
	for i := 0; i < len(toks); i++ {
		if toks[i].Text != "{" || toks[i].Kind != Operator {
			continue
		}
		// 前一个有效 token 必须表明这是表达式位置
		p := prevSig(toks, i-1)
		if p < 0 || !exprStartBefore(toks, p) {
			continue
		}
		// '{' 后第一个有效 token 是 Ident 且后跟 ':'
		first := nextSig(toks, i+1)
		if first < 0 || toks[first].Kind != Ident || isGoKeyword(toks[first].Text) {
			continue
		}
		colon := nextSig(toks, first+1)
		if colon < 0 || toks[colon].Text != ":" {
			continue
		}
		close := matchDelimSkipSpace(toks, i, "{", "}")
		if close <= i {
			continue
		}
		lit := objectLit{open: i, close: close, pos: toks[i].Pos}
		// 解析字段：顶层逗号切段
		for _, seg := range splitTopLevelSegs(toks, i+1, close) {
			colonIdx := -1
			depth := 0
			for k := seg[0]; k < seg[1]; k++ {
				switch toks[k].Text {
				case "(", "[", "{":
					depth++
				case ")", "]", "}":
					if depth > 0 {
						depth--
					}
				case ":":
					if depth == 0 {
						colonIdx = k
					}
				}
			}
			if colonIdx < 0 {
				continue
			}
			nameIdx := nextSigIn(toks, seg[0], colonIdx)
			if nameIdx < 0 || toks[nameIdx].Kind != Ident {
				continue
			}
			valFrom := nextSigIn(toks, colonIdx+1, seg[1])
			if valFrom < 0 {
				continue
			}
			lit.fields = append(lit.fields, objField{
				name:    toks[nameIdx].Text,
				pos:     toks[nameIdx].Pos,
				valFrom: valFrom,
				valTo:   seg[1],
			})
		}
		out = append(out, lit)
	}
	return out
}

// exprStartBefore 判断 toks[p] 是否是「表达式起始」的前缀 token，
// 即其后的 '{' 可解释为对象字面量而非代码块/复合类型。
func exprStartBefore(toks []Token, p int) bool {
	t := toks[p]
	switch t.Text {
	case ":=", "=", ",", "(", "[", "return", ":":
		return true
	}
	// '{' 作为外层对象字面量的字段值起点（嵌套对象）也允许
	if t.Text == "{" {
		return true
	}
	return false
}

// inferValueType 从值表达式 token 区间反推类型文本。
func inferValueType(toks []Token, from, to int) string {
	if from >= to {
		return "any"
	}
	// 嵌套对象字面量：递归生成匿名 struct 类型
	if toks[from].Text == "{" && toks[from].Kind == Operator {
		close := matchDelimSkipSpace(toks, from, "{", "}")
		if close > from && close <= to {
			return buildAnonymousStructType(toks, from, close)
		}
	}
	if to-from == 1 {
		switch toks[from].Kind {
		case String, Template:
			return "string"
		case Rune:
			return "rune"
		case Number:
			if strings.ContainsAny(toks[from].Text, ".eEpP") {
				return "float64"
			}
			return "int"
		}
	}
	// 已是展开后的 struct 字面量 / 类型（嵌套对象被内层替换后的形态）。
	// 必须先于 bool 扫描：内层 struct 的值里可能含 true/false，不能误判为 bool。
	firstSig := nextSigIn(toks, from, to)
	if firstSig >= 0 && toks[firstSig].is("struct") {
		open := nextSigIn(toks, firstSig+1, to)
		if open >= 0 && toks[open].Text == "{" {
			close := matchDelimSkipSpace(toks, open, "{", "}")
			if close > open && close <= to {
				return exprText(toks, firstSig, close+1)
			}
		}
	}
	// bool 字面量（值区间可能含尾随空白，不能只判单 token）
	for i := from; i < to; i++ {
		if toks[i].is("true") || toks[i].is("false") {
			return "bool"
		}
	}
	// 含字符串字面量的拼接 → string
	for i := from; i < to; i++ {
		if toks[i].Kind == String || toks[i].Kind == Template {
			return "string"
		}
	}
	// 浮点字面量 → float64，整数字面量 → int
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
	return "any"
}

// buildAnonymousStructType 为 [open,close) 内的对象字面量生成 `struct{ name T; ... }` 类型文本。
func buildAnonymousStructType(toks []Token, open, close int) string {
	var parts []string
	for _, seg := range splitTopLevelSegs(toks, open+1, close) {
		colonIdx := -1
		depth := 0
		for k := seg[0]; k < seg[1]; k++ {
			switch toks[k].Text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				if depth > 0 {
					depth--
				}
			case ":":
				if depth == 0 {
					colonIdx = k
				}
			}
		}
		if colonIdx < 0 {
			continue
		}
		nameIdx := nextSigIn(toks, seg[0], colonIdx)
		if nameIdx < 0 {
			continue
		}
		valFrom := nextSigIn(toks, colonIdx+1, seg[1])
		parts = append(parts, toks[nameIdx].Text+" "+inferValueType(toks, valFrom, seg[1]))
	}
	return "struct{ " + strings.Join(parts, "; ") + " }"
}

// objectLitError 是对象字面量校验失败的错误，携带 GX 坐标。
type objectLitError struct {
	pos Pos
	msg string
}

func (e *objectLitError) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.pos.Line, e.pos.Col, e.msg)
}

// ---------- 期望类型解析 ----------

// resolveExpectType 为对象字面量解析期望类型名。
// 返回类型名（具名 struct 或匿名 struct 文本）与「该类型是否已知为 struct」。
// an 可为 nil；structs 是 collectStructTypes 的结果。
func resolveExpectType(toks []Token, lit *objectLit, an *Analysis, structs map[string]*structInfo) string {
	open := lit.open
	p := prevSig(toks, open-1)
	if p < 0 {
		return ""
	}
	// 1) var p point = {...}：'=' 前是类型名（var 声明带显式类型）
	if toks[p].Text == "=" {
		// 往前找紧邻 '=' 的类型 token（var name TYPE = 里的 TYPE）
		typeIdx := prevSig(toks, p-1)
		if typeIdx >= 0 && isTypeStartTok(toks[typeIdx]) {
			// 确认它属于 var 声明：往前找 var name TYPE 结构
			if typ := lookupVarTypeAtEq(toks, p); typ != "" {
				return typ
			}
		}
		// 赋值场景 p = {...}：变量名声明类型已知
		if nameIdx := prevSig(toks, p-1); nameIdx >= 0 && toks[nameIdx].Kind == Ident {
			if typ := lookupVarType(toks, nameIdx); typ != "" {
				return typ
			}
		}
		return ""
	}
	// 2) f({...})：找调用的函数名与实参序号
	if toks[p].Text == "(" || toks[p].Text == "," {
		return inferArgExpectType(toks, open, an)
	}
	// 3) return {...}
	if toks[p].is("return") {
		return inferReturnExpectType(toks, open, an)
	}
	return ""
}

// lookupVarTypeAtEq 处理 `var name TYPE = {`：'=' 前的 TYPE 即期望类型。
// eqIdx 是 '=' 的下标；TYPE 是它前面紧邻的有效 token 区间。
func lookupVarTypeAtEq(toks []Token, eqIdx int) string {
	typeIdx := prevSig(toks, eqIdx-1)
	if typeIdx < 0 || !isTypeStartTok(toks[typeIdx]) {
		return ""
	}
	// 确认存在 `var name TYPE` 结构：TYPE 之前应是变量名，再之前是 var
	nameIdx := prevSig(toks, typeIdx-1)
	if nameIdx < 0 || toks[nameIdx].Kind != Ident {
		return ""
	}
	varIdx := prevSig(toks, nameIdx-1)
	if varIdx < 0 || !toks[varIdx].is("var") {
		return ""
	}
	// 取 TYPE 文本：从 typeIdx 到 '=' 之前（跳过空白）
	te := typeRangeEnd(toks, typeIdx)
	if te > eqIdx {
		te = eqIdx
	}
	if te <= typeIdx {
		return ""
	}
	return exprText(toks, typeIdx, te)
}

// lookupVarType 在变量声明处查 name 的显式类型：
//
//	var name TYPE = ...
//	var name TYPE
//
// 返回类型文本。
func lookupVarType(toks []Token, nameIdx int) string {
	name := toks[nameIdx].Text
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("var") {
			continue
		}
		j := nextSig(toks, i+1)
		if j < 0 || toks[j].Text != name {
			// var (...) 分组：稍后简化处理
			continue
		}
		k := nextSig(toks, j+1)
		if k < 0 {
			continue
		}
		// var name TYPE = ... 或 var name TYPE
		if toks[k].Text == "=" {
			return "" // 无显式类型
		}
		te := typeRangeEnd(toks, k)
		if te > k {
			return exprText(toks, k, te)
		}
	}
	return ""
}

// inferArgExpectType 推断对象字面量作为函数实参时的期望类型。
// 找到包裹该字面量的最近一层函数调用 f(...)，确定它是第几个实参，
// 再从 f 的声明里取对应参数的原始类型 token 文本（保留类型名，不展开别名）。
func inferArgExpectType(toks []Token, open int, an *Analysis) string {
	// 向左找到最近一层未匹配的 '('（即实参列表起点）
	depth := 0
	openCall := -1
	for i := open - 1; i >= 0; i-- {
		t := toks[i]
		if t.Text == ")" || t.Text == "]" || t.Text == "}" {
			depth++
		} else if t.Text == "(" || t.Text == "[" || t.Text == "{" {
			if depth == 0 {
				if t.Text == "(" {
					openCall = i
				}
				break
			}
			depth--
		}
	}
	if openCall < 0 {
		return ""
	}
	// 调用名是 '(' 前的标识符
	fnIdx := prevSig(toks, openCall-1)
	if fnIdx < 0 || toks[fnIdx].Kind != Ident {
		return ""
	}
	funcName := toks[fnIdx].Text
	// 实参序号
	argIdx := 0
	d := 0
	for i := openCall + 1; i < open; i++ {
		t := toks[i]
		switch t.Text {
		case "(", "[", "{":
			d++
		case ")", "]", "}":
			if d > 0 {
				d--
			}
		case ",":
			if d == 0 {
				argIdx++
			}
		}
	}
	// 从 func 声明取第 argIdx 个参数的原始类型文本
	return declaredParamTypeAt(toks, funcName, argIdx)
}

// declaredParamTypeAt 从 `func name(p0 T0, p1 T1, ...)` 声明里取第 idx 个参数的类型文本。
// 用原始 token（不展开别名），避免 FuncSig 的别名展开破坏类型名。
func declaredParamTypeAt(toks []Token, funcName string, idx int) string {
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("func") {
			continue
		}
		nameIdx := funcNameAt(toks, i)
		if nameIdx < 0 || toks[nameIdx].Text != funcName {
			continue
		}
		open, close, ok := funcParamParens(toks, i)
		if !ok {
			continue
		}
		segs := splitTopLevelSegs(toks, open+1, close)
		if idx >= len(segs) {
			return ""
		}
		seg := segs[idx]
		first := nextSigIn(toks, seg[0], seg[1])
		if first < 0 {
			return ""
		}
		second := nextSigIn(toks, first+1, seg[1])
		if second < 0 || !isTypeStartTok(toks[second]) {
			return "" // 无名参数或共享类型，简化处理
		}
		te := typeRangeEnd(toks, second)
		if te > seg[1] {
			te = seg[1]
		}
		return exprText(toks, second, te)
	}
	return ""
}

// inferReturnExpectType 推断 return 语句里对象字面量的期望类型（函数返回类型）。
// 不依赖 Analysis 的 token 下标（会因前面的 transform 漂移），而是从 return
// 位置向前找最近的 func 声明，取其 ')' 后的返回类型 token 文本。
func inferReturnExpectType(toks []Token, open int, an *Analysis) string {
	// 从 return 向前找最近的 func 关键字
	funcIdx := -1
	for i := open - 1; i >= 0; i-- {
		if toks[i].is("func") {
			funcIdx = i
			break
		}
	}
	if funcIdx < 0 {
		return ""
	}
	// 找参数列表的 '('：func 后可能有函数名（或接收者）
	openParen := nextSig(toks, funcIdx+1)
	if openParen < 0 {
		return ""
	}
	if toks[openParen].Kind == Ident && !toks[openParen].is("func") {
		openParen = nextSig(toks, openParen+1) // 跳过函数名
	} else if toks[openParen].Text == "(" {
		// 方法：func (recv T) name(...) —— 跳过接收者和名字
		rcvClose := matchDelimSkipSpace(toks, openParen, "(", ")")
		if rcvClose < 0 {
			return ""
		}
		nameIdx := nextSig(toks, rcvClose+1)
		if nameIdx >= 0 {
			openParen = nextSig(toks, nameIdx+1)
		}
	}
	if openParen < 0 || toks[openParen].Text != "(" {
		return ""
	}
	closeParen := matchDelimSkipSpace(toks, openParen, "(", ")")
	if closeParen <= openParen {
		return ""
	}
	// ')' 之后到 '{' 之间是返回类型
	k := nextSig(toks, closeParen+1)
	if k < 0 || toks[k].Text == "{" {
		return "" // 无返回类型
	}
	// 具名返回 (x T) 简化：取括号内第一个类型；单类型直接用
	if toks[k].Text == "(" {
		rc := matchDelimSkipSpace(toks, k, "(", ")")
		if rc > k {
			inner := nextSig(toks, k+1)
			if inner >= 0 {
				second := nextSig(toks, inner+1)
				typeStart := inner
				if second < rc && isTypeStartTok(toks[second]) {
					typeStart = second // 命名返回，跳过名字
				}
				te := typeRangeEnd(toks, typeStart)
				if te > rc {
					te = rc
				}
				return exprText(toks, typeStart, te)
			}
		}
		return ""
	}
	te := typeRangeEnd(toks, k)
	return exprText(toks, k, te)
}

// ---------- 校验与改写 ----------

// validateObjectLit 校验对象字面量的字段是否符合期望 struct 结构。
// 返回该 struct 的字段类型表；校验失败返回 *objectLitError。
func validateObjectLit(lit *objectLit, expect string, structs map[string]*structInfo) (*structInfo, *objectLitError) {
	info, ok := structs[expect]
	if !ok {
		// 期望类型不是本文件已知的具名 struct → 无法校验，视为无期望类型（宽松）
		return nil, nil
	}
	// 校验每个字面量字段
	seen := map[string]bool{}
	for _, f := range lit.fields {
		ftype, exists := info.fields[f.name]
		if !exists {
			return nil, &objectLitError{pos: f.pos, msg: fmt.Sprintf("未知字段 %q（类型 %s 的字段有: %s）", f.name, expect, strings.Join(info.order, ", "))}
		}
		seen[f.name] = true
		// 值类型兼容性：无类型常量按类型族放宽；具类型值精确匹配
		vt := f.valType
		if vt != "" && vt != "any" && !typeCompatible(ftype, vt) {
			return nil, &objectLitError{pos: f.pos, msg: fmt.Sprintf("字段 %q 类型不匹配：期望 %s，实际 %s", f.name, ftype, vt)}
		}
	}
	// 校验缺失字段：所有 struct 字段都必须提供
	for _, name := range info.order {
		if !seen[name] {
			return nil, &objectLitError{pos: lit.pos, msg: fmt.Sprintf("缺少字段 %q（类型 %s）", name, expect)}
		}
	}
	return info, nil
}

// typeCompatible 判断值类型 vt 能否赋给字段类型 target。
func typeCompatible(target, vt string) bool {
	if target == vt || target == "any" || vt == "any" {
		return true
	}
	// 无类型整数字面量反推为 int，可赋给整数/浮点族
	if vt == "int" {
		return isIntegerType(target) || isFloatType(target)
	}
	if vt == "float64" {
		return isFloatType(target)
	}
	if vt == "string" {
		return isStringType(target)
	}
	if vt == "bool" {
		return target == "bool"
	}
	return false
}

// transformObjectLits 是对象字面量变换入口。
// 返回一个错误（带 GX 坐标）表示校验失败；成功则返回改写后的 token 流。
func transformObjectLits(in []Token, an *Analysis) ([]Token, error) {
	structs := collectStructTypes(in)
	out := append([]Token(nil), in...)

	// 迭代处理：每轮只替换「叶子」对象字面量（内部不含其他对象字面量）。
	// 内层先变成 struct{...}{...}，外层随后处理时其字段值已是合法 Go 类型文本，
	// 从而正确支持嵌套对象字面量。
	for {
		lits := detectObjectLits(out)
		if len(lits) == 0 {
			break
		}
		leaves := leafObjectLits(lits)
		if len(leaves) == 0 {
			break
		}

		type repl struct {
			from, to int
			toks     []Token
		}
		var reps []repl
		for _, lit := range leaves {
			for j := range lit.fields {
				lit.fields[j].valType = inferValueType(out, lit.fields[j].valFrom, lit.fields[j].valTo)
			}
			lit.expectTyp = resolveExpectType(out, &lit, an, structs)

			var typeText string
			if lit.expectTyp != "" {
				info, err := validateObjectLit(&lit, lit.expectTyp, structs)
				if err != nil {
					return nil, err
				}
				if info != nil {
					typeText = lit.expectTyp // 省略结构体类型名，用期望类型补全
				} else {
					typeText = buildAnonymousStructType(out, lit.open, lit.close)
				}
			} else {
				typeText = buildAnonymousStructType(out, lit.open, lit.close)
			}

			p := lit.pos
			var toks []Token
			toks = append(toks, New(typeText).AllTokens()...)
			for k := range toks {
				toks[k].Pos = p
				toks[k].IsGen = true
			}
			if len(toks) > 0 && toks[len(toks)-1].Kind == EOF {
				toks = toks[:len(toks)-1]
			}
			toks = append(toks, tokOp("{", p))
			for k := lit.open + 1; k < lit.close; k++ {
				toks = append(toks, out[k])
			}
			toks = append(toks, tokOp("}", p))
			reps = append(reps, repl{from: lit.open, to: lit.close + 1, toks: toks})
		}

		// 从后往前替换，避免下标漂移
		for k := len(reps) - 1; k >= 0; k-- {
			r := reps[k]
			out = append(out[:r.from], append(r.toks, out[r.to:]...)...)
		}
	}
	return out, nil
}

// leafObjectLits 返回不包含其他对象字面量的最内层字面量集合。
func leafObjectLits(lits []objectLit) []objectLit {
	var out []objectLit
	for i, l := range lits {
		contains := false
		for j, m := range lits {
			if i == j {
				continue
			}
			if m.open > l.open && m.close < l.close {
				contains = true
				break
			}
		}
		if !contains {
			out = append(out, l)
		}
	}
	return out
}
