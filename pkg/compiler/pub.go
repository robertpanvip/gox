package compiler

// pub 可见性转换：gx 统一小写书写，pub 标记的首字母在转译时大写。
//
// 作用域：紧随 pub 的标识符；对 func 会跳过接收者找方法名；
// 对 var/const 分组声明会大写组内每一行的首个标识符。

type stream struct {
	toks []Token
	// pubNames 记录「小写原名 → 大写导出名」，供小写形式的引用处还原。
	// 全大写书写的引用原样保留，不进此表。
	pubNames map[string]string
}

// ns 返回 i 之后第一个非空白/注释 token 的下标。
func (s *stream) ns(i int) int {
	for i < len(s.toks) {
		k := s.toks[i].Kind
		if k == Space || k == Newline || k == Comment {
			i++
			continue
		}
		return i
	}
	return i
}

// recordName 记录一处「小写原名 → 大写导出名」映射。
func (s *stream) recordName(lower, upper string) {
	if s.pubNames != nil && lower != upper {
		if _, exists := s.pubNames[lower]; !exists {
			s.pubNames[lower] = upper
		}
	}
}

func (s *stream) upper(i int) {
	if i < len(s.toks) && s.toks[i].Kind == Ident && s.toks[i].Text != "_" {
		orig := s.toks[i].Text
		up := upperFirst(orig)
		s.toks[i].Text = up
		s.recordName(orig, up)
	}
}

// skipParens 给定 '(' 下标，返回匹配 ')' 之后的下标。
func (s *stream) skipParens(open int) int {
	depth := 0
	for i := open; i < len(s.toks); i++ {
		switch s.toks[i].Text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return open + 1
}

// upperGroup 处理 `pub var (` / `pub const (` 分组：每行首标识符大写。
func (s *stream) upperGroup(open int) int {
	depth := 0
	lineStart := true
	closed := len(s.toks)
	for i := open; i < len(s.toks); i++ {
		t := s.toks[i]
		if t.Text == "(" {
			depth++
			if i == open {
				lineStart = true
				continue
			}
		}
		if t.Text == ")" {
			depth--
			if depth == 0 {
				closed = i
				break
			}
		}
		if t.Kind == Newline {
			lineStart = true
			continue
		}
		if t.Kind == Space || t.Kind == Comment {
			continue
		}
		if lineStart {
			lineStart = false
			if t.Kind == Ident && t.Text != "_" && !isGoKeyword(t.Text) {
				s.upper(i)
			}
		}
	}
	return closed
}

var goKeywords = map[string]bool{
	"break": true, "default": true, "func": true, "interface": true, "select": true,
	"case": true, "defer": true, "go": true, "map": true, "struct": true,
	"chan": true, "else": true, "goto": true, "package": true, "switch": true,
	"const": true, "fallthrough": true, "if": true, "range": true, "type": true,
	"continue": true, "for": true, "import": true, "return": true, "var": true,
}

func isGoKeyword(s string) bool { return goKeywords[s] }

// applyPubAt 对 k 处的声明关键字施加 pub 语义。
func (s *stream) applyPubAt(k int) {
	if k >= len(s.toks) || s.toks[k].Kind != Ident {
		return
	}
	switch s.toks[k].Text {
	case "func":
		j := s.ns(k + 1)
		if j < len(s.toks) && s.toks[j].Text == "(" {
			j = s.ns(s.skipParens(j))
		}
		if j < len(s.toks) && s.toks[j].Kind == Ident {
			s.upper(j)
		}
	case "type":
		s.upper(s.ns(k + 1))
	case "var", "const":
		j := s.ns(k + 1)
		if j < len(s.toks) && s.toks[j].Text == "(" {
			s.upperGroup(j)
		} else {
			s.upper(j)
		}
	case "enum":
		// 由 enum 转换层负责（pub enum → 类型名与成员均导出）
	default:
		if !isGoKeyword(s.toks[k].Text) {
			s.upper(k)
		}
	}
}

// shadowRange 表示一个局部变量名在函数体内的遮蔽区间 [From,To)。
// 在该区间内，同名标识符指向局部变量而非包级 pub 符号，restoreRefs 不得还原。
type shadowRange struct {
	name     string
	from, to int
}

// collectLocalShadows 扫描各函数体内由 := / var 声明的局部变量，
// 生成遮蔽区间（整个函数体）。用于避免 restoreRefs 把与 pub 同名的
// 局部变量误大写（M-5：pub var foo; func f(){ foo := 2 } → Foo := 2）。
func collectLocalShadows(toks []Token) []shadowRange {
	var out []shadowRange
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("func") {
			continue
		}
		// 找函数体 '{'：跳过接收者/参数/返回值的括号
		brace := -1
		for j := i + 1; j < len(toks); j++ {
			if toks[j].Text == "{" {
				brace = j
				break
			}
			if toks[j].Text == ";" || toks[j].Kind == Newline {
				// 函数签名可能跨行，不能仅凭换行停止；继续找 '{'
				continue
			}
		}
		if brace < 0 {
			continue
		}
		end := matchDelim(toks, brace, "{", "}")
		if end <= brace {
			continue
		}
		// 收集函数体内 := 左侧与 var 声明的名字（去重）
		seen := map[string]bool{}
		for k := brace + 1; k < end; k++ {
			var name string
			if toks[k].Text == ":=" {
				if p := prevSig(toks, k-1); p > brace && toks[p].Kind == Ident {
					name = toks[p].Text
				}
			} else if toks[k].is("var") {
				if n := nextSig(toks, k+1); n > brace && n < end && toks[n].Kind == Ident {
					name = toks[n].Text
				}
			}
			if name != "" && !seen[name] {
				seen[name] = true
				out = append(out, shadowRange{name, brace, end})
			}
		}
		i = end // 跳过整个函数体，避免嵌套重复扫描
	}
	return out
}

// restoreRefs 把引用处的「小写 pub 原名」还原为大写导出名。
//
// gx 约定一切标识符小写书写；pub 声明的名字在生成 Go 时被大写，
// 因此所有对它们的引用（函数调用、类型引用、字段/方法访问）也要同步大写。
// names 由 pub/enum 两层收集；已是大写或不在表中的标识符一律不动。
// 与 pub 同名的局部变量（及其函数体内的引用）不还原（M-5）。
func restoreRefs(in []Token, names map[string]string) []Token {
	if len(names) == 0 {
		return in
	}
	shadows := collectLocalShadows(in)
	out := append([]Token{}, in...)
	for i := range out {
		t := &out[i]
		if t.Kind != Ident || !isLowerStart(t.Text) {
			continue
		}
		if isGoKeyword(t.Text) || t.Text == "pub" || t.Text == "enum" {
			continue
		}
		// 位于同名局部变量的函数体遮蔽区间内 → 不还原
		shadowed := false
		for _, sh := range shadows {
			if sh.name == t.Text && i >= sh.from && i < sh.to {
				shadowed = true
				break
			}
		}
		if shadowed {
			continue
		}
		if up, ok := names[t.Text]; ok {
			t.Text = up
		}
	}
	return out
}

// transformPub 删除 pub token 并大写其目标标识符。
// names 收集「小写原名 → 大写导出名」，供引用处统一还原。
func transformPub(in []Token, names map[string]string) []Token {
	s := &stream{toks: append([]Token{}, in...), pubNames: names}
	out := make([]Token, 0, len(s.toks))
	for i := 0; i < len(s.toks); i++ {
		t := s.toks[i]
		if t.is("pub") {
			k := s.ns(i + 1)
			s.applyPubAt(k)
			continue // 丢弃 pub 本身
		}
		out = append(out, t)
	}
	return out
}
