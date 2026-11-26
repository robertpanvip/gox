package interp

import (
	"reflect"
	"strings"

	"github.com/robertpanvip/gox/pkg/compiler"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// jsxBuilder 把一段 JSX 原文构建成 gxscene.Instance。
// 采用编译期 opcode 的同一套结构，只是解析发生在运行时。
type jsxBuilder struct {
	ops    []gxscene.Op
	getters []func() any
	env    *Env
}

func (b *jsxBuilder) slot(g func() any) int {
	i := len(b.getters)
	b.getters = append(b.getters, g)
	return i
}

// renderJSX 解析 JSX 字符串并在 env 中渲染成场景树。
func renderJSX(raw string, env *Env) *gxscene.Instance {
	b := &jsxBuilder{env: env}
	p := &jsxParser{src: raw}
	node := p.parseElement()
	if node == nil { return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{}}) }
	b.buildNode(node)
	tpl := gxscene.Template{Ops: b.ops, N: len(b.getters)}
	inst := gxscene.Render(tpl, b.getters...)
	return inst
}

// ---- 运行时 JSX 解析（字符级） ----

type jsxNodeRT struct {
	name     string
	attrs    map[string]jsxAttrRT // name → (kind, value)
	attrOrder []string
	children []jsxChildRT
	selfClose bool
}
type jsxAttrRT struct{ kind int; value string } // 0=string 1=expr
type jsxChildRT struct {
	kind int // 0=text 1=expr 2=element 3=cond
	text string
	node *jsxNodeRT
	cond *jsxCondRT
}
type jsxCondRT struct {
	branches []jsxCondBranchRT // test="" 表示 else
}
type jsxCondBranchRT struct {
	test string
	children []jsxChildRT
}

type jsxParser struct {
	src string
	i   int
}

func (p *jsxParser) parseElement() *jsxNodeRT {
	p.skipWS()
	if !p.eat("<") { return nil }
	name := p.readName()
	node := &jsxNodeRT{name: name, attrs: make(map[string]jsxAttrRT)}
	// 属性
	for {
		p.skipWS()
		if p.eat("/") { p.eat(">"); node.selfClose = true; return node }
		if p.eat(">") { break }
		attrName := p.readName()
		if attrName == "" { p.i++; continue }
		p.skipWS()
		if p.eat("=") {
			p.skipWS()
			if p.eat("{") {
				val := p.readBraced()
				node.attrs[attrName] = jsxAttrRT{kind: 1, value: val}
			} else {
				node.attrs[attrName] = jsxAttrRT{kind: 0, value: p.readQuoted()}
			}
		} else {
			node.attrs[attrName] = jsxAttrRT{kind: 0, value: "true"}
		}
		node.attrOrder = append(node.attrOrder, attrName)
	}
	if node.selfClose { return node }
	// 子节点
	for p.i < len(p.src) {
		if p.at("</") { p.i += 2; p.readName(); p.eat(">"); break }
		if p.at("if ") || p.at("if\n") || p.at("if{") {
			node.children = append(node.children, jsxChildRT{kind: 3, cond: p.parseCond()})
			continue
		}
		if p.eat("{") {
			expr := p.readBraced()
			// <For> 的 children 是 lambda：{ (t string) => <...> }
			node.children = append(node.children, jsxChildRT{kind: 1, text: expr})
			continue
		}
		if p.eat("<") {
			child := &jsxParser{src: p.src, i: p.i - 1}
			cn := child.parseElement()
			p.i = child.i
			if cn != nil { node.children = append(node.children, jsxChildRT{kind: 2, node: cn}) }
			continue
		}
		text := p.readText()
		if text != "" { node.children = append(node.children, jsxChildRT{kind: 0, text: text}) }
	}
	return node
}

func (p *jsxParser) parseCond() *jsxCondRT {
	cond := &jsxCondRT{}
	// if <expr> { children } [else { children }]
	p.skipWS()
	p.i += 2 // "if"
	p.skipWS()
	// 条件到 '{'（可能含括号表达式）
	start := p.i
	depth := 0
	for p.i < len(p.src) {
		c := p.src[p.i]
		if c == '(' { depth++ }
		if c == ')' { depth-- }
		if c == '{' && depth == 0 { break }
		p.i++
	}
	test := strings.TrimSpace(p.src[start:p.i])
	br := jsxCondBranchRT{test: test}
	br.children = p.parseCondChildren()
	cond.branches = append(cond.branches, br)
	p.skipWS()
	if strings.HasPrefix(p.src[p.i:], "else") {
		p.i += 4
		p.skipWS()
		eb := jsxCondBranchRT{test: ""}
		eb.children = p.parseCondChildren()
		cond.branches = append(cond.branches, eb)
	}
	return cond
}

func (p *jsxParser) parseCondChildren() []jsxChildRT {
	out := []jsxChildRT{}
	p.skipWS()
	if !p.eat("{") { return out }
	for p.i < len(p.src) && !p.at("}") {
		if p.eat("<") {
			child := &jsxParser{src: p.src, i: p.i - 1}
			cn := child.parseElement()
			p.i = child.i
			if cn != nil { out = append(out, jsxChildRT{kind: 2, node: cn}) }
			continue
		}
		text := p.readText()
		if text != "" { out = append(out, jsxChildRT{kind: 0, text: text}) }
	}
	p.eat("}")
	return out
}

func (p *jsxParser) at(s string) bool { return strings.HasPrefix(p.src[p.i:], s) }
func (p *jsxParser) eat(s string) bool {
	if p.at(s) { p.i += len(s); return true }
	return false
}
func (p *jsxParser) skipWS() {
	for p.i < len(p.src) && (p.src[p.i] == ' ' || p.src[p.i] == '\t' || p.src[p.i] == '\n' || p.src[p.i] == '\r') { p.i++ }
}
func (p *jsxParser) readName() string {
	start := p.i
	for p.i < len(p.src) {
		c := p.src[p.i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || (c >= '0' && c <= '9') || c == '-' { p.i++ } else { break }
	}
	return p.src[start:p.i]
}
func (p *jsxParser) readQuoted() string {
	if !p.eat("\"") { return "" }
	start := p.i
	for p.i < len(p.src) && p.src[p.i] != '"' {
		if p.src[p.i] == '\\' && p.i+1 < len(p.src) { p.i++ }
		p.i++
	}
	v := p.src[start:p.i]
	p.eat("\"")
	return v
}
func (p *jsxParser) readBraced() string {
	// 已吃掉 '{'，读到匹配的 '}'（含嵌套）。
	start := p.i
	depth := 1
	for p.i < len(p.src) && depth > 0 {
		c := p.src[p.i]
		if c == '{' { depth++ }
		if c == '}' { depth-- }
		if depth == 0 { break }
		p.i++
	}
	v := p.src[start:p.i]
	p.eat("}")
	return v
}
func (p *jsxParser) readText() string {
	start := p.i
	for p.i < len(p.src) && p.src[p.i] != '<' && p.src[p.i] != '{' { p.i++ }
	return p.src[start:p.i]
}

// ---- 从 jsxNodeRT 构建 opcode ----

func (b *jsxBuilder) buildNode(node *jsxNodeRT) {
	// 大写开头 = 组件调用
	if len(node.name) > 0 && node.name[0] >= 'A' && node.name[0] <= 'Z' {
		b.buildComponent(node)
		return
	}
	b.ops = append(b.ops, gxscene.Op{Code: gxscene.OpElem, Arg: node.name})
	// For 特殊：<For each={...}>{lambda}</For>
	if node.name == "For" {
		b.buildFor(node)
		b.ops = append(b.ops, gxscene.Op{Code: gxscene.OpClose})
		return
	}
	// 属性：按 JSX 里的顺序发出（与编译器一致）
	for _, name := range node.attrOrder {
		a := node.attrs[name]
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "on") && len(name) > 2 {
			// 事件：getter 必须返回后端能派发的真实 func()/func(EventContext)，
			// 而不是 *Function 对象——gxscene.dispatchLocal 按具体签名 type switch。
			fn := b.expr(a.value)
			slot := b.slot(func() any { return toHandler(fn(b.env), b.env) })
			b.ops = append(b.ops, gxscene.Op{Code: gxscene.OpEvent, Key: name, Slot: slot})
			continue
		}
		if a.kind == 1 {
			// 动态属性（style / class 等）
			expr := b.expr(a.value)
			slot := b.slot(func() any { return toAttrValue(expr(b.env)) })
			b.ops = append(b.ops, gxscene.Op{Code: gxscene.OpDynAttr, Key: name, Slot: slot})
		} else {
			b.ops = append(b.ops, gxscene.Op{Code: gxscene.OpAttr, Key: name, Arg: a.value})
		}
	}
	// 子节点
	for _, ch := range node.children {
		b.buildChild(ch)
	}
	b.ops = append(b.ops, gxscene.Op{Code: gxscene.OpClose})
}

func (b *jsxBuilder) buildChild(ch jsxChildRT) {
	switch ch.kind {
	case 0: // 文本
		b.ops = append(b.ops, gxscene.Op{Code: gxscene.OpChildText, Arg: ch.text})
	case 1: // {expr}
		expr := b.expr(ch.text)
		// 检测 For 的 lambda children（含 "=>" 且含 JSX）
		if strings.Contains(ch.text, "=>") {
			return // 由父节点 For 的 buildFor 处理
		}
		slot := b.slot(func() any { return expr(b.env) })
		b.ops = append(b.ops, gxscene.Op{Code: gxscene.OpDynChild, Slot: slot})
	case 2: // 元素
		b.buildNode(ch.node)
	case 3: // 条件分支
		b.buildCond(ch.cond)
	}
}

func (b *jsxBuilder) buildCond(c *jsxCondRT) {
	selectors := make([]func() any, len(c.branches))
	builders := make([]func() *gxscene.Instance, len(c.branches))
	for i, br := range c.branches {
		test := br.test
		selectors[i] = func() any {
			if test == "" { return true }
			return toBool(b.expr(test)(b.env))
		}
		children := br.children
		builders[i] = func() *gxscene.Instance {
			sub := &jsxBuilder{env: b.env}
			for _, ch := range children { sub.buildChild(ch) }
			return gxscene.Render(gxscene.Template{Ops: sub.ops, N: len(sub.getters)}, sub.getters...)
		}
	}
	selector := func() int {
		for i, s := range selectors {
			if toBool(s()) { return i }
		}
		return len(selectors) - 1
	}
	slot := b.slot(gxscene.BranchGetter(selector, builders...))
	b.ops = append(b.ops, gxscene.Op{Code: gxscene.OpBranch, Slot: slot})
}

func (b *jsxBuilder) buildFor(node *jsxNodeRT) {
	eachAttr := node.attrs["each"]
	if eachAttr.value == "" { return }
	eachExpr := b.expr(eachAttr.value)
	// 找到 lambda children
	var lambdaSrc string
	for _, ch := range node.children {
		if ch.kind == 1 && strings.Contains(ch.text, "=>") { lambdaSrc = ch.text; break }
	}
	// 解析 lambda: (t string) => <JSX>
	ls := newStream(lambdaSrc)
	// 吃到 '=>'
	for !ls.eof() && !ls.at("=>") { ls.next() }
	ls.eat("=>")
	var itemName string
	ps := newStream(lambdaSrc)
	if ps.at("(") {
		ps.next()
		itemName = ps.next().Text
	}
	if itemName == "" { itemName = "item" }
	jsxRaw := ""
	for !ls.eof() {
		if ls.cur().Kind == compiler.JSX { jsxRaw = ls.cur().Text; break }
		ls.next()
	}
	if jsxRaw == "" {
		// lambda 体可能整体被词法器识别成 JSX token：重新从原文提取
		idx := strings.Index(lambdaSrc, "=>")
		rest := strings.TrimSpace(lambdaSrc[idx+2:])
		if strings.HasPrefix(rest, "<") { jsxRaw = rest }
	}
	each := func() []any {
		v := eachExpr(b.env)
		xs := toSlice(v)
		out := make([]any, len(xs))
		copy(out, xs)
		return out
	}
	getter := gxscene.ForGetterOf[any](each, nil, func(item any) *gxscene.Instance {
		local := newEnv(b.env)
		local.define(itemName, item, true) // item getter t() 读实时值
		return renderJSX(jsxRaw, local)
	})
	slot := b.slot(getter)
	b.ops = append(b.ops, gxscene.Op{Code: gxscene.OpFor, Slot: slot})
}

// toHandler 把解释器函数值转成后端可派发的具体函数类型。
// 后端 dispatchLocal 只认 func() 与 func(gxscene.EventContext)，必须匹配。
func toHandler(v Value, env *Env) any {
	switch f := v.(type) {
	case Function:
		fn := f
		return func() { callValue(fn, nil, env) }
	case *Function:
		fn := f
		// 带 EventContext 参数签名（demo 13 的 onCtx）
		if len(fn.Params) >= 1 && strings.Contains(fn.Params[0].Type, "EventContext") {
			p := fn.Params[0].Name
			return func(ctx gxscene.EventContext) {
				local := newEnv(fn.Closure)
				local.define(p, ctx, false)
				execBlock(fn.Body, local)
			}
		}
		return func() { callValue(fn, nil, env) }
	case func():
		return f
	case func(gxscene.EventContext):
		return f
	}
	return nil
}

func (b *jsxBuilder) buildComponent(node *jsxNodeRT) {
	// <Card title={text}/> → card(Props)
	fnName := lowerFirst(node.name)
	fn := b.env.lookupFunc(fnName)
	if fn == nil { return }
	props := make(map[string]any)
	for _, name := range node.attrOrder {
		a := node.attrs[name]
		if a.kind == 1 {
			expr := b.expr(a.value)
			// 动态 prop 必须存为 signalReader（具名 func() Value），对齐引擎的
			// deref 约定。若存匿名 func() any，selectorExpr 读取 p.title 时
			// deref 无法识别，会把 getter 函数本身 fmt.Sprint 成 0x... 指针地址。
			props[name] = signalReader(func() Value { return expr(b.env) })
		} else {
			props[name] = a.value
		}
	}
	slot := b.slot(func() any {
		local := newEnv(fn.Closure)
		for _, p := range fn.Params {
			if p.Name == "p" { local.define("p", props, false) }
		}
		v := execBlock(fn.Body, local)
		if inst, ok := v.(*gxscene.Instance); ok { return inst }
		return nil
	})
	b.ops = append(b.ops, gxscene.Op{Code: gxscene.OpDynChild, Slot: slot})
}

func lowerFirst(s string) string {
	if s == "" { return s }
	r := []rune(s)
	if r[0] >= 'A' && r[0] <= 'Z' { r[0] += 32 }
	return string(r)
}

// expr 解析一段表达式源码为可重复求值的闭包。
func (b *jsxBuilder) expr(src string) func(*Env) Value {
	e := parseExpr(newStream(src), 1)
	return func(env *Env) Value { return e.eval(env) }
}

// toAttrValue 把求值结果转成后端能用的属性值：style 对象 → gxscene.Style。
func toAttrValue(v Value) any {
	if m, ok := v.(map[string]Value); ok {
		st := gxscene.Style{}
		rv := reflect.ValueOf(&st).Elem()
		for k, val := range m {
			field := kebabToField(k)
			f := rv.FieldByName(field)
			if f.IsValid() && f.CanSet() { f.SetString(toStr(val)) }
		}
		return st
	}
	return v
}

func kebabToField(k string) string {
	parts := strings.Split(k, "-")
	var b strings.Builder
	for _, p := range parts {
		if p == "" { continue }
		b.WriteString(strings.ToUpper(p[:1]))
		if len(p) > 1 { b.WriteString(p[1:]) }
	}
	return b.String()
}
