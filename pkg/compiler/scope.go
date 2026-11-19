package compiler

import "strconv"

// Scope 收集「本文件小写声明的符号」，供外部符号还原时防误伤。
//
// GX 书写约定：外部 Go 符号按真实拼写照抄即可（NewRequest、Method）。
// 全大写缩写（URL、StatusOK）直接写大写，转译器原样保留；也兼容「首字母
// 小写」的旧写法（newRequest、uRL、method），selector 右侧的小写名由
// upperFirst 还原。全程无需 importer、无类型推导。要排除的是「自己声明的
// 私有字段/变量/方法」——它们同样是小写书写，生成 Go 时保持小写，不能还原。
//
// Scope 在 pub 转换之后收集：pub 声明的名字此时已大写，不会被误收为私有名。

type Scope struct {
	imports map[string]bool // import 包名 / 别名
	private map[string]bool // 本文件小写声明的名字（参数/局部变量/私有字段/私有方法/私有类型等）
}

func (s *Scope) HasImport(name string) bool { return s.imports[name] }
func (s *Scope) IsPrivate(name string) bool { return s.private[name] }

// collectScope 在 pub 转换之后调用；此时 pub 名已大写，天然排除。
func collectScope(toks []Token) *Scope {
	s := &Scope{imports: map[string]bool{}, private: map[string]bool{}}
	for _, sp := range collectImportSpecs(toks) {
		s.imports[sp.pkg] = true
	}
	s.collectPrivate(toks)
	return s
}

type importSpec struct {
	alias string // 显式别名，可能为空
	path  string // import 路径（未加引号）
	pkg   string // 实际包名（别名或由路径推断）
}

// collectImportSpecs 解析 import 声明，拿到别名、路径与实际包名。
func collectImportSpecs(toks []Token) []importSpec {
	var specs []importSpec
	add := func(alias, lit string) {
		realPath, err := strconv.Unquote(lit)
		if err != nil {
			realPath = stringsTrim(lit, "\"`")
		}
		pkg := alias
		if pkg == "" {
			pkg = pkgNameFromPath(lit)
		}
		if realPath == "" || pkg == "_" || pkg == "." {
			return
		}
		specs = append(specs, importSpec{alias: alias, path: realPath, pkg: pkg})
	}
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("import") {
			continue
		}
		j := i + 1
		for j < len(toks) {
			k := toks[j].Kind
			if k == Space || k == Newline || k == Comment {
				j++
				continue
			}
			break
		}
		if j < len(toks) && toks[j].Text == "(" {
			j++
			alias := ""
			for j < len(toks) && toks[j].Text != ")" {
				t := toks[j]
				switch {
				case t.Kind == Newline:
					j++
					continue
				case t.Kind == Ident && alias == "":
					alias = t.Text
				case t.Kind == String:
					add(alias, t.Text)
					alias = ""
				}
				j++
			}
			continue
		}
		alias := ""
		for j < len(toks) && toks[j].Kind != Newline && !toks[j].is("import") {
			t := toks[j]
			if t.Kind == Ident && alias == "" {
				alias = t.Text
			}
			if t.Kind == String {
				add(alias, t.Text)
				break
			}
			j++
		}
	}
	return specs
}

// stringsTrim 是 strings.Trim 的小副本，避免为此再引入 strings 包（本文件只用这一处）。
func stringsTrim(s, cutset string) string {
	for len(s) > 0 && containsRune(cutset, rune(s[0])) {
		s = s[1:]
	}
	for len(s) > 0 && containsRune(cutset, rune(s[len(s)-1])) {
		s = s[:len(s)-1]
	}
	return s
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

// collectPrivate 扫描所有「小写声明」位置。pub 名此时已大写，自动排除。
func (s *Scope) collectPrivate(toks []Token) {
	add := func(t Token) {
		if t.Kind == Ident && isLowerStart(t.Text) && !isGoKeyword(t.Text) {
			s.private[t.Text] = true
		}
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch t.Text {
		case "func":
			s.collectFuncDecls(toks, i, add)
		case "var", "const", "type":
			j := nextSig(toks, i+1)
			if j < 0 {
				continue
			}
			if toks[j].Text == "(" {
				s.collectGroupNames(toks, j, add)
				continue
			}
			// 包级 var/const/type 的名字不进私有表：它们不会出现在 selector 右侧，
			// 若收录反而会误保护「同名的外部结构体字段」。真正要保护的是 struct 字段。
			if t.is("type") {
				if st := nextSig(toks, j+1); st >= 0 && toks[st].is("struct") {
					if br := nextSig(toks, st+1); br >= 0 && toks[br].Text == "{" {
						s.collectStructFields(toks, br, add)
					}
				}
			}
		case "struct":
			// 匿名 struct（对象字面量降级产物等）：收集其字段名，
			// 避免 transformExternal 把 x.inner 误当外部字段大写。
			if br := nextSig(toks, i+1); br >= 0 && toks[br].Text == "{" {
				s.collectStructFields(toks, br, add)
			}
		case ":=":
			p := prevSig(toks, i-1)
			for p >= 0 {
				if p < len(toks) {
					add(toks[p])
				}
				q := prevSig(toks, p-1)
				if q >= 0 && toks[q].Text == "," {
					p = prevSig(toks, q-1)
				} else {
					break
				}
			}
		}
	}
}

// collectFuncDecls 收集接收者名、方法名、参数名。
func (s *Scope) collectFuncDecls(toks []Token, fi int, add func(Token)) {
	j := nextSig(toks, fi+1)
	if j < 0 {
		return
	}
	if toks[j].Text == "(" {
		s.collectParenIdents(toks, j, add) // 接收者 (p point)
		close := matchDelim(toks, j, "(", ")")
		j = nextSig(toks, close+1)
	}
	if j < 0 {
		return
	}
	add(toks[j]) // 函数名 / 方法名
	k := nextSig(toks, j+1)
	if k >= 0 && toks[k].Text == "(" {
		s.collectParenIdents(toks, k, add) // 参数
	}
}

// collectParenIdents 按逗号/换行的分段，收集每段首个标识符（参数/接收者名）。
func (s *Scope) collectParenIdents(toks []Token, open int, add func(Token)) {
	close := matchDelim(toks, open, "(", ")")
	if close <= open {
		return
	}
	first := true
	for i := open + 1; i < close; i++ {
		t := toks[i]
		if t.Kind == Space || t.Kind == Comment {
			continue
		}
		if t.Kind == Newline || t.Text == "," {
			first = true
			continue
		}
		if first {
			add(t)
			first = false
		}
	}
}

// collectGroupNames 收集 var ( ... ) / const ( ... ) 每行首个标识符。
func (s *Scope) collectGroupNames(toks []Token, open int, add func(Token)) {
	close := matchDelim(toks, open, "(", ")")
	if close <= open {
		return
	}
	lineStart := true
	for i := open + 1; i < close; i++ {
		t := toks[i]
		if t.Kind == Newline {
			lineStart = true
			continue
		}
		if t.Kind == Space || t.Kind == Comment {
			continue
		}
		if lineStart {
			add(t)
			lineStart = false
		}
	}
}

// collectStructFields 收集 struct { ... } 顶层每行首个标识符（字段名）。
func (s *Scope) collectStructFields(toks []Token, open int, add func(Token)) {
	close := matchDelim(toks, open, "{", "}")
	if close <= open {
		return
	}
	depth, lineStart := 0, true
	for i := open + 1; i <= close; i++ {
		t := toks[i]
		switch t.Text {
		case "{":
			depth++
		case "}":
			depth--
		}
		if depth > 0 {
			continue
		}
		if t.Kind == Newline || t.Text == ";" {
			lineStart = true
			continue
		}
		if t.Kind == Space || t.Kind == Comment {
			continue
		}
		if lineStart {
			add(t)
			lineStart = false
		}
	}
}

func matchDelim(toks []Token, open int, l, r string) int {
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
