package compiler

import (
	"fmt"
	"strings"
)

// destructure.go — GX 对象解构声明 → Go 逐字段变量声明。
//
// GX 写法（复用 var / :=，不引入新关键字）：
//
//	var { name, age } = p     // → var name = p.name; var age = p.age
//	{ name, age } := p        // → name := p.name; age := p.age
//
// 「构造后立刻解构」的类型检查（用户定义）：
//   - S₁ = 构造对象字面量的字段集（由 transformObjectLits 层校验 ⊆ fields(T)）
//   - S₂ = 解构模式的字段名集合
//   - 本层校验 S₂ ⊆ fields(T)；S₁ 已由上一层保证，故 S₁ ∪ S₂ 都在 T 里。
//
// fields(T) 来源：右值标识符的声明类型（var p Student / p := Student{...} /
// p := struct{...}{...}）。类型未知时宽松生成（不校验），与对象字面量一致。
//
// 校验失败（解构了 T 不存在的字段）→ 编译报错，位置为该字段名的 GX 坐标。

// destructName 是解构模式里的一个字段名。
type destructName struct {
	name string
	pos  Pos
}

// destructure 是一处对象解构声明。
type destructure struct {
	open, close int // '{' / '}' 下标
	from, to    int // 整体替换区间 [from, to)
	names       []destructName
	rhsFrom     int  // 右值表达式 token 起点
	useVar      bool // true → 生成 var name = p.name；false → name := p.name
}

// destructError 是解构校验失败的错误，携带 GX 坐标。
type destructError struct {
	pos Pos
	msg string
}

func (e *destructError) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.pos.Line, e.pos.Col, e.msg)
}

// detectDestructures 扫描 token 流里的对象解构声明。
//
// 识别条件：'{' ... '}' 内是纯标识符列表（逗号分隔、无冒号、无值），且 '}' 后
// 紧跟 '=' 或 ':='。':=' 形式要求 '{' 处于语句起始位置（排除 map/struct 字面量
// CompositeLit）；'=' 形式要求 '{' 前是 var 关键字。
func detectDestructures(toks []Token) []destructure {
	var out []destructure
	for i := 0; i < len(toks); i++ {
		if toks[i].Text != "{" || toks[i].Kind != Operator {
			continue
		}
		close := matchDelimSkipSpace(toks, i, "{", "}")
		if close <= i {
			continue
		}
		names, ok := parseDestructNames(toks, i+1, close)
		if !ok || len(names) == 0 {
			continue
		}
		eq := nextSig(toks, close+1)
		if eq < 0 || (toks[eq].Text != "=" && toks[eq].Text != ":=") {
			continue
		}
		useVar := toks[eq].Text == "="
		p := prevSig(toks, i-1)
		from := i
		if useVar {
			// '=' 形式：必须是 var { ... } = expr
			if p < 0 || !toks[p].is("var") {
				continue
			}
			from = p
		} else {
			// ':=' 形式：'{' 必须处于语句起始（行首 / ; / { / } / 开头）
			if !destructStmtStart(toks, p) {
				continue
			}
		}
		rhsFrom := nextSig(toks, eq+1)
		if rhsFrom < 0 || toks[rhsFrom].Kind == Newline {
			continue
		}
		rhsTo := exprEndInStmt(toks, rhsFrom, len(toks))
		if rhsTo <= rhsFrom {
			continue
		}
		out = append(out, destructure{
			open: i, close: close, from: from, to: rhsTo,
			names: names, rhsFrom: rhsFrom, useVar: useVar,
		})
	}
	return out
}

// destructStmtStart 判断 p 处的 token 是否标志着一条新语句的开始。
func destructStmtStart(toks []Token, p int) bool {
	if p < 0 {
		return true
	}
	if toks[p].Kind == Newline {
		return true
	}
	switch toks[p].Text {
	case ";", "{", "}":
		return true
	}
	return false
}

// parseDestructNames 解析 '{' 与 '}' 之间的解构名列表：每段必须是单个标识符。
// 含冒号（对象字面量）、含值表达式、空段一律判定为非解构。
func parseDestructNames(toks []Token, start, end int) ([]destructName, bool) {
	segs := splitTopLevelSegs(toks, start, end)
	var out []destructName
	for _, seg := range segs {
		first := nextSigIn(toks, seg[0], seg[1])
		last := prevSigIn(toks, seg[1]-1, seg[0])
		if first < 0 || last < 0 || first != last {
			return nil, false // 段内有多个有效 token（冒号/值）
		}
		t := toks[first]
		if t.Kind != Ident || isGoKeyword(t.Text) || t.Text == "_" {
			return nil, false
		}
		out = append(out, destructName{name: t.Text, pos: t.Pos})
	}
	return out, len(out) > 0
}

// destructSourceFields 推断右值标识符 ident 的字段集合。
// 返回 nil 表示无法推断（调用方宽松处理，不校验）。
func destructSourceFields(toks []Token, ident string, at int, structs map[string]*structInfo, b *Bindings) map[string]bool {
	// 1) 先用共享类型推断（var p Student / p := 字面量）
	if typ := lookupIdentType(b, toks, ident, at); typ != "" {
		if info, ok := structs[typ]; ok {
			return fieldSet(info)
		}
		if fields := fieldsFromStructText(typ); fields != nil {
			return fields
		}
	}
	// 2) 扫描 ident 的声明，解析其复合字面量的字段
	//    覆盖 p := struct{...}{...} / p := Student{...}（inferIdentType 不识别复合字面量）
	for i := 0; i < at && i < len(toks); i++ {
		if toks[i].Kind != Ident || toks[i].Text != ident {
			continue
		}
		n := nextSig(toks, i+1)
		if n < 0 || n >= at || (toks[n].Text != ":=" && toks[n].Text != "=") {
			continue
		}
		e := nextSig(toks, n+1)
		if e < 0 || e >= at {
			continue
		}
		if toks[e].is("struct") {
			open := nextSig(toks, e+1)
			if open < 0 || toks[open].Text != "{" {
				continue
			}
			close := matchDelimSkipSpace(toks, open, "{", "}")
			if close > open {
				return fieldsFromStructTokens(toks, open+1, close)
			}
		} else if toks[e].Kind == Ident {
			if info, ok := structs[toks[e].Text]; ok {
				return fieldSet(info)
			}
		}
	}
	return nil
}

func fieldSet(info *structInfo) map[string]bool {
	out := make(map[string]bool, len(info.fields))
	for name := range info.fields {
		out[name] = true
	}
	return out
}

// fieldsFromStructTokens 从 struct 体 token 区间解析字段名集合。
func fieldsFromStructTokens(toks []Token, start, end int) map[string]bool {
	out := map[string]bool{}
	for _, seg := range splitStructFields(toks, start, end) {
		name, ftype := parseStructField(toks, seg[0], seg[1])
		if name != "" && ftype != "" {
			out[name] = true
		}
	}
	return out
}

// fieldsFromStructText 从 `struct{ name T; ... }` 文本解析字段名集合。
func fieldsFromStructText(text string) map[string]bool {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "struct") {
		return nil
	}
	toks := New(t).AllTokens()
	open := -1
	for i, tok := range toks {
		if tok.Text == "{" && tok.Kind == Operator {
			open = i
			break
		}
	}
	if open < 0 {
		return nil
	}
	close := matchDelimSkipSpace(toks, open, "{", "}")
	if close <= open {
		return nil
	}
	return fieldsFromStructTokens(toks, open+1, close)
}

// rhsIdentifier 若右值是单个标识符（可选 selector 的接收者），返回该标识符。
// 解构校验只针对「标识符右值」，复杂表达式跳过校验（宽松生成）。
func rhsIdentifier(toks []Token, from, to int) string {
	first := nextSigIn(toks, from, to)
	last := prevSigIn(toks, to-1, from)
	if first < 0 || last < 0 || first != last || toks[first].Kind != Ident {
		return ""
	}
	return toks[first].Text
}

// expandDestructures 校验并改写所有对象解构。
// 校验失败返回 *destructError（带 GX 坐标）。
func expandDestructures(in []Token, b *Bindings) ([]Token, error) {
	structs := collectStructTypes(in)
	ds := detectDestructures(in)
	if len(ds) == 0 {
		return in, nil
	}
	out := append([]Token(nil), in...)
	// 从后往前替换，避免下标漂移
	for k := len(ds) - 1; k >= 0; k-- {
		d := ds[k]
		rhsIdent := rhsIdentifier(out, d.rhsFrom, d.to)
		fields := map[string]bool(nil)
		if rhsIdent != "" {
			fields = destructSourceFields(out, rhsIdent, d.rhsFrom, structs, b)
		}
		// 校验 S₂ ⊆ fields(T)；fields 为 nil（类型未知）时宽松放行
		if fields != nil {
			for _, nm := range d.names {
				if !fields[nm.name] {
					have := make([]string, 0, len(fields))
					for f := range fields {
						have = append(have, f)
					}
					return nil, &destructError{pos: nm.pos, msg: fmt.Sprintf(
						"解构 %s 没有字段 %q（可用字段: %s）", rhsIdent, nm.name, strings.Join(have, ", "))}
				}
			}
		}
		// 右值 token（去掉尾部空白/注释）
		rhs := make([]Token, 0, d.to-d.rhsFrom)
		for i := d.rhsFrom; i < d.to; i++ {
			if toks := out[i]; toks.Kind != Space && toks.Kind != Comment {
				rhs = append(rhs, toks)
			}
		}
		// M-6：复杂右值（非单个标识符）只求值一次——先存临时变量，各字段从它取。
		// 简单标识符（如 p）重复引用无副作用，保留原样以保持生成代码简洁。
		base := rhs
		var gen []Token
		if len(rhs) != 1 || rhs[0].Kind != Ident {
			tmp := fmt.Sprintf("__gx_ds_%d_%d", d.rhsFrom, len(d.names))
			tp := rhs[0].Pos
			gen = append(gen, tokIdent(tmp, tp), tokSpace(" ", tp), tokOp(":=", tp), tokSpace(" ", tp))
			gen = append(gen, rhs...)
			gen = append(gen, tokNewline(tp))
			base = []Token{tokIdent(tmp, tp)}
		}
		// 生成 name := p.name / var name = p.name，每个字段一行
		for _, nm := range d.names {
			p := nm.pos
			if d.useVar {
				gen = append(gen, tokIdent("var", p), tokSpace(" ", p))
			}
			gen = append(gen, tokIdent(nm.name, p), tokSpace(" ", p))
			if d.useVar {
				gen = append(gen, tokOp("=", p))
			} else {
				gen = append(gen, tokOp(":=", p))
			}
			gen = append(gen, tokSpace(" ", p))
			gen = append(gen, base...)
			gen = append(gen, tokOp(".", p), tokIdent(nm.name, p), tokNewline(p))
		}
		out = append(out[:d.from], append(gen, out[d.to:]...)...)
	}
	return out, nil
}
