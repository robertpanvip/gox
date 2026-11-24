package interp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/robertpanvip/gox/pkg/compiler"
	"github.com/robertpanvip/gox/runtime/signal"
)

// stream 是过滤掉空白/注释后的 token 流，带位置回溯。
type stream struct {
	toks []compiler.Token
	i    int
}

func newStream(src string) *stream {
	raw := compiler.New(src).AllTokens()
	out := make([]compiler.Token, 0, len(raw))
	for _, t := range raw {
		if t.Kind == compiler.Space || t.Kind == compiler.Newline || t.Kind == compiler.Comment {
			continue
		}
		out = append(out, t)
	}
	return &stream{toks: out}
}

func (s *stream) cur() compiler.Token {
	if s.i >= len(s.toks) { return compiler.Token{Kind: compiler.EOF} }
	return s.toks[s.i]
}
func (s *stream) at(text string) bool { return s.cur().Text == text && s.cur().Kind != compiler.EOF }
func (s *stream) next() compiler.Token {
	t := s.cur()
	if s.i < len(s.toks) { s.i++ }
	return t
}
func (s *stream) eat(text string) bool {
	if s.at(text) { s.i++; return true }
	return false
}
func (s *stream) eof() bool { return s.cur().Kind == compiler.EOF }

// ---- 表达式 AST ----

type Expr interface{ eval(*Env) Value }

type litExpr struct{ v Value }
func (e litExpr) eval(*Env) Value { return e.v }

type identExpr struct{ name string }
func (e identExpr) eval(env *Env) Value {
	if c := env.lookup(e.name); c != nil { return c.Get() }
	// 具名函数：事件属性 onClick={inc} 传函数原值（与编译器一致）。
	if f := env.lookupFunc(e.name); f != nil { return f }
	// 内建标识符
	switch e.name {
	case "true": return true
	case "false": return false
	case "nil": return nil
	}
	return nil
}

type unaryExpr struct{ op string; x Expr }
func (e unaryExpr) eval(env *Env) Value {
	v := e.x.eval(env)
	switch e.op {
	case "!": return !toBool(v)
	case "-": return -toFloat(v)
	case "+": return toFloat(v)
	}
	return nil
}

type binaryExpr struct{ op string; l, r Expr }
func (e binaryExpr) eval(env *Env) Value {
	// 短路
	if e.op == "&&" { return toBool(e.l.eval(env)) && toBool(e.r.eval(env)) }
	if e.op == "||" { return toBool(e.l.eval(env)) || toBool(e.r.eval(env)) }
	l := e.l.eval(env)
	r := e.r.eval(env)
	switch e.op {
	case "+":
		// 字符串拼接优先：任一侧是 string 就按字符串拼（GX 的 + 对 string 是拼接）。
		if _, ok := l.(string); ok { return toStr(l) + toStr(r) }
		if _, ok := r.(string); ok { return toStr(l) + toStr(r) }
		return toFloat(l) + toFloat(r)
	case "-": return toFloat(l) - toFloat(r)
	case "*": return toFloat(l) * toFloat(r)
	case "/":
		d := toFloat(r)
		if d == 0 { return 0.0 }
		return toFloat(l) / d
	case "%":
		d := toInt(r)
		if d == 0 { return 0 }
		return toInt(l) % d
	case "==": return equal(l, r)
	case "!=": return !equal(l, r)
	case "<": return toFloat(l) < toFloat(r)
	case ">": return toFloat(l) > toFloat(r)
	case "<=": return toFloat(l) <= toFloat(r)
	case ">=": return toFloat(l) >= toFloat(r)
	}
	return nil
}

func equal(a, b Value) bool {
	if s, ok := a.(string); ok { return s == toStr(b) }
	return toFloat(a) == toFloat(b)
}

// callExpr 函数调用：f(args) 或 selector.method(args)（method 由 selectorExpr 包装）。
type callExpr struct{ fn Expr; args []Expr }
func (e callExpr) eval(env *Env) Value {
	// 内建函数按 callee 的标识名分派（method 调用由 methodExpr 单独处理）。
	if id, ok := e.fn.(identExpr); ok {
		if v := builtin(id.name, e.args, env); v != builtinNoMatch { return v }
	}
	fn := e.fn.eval(env)
	args := make([]Value, len(e.args))
	for i, a := range e.args { args[i] = a.eval(env) }
	return callValue(fn, args, env)
}

var builtinNoMatch = &struct{}{}

func builtin(name string, args []Expr, env *Env) Value {
	switch name {
	case "len":
		if len(args) == 0 { return 0 }
		v := args[0].eval(env)
		switch x := v.(type) {
		case string: return len([]rune(x))
		case []Value: return len(x)
		case []string: return len(x)
		case []int: return len(x)
		case map[string]Value: return len(x)
		}
		return 0
	case "string":
		if len(args) == 0 { return "" }
		return toStr(args[0].eval(env))
	case "int":
		if len(args) == 0 { return 0 }
		return toInt(args[0].eval(env))
	case "append":
		if len(args) < 2 { return nil }
		base := toSlice(args[0].eval(env))
		out := make([]Value, 0, len(base)+len(args)-1)
		out = append(out, base...)
		for _, a := range args[1:] { out = append(out, a.eval(env)) }
		return out
	case "fmt", "strconv":
		// pkg 前缀在 selectorExpr 里处理；这里不匹配。
		return builtinNoMatch
	}
	return builtinNoMatch
}

func callValue(fn Value, args []Value, env *Env) Value {
	switch f := fn.(type) {
	case *Function:
		if f.Native != nil { return f.Native(args) }
		local := newEnv(f.Closure)
		for i, p := range f.Params {
			v := Value(nil)
			if i < len(args) { v = args[i] }
			if p.SigField { local.define(p.Name, v, true) } else { local.define(p.Name, v, false) }
		}
		return execBlock(f.Body, local)
	case signalReader:
		// item getter t()：读切片元素信号的当前值。
		return f()
	}
	return nil
}

// signalReader 表示 For 元素 getter（t()）。
type signalReader func() Value

// selectorExpr 处理 a.b：字段访问 / 包函数调用 / 方法链。
type selectorExpr struct{ obj Expr; name string }
func (e selectorExpr) eval(env *Env) Value {
	obj := e.obj.eval(env)
	if m, ok := obj.(map[string]Value); ok {
		if v, ok2 := m[e.name]; ok2 { return deref(v) }
	}
	if id, ok := e.obj.(identExpr); ok {
		switch id.name {
		case "fmt":
			return &Function{Native: func(a []Value) Value {
				var b strings.Builder
				for _, x := range a { b.WriteString(toStr(x)) }
				return b.String()
			}}
		case "strconv":
			return &Function{Native: func(a []Value) Value {
				if len(a) == 0 { return "" }
				return toStr(a[0])
			}}
		case "Signal":
			// Signal.computed / Signal.batch 由 callExpr 里的 methodExpr 处理。
		}
	}
	return nil
}

func deref(v Value) Value {
	if s, ok := v.(signalReader); ok { return s() }
	return v
}

// methodExpr 方法链 x.method(args)：filter/map/reduce/String/computed/batch。
type methodExpr struct{ recv Expr; name string; args []Expr }
func (e methodExpr) eval(env *Env) Value {
	recv := e.recv.eval(env)
	args := make([]Value, len(e.args))
	for i, a := range e.args { args[i] = a.eval(env) }
	switch e.name {
	case "filter", "Filter":
		src := toSlice(recv)
		out := []Value{}
		for _, item := range src {
			if len(args) > 0 && toBool(callValue(args[0], []Value{item}, env)) { out = append(out, item) }
		}
		return out
	case "map", "Map":
		src := toSlice(recv)
		out := make([]Value, 0, len(src))
		for _, item := range src { out = append(out, callValue(args[0], []Value{item}, env)) }
		return out
	case "reduce", "Reduce":
		src := toSlice(recv)
		acc := Value(0)
		if len(args) > 1 { acc = args[1] }
		for _, item := range src { acc = callValue(args[0], []Value{acc, item}, env) }
		return acc
	case "String":
		return toStr(recv)
	case "computed":
		// Signal.computed(() => expr)：recv 是 Signal 包标记。
		if len(args) > 0 {
			fn := args[0]
			return signal.Computed[Value](func() Value { return callValue(fn, nil, env) })
		}
	}
	return nil
}

// indexExpr a[i]
type indexExpr struct{ obj, idx Expr }
func (e indexExpr) eval(env *Env) Value {
	obj := e.obj.eval(env)
	i := toInt(e.idx.eval(env))
	xs := toSlice(obj)
	if i < 0 || i >= len(xs) { return nil }
	return deref(xs[i])
}

// structLitExpr T{field: val, ...} / []T{...} / map 字面量
type structLitExpr struct {
	typeName string
	fields   []struct{ name string; expr Expr }
	elems    []Expr // 数组字面量元素
	isArray  bool
}
func (e structLitExpr) eval(env *Env) Value {
	if e.isArray {
		out := make([]Value, len(e.elems))
		for i, x := range e.elems { out[i] = x.eval(env) }
		return out
	}
	m := make(map[string]Value)
	for _, f := range e.fields { m[f.name] = f.expr.eval(env) }
	return m
}

// templateExpr `text ${expr} ...`
type templateExpr struct{ parts []Expr; lits []string }
func (e templateExpr) eval(env *Env) Value {
	var b strings.Builder
	for i, lit := range e.lits {
		b.WriteString(lit)
		if i < len(e.parts) { b.WriteString(toStr(e.parts[i].eval(env))) }
	}
	return b.String()
}

// jsxExpr 在表达式里出现 JSX（lambda 返回值、view return）——延迟到 jsx.go 实现。
type jsxExpr struct{ raw string }
func (e jsxExpr) eval(env *Env) Value { return renderJSX(e.raw, env) }

// lambdaExpr () => expr / (x T) => expr / func(...) {...}
type lambdaExpr struct {
	params []Param
	body   Expr
	stmts  []Stmt
}
func (e lambdaExpr) eval(env *Env) Value {
	return &Function{Params: e.params, Body: e.stmts, Closure: env}
}

// ---- Pratt 表达式解析 ----

var prec = map[string]int{
	"||": 1, "&&": 2,
	"==": 3, "!=": 3,
	"<": 4, ">": 4, "<=": 4, ">=": 4,
	"+": 5, "-": 5,
	"*": 6, "/": 6, "%": 6,
}

func parseExpr(s *stream, minPrec int) Expr {
	left := parsePrefix(s)
	for !s.eof() {
		op := s.cur().Text
		p, ok := prec[op]
		if !ok || p < minPrec { break }
		s.next()
		right := parseExpr(s, p+1)
		left = binaryExpr{op: op, l: left, r: right}
	}
	return left
}

func parsePrefix(s *stream) Expr {
	t := s.next()
	switch t.Kind {
	case compiler.Number:
		if strings.Contains(t.Text, ".") {
			f, _ := strconv.ParseFloat(t.Text, 64); return litExpr{f}
		}
		n, _ := strconv.Atoi(t.Text); return litExpr{n}
	case compiler.String:
		return litExpr{unquote(t.Text)}
	case compiler.Template:
		return parseTemplate(t.Text)
	case compiler.JSX:
		return jsxExpr{raw: t.Text}
	}
	switch t.Text {
	case "!", "-", "+":
		return unaryExpr{op: t.Text, x: parseExpr(s, 7)}
	case "(":
		// 可能是 lambda 参数或括号表达式：先吃 '(', 试探。
		save := s.i
		params, ok := tryLambdaParams(s)
		if ok && s.eat("=>") {
			if s.at("{") {
				body := parseBlock(s)
				return lambdaExpr{params: params, stmts: body}
			}
			body := parseExpr(s, 1)
			return lambdaExpr{params: params, body: body, stmts: []Stmt{&returnStmt{expr: body}}}
		}
		s.i = save
		inner := parseExpr(s, 1)
		s.eat(")")
		return inner
	case "func":
		// func 字面量 func(x T) T { return ... }
		params := parseParams(s)
		// 跳过返回类型（到 '{'）
		for !s.eof() && !s.at("{") { s.next() }
		body := parseBlock(s)
		return lambdaExpr{params: params, stmts: body}
	case "[":
		// []T{...} 数组字面量
		if s.at("]") { s.next() }
		for !s.eof() && !s.at("{") { s.next() }
		return parseArrayLit(s)
	case "{":
		// 对象字面量 { key: val } （style 插值、struct 字段）
		return parseObjectLit(s)
	}
	// 标识符（可能带选择器/调用/索引链）
	if t.Kind == compiler.Ident {
		return parsePostfix(s, identExpr{name: t.Text})
	}
	return litExpr{nil}
}

func parsePostfix(s *stream, base Expr) Expr {
	for !s.eof() {
		if s.at(".") {
			s.next()
			name := s.next().Text
			if s.at("(") {
				args := parseArgs(s)
				// Signal.computed / Signal.batch
				if id, ok := base.(identExpr); ok && id.name == "Signal" {
					if name == "computed" && len(args) > 0 {
						fn := args[0]
						return &signalComputedExpr{fn: fn}
					}
					if name == "batch" && len(args) > 0 {
						return &signalBatchExpr{fn: args[0]}
					}
				}
				base = methodExpr{recv: base, name: name, args: args}
			} else {
				base = selectorExpr{obj: base, name: name}
			}
			continue
		}
		if s.at("(") {
			base = callExpr{fn: base, args: parseArgs(s)}
			continue
		}
		if s.at("[") {
			s.next()
			idx := parseExpr(s, 1)
			s.eat("]")
			base = indexExpr{obj: base, idx: idx}
			continue
		}
		if s.at("{") {
			// T{...} struct 字面量（ident 后跟 '{'）
			if id, ok := base.(identExpr); ok {
				base = parseStructLit(s, id.name)
				continue
			}
		}
		break
	}
	return base
}

type signalComputedExpr struct{ fn Expr }
func (e signalComputedExpr) eval(env *Env) Value {
	f := e.fn.eval(env)
	return signal.Computed[Value](func() Value { return callValue(f, nil, env) })
}

type signalBatchExpr struct{ fn Expr }
func (e signalBatchExpr) eval(env *Env) Value {
	f := e.fn.eval(env)
	signal.Batch(func() { callValue(f, nil, env) })
	return nil
}

func parseArgs(s *stream) []Expr {
	args := []Expr{}
	if !s.eat("(") { return args }
	for !s.eof() && !s.at(")") {
		args = append(args, parseExpr(s, 1))
		if !s.eat(",") { break }
	}
	s.eat(")")
	return args
}

func tryLambdaParams(s *stream) ([]Param, bool) {
	params := []Param{}
	if s.eat(")") { return params, true }
	for !s.eof() && !s.at(")") {
		name := s.next().Text
		if name == "," || name == ")" { break }
		typ := ""
		if !s.at(",") && !s.at(")") { typ = s.next().Text }
		params = append(params, Param{Name: name, Type: typ})
		if !s.eat(",") { break }
	}
	return params, s.eat(")")
}

func parseParams(s *stream) []Param {
	params := []Param{}
	if !s.eat("(") { return params }
	for !s.eof() && !s.at(")") {
		name := s.next().Text
		typ := ""
		if !s.at(",") && !s.at(")") { typ = s.next().Text }
		params = append(params, Param{Name: name, Type: typ})
		if !s.eat(",") { break }
	}
	s.eat(")")
	return params
}

func parseArrayLit(s *stream) Expr {
	if !s.eat("{") { return litExpr{[]Value{}} }
	elems := []Expr{}
	for !s.eof() && !s.at("}") {
		elems = append(elems, parseExpr(s, 1))
		if !s.eat(",") { break }
	}
	s.eat("}")
	return structLitExpr{isArray: true, elems: elems}
}

func parseStructLit(s *stream, typeName string) Expr {
	if !s.eat("{") { return litExpr{map[string]Value{}} }
	fields := []struct{ name string; expr Expr }{}
	for !s.eof() && !s.at("}") {
		name := s.next().Text
		s.eat(":")
		val := parseExpr(s, 1)
		fields = append(fields, struct{ name string; expr Expr }{name, val})
		if !s.eat(",") { break }
	}
	s.eat("}")
	return structLitExpr{typeName: typeName, fields: fields}
}

func parseObjectLit(s *stream) Expr {
	if !s.eat("{") { return litExpr{map[string]Value{}} }
	fields := []struct{ name string; expr Expr }{}
	for !s.eof() && !s.at("}") {
		name := unquote(s.next().Text)
		s.eat(":")
		val := parseExpr(s, 1)
		fields = append(fields, struct{ name string; expr Expr }{name, val})
		if !s.eat(",") { break }
	}
	s.eat("}")
	return structLitExpr{fields: fields}
}

func parseTemplate(raw string) Expr {
	if len(raw) < 2 { return litExpr{""} }
	inner := raw[1:len(raw)-1]
	lits := []string{}
	parts := []Expr{}
	start := 0
	for i := 0; i < len(inner); i++ {
		if inner[i] == '$' && i+1 < len(inner) && inner[i+1] == '{' {
			lits = append(lits, inner[start:i])
			depth, j := 1, i+2
			for j < len(inner) && depth > 0 {
				if inner[j] == '{' { depth++ }
				if inner[j] == '}' { depth-- }
				j++
			}
			exprSrc := inner[i+2 : j-1]
			parts = append(parts, parseExpr(newStream(exprSrc), 1))
			i = j - 1
			start = j
		}
	}
	lits = append(lits, inner[start:])
	return templateExpr{lits: lits, parts: parts}
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		v, err := strconv.Unquote(s)
		if err == nil { return v }
		return s[1:len(s)-1]
	}
	if len(s) >= 2 && s[0] == '`' && s[len(s)-1] == '`' { return s[1:len(s)-1] }
	return s
}

var _ = fmt.Sprint // 保留 fmt 依赖以便扩展诊断
