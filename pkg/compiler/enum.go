package compiler

// enum 转换：enum name { a, b=10, c } → type Name int + const 块。
// pub enum 同时导出类型名与所有成员；无 pub 则保持小写。

type enumMember struct {
	name  string
	pos   Pos
	value []Token
}

func transformEnum(in []Token, names map[string]string) []Token {
	s := &stream{toks: in, pubNames: names}
	out := make([]Token, 0, len(in))
	for i := 0; i < len(s.toks); i++ {
		t := s.toks[i]
		isPub := false
		enumIdx := -1
		if t.is("pub") {
			k := s.ns(i + 1)
			if k < len(s.toks) && s.toks[k].is("enum") {
				isPub = true
				enumIdx = k
			}
		} else if t.is("enum") {
			enumIdx = i
		}
		if enumIdx < 0 {
			out = append(out, t)
			continue
		}
		gen, end := s.genEnum(enumIdx, isPub)
		out = append(out, gen...)
		i = end // 跳过 enum ... '}'
	}
	return out
}

// genEnum 生成 type+const token，并返回 '}' 的下标。
func (s *stream) genEnum(enumIdx int, isPub bool) ([]Token, int) {
	p := s.toks[enumIdx].Pos
	nameIdx := s.ns(enumIdx + 1)
	name := "enum"
	namePos := p
	if nameIdx < len(s.toks) && s.toks[nameIdx].Kind == Ident {
		name = s.toks[nameIdx].Text
		namePos = s.toks[nameIdx].Pos
	}
	openIdx := s.ns(nameIdx + 1)
	closeIdx := s.findBraceClose(openIdx)

	typeName := name
	if isPub {
		typeName = upperFirst(name)
		s.recordName(name, typeName)
	}

	members := s.collectMembers(openIdx, closeIdx)

	out := []Token{
		tokIdent("type", p), tokSpace(" ", p), tokIdent(typeName, namePos),
		tokSpace(" ", p), tokIdent("int", p), tokNewline(p),
		tokIdent("const", p), tokSpace(" ", p), tokOp("(", p), tokNewline(p),
	}
	for _, m := range members {
		mn := m.name
		if isPub {
			mn = upperFirst(m.name)
			s.recordName(m.name, mn)
		}
		out = append(out,
			tokSpace("\t", p), tokIdent(mn, m.pos), tokSpace(" ", p),
			tokIdent(typeName, p), tokSpace(" ", p), tokOp("=", p), tokSpace(" ", p))
		if len(m.value) > 0 {
			for _, vt := range m.value {
				vt.Pos = m.pos
				vt.IsGen = true
				out = append(out, vt)
			}
		} else {
			out = append(out, tokIdent("iota", m.pos))
		}
		out = append(out, tokNewline(p))
	}
	out = append(out, tokOp(")", p), tokNewline(p))
	return out, closeIdx
}

func (s *stream) findBraceClose(open int) int {
	depth := 0
	for i := open; i < len(s.toks); i++ {
		switch s.toks[i].Text {
		case "{":
			depth++
		case "}":
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(s.toks) - 1
}

func (s *stream) collectMembers(open, close int) []enumMember {
	var members []enumMember
	var seg []Token
	depth := 0
	flush := func() {
		idx := 0
		for idx < len(seg) {
			k := seg[idx].Kind
			if k == Space || k == Newline || k == Comment {
				idx++
				continue
			}
			break
		}
		if idx >= len(seg) || seg[idx].Kind != Ident {
			seg = nil
			return
		}
		m := enumMember{name: seg[idx].Text, pos: seg[idx].Pos}
		k := idx + 1
		for k < len(seg) && (seg[k].Kind == Space || seg[k].Kind == Comment) {
			k++
		}
		if k < len(seg) && seg[k].Text == "=" {
			k++
			for k < len(seg) && (seg[k].Kind == Space || seg[k].Kind == Comment || seg[k].Kind == Newline) {
				k++
			}
			m.value = append([]Token{}, seg[k:]...)
		}
		members = append(members, m)
		seg = nil
	}
	for i := open + 1; i < close; i++ {
		t := s.toks[i]
		switch t.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		}
		if depth == 0 && (t.Text == "," || t.Kind == Newline) {
			flush()
			continue
		}
		seg = append(seg, t)
	}
	flush()
	return members
}
