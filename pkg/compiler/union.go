package compiler

import "sort"

// union 转换：把「类型位置」的联合类型 A|B|C 擦除为单个 any token。
//
//	// GX                          // 生成的 Go
//	var x int | string       →     var x any
//	func f(v int | string){} →     func f(v any){}
//	type Num = int | float64 →     type Num = any
//
// 设计要点：
//   - 只在「显式类型标注」位置识别联合：var/const/type 声明、函数参数/返回值/
//     接收者、struct 字段。这些位置的 '|' 不可能是位运算，消歧天然成立。
//   - 函数体里的位运算 a|b、初始化 `var x = a|b` 没有类型锚点，不会被碰。
//   - 每个联合成员复用 typeEnd 解析，支持 int/string/pkg.Type/[]T/[N]T/
//     *T/map[K]V/Foo[T] 等；联合可出现在这些类型的任意一层顶端。
//   - 整段 A|B|C 塌缩为一个 any token，位置取联合起点（sourcemap 可回投）。
//   - 擦除是「弱联合」：Go 侧失去编译期类型安全，需配合 is 守卫窄化使用。
//
// 已知限制：
//   - 不处理 interface/struct 内联类型、函数类型签名内部的联合；
//   - 不处理「嵌套联合」（联合作为切片元素/map 值的一部分，如 []int|string
//     按 Go 优先级解析为 ([]int)|string，整段擦除为 any）。

// transformUnion 把类型位置的联合类型擦除为 any。
func transformUnion(in []Token) []Token {
	roots := findTypeRoots(in)
	if len(roots) == 0 {
		return in
	}

	type repl struct{ s, e int }
	var reps []repl
	seen := map[int]bool{}
	for _, r := range roots {
		if r <= 0 || r >= len(in) || seen[r] {
			continue
		}
		seen[r] = true
		s, e, ok := unionSpan(in, r)
		if ok && e > s {
			reps = append(reps, repl{s, e})
		}
	}
	if len(reps) == 0 {
		return in
	}

	// 按起点升序去重重叠区间，再从后往前替换，避免下标失效。
	sort.Slice(reps, func(i, j int) bool { return reps[i].s < reps[j].s })
	var filtered []repl
	for _, rp := range reps {
		if len(filtered) > 0 && rp.s < filtered[len(filtered)-1].e {
			continue
		}
		filtered = append(filtered, rp)
	}

	out := append([]Token(nil), in...)
	for i := len(filtered) - 1; i >= 0; i-- {
		rp := filtered[i]
		p := in[rp.s].Pos
		anyTok := Token{Kind: Ident, Text: "any", Pos: p, IsGen: true}
		out = append(out[:rp.s], append([]Token{anyTok}, out[rp.e:]...)...)
	}
	return out
}

// unionSpan 从类型起点解析类型；若含顶层联合，返回需替换的区间 [start,end)。
func unionSpan(toks []Token, start int) (int, int, bool) {
	e := typeEnd(toks, start)
	if e <= start {
		return start, start, false
	}
	has := false
	for {
		bar := nextSig(toks, e)
		if bar < 0 || toks[bar].Kind != Operator || toks[bar].Text != "|" {
			break
		}
		n := nextSig(toks, bar+1)
		if n < 0 || !isTypeStartTok(toks[n]) {
			break
		}
		ne := typeEnd(toks, n)
		if ne <= n {
			break
		}
		e = ne
		has = true
	}
	return start, e, has
}

// isTypeStartTok 判断 token 是否可能是一个类型的起始。
func isTypeStartTok(t Token) bool {
	if t.Kind == Operator {
		switch t.Text {
		case "*", "[", "<-", "(":
			return true
		}
		return false
	}
	if t.Kind != Ident {
		return false
	}
	// 排除语句关键字（它们在类型位置不会被当作类型）
	switch t.Text {
	case "return", "if", "else", "for", "break", "continue", "goto", "defer",
		"go", "switch", "case", "default", "range", "var", "const", "import", "package":
		return false
	}
	return true
}

// findTypeRoots 收集所有「显式类型标注」的起点下标。
func findTypeRoots(toks []Token) []int {
	var roots []int
	for i := 0; i < len(toks); i++ {
		switch toks[i].Text {
		case "var", "const":
			roots = append(roots, declRoots(toks, i, false)...)
		case "type":
			if !toks[i].is("type") {
				continue
			}
			roots = append(roots, declRoots(toks, i, true)...)
		case "func":
			if !toks[i].is("func") {
				continue
			}
			roots = append(roots, funcRoots(toks, i)...)
		case "struct":
			if !toks[i].is("struct") {
				continue
			}
			roots = append(roots, fieldRoots(toks, i)...)
		}
	}
	return roots
}

// declRoots 处理 var/const/type 声明（含分组），返回类型起点。
func declRoots(toks []Token, kw int, isTypeDecl bool) []int {
	j := nextSig(toks, kw+1)
	if j < 0 {
		return nil
	}
	if toks[j].Text == "(" {
		close := matchDelim(toks, j, "(", ")")
		if close <= j {
			return nil
		}
		var roots []int
		lineStart := j + 1
		for k := j + 1; k <= close; k++ {
			if k == close || toks[k].Kind == Newline {
				if r := oneDeclRoot(toks, lineStart, k, isTypeDecl); r > 0 {
					roots = append(roots, r)
				}
				lineStart = k + 1
			}
		}
		return roots
	}
	if r := oneDeclRoot(toks, j, -1, isTypeDecl); r > 0 {
		return []int{r}
	}
	return nil
}

// oneDeclRoot 解析单条声明：var/const 跳过名字列表，type 跳过 = ，返回类型起点。
func oneDeclRoot(toks []Token, start, limit int, isTypeDecl bool) int {
	j := nextSig(toks, start)
	if j < 0 || (limit > 0 && j >= limit) || toks[j].Kind != Ident {
		return -1
	}
	if isTypeDecl {
		k := nextSig(toks, j+1)
		if k < 0 || (limit > 0 && k >= limit) {
			return -1
		}
		// 泛型类型声明 type Foo[T any] = T|E：跳过 [T any] 参数列表
		if toks[k].Text == "[" {
			close := matchDelimSkipSpace(toks, k, "[", "]")
			if close <= k {
				return -1
			}
			k = nextSig(toks, close+1)
			if k < 0 || (limit > 0 && k >= limit) {
				return -1
			}
		}
		if toks[k].Text == "=" {
			k = nextSig(toks, k+1)
		}
		if k >= 0 && (limit <= 0 || k < limit) && isTypeStartTok(toks[k]) {
			return k
		}
		return -1
	}
	// var/const：跳过逗号分隔的名字列表；遇到 '=' 表示推断初始化，无显式类型。
	k := j
	for {
		c := nextSig(toks, k+1)
		if c < 0 || (limit > 0 && c >= limit) {
			return -1
		}
		if toks[c].Text == "," {
			k = nextSig(toks, c+1)
			if k < 0 || (limit > 0 && k >= limit) || toks[k].Kind != Ident {
				return -1
			}
			continue
		}
		if toks[c].Text == "=" {
			return -1
		}
		if isTypeStartTok(toks[c]) {
			return c
		}
		return -1
	}
}

// funcRoots 处理函数签名：接收者、参数、返回值。
func funcRoots(toks []Token, fi int) []int {
	j := nextSig(toks, fi+1)
	if j < 0 {
		return nil
	}
	var roots []int
	if toks[j].Text == "(" {
		close := matchDelim(toks, j, "(", ")")
		if close <= j {
			return nil
		}
		roots = append(roots, paramRoots(toks, j, close)...)
		after := nextSig(toks, close+1)
		if after >= 0 && toks[after].Kind == Ident {
			paren := nextSig(toks, after+1)
			if paren >= 0 && toks[paren].Text == "(" {
				c2 := matchDelim(toks, paren, "(", ")")
				roots = append(roots, paramRoots(toks, paren, c2)...)
				roots = append(roots, resultRoots(toks, c2)...)
				return roots
			}
		}
		roots = append(roots, resultRoots(toks, close)...)
		return roots
	}
	j = nextSig(toks, j+1)
	// 泛型函数 func f[T any](...)：跳过 [T any] 参数列表再找 '('
	if j >= 0 && toks[j].Text == "[" {
		close := matchDelimSkipSpace(toks, j, "[", "]")
		if close <= j {
			return nil
		}
		j = nextSig(toks, close+1)
	}
	if j >= 0 && toks[j].Text == "(" {
		c := matchDelim(toks, j, "(", ")")
		roots = append(roots, paramRoots(toks, j, c)...)
		roots = append(roots, resultRoots(toks, c)...)
	}
	return roots
}

// resultRoots 处理返回值：单个类型或括号列表。
func resultRoots(toks []Token, close int) []int {
	j := nextSig(toks, close+1)
	if j < 0 || toks[j].Text == "{" {
		return nil
	}
	if toks[j].Text == "(" {
		c := matchDelim(toks, j, "(", ")")
		return paramRoots(toks, j, c)
	}
	if isTypeStartTok(toks[j]) {
		return []int{j}
	}
	return nil
}

// paramRoots 处理括号内的参数/返回值列表：按逗号分段找每段类型起点。
func paramRoots(toks []Token, open, close int) []int {
	var roots []int
	segStart := open + 1
	for i := open + 1; i <= close; i++ {
		if i == close || toks[i].Text == "," {
			if r := oneParamRoot(toks, segStart, i); r > 0 {
				roots = append(roots, r)
			}
			segStart = i + 1
		}
	}
	return roots
}

// oneParamRoot 解析一段参数：有两个标识符时第一个是名字、第二个是类型；
// 只有一个时它本身是类型（无名参数）。即使误判，起点仍落在类型 token 上，无害。
func oneParamRoot(toks []Token, start, limit int) int {
	first := nextSig(toks, start)
	if first < 0 || first >= limit || !isTypeStartTok(toks[first]) {
		return -1
	}
	second := nextSig(toks, first+1)
	if second >= 0 && second < limit && isTypeStartTok(toks[second]) {
		return second
	}
	return first
}

// fieldRoots 处理 struct 字段：按行/分号分段，取每段类型起点。
func fieldRoots(toks []Token, si int) []int {
	br := nextSig(toks, si+1)
	if br < 0 || br >= len(toks) || toks[br].Text != "{" {
		return nil
	}
	close := matchDelim(toks, br, "{", "}")
	if close <= br {
		return nil
	}
	var roots []int
	segStart := br + 1
	for i := br + 1; i <= close; i++ {
		if i == close || toks[i].Kind == Newline || toks[i].Text == ";" {
			if r := oneParamRoot(toks, segStart, i); r > 0 {
				roots = append(roots, r)
			}
			segStart = i + 1
		}
	}
	return roots
}
