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

// restoreRefs 把引用处的「小写 pub 原名」还原为大写导出名。
//
// gx 约定一切标识符小写书写；pub 声明的名字在生成 Go 时被大写，
// 因此所有对它们的引用（函数调用、类型引用、字段/方法访问）也要同步大写。
// names 由 pub/enum 两层收集；已是大写或不在表中的标识符一律不动。
func restoreRefs(in []Token, names map[string]string) []Token {
	if len(names) == 0 {
		return in
	}
	out := append([]Token{}, in...)
	for i := range out {
		t := &out[i]
		if t.Kind != Ident || !isLowerStart(t.Text) {
			continue
		}
		if isGoKeyword(t.Text) || t.Text == "pub" || t.Text == "enum" {
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
