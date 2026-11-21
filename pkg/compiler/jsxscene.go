package compiler

import (
	"strconv"
	"strings"
)

// ---------------- JSX → gxscene opcode 模板 ----------------
//
// 这是唯一的 JSX 后端。JSX 被拆成两部分：
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

// jsxSceneCtx 携带 JSX scene 转换所需的跨阶段信息：
//   - sigs：signal 变量名集合（插值里读补 ()）
//   - comps：强类型组件契约表（组件名 → props 具名 struct 类型名）。
//     命中契约的组件生成裸对象字面量 Card({...})，由管线第二次 objectlit
//     从签名补全 Card(CardProps{...}) 并做字段校验；未命中的走老式
//     gxscene.Props getter-map 路径（getter 保鲜）。
type jsxSceneCtx struct {
	sigs  map[string]bool
	comps map[string]string
	// sigTypes: signal 变量声明类型（改写前收集，如 users→[]User），
	// 供 <For> 在 render 参数无类型注解时从 each 推断元素类型。
	sigTypes map[string]string
	// bindings: 统一绑定表，回退查询普通变量/函数形参类型。
	bindings *Bindings
	// jsxPos: 当前 JSX token 在原始源码中的绝对位置。JSX 内部节点（如嵌套
	// 的 <For>）的 pos 是解析器生成的相对偏移，不能直接用于 bindings 查询，
	// 故以根 JSX token 的绝对 pos 作为锚点。
	jsxPos Pos
}

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
	raw  bool    // 为 true 时 call 本身就是一个 func() any getter（如 BranchGetter），不再包 return
}

// componentNameTokens 发射组件名（支持带点的限定名，如 ui.Card）。
func componentNameTokens(n *jsxNode, p Pos) []Token {
	var out []Token
	parts := strings.Split(n.name, ".")
	for i, part := range parts {
		if i > 0 {
			out = append(out, tokOp(".", p))
		}
		out = append(out, tokIdent(part, p))
	}
	return out
}

// compCallTokens 生成一次复合组件调用。分两条路径：
//
//  1. 强类型契约（func Card(props CardProps)）：生成裸对象字面量
//     Card({ title: expr, n: expr, children: expr })
//     属性值直接是快照表达式（不包 getter）；由管线第二次 transformObjectLits
//     补全为 Card(CardProps{...})，并校验字段存在/类型/缺失。
//
//  2. 老式 gxscene.Props：Card(gxscene.Props{"title": func() any{...}, ...})
//     动态属性包成 getter（组件只执行一次，靠 getter 读最新值），children 编成
//     独立子 Render 实例经 "children" 传入。
func compCallTokens(n *jsxNode, ctx *jsxSceneCtx) []Token {
	compName := n.name
	if idx := strings.LastIndex(compName, "."); idx >= 0 {
		compName = compName[idx+1:]
	}
	if _, typed := ctx.comps[compName]; typed {
		return compCallTyped(n, ctx)
	}
	return compCallProps(n, ctx)
}

// compCallTyped 生成强类型契约组件调用：Card({ title: expr, ... })。
// 属性值直传快照表达式，children 直接是子 Render 表达式（快照子树）。
func compCallTyped(n *jsxNode, ctx *jsxSceneCtx) []Token {
	p := n.pos
	out := componentNameTokens(n, p)
	out = append(out, tokOp("(", p), tokOp("{", p))

	for _, a := range n.attrs {
		out = append(out, tokIdent(a.name, a.pos), tokOp(":", p))
		switch a.kind {
		case attrBool:
			out = append(out, tokIdent("true", a.pos))
		case attrString:
			out = append(out, tokString(normalizeStringLit(a.value), a.pos))
		case attrExpr:
			out = append(out, lexExprScene(a.value, a.pos, ctx)...)
		}
		out = append(out, tokOp(",", p))
	}

	if len(n.children) > 0 {
		out = append(out, tokIdent("children", p), tokOp(":", p))
		wrap := &jsxNode{name: "#frag", pos: p, children: n.children}
		out = append(out, genSceneExpr(wrap, ctx)...)
		out = append(out, tokOp(",", p))
	}

	out = append(out, tokOp("}", p), tokOp(")", p))
	return out
}

// compCallProps 生成老式 gxscene.Props getter-map 组件调用。
func compCallProps(n *jsxNode, ctx *jsxSceneCtx) []Token {
	p := n.pos
	out := componentNameTokens(n, p)
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
			out = append(out, getterCallTokens(a.value, a.pos, ctx)...)
		}
		out = append(out, tokOp(",", p))
	}

	if len(n.children) > 0 {
		out = append(out, tokString(strconv.Quote("children"), p), tokOp(":", p))
		out = append(out, childrenGetterTokens(n, ctx)...)
		out = append(out, tokOp(",", p))
	}

	out = append(out, tokOp("}", p), tokOp(")", p))
	return out
}

// childrenGetterTokens 把组件子节点编成 `func() any{ return <Render 表达式> }`，
// 直接发射 token，避免经由源码字符串往返重新词法化。
func childrenGetterTokens(n *jsxNode, ctx *jsxSceneCtx) []Token {
	p := n.pos
	wrap := &jsxNode{name: "#frag", pos: p, children: n.children}
	out := []Token{
		tokIdent("func", p), tokOp("(", p), tokOp(")", p), tokSpace(" ", p),
		tokIdent("any", p), tokOp("{", p), tokSpace(" ", p), tokIdent("return", p), tokSpace(" ", p),
	}
	out = append(out, genSceneExpr(wrap, ctx)...)
	out = append(out, tokSpace(" ", p), tokOp("}", p))
	return out
}

// getterCallTokens 生成 `func() any{ return <expr> }`，expr 可为源码或已经在 toks 中的表达式。
func getterCallTokens(expr string, pos Pos, ctx *jsxSceneCtx) []Token {
	out := []Token{
		tokIdent("func", pos), tokOp("(", pos), tokOp(")", pos), tokSpace(" ", pos),
		tokIdent("any", pos), tokOp("{", pos), tokSpace(" ", pos), tokIdent("return", pos), tokSpace(" ", pos),
	}
	out = append(out, lexExprScene(expr, pos, ctx)...)
	out = append(out, tokSpace(" ", pos), tokOp("}", pos))
	return out
}

// isEventAttrName 判断是否为事件属性。规则极宽：以 "on" 开头且后面还有内容即视为事件，
// 前端**不设白名单**——onClick/onMouseEnter 等标准事件与后端自定义的 onFoo 一视同仁，
// 全部原样透传。唯一的例外是 "once"/"only" 这类恰好以 on 开头的普通属性，这里用
// 「第 3 个字符为大写字母」收窄：GX/JS 事件命名惯例是 onXxx（X 大写），故不会误伤。
func isEventAttrName(name string) bool {
	if len(name) <= 2 || name[0] != 'o' || name[1] != 'n' {
		return false
	}
	c := name[2]
	return c >= 'A' && c <= 'Z'
}

// eventGetter 为事件属性生成取值 getter。attrExpr 走正常表达式词法化；
// 静态字符串/bool 事件包成常量 getter（后端按需解释，前端不假设签名）。
func eventGetter(a jsxAttr) sceneGetter {
	switch a.kind {
	case attrExpr:
		return sceneGetter{expr: a.value, pos: a.pos}
	case attrString:
		return sceneGetter{pos: a.pos, call: []Token{tokString(normalizeStringLit(a.value), a.pos)}}
	default: // attrBool
		return sceneGetter{pos: a.pos, call: []Token{tokIdent("true", a.pos)}}
	}
}

// walkScene 深度优先遍历 jsxNode，产出 opcode 序列与 getter 列表。
// slotCounter 在各子节点间共享，保证插槽索引连续。
func walkScene(n *jsxNode, ops *[]sceneOp, getters *[]sceneGetter, slotCounter *int, ctx *jsxSceneCtx) {
	// 原生元素：打开标签。
	*ops = append(*ops, sceneOp{code: "OpElem", argQuote: strconv.Quote(n.name), slot: -1})

	for _, a := range n.attrs {
		// 事件属性 on*：无条件透传为 OpEvent，前端不设白名单、不校验事件名，
		// 后端自定义事件（如 onFoo）同样原样进 Node.Handlers。静态字符串/bool 的
		// on* 也放行（编译为常量 getter，后端按需解释）。
		if isEventAttrName(a.name) {
			s := *slotCounter
			*slotCounter++
			*ops = append(*ops, sceneOp{code: "OpEvent", key: a.name, slot: s})
			*getters = append(*getters, eventGetter(a))
			continue
		}
		switch a.kind {
		case attrBool:
			*ops = append(*ops, sceneOp{code: "OpAttr", key: a.name, argQuote: strconv.Quote("true"), slot: -1})
		case attrString:
			*ops = append(*ops, sceneOp{code: "OpAttr", key: a.name, argQuote: normalizeStringLit(a.value), slot: -1})
		case attrExpr:
			s := *slotCounter
			*slotCounter++
			*ops = append(*ops, sceneOp{code: "OpDynAttr", key: a.name, slot: s})
			*getters = append(*getters, sceneGetter{expr: a.value, pos: a.pos})
		}
	}

	walkSceneChildren(n, ops, getters, slotCounter, ctx)

	*ops = append(*ops, sceneOp{code: "OpClose", slot: -1})
}

// walkSceneChildren 遍历子节点；复合组件子节点发射 OpComp 插槽。
func walkSceneChildren(n *jsxNode, ops *[]sceneOp, getters *[]sceneGetter, slotCounter *int, ctx *jsxSceneCtx) {
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
			*getters = append(*getters, sceneGetter{expr: c.text, pos: c.pos})
		case childElement:
			if c.node.name == "For" {
				// Keyed 列表：发射 OpFor 插槽，getter 是 ForGetterOf(...) 工厂。
				s := *slotCounter
				*slotCounter++
				*ops = append(*ops, sceneOp{code: "OpFor", slot: s})
				*getters = append(*getters, sceneGetter{pos: c.node.pos, call: forTokens(c.node, ctx), raw: true})
			} else if c.node.component {
				// 复合组件：发射 OpComp 插槽，getter 是一次组件调用。
				s := *slotCounter
				*slotCounter++
				*ops = append(*ops, sceneOp{code: "OpComp", argQuote: strconv.Quote(c.node.name), slot: s})
				*getters = append(*getters, sceneGetter{pos: c.node.pos, call: compCallTokens(c.node, ctx)})
			} else {
				walkScene(c.node, ops, getters, slotCounter, ctx)
			}
		case childCond:
			// 条件分支：发射 OpBranch 插槽，getter 是 gxscene.BranchGetter(...)。
			s := *slotCounter
			*slotCounter++
			*ops = append(*ops, sceneOp{code: "OpBranch", slot: s})
			*getters = append(*getters, sceneGetter{pos: c.cond.pos, call: branchGetterTokens(c.cond, ctx), raw: true})
		}
	}
}

// branchGetterTokens 为条件分支生成 getter 表达式：
//
//	gxscene.BranchGetter(
//	    func() int { if <test0> { return 0 }; if <test1> { return 1 }; return 2 },
//	    func() *gxscene.Instance { return <branch0 子树> },
//	    func() *gxscene.Instance { return <branch1 子树> },
//	    ...
//	)
//
// 最后一条无 test 的分支是 else 兜底。若没有显式 else，selector 返回 -1，
// Branch.Resolve 会渲染空。
func branchGetterTokens(c *jsxCond, ctx *jsxSceneCtx) []Token {
	p := c.pos
	out := []Token{
		tokIdent("gxscene", p), tokOp(".", p), tokIdent("BranchGetter", p), tokOp("(", p),
	}
	// selector
	out = append(out, tokIdent("func", p), tokOp("(", p), tokOp(")", p), tokSpace(" ", p),
		tokIdent("int", p), tokOp("{", p))
	for i, br := range c.branches {
		if br.test == "" {
			continue // else 兜底，不进 if 链
		}
		out = append(out, tokIdent("if", p), tokSpace(" ", p))
		out = append(out, lexExprScene(br.test, br.pos, ctx)...)
		out = append(out, tokSpace(" ", p), tokOp("{", p), tokSpace(" ", p),
			tokIdent("return", p), tokSpace(" ", p), tokNumber(strconv.Itoa(i), p), tokSpace(" ", p),
			tokOp("}", p), tokNewline(p))
	}
	// 兜底返回值：有 else 分支则返回其索引，否则 -1（渲染空）。
	fallback := "-1"
	for i, br := range c.branches {
		if br.test == "" {
			fallback = strconv.Itoa(i)
			break
		}
	}
	out = append(out, tokIdent("return", p), tokSpace(" ", p), tokNumber(fallback, p), tokSpace(" ", p), tokOp("}", p))

	// builders：每个分支一个 func() *gxscene.Instance { return <子树> }
	for _, br := range c.branches {
		out = append(out, tokOp(",", p), tokSpace(" ", p))
		out = append(out, tokIdent("func", p), tokOp("(", p), tokOp(")", p), tokSpace(" ", p),
			tokOp("*", p), tokIdent("gxscene", p), tokOp(".", p), tokIdent("Instance", p),
			tokSpace(" ", p), tokOp("{", p), tokSpace(" ", p),
			tokIdent("return", p), tokSpace(" ", p))
		wrap := &jsxNode{name: "#frag", pos: br.pos, children: br.children}
		out = append(out, genSceneExpr(wrap, ctx)...)
		out = append(out, tokSpace(" ", p), tokOp("}", p))
	}
	out = append(out, tokOp(")", p))
	return out
}

// forTokens 为 <For> 节点生成 gxscene.ForGetterOf[T](each, key, render)。
//
// 语法（render lambda 参数类型可省略，从 each 推断；显式注解优先）：
//
//	<For each={users} key={(u) => u.id}>
//	    {(u) => <li>{u.name}</li>}       // 从 users 的 []User 推断出 User
//	</For>
//
// 生成：
//
//	gxscene.ForGetterOf[User](
//	    func() []User { return users() },
//	    func(u User) any { return u.id },
//	    func(u User) *gxscene.Instance { return <li>..</li> },
//	)
//
// 元素类型推断优先级：render 参数显式注解 > each 集合类型（signal 声明字面量 /
// 绑定表变量 / 函数返回值）> 降级 any。key lambda 的参数若不写类型，复用同一元素类型。
func forTokens(n *jsxNode, ctx *jsxSceneCtx) []Token {
	p := n.pos
	var eachExpr, keyLambda string
	for _, a := range n.attrs {
		switch a.name {
		case "each":
			eachExpr = a.value
		case "key":
			keyLambda = a.value
		}
	}
	// children 里的首个表达式是 render lambda。
	renderLambda := ""
	for _, c := range n.children {
		if c.kind == childExpr {
			renderLambda = c.text
			break
		}
	}

	rParams, rBody, rOK := parseForLambda(renderLambda)
	elemType, paramName := "any", "it"
	if rOK {
		if rParams.name != "" {
			paramName = rParams.name
		}
		if rParams.typ != "" {
			// 显式注解优先（保留原有行为）。
			elemType = rParams.typ
		} else if inferred := ctx.inferForElemType(eachExpr, ctx.jsxPos); inferred != "" {
			// 无类型注解时，从 each 集合表达式推断元素类型。
			elemType = inferred
		}
	}

	out := []Token{
		tokIdent("gxscene", p), tokOp(".", p), tokIdent("ForGetterOf", p), tokOp("[", p),
		tokIdent(strings.TrimSpace(elemType), p), tokOp("]", p), tokOp("(", p),
	}

	// each getter: func() []T { return <eachExpr> }
	out = append(out, tokIdent("func", p), tokOp("(", p), tokOp(")", p), tokSpace(" ", p),
		tokOp("[", p), tokOp("]", p), tokIdent(strings.TrimSpace(elemType), p), tokSpace(" ", p),
		tokOp("{", p), tokSpace(" ", p), tokIdent("return", p), tokSpace(" ", p))
	out = append(out, lexExprScene(eachExpr, p, ctx)...)
	out = append(out, tokSpace(" ", p), tokOp("}", p), tokOp(",", p))

	// key: func(T) any { return <keyBody> }；无 key 时为 nil。
	if keyLambda == "" {
		out = append(out, tokIdent("nil", p))
	} else {
		kParams, kBody, kOK := parseForLambda(keyLambda)
		kName, kType := paramName, elemType
		if kOK {
			if kParams.name != "" {
				kName = kParams.name
			}
			if kParams.typ != "" {
				kType = kParams.typ
			}
		}
		out = append(out, tokIdent("func", p), tokOp("(", p), tokIdent(kName, p), tokSpace(" ", p),
			tokIdent(kType, p), tokOp(")", p), tokSpace(" ", p), tokIdent("any", p), tokSpace(" ", p),
			tokOp("{", p), tokSpace(" ", p), tokIdent("return", p), tokSpace(" ", p))
		// key body 是以 kName 为自由变量的表达式，需补 signal 读 ()。
		bodyToks := New(kBody).AllTokens()
		if len(ctx.sigs) > 0 {
			bodyToks = rewriteJSXInterpSignals(bodyToks, ctx.sigs)
		}
		for _, t := range bodyToks {
			if t.Kind == EOF {
				continue
			}
			t.Pos = p
			t.IsGen = true
			out = append(out, t)
		}
		out = append(out, tokSpace(" ", p), tokOp("}", p))
	}
	out = append(out, tokOp(",", p))

	// render: func(T) *gxscene.Instance { return <renderBody JSX> }
	out = append(out, tokIdent("func", p), tokOp("(", p), tokIdent(paramName, p), tokSpace(" ", p),
		tokIdent(elemType, p), tokOp(")", p), tokSpace(" ", p),
		tokOp("*", p), tokIdent("gxscene", p), tokOp(".", p), tokIdent("Instance", p), tokSpace(" ", p),
		tokOp("{", p), tokSpace(" ", p), tokIdent("return", p), tokSpace(" ", p))
	// renderBody 是 JSX（如 <li>{u.name}</li>），走 scene 表达式转换。
	bodyToks := New(rBody).AllTokens()
	bodyToks = transformJSXScene(bodyToks, ctx)
	for _, t := range bodyToks {
		if t.Kind == EOF {
			continue
		}
		t.Pos = p
		t.IsGen = true
		out = append(out, t)
	}
	out = append(out, tokSpace(" ", p), tokOp("}", p))
	out = append(out, tokOp(")", p))
	return out
}

// forLambdaParam 是 For 的 lambda 参数解析结果。
type forLambdaParam struct {
	name string
	typ  string
}

// parseForLambda 解析 `(u User) => body` / `(u) => body` 形式，返回参数与 body。
func parseForLambda(src string) (forLambdaParam, string, bool) {
	arrow := strings.Index(src, "=>")
	if arrow < 0 {
		return forLambdaParam{}, src, false
	}
	params := strings.TrimSpace(src[:arrow])
	body := strings.TrimSpace(src[arrow+2:])
	params = strings.TrimSpace(strings.TrimPrefix(strings.TrimSuffix(params, ")"), "("))
	fields := strings.Fields(params)
	fp := forLambdaParam{}
	if len(fields) >= 1 {
		fp.name = fields[0]
	}
	if len(fields) >= 2 {
		fp.typ = strings.Join(fields[1:], " ")
	}
	return fp, body, true
}

// inferForElemType 从 <For each={expr}> 的 each 表达式推断元素类型文本，
// 供 render 参数无显式类型注解时实例化 ForGetterOf[T]。
//
// each 支持三种形态：
//   - 裸标识符：users（signal 或普通变量）
//   - signal 读调用：users()
//   - 函数调用：getUsers() / pkg.Items()
//
// 推断路径：取根标识符 → 先查 sigTypes（signal 声明原始类型），
// 再查 bindings（普通变量 / 函数形参 / 函数返回值）→ sliceElemType 提取元素。
// 无法判定时返回 ""，调用方降级为 any。
func (ctx *jsxSceneCtx) inferForElemType(eachExpr string, pos Pos) string {
	root := rootIdentOfExpr(eachExpr)
	if root == "" {
		return ""
	}
	collType := ""
	if ctx.sigTypes != nil {
		collType = ctx.sigTypes[root]
	}
	if collType == "" && ctx.bindings != nil {
		collType = ctx.bindings.TypeAt(root, pos)
		// 函数返回值：each 是 getUsers() 形态时，TypeAt 拿不到，取 FuncResult。
		if collType == "" && strings.Contains(eachExpr, "(") {
			collType = ctx.bindings.FuncResult(root)
		}
	}
	if collType == "" {
		return ""
	}
	return sliceElemType(collType)
}

// rootIdentOfExpr 取表达式的根标识符（跳过空白，取第一个标识符）。
// users → users；users() → users；pkg.Items() → pkg（保守取首段，调用方再按函数兜底）。
func rootIdentOfExpr(expr string) string {
	for _, t := range New(expr).AllTokens() {
		if t.Kind == EOF {
			break
		}
		if t.Kind == Ident && !isGoKeyword(t.Text) {
			return t.Text
		}
	}
	return ""
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
// sigs 非空时对插值里的 signal 变量补读调用（{count} → count()），
// 弥补 transformSignals 无法穿透 JSX 整体 token 的盲区。
func lexExprScene(expr string, base Pos, ctx *jsxSceneCtx) []Token {
	toks := New(expr).AllTokens()
	if len(ctx.sigs) > 0 {
		// 按 JSX 根位置解析绑定：被局部变量遮蔽的同名 signal 不应改写（{count}
		// 在 count := 3 作用域内读的是局部变量）。sigs 是全局名字集合，需先按
		// bindings+jsxPos 过滤出该位置真正指向 signal 的名字，再交回形态改写逻辑。
		effective := ctx.sigs
		if ctx.bindings != nil {
			effective = make(map[string]bool, len(ctx.sigs))
			for name := range ctx.sigs {
				if isSignalRefAt(ctx.bindings, name, ctx.jsxPos) {
					effective[name] = true
				}
			}
		}
		toks = rewriteJSXInterpSignals(toks, effective)
	}
	// 对象字面量自动推断：{ { name: "x", age: 1 } } → struct{ name string; age int }{...}。
	// 插值片段无期望类型上下文，走匿名 struct 推断路径。
	if converted, err := transformObjectLitExpr(toks); err == nil {
		toks = converted
	}
	toks = transformJSXScene(toks, ctx)
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
func getterTokens(g sceneGetter, ctx *jsxSceneCtx) []Token {
	p := g.pos
	// raw 模式：call 本身就是一个 func() any getter（BranchGetter 工厂的返回值），
	// 直接发射，不能包一层 return——否则 getter 返回的会是 getter 函数本身而非 *Branch。
	if g.raw && len(g.call) > 0 {
		return g.call
	}
	out := []Token{
		tokIdent("func", p), tokOp("(", p), tokOp(")", p), tokSpace(" ", p),
		tokIdent("any", p), tokOp("{", p), tokSpace(" ", p), tokIdent("return", p), tokSpace(" ", p),
	}
	if len(g.call) > 0 {
		out = append(out, g.call...)
	} else {
		out = append(out, lexExprScene(g.expr, p, ctx)...)
	}
	out = append(out, tokSpace(" ", p), tokOp("}", p))
	return out
}

// genSceneExpr 为一个 JSX 根元素生成完整的表达式 token 序列。
// 根是复合组件时产出一次组件调用（返回 *Instance）；否则产出 gxscene.Render(...)。
func genSceneExpr(node *jsxNode, ctx *jsxSceneCtx) []Token {
	if node.name == "For" {
		return forTokens(node, ctx)
	}
	if node.component {
		return compCallTokens(node, ctx)
	}
	p := node.pos
	var ops []sceneOp
	var getters []sceneGetter
	slot := 0
	walkScene(node, &ops, &getters, &slot, ctx)

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
		out = append(out, getterTokens(g, ctx)...)
	}
	out = append(out, tokOp(")", p))
	return out
}

// transformJSXScene 把 token 流里的 JSX 元素替换为 gxscene opcode 渲染表达式。
// 管线的唯一 JSX 后端。
func transformJSXScene(in []Token, ctx *jsxSceneCtx) []Token {
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
			// 记录根 JSX token 的绝对位置，供内部节点（如嵌套 <For>）查询绑定表。
			ctx.jsxPos = t.Pos
			parser := newJSXParser(t.Text, t.Pos)
			node := parser.parseElement()
			out = append(out, genSceneExpr(node, ctx)...)
			continue
		}
		out = append(out, t)
	}
	return out
}
