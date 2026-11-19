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
// import 路径由 module 决定：<module>/list。

var chainMethods = map[string]string{
	"map": "Map", "filter": "Filter", "reduce": "Reduce",
}

const defaultModule = "github.com/robertpanvip/gox"

func listImportPath(module string) string {
	if module == "" {
		module = defaultModule
	}
	return module + "/list"
}

func transformCollections(in []Token, module string) []Token {
	out := upperChainMethods(in)
	out, promoted := promoteAllSlices(out, "list")
	if !promoted {
		return out // 没有切片被提升，不注入无用 import
	}
	return injectListImport(out, listImportPath(module))
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
			if te > i+2 { // ']' 后确有类型表达式
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

// typeEnd 解析一个类型表达式的结束下标（exclusive）。
// 支持指针、嵌套切片/数组、包限定、泛型实例化、map[K]V 的值类型。
func typeEnd(toks []Token, start int) int {
	i := start
	if i >= len(toks) {
		return i
	}
	if toks[i].Text == "*" {
		i++
		if i >= len(toks) {
			return i
		}
	}
	// 切片/数组类型 [...]T：递归解析元素类型（[]int、[3]int、[][]int…）
	if toks[i].Text == "[" {
		close := matchBracket(toks, i)
		if close <= i {
			return i
		}
		return typeEnd(toks, close+1)
	}
	if toks[i].Kind != Ident {
		return i
	}
	base := toks[i].Text
	i++
	// 包限定 pkg.Type
	if i+1 < len(toks) && toks[i].Text == "." && toks[i+1].Kind == Ident {
		i += 2
	}
	// 方括号后缀：map[K]V 需连值类型一起吞掉；Foo[T]/[N] 实例化到此为止
	if i < len(toks) && toks[i].Text == "[" {
		close := matchBracket(toks, i)
		if close <= i {
			return i
		}
		i = close + 1
		if base == "map" {
			return typeEnd(toks, i)
		}
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
