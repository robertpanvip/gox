package compiler

import "strconv"

// collections 转换：把集合链式调用与切片类型提升为 gx 运行时。
//
// 1) 方法名还原：xs.map(f) -> xs.Map(f)（Go 1.27 泛型方法，换型链式）。
// 2) 全局切片提升：所有切片类型 []T -> list.List[T]，使原生 []T 直接获得
//    Map/Filter/Reduce 方法。List[T] 与 []T 底层相同，可双向赋值。
//
// 提升只匹配「空方括号 [] 紧跟类型表达式」——这是切片类型独有的形态，
// 天然不误伤：数组 [3]T（方括号非空）、索引 a[i]、切片表达式 a[1:2]、
// map[K]V（map 的方括号非空）、泛型实例 Foo[T]（方括号非空）。
//
// import 路径由 module 决定：<module>/runtime/list。

var chainMethods = map[string]string{
	"map": "Map", "filter": "Filter", "reduce": "Reduce",
}

const defaultModule = "github.com/robertpanvip/gox"

func listImportPath(module string) string {
	if module == "" {
		module = defaultModule
	}
	return module + "/runtime/list"
}

// sceneImportPath 是 gxscene op-code 运行时的 import 路径：<module>/runtime/scene。
func sceneImportPath(module string) string {
	if module == "" {
		module = defaultModule
	}
	return module + "/runtime/scene"
}

func transformCollections(in []Token, module string) []Token {
	out := upperChainMethods(in)
	out, _ = promoteAllSlices(out, "list")
	out = wrapChainReceivers(out, collectListVars(out))
	// import 由 ReconcileImports 统一注入（按实际使用情况），此处不再注入。
	return out
}

// collectListVars 收集所有「已提升为 list.List 的变量名」：声明/初始化/参数中
// 紧邻 list.List[...] 类型之前的标识符。这些变量已是 List，无需再 list.Of 包装。
func collectListVars(toks []Token) map[string]bool {
	vars := map[string]bool{}
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("list") {
			continue
		}
		j := nextSig(toks, i+1)
		if j < 0 || toks[j].Text != "." {
			continue
		}
		j = nextSig(toks, j+1)
		if j < 0 || !toks[j].is("List") {
			continue
		}
		// list.List 前可能是：':=' / 'var' / 变量名（参数）。
		p := prevSig(toks, i-1)
		if p < 0 {
			continue
		}
		if toks[p].Text == ":=" || toks[p].is("var") {
			// 赋值/声明：变量名在 := / var 之前
			v := prevSig(toks, p-1)
			if v >= 0 && toks[v].Kind == Ident && !isGoKeyword(toks[v].Text) {
				vars[toks[v].Text] = true
			}
		} else if toks[p].Kind == Ident && !isGoKeyword(toks[p].Text) {
			// 参数形态 xs list.List[T]
			vars[toks[p].Text] = true
		}
	}
	return vars
}

// wrapChainReceivers 给「标识符接收者」的链式方法调用插入 list.Of(...) 包装，
// 使标准库/外部函数返回的原生 []T 也能使用 Map/Filter/Reduce/Each。
//
// 转译：xs.map(f)            → list.Of(xs).Map(f)
//
// 只包装链首（接收者是裸标识符、前面不是 '.' 或已有包装）。后续链环
// xs.map(f).filter(g) 的 filter 接收者是上一环结果（已是 List），无需包装。
// 复杂接收者（如 strings.Fields(x).map(f)）暂不处理——保持原样，不劣化。
func wrapChainReceivers(in []Token, listVars map[string]bool) []Token {
	chainSet := map[string]bool{"Map": true, "Filter": true, "Reduce": true, "Each": true}
	// 先标记需要包装的位置（接收者标识符下标 → 方法调用右括号下标）
	type wrap struct{ recv, close int }
	var wraps []wrap
	for i := 0; i < len(in); i++ {
		if in[i].Kind != Operator || in[i].Text != "." {
			continue
		}
		// '.' 后是链式方法名
		j := nextSig(in, i+1)
		if j < 0 || in[j].Kind != Ident || !chainSet[in[j].Text] {
			continue
		}
		// 方法名后必须是 '('（方法调用）
		paren := nextSig(in, j+1)
		if paren < 0 || in[paren].Text != "(" {
			continue
		}
		close := matchDelimSkipSpace(in, paren, "(", ")")
		if close <= paren {
			continue
		}
		// '.' 前的接收者必须是标识符
		recv := prevSig(in, i-1)
		if recv < 0 || in[recv].Kind != Ident {
			continue
		}
		// 排除字段访问（接收者前是 '.'）、关键字、已有包装/包限定
		before := prevSig(in, recv-1)
		if before >= 0 && in[before].Text == "." {
			continue
		}
		if isGoKeyword(in[recv].Text) || in[recv].Text == "list" || in[recv].Text == "gxvdom" {
			continue
		}
		// 已是 List 的变量（来自提升的 []T 字面量/参数）无需包装
		if listVars[in[recv].Text] {
			continue
		}
		wraps = append(wraps, wrap{recv: recv, close: close})
	}
	if len(wraps) == 0 {
		return in
	}
	// 从后往前插入，避免下标漂移
	out := append([]Token(nil), in...)
	for k := len(wraps) - 1; k >= 0; k-- {
		w := wraps[k]
		p := out[w.recv].Pos
		// 接收者前插 'list.Of('，接收者后（'.' 前）插 ')'：list.Of(xs).Map(f)
		out = append(out[:w.recv+1], append([]Token{tokOp(")", p)}, out[w.recv+1:]...)...)
		open := []Token{
			tokIdent("list", p), tokOp(".", p), tokIdent("Of", p), tokOp("(", p),
		}
		out = append(out[:w.recv], append(open, out[w.recv:]...)...)
	}
	return out
}

// upperChainMethods 把 .map/.filter/.reduce 方法名首字母大写。
func upperChainMethods(in []Token) []Token {
	out := append([]Token{}, in...)
	for i := 0; i < len(out); i++ {
		if out[i].Kind != Operator || out[i].Text != "." {
			continue
		}
		j := nextSig(out, i+1)
		if j < 0 || j >= len(out) || out[j].Kind != Ident {
			continue
		}
		up, ok := chainMethods[out[j].Text]
		if !ok {
			continue
		}
		out[j].Text = up
	}
	return out
}

// promoteAllSlices 把所有 []T 改写为 alias.List[T]，返回是否发生过提升。
func promoteAllSlices(toks []Token, alias string) ([]Token, bool) {
	res := make([]Token, 0, len(toks))
	changed := false
	for i := 0; i < len(toks); {
		t := toks[i]
		// 识别空方括号：'[' 紧随 ']'（切片类型 []T 的形态，严格相邻）
		if t.Kind == Operator && t.Text == "[" && i+1 < len(toks) && toks[i+1].Text == "]" && !toks[i+1].IsGen {
			te := typeEnd(toks, i+2)
			if te > i+2 && verifyTypeExprAt(toks, i+2, te) { // ']' 后须是合法 Go 类型表达式
				p := t.Pos
				gen := []Token{
					tokIdent(alias, p), tokOp(".", p), tokIdent("List", p), tokOp("[", p),
				}
				// 递归提升类型内部可能存在的切片（如 [][]int）
				inner, _ := promoteAllSlices(toks[i+2:te], alias)
				gen = append(gen, inner...)
				gen = append(gen, tokOp("]", p))
				res = append(res, gen...)
				i = te
				changed = true
				continue
			}
		}
		res = append(res, t)
		i++
	}
	return res, changed
}

// typeEnd 解析一个 Go 类型表达式的结束下标（exclusive）。
//
// 覆盖 Go 规范允许的完整类型语法，而不只基础/指针/切片/数组：
//   - 具名类型、包限定 pkg.Type、泛型实例 Foo[T]
//   - 指针 *T、切片 []T、数组 [N]T（递归解析元素）
//   - map[K]V（连值类型一起）
//   - 通道 chan T / chan<- T / <-chan T
//   - 函数 func(A) B / func(A) (B, C) / func()
//   - 结构体 struct{...}、接口 interface{...}
//   - 括号类型 (T)
//
// 这样 []func(int) bool、[]struct{...} 等复杂类型会被整体识别为一个
// 类型表达式，集合提升生成 list.List[func(int) bool] 而非非法的
// list.List[func](int) bool。
func typeEnd(toks []Token, start int) int {
	i := nextSig(toks, start)
	if i < 0 {
		return maxInt(start, 0)
	}
	t := toks[i]

	// 指针：*T（可含空格，如 * pkg.Type）
	if t.Text == "*" {
		return typeEnd(toks, i+1)
	}

	// 通道方向前缀：<-chan T
	if t.Text == "<-" {
		j := nextSig(toks, i+1)
		if j >= 0 && toks[j].is("chan") {
			return typeEnd(toks, j+1) // chan 后即为元素类型
		}
		return i + 1
	}

	switch {
	case t.is("chan"):
		// chan T 或 chan<- T
		j := nextSig(toks, i+1)
		if j >= 0 && toks[j].Text == "<-" {
			j = nextSig(toks, j+1)
		}
		if j >= 0 {
			return typeEnd(toks, j)
		}
		return i + 1

	case t.is("map"):
		// map[K]V：吞掉 [K] 后继续解析 V
		j := nextSig(toks, i+1)
		if j >= 0 && toks[j].Text == "[" {
			close := matchDelimSkipSpace(toks, j, "[", "]")
			if close > j {
				return typeEnd(toks, close+1)
			}
		}
		return i + 1

	case t.is("func"):
		// 函数类型：func 参数表 [结果]
		j := nextSig(toks, i+1)
		if j < 0 || toks[j].Text != "(" {
			return i + 1
		}
		close := matchDelimSkipSpace(toks, j, "(", ")")
		if close <= j {
			return close
		}
		// 结果：紧跟 '(' 表示多值结果 (B, C)；否则是单个类型表达式 B。
		r := nextSig(toks, close+1)
		if r < 0 {
			return close + 1
		}
		if toks[r].Text == "(" {
			rc := matchDelimSkipSpace(toks, r, "(", ")")
			if rc > r {
				return rc + 1
			}
			return r + 1
		}
		if isTypeStartTok(toks[r]) && !isTypeBoundaryTok(toks[r]) {
			return typeEnd(toks, r)
		}
		return close + 1

	case t.is("struct") || t.is("interface"):
		// struct{...} / interface{...}
		j := nextSig(toks, i+1)
		if j >= 0 && toks[j].Text == "{" {
			close := matchDelimSkipSpace(toks, j, "{", "}")
			if close > j {
				return close + 1
			}
		}
		return i + 1

	case t.Text == "[":
		// 切片 []T 或数组 [N]T：解析 ']' 后的元素类型
		close := matchDelimSkipSpace(toks, i, "[", "]")
		if close <= i {
			return i + 1
		}
		return typeEnd(toks, close+1)

	case t.Text == "(":
		// 括号类型 (T)
		close := matchDelimSkipSpace(toks, i, "(", ")")
		if close > i {
			return close + 1
		}
		return i + 1

	case t.Kind == Ident:
		// 具名类型 / 包限定 / 泛型实例
		i++
		j := nextSig(toks, i)
		if j >= 0 && j+1 < len(toks) && toks[j].Text == "." && toks[j+1].Kind == Ident {
			i = j + 2
		}
		j = nextSig(toks, i)
		if j >= 0 && toks[j].Text == "[" {
			close := matchDelimSkipSpace(toks, j, "[", "]")
			if close > j {
				return close + 1
			}
		}
		return i

	default:
		return i + 1
	}
}

// isTypeBoundaryTok 标记那些既可能是类型起始、也可能是一个类型表达式结束边界
// 的 token。用于 func 结果判定时避免误吞 ',' ')' ']' '}' 等分隔符。
func isTypeBoundaryTok(t Token) bool {
	if t.Kind != Operator {
		return false
	}
	switch t.Text {
	case ",", ")", "]", "}", ";", "=", "{":
		return true
	}
	return false
}

// matchDelimSkipSpace 与 matchDelim 相同，但 l/open 通过 nextSig 跳过空白定位；
// 返回配对的右括号下标，失败返回 open。
func matchDelimSkipSpace(toks []Token, open int, l, r string) int {
	if open >= len(toks) || toks[open].Text != l {
		return open
	}
	d := 0
	for i := open; i < len(toks); i++ {
		switch toks[i].Text {
		case l:
			d++
		case r:
			d--
			if d == 0 {
				return i
			}
		}
	}
	return open
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func matchBracket(toks []Token, open int) int {
	d := 0
	for i := open; i < len(toks); i++ {
		if toks[i].Text == "[" {
			d++
		}
		if toks[i].Text == "]" {
			d--
			if d == 0 {
				return i
			}
		}
	}
	return open
}

func nextSig(toks []Token, i int) int {
	for i < len(toks) {
		k := toks[i].Kind
		if k == Space || k == Newline || k == Comment {
			i++
			continue
		}
		return i
	}
	return -1
}

func prevSig(toks []Token, i int) int {
	for i >= 0 {
		k := toks[i].Kind
		if k == Space || k == Newline || k == Comment {
			i--
			continue
		}
		return i
	}
	return -1
}

func injectListImport(toks []Token, path string) []Token {
	quoted := strconv.Quote(path)
	for _, t := range toks {
		if t.Kind == String && t.Text == quoted {
			return toks // 已存在
		}
	}
	insert := 0
	for i, t := range toks {
		if t.is("package") {
			for j := i + 1; j < len(toks); j++ {
				if toks[j].Kind == Newline {
					insert = j + 1
					break
				}
			}
			break
		}
	}
	p := toks[0].Pos
	imp := []Token{
		tokNewline(p),
		tokIdent("import", p), tokSpace(" ", p), tokString(quoted, p), tokNewline(p),
	}
	out := append([]Token{}, toks[:insert]...)
	out = append(out, imp...)
	out = append(out, toks[insert:]...)
	return out
}
