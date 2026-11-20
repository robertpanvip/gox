package compiler

import (
	"strconv"
	"strings"
)

// ---------------- JSX → gxscene opcode 模板（路线三） ----------------
//
// 与 jsx.go 的 createElement 路径并列。JSX 被拆成两部分：
//
//   - 静态结构 → gxscene.Template{Ops: []gxscene.Op{...}, N: n}
//     opcode 在编译期确定；gxscene.Render 内部按 opcode 结构缓存静态骨架，只解析一次。
//   - 动态插值 {expr} / attr={expr} → func() any { return expr } getter，
//     按 slot 顺序传给 Render，绑定到常驻场景树的对应节点。
//
// 产物示例：
//
//	<div class="card"><h1>{title}</h1></div>
//	→ gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
//	      {Code:gxscene.OpElem,Arg:"div"},
//	      {Code:gxscene.OpAttr,Key:"class",Arg:"card"},
//	      {Code:gxscene.OpElem,Arg:"h1"},
//	      {Code:gxscene.OpDynChild,Slot:0},
//	      {Code:gxscene.OpClose}, {Code:gxscene.OpClose},
//	  }, N:1}, func() any { return title })

// sceneOp 是一条待发射的 opcode 描述（编译期中间结构）。
type sceneOp struct {
	code     string // gxscene.OpElem / OpClose / ...（不含包名）
	argQuote string // 已 quote 的字符串（Arg），空表示无
	key      string // 属性名（Key），空表示无
	slot     int    // 动态插槽索引，-1 表示无
}

// sceneGetter 是一个动态插槽的取值表达式源码。
// 复合组件插槽用 call 字段直接给出调用表达式 token（而非源码串）。
type sceneGetter struct {
	expr string
	pos  Pos
	call []Token // 非空时优先于 expr，用于组件调用 func() any { return <call> }
}

// compCallTokens 生成一次复合组件调用的取值表达式：
//
//	Card(gxscene.Props{"title": func() any{ return title }, ...})
//
// 动态属性包成 getter（组件只执行一次，靠 getter 读最新值）；
// 静态属性直接放字面值；children 若存在则编成一个独立的子 Render 实例，
// 通过 "children" 属性传入（组件体用 gxscene.Child(props)() 读取）。
func compCallTokens(n *jsxNode) []Token {
	p := n.pos
	var out []Token
	// 组件名支持带点的限定名（如 ui.Card）
	parts := strings.Split(n.name, ".")
	for i, part := range parts {
		if i > 0 {
			out = append(out, tokOp(".", p))
		}
		out = append(out, tokIdent(part, p))
	}
	out = append(out, tokOp("(", p),
		tokIdent("gxscene", p), tokOp(".", p), tokIdent("Props", p), tokOp("{", p))

	for _, a := range n.attrs {
		out = append(out, tokString(strconv.Quote(a.name), a.pos), tokOp(":", p))
		switch a.kind {
		case attrBool:
			out = append(out, tokIdent("true", a.pos))
		case attrString:
			out = append(out, tokString(normalizeStringLit(a.value), a.pos))
		case attrExpr:
			out = append(out, getterCallTokens(a.value, a.pos)...)
		}
		out = append(out, tokOp(",", p))
	}

	if len(n.children) > 0 {
		out = append(out, tokString(strconv.Quote("children"), p), tokOp(":", p))
		out = append(out, childrenGetterTokens(n)...)
		out = append(out, tokOp(",", p))
	}

	out = append(out, tokOp("}", p), tokOp(")", p))
	return out
}

// childrenGetterTokens 把组件子节点编成 `func() any{ return <Render 表达式> }`，
// 直接发射 token，避免经由源码字符串往返重新词法化。
func childrenGetterTokens(n *jsxNode) []Token {
	p := n.pos
	wrap := &jsxNode{name: "#frag", pos: p, children: n.children}
	out := []Token{
		tokIdent("func", p), tokOp("(", p), tokOp(")", p), tokSpace(" ", p),
		tokIdent("any", p), tokOp("{", p), tokSpace(" ", p), tokIdent("return", p), tokSpace(" ", p),
	}
	out = append(out, genSceneExpr(wrap)...)
	out = append(out, tokSpace(" ", p), tokOp("}", p))
	return out
}

// getterCallTokens 生成 `func() any{ return <expr> }`，expr 可为源码或已经在 toks 中的表达式。
func getterCallTokens(expr string, pos Pos) []Token {
	out := []Token{
		tokIdent("func", pos), tokOp("(", pos), tokOp(")", pos), tokSpace(" ", pos),
		tokIdent("any", pos), tokOp("{", pos), tokSpace(" ", pos), tokIdent("return", pos), tokSpace(" ", pos),
	}
	out = append(out, lexExprScene(expr, pos)...)
	out = append(out, tokSpace(" ", pos), tokOp("}", pos))
	return out
}

// walkScene 深度优先遍历 jsxNode，产出 opcode 序列与 getter 列表。
// slotCounter 在各子节点间共享，保证插槽索引连续。
func walkScene(n *jsxNode, ops *[]sceneOp, getters *[]sceneGetter, slotCounter *int) {
	// 原生元素：打开标签。
	*ops = append(*ops, sceneOp{code: "OpElem", argQuote: strconv.Quote(n.name), slot: -1})

	for _, a := range n.attrs {
		switch a.kind {
		case attrBool:
			*ops = append(*ops, sceneOp{code: "OpAttr", key: a.name, argQuote: strconv.Quote("true"), slot: -1})
		case attrString:
			*ops = append(*ops, sceneOp{code: "OpAttr", key: a.name, argQuote: normalizeStringLit(a.value), slot: -1})
		case attrExpr:
			s := *slotCounter
			*slotCounter++
			*ops = append(*ops, sceneOp{code: "OpDynAttr", key: a.name, slot: s})
			*getters = append(*getters, sceneGetter{a.value, a.pos, nil})
		}
	}

	walkSceneChildren(n, ops, getters, slotCounter)

	*ops = append(*ops, sceneOp{code: "OpClose", slot: -1})
}

// walkSceneChildren 遍历子节点；复合组件子节点发射 OpComp 插槽。
func walkSceneChildren(n *jsxNode, ops *[]sceneOp, getters *[]sceneGetter, slotCounter *int) {
	for _, c := range n.children {
		switch c.kind {
		case childText:
			txt := normalizeText(c.text)
			if txt == "" {
				continue
			}
			*ops = append(*ops, sceneOp{code: "OpChildText", argQuote: strconv.Quote(txt), slot: -1})
		case childExpr:
			s := *slotCounter
			*slotCounter++
			*ops = append(*ops, sceneOp{code: "OpDynChild", slot: s})
			*getters = append(*getters, sceneGetter{c.text, c.pos, nil})
		case childElement:
			if c.node.component {
				// 复合组件：发射 OpComp 插槽，getter 是一次组件调用。
				s := *slotCounter
				*slotCounter++
				*ops = append(*ops, sceneOp{code: "OpComp", argQuote: strconv.Quote(c.node.name), slot: s})
				*getters = append(*getters, sceneGetter{pos: c.node.pos, call: compCallTokens(c.node)})
			} else {
				walkScene(c.node, ops, getters, slotCounter)
			}
		}
	}
}

// sceneOpTokens 把一个 sceneOp 转成 Go token 序列：
//
//	gxscene.Op{Code:gxscene.OpElem,Arg:"div"}
func sceneOpTokens(op sceneOp, p Pos) []Token {
	out := []Token{
		tokIdent("gxscene", p), tokOp(".", p), tokIdent("Op", p), tokOp("{", p),
		tokIdent("Code", p), tokOp(":", p),
		tokIdent("gxscene", p), tokOp(".", p), tokIdent(op.code, p), tokOp(",", p),
	}
	if op.argQuote != "" {
		out = append(out, tokIdent("Arg", p), tokOp(":", p), tokString(op.argQuote, p), tokOp(",", p))
	}
	if op.key != "" {
		out = append(out, tokIdent("Key", p), tokOp(":", p), tokString(strconv.Quote(op.key), p), tokOp(",", p))
	}
	if op.slot >= 0 {
		out = append(out, tokIdent("Slot", p), tokOp(":", p), tokNumber(strconv.Itoa(op.slot), p), tokOp(",", p))
	}
	out = append(out, tokOp("}", p))
	return out
}

// tokNumber 构造数字字面量 token（scene opcode 的 Slot/N 使用）。
func tokNumber(s string, p Pos) Token { return Token{Kind: Number, Text: s, Pos: p, IsGen: true} }

// lexExprScene 把 {expr} 内源码词法化；若含嵌套 JSX 则递归走 scene 转换。
func lexExprScene(expr string, base Pos) []Token {
	toks := New(expr).AllTokens()
	toks = transformJSXScene(toks)
	var out []Token
	for _, t := range toks {
		if t.Kind == EOF {
			continue
		}
		t.Pos = base
		t.IsGen = true
		out = append(out, t)
	}
	return out
}

// getterTokens 生成 func() any { return <expr> } token 序列。
// 复合组件槽位的 expr 是一整次调用，直接发射已构造好的 call token。
func getterTokens(g sceneGetter) []Token {
	p := g.pos
	out := []Token{
		tokIdent("func", p), tokOp("(", p), tokOp(")", p), tokSpace(" ", p),
		tokIdent("any", p), tokOp("{", p), tokSpace(" ", p), tokIdent("return", p), tokSpace(" ", p),
	}
	if len(g.call) > 0 {
		out = append(out, g.call...)
	} else {
		out = append(out, lexExprScene(g.expr, p)...)
	}
	out = append(out, tokSpace(" ", p), tokOp("}", p))
	return out
}

// genSceneExpr 为一个 JSX 根元素生成完整的表达式 token 序列。
// 根是复合组件时产出一次组件调用（返回 *Instance）；否则产出 gxscene.Render(...)。
func genSceneExpr(node *jsxNode) []Token {
	if node.component {
		return compCallTokens(node)
	}
	p := node.pos
	var ops []sceneOp
	var getters []sceneGetter
	slot := 0
	walkScene(node, &ops, &getters, &slot)

	// gxscene.Render(gxscene.Template{Ops: []gxscene.Op{ ... }, N: n}, getters...)
	out := []Token{
		tokIdent("gxscene", p), tokOp(".", p), tokIdent("Render", p), tokOp("(", p),
		tokIdent("gxscene", p), tokOp(".", p), tokIdent("Template", p), tokOp("{", p),
		tokIdent("Ops", p), tokOp(":", p),
		tokOp("[", p), tokOp("]", p), tokIdent("gxscene", p), tokOp(".", p), tokIdent("Op", p), tokOp("{", p),
	}
	for _, op := range ops {
		out = append(out, sceneOpTokens(op, p)...)
		out = append(out, tokOp(",", p))
	}
	out = append(out, tokOp("}", p), tokOp(",", p))
	out = append(out, tokIdent("N", p), tokOp(":", p), tokNumber(strconv.Itoa(len(getters)), p), tokOp(",", p), tokOp("}", p))
	for _, g := range getters {
		out = append(out, tokOp(",", p), tokSpace(" ", p))
		out = append(out, getterTokens(g)...)
	}
	out = append(out, tokOp(")", p))
	return out
}

// transformJSXScene 把 token 流里的 JSX 元素替换为 gxscene opcode 渲染表达式。
// 与 transformJSX 并列，由管线按模式二选一调用。
func transformJSXScene(in []Token) []Token {
	found := false
	for _, t := range in {
		if t.Kind == JSX {
			found = true
			break
		}
	}
	if !found {
		return in
	}
	var out []Token
	for _, t := range in {
		if t.Kind == JSX {
			parser := newJSXParser(t.Text, t.Pos)
			node := parser.parseElement()
			out = append(out, genSceneExpr(node)...)
			continue
		}
		out = append(out, t)
	}
	return out
}
