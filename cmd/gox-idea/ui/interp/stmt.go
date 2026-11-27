package interp

import (
	"github.com/robertpanvip/gox-core/pkg/compiler"
)

// Stmt 是语句 AST。
type Stmt interface{ exec(*Env) }

type sigStmt struct{ name string; expr Expr }
func (s *sigStmt) exec(env *Env) { env.define(s.name, s.expr.eval(env), true) }

type varStmt struct{ name string; expr Expr; sigField bool }
func (s *varStmt) exec(env *Env) {
	v := Value(nil)
	if s.expr != nil { v = s.expr.eval(env) }
	env.define(s.name, v, s.sigField)
}

type assignStmt struct{ name string; expr Expr }
func (s *assignStmt) exec(env *Env) { env.assign(s.name, s.expr.eval(env)) }

// destructureStmt 对象解构 var { x, y } = p：从 struct 字面量（map）取字段。
type destructureStmt struct{ names []string; expr Expr }
func (s *destructureStmt) exec(env *Env) {
	v := s.expr.eval(env)
	if m, ok := v.(map[string]Value); ok {
		for _, name := range s.names { env.define(name, deref(m[name]), false) }
	}
}

type returnStmt struct{ expr Expr }
func (s *returnStmt) exec(*Env) {}
func (s *returnStmt) value(env *Env) Value { if s.expr == nil { return nil }; return s.expr.eval(env) }

type exprStmt struct{ expr Expr }
func (s *exprStmt) exec(env *Env) { s.expr.eval(env) }

type blockStmt struct{ stmts []Stmt }
func (s *blockStmt) exec(env *Env) { execBlock(s.stmts, env) }

type ifStmt struct {
	cond Expr
	then []Stmt
	else_ []Stmt
}
func (s *ifStmt) exec(env *Env) {
	if toBool(s.cond.eval(env)) { execBlock(s.then, env) } else { execBlock(s.else_, env) }
}

// execBlock 顺序执行语句，遇到 return 提前返回其值。
type execResult struct{ returned bool; value Value }

func execBlock(stmts []Stmt, env *Env) Value {
	for _, st := range stmts {
		if r, ok := st.(*returnStmt); ok { return r.value(env) }
		st.exec(env)
	}
	return nil
}

// ---- 顶层解析 ----

type Program struct {
	sigs  []*sigStmt
	funcs map[string]*Function
	env   *Env
}

// parseProgram 解析顶层：sig / var / func / enum / type 声明。
func parseProgram(src string) *Program {
	s := newStream(src)
	p := &Program{funcs: make(map[string]*Function), env: newEnv(nil)}
	for !s.eof() {
		switch s.cur().Text {
		case "sig":
			s.next()
			name := s.next().Text
			s.eat("=")
			expr := parseExpr(s, 1)
			p.sigs = append(p.sigs, &sigStmt{name: name, expr: expr})
		case "var":
			s.next()
			name := s.next().Text
			// var { x, y } = p 解构：暂按普通 var 跳过花括号。
			if s.eat("=") {
				p.env.define(name, parseExpr(s, 1).eval(p.env), false)
			} else {
				p.env.define(name, nil, false)
			}
		case "pub":
			s.next()
			// pub enum / pub func / pub type
			if s.at("func") {
				parseFuncInto(s, p)
			} else {
				skipDecl(s)
			}
		case "func":
			parseFuncInto(s, p)
		case "type":
			// type name struct { sig field T ... } —— 收集 sig 字段名供构造时识别。
			parseTypeDecl(s, p)
		case "enum":
			parseEnumDecl(s, p)
		default:
			s.next()
		}
	}
	// 先建 sig（函数体可能引用）
	for _, st := range p.sigs { st.exec(p.env) }
	return p
}

func parseFuncInto(s *stream, p *Program) {
	s.eat("func")
	name := s.next().Text
	params := parseParams(s)
	// 跳过返回类型（到 '{'）
	for !s.eof() && !s.at("{") { s.next() }
	body := parseBlock(s)
	p.funcs[name] = &Function{Params: params, Body: body, Closure: p.env}
	p.env.funcs[name] = p.funcs[name]
}

func skipDecl(s *stream) {
	for !s.eof() {
		if s.at("{") { parseBlock(s); return }
		s.next()
	}
}

// sigFieldsByType 记录每个 struct 的 sig 字段名（demo 14: type state struct { sig count int }）。
var sigFieldsByType = map[string]map[string]bool{}

func parseTypeDecl(s *stream, p *Program) {
	s.eat("type")
	name := s.next().Text
	fields := map[string]bool{}
	// 找到 struct { ... }
	for !s.eof() && !s.at("{") { s.next() }
	if s.eat("{") {
		for !s.eof() && !s.at("}") {
			isSig := s.eat("sig")
			fname := s.next().Text
			if isSig { fields[fname] = true }
			for !s.eof() && !s.at("}") && !s.at("\n") && s.cur().Kind != compiler.EOF {
				if s.at("}") { break }
				s.next()
			}
		}
		s.eat("}")
	}
	if len(fields) > 0 { sigFieldsByType[name] = fields }
}

func parseEnumDecl(s *stream, p *Program) {
	// pub enum color string { red = "#..", ... } —— 生成 colorRed / colorGreen 等 sig/常量。
	s.eat("enum")
	typeName := s.next().Text
	for !s.eof() && !s.at("{") { s.next() }
	if !s.eat("{") { return }
	for !s.eof() && !s.at("}") {
		member := s.next().Text
		s.eat("=")
		val := parseExpr(s, 1).eval(p.env)
		p.env.define(typeName+upperFirst(member), val, false)
		if !s.eat(",") { break }
	}
	s.eat("}")
}

func upperFirst(s string) string {
	if s == "" { return s }
	r := []rune(s)
	if r[0] >= 'a' && r[0] <= 'z' { r[0] -= 32 }
	return string(r)
}

// parseBlock 解析 { ... } 内的语句。
func parseBlock(s *stream) []Stmt {
	stmts := []Stmt{}
	if !s.eat("{") { return stmts }
	for !s.eof() && !s.at("}") {
		st := parseStmt(s)
		if st != nil { stmts = append(stmts, st) }
	}
	s.eat("}")
	return stmts
}

func parseStmt(s *stream) Stmt {
	switch s.cur().Text {
	case "return":
		s.next()
		if s.at("}") { return &returnStmt{} }
		return &returnStmt{expr: parseExpr(s, 1)}
	case "sig":
		s.next()
		name := s.next().Text
		s.eat("=")
		return &sigStmt{name: name, expr: parseExpr(s, 1)}
	case "var":
		s.next()
		// 对象解构 var { x, y } = p
		if s.at("{") {
			s.next()
			names := []string{}
			for !s.eof() && !s.at("}") {
				names = append(names, s.next().Text)
				if !s.eat(",") { break }
			}
			s.eat("}")
			s.eat("=")
			src := parseExpr(s, 1)
			return &destructureStmt{names: names, expr: src}
		}
		name := s.next().Text
		s.eat("=")
		return &varStmt{name: name, expr: parseExpr(s, 1)}
	case "if":
		s.next()
		cond := parseExpr(s, 1)
		then := parseBlock(s)
		else_ := []Stmt{}
		if s.eat("else") { else_ = parseBlock(s) }
		return &ifStmt{cond: cond, then: then, else_: else_}
	case "{":
		return &blockStmt{stmts: parseBlock(s)}
	case "func":
		// 函数内的 func 字面量：作为表达式语句吃掉。
		return &exprStmt{expr: parseExpr(s, 1)}
	}
	// 赋值 or 表达式 / := 声明
	save := s.i
	if s.cur().Kind == compiler.Ident {
		name := s.next().Text
		if s.eat(":=") { return &varStmt{name: name, expr: parseExpr(s, 1)} }
		if s.at("=") { s.next(); return &assignStmt{name: name, expr: parseExpr(s, 1)} }
	}
	s.i = save
	return &exprStmt{expr: parseExpr(s, 1)}
}
