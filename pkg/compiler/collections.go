package compiler

import "strconv"

// collections 转换：把集合链式调用从 gx 的小写写法还原为 gxcollections.List 的泛型方法。
//
// gx 里写（一律小写）：
//
//	nums := []int{1, 2, 3}
//	labels := nums.map(strconv.itoa).filter(func(s string) bool { return s != "" })
//
// 生成：
//
//	nums := gxcollections.List[int]{1, 2, 3}
//	labels := nums.Map(strconv.Itoa).Filter(func(s string) bool { return s != "" })
//
// 「按需提升」：只有被 map/filter/reduce 调用的接收者变量，其声明处的 []T
// 才改写成 gxcollections.List[T]；其它切片保持原生 []T。依赖 Go 1.27 泛型方法，
// Map/Reduce 可换型并保持链式（List[T].Map[R func(T)R] List[R]）。

var chainMethods = map[string]string{
	"map": "Map", "filter": "Filter", "reduce": "Reduce",
}

const gxCollectionsPath = "github.com/robertpanvip/gox/gxcollections"

func transformCollections(in []Token) []Token {
	out := append([]Token{}, in...)
	promote := map[string]bool{}

	// 1) 方法名小写 → 大写，并收集接收者变量名。
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
		k := nextSig(out, j+1)
		if k < 0 || k >= len(out) || out[k].Text != "(" {
			continue
		}
		out[j].Text = up // map → Map
		p := prevSig(out, i-1)
		if p >= 0 && out[p].Kind == Ident && isLowerStart(out[p].Text) {
			promote[out[p].Text] = true
		}
	}
	if len(promote) == 0 {
		return in
	}

	// 2) 提升接收者声明处的 []T → gxcollections.List[T]。
	out = promoteDecls(out, promote)

	// 3) 注入 gxcollections import。
	out = injectCollectionsImport(out)
	return out
}

// promoteDecls 对接收者变量的声明/参数/字面量处的 []T 做类型提升。
func promoteDecls(toks []Token, promote map[string]bool) []Token {
	res := make([]Token, 0, len(toks))
	for i := 0; i < len(toks); {
		t := toks[i]
		if t.Kind == Ident && promote[t.Text] {
			j := nextSig(toks, i+1)
			if j >= 0 && j < len(toks) && toks[j].Text == ":=" {
				j = nextSig(toks, j+1)
				// nums := make([]T, ...) — 跳到 make 的参数
				if j >= 0 && j < len(toks) && toks[j].Text == "make" {
					j = nextSig(toks, j+1)
					if j >= 0 && j < len(toks) && toks[j].Text == "(" {
						j = nextSig(toks, j+1)
					}
				}
			}
			if j >= 0 && j+1 < len(toks) && toks[j].Text == "[" && toks[j+1].Text == "]" {
				gen, next := rewriteSliceType(toks, j)
				res = append(res, toks[i:j]...)
				res = append(res, gen...)
				i = next
				continue
			}
		}
		res = append(res, t)
		i++
	}
	return res
}

// rewriteSliceType 把 [ ] T 改写成 gxcollections.List [ T ]，返回生成 token 与消费到的下标。
func rewriteSliceType(toks []Token, bracket int) ([]Token, int) {
	ts := nextSig(toks, bracket+2)
	if ts < 0 {
		ts = bracket + 2
	}
	te := typeEnd(toks, ts)
	p := toks[bracket].Pos
	gen := []Token{
		tokIdent("gxcollections", p), tokOp(".", p), tokIdent("List", p), tokOp("[", p),
	}
	gen = append(gen, toks[ts:te]...)
	gen = append(gen, tokOp("]", p))
	return gen, te
}

// typeEnd 解析一个类型表达式的结束下标（exclusive），支持指针/嵌套切片/map/包限定/泛型实例化。
func typeEnd(toks []Token, start int) int {
	i := start
	if i >= len(toks) {
		return i
	}
	if toks[i].Text == "*" {
		i++
	}
	if i >= len(toks) {
		return i
	}
	switch {
	case toks[i].Text == "map":
		i++
		if i < len(toks) && toks[i].Text == "[" {
			i = matchBracket(toks, i) + 1
		}
		i = typeEnd(toks, i)
	case toks[i].Text == "[":
		i = matchBracket(toks, i) + 1
		i = typeEnd(toks, i)
	case toks[i].Kind == Ident:
		i++
		if i+1 < len(toks) && toks[i].Text == "." && toks[i+1].Kind == Ident {
			i += 2
		}
		if i < len(toks) && toks[i].Text == "[" {
			i = matchBracket(toks, i) + 1
		}
	default:
		i++
	}
	return i
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

func injectCollectionsImport(toks []Token) []Token {
	for _, t := range toks {
		if t.Kind == String && t.Text == strconv.Quote(gxCollectionsPath) {
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
		tokIdent("import", p), tokSpace(" ", p), tokString(strconv.Quote(gxCollectionsPath), p), tokNewline(p),
	}
	out := append([]Token{}, toks[:insert]...)
	out = append(out, imp...)
	out = append(out, toks[insert:]...)
	return out
}
