package compiler

import (
	"encoding/json"
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
//   - sigs：sig 变量名集合（插值里读补 ()）
//   - comps：强类型组件契约表（组件名 → props 具名 struct 类型名）。
//     命中契约的组件生成裸对象字面量 Card({...})，由管线第二次 objectlit
//     从签名补全 Card(CardProps{...}) 并做字段校验；未命中的走老式
//     gxscene.Props getter-map 路径（getter 保鲜）。
type jsxSceneCtx struct {
	sigs  map[string]bool
	comps map[string]string
	// explicitSigCallErr: JSX 插值里用户显式调用 sig（{count()}）时记录，
	// 编译结束后由 transformJSXScene 返回给上层报错。
	explicitSigCallErr error
	// sigTypes: sig 变量声明类型（改写前收集，如 users→[]User），
	// 供 <For> 在 render 参数无类型注解时从 each 推断元素类型。
	sigTypes map[string]string
	// bindings: 统一绑定表，回退查询普通变量/函数形参类型。
	bindings *Bindings
	// jsxPos: 当前 JSX token 在原始源码中的绝对位置。JSX 内部节点（如嵌套
	// 的 <For>）的 pos 是解析器生成的相对偏移，不能直接用于 bindings 查询，
	// 故以根 JSX token 的绝对 pos 作为锚点。
	jsxPos Pos
	// forItemParam: 当前正在生成的 <For> render lambda 的参数名（信号化模式）。
	// 该参数是 item getter（func() T），render body 里对它的读取须补写成 u()，
	// 才能让子树 getter 登记对 item sig 的依赖。仅 render body 生成期间非空。
	forItemParam string
	// jsxPosLocked: 为真时 transformJSXScene 不更新 jsxPos。<For> render body 是
	// 重新词法化的独立片段，其内部 JSX 的 Pos 是相对假坐标（Index 很小），若覆盖
	// jsxPos 会让插值里的函数内 sig 绑定查询落空（A1：列表显示函数地址）。
	// 生成 render body 期间锁定，强制用外层 <For> 的真实绝对坐标作锚点。
	jsxPosLocked bool
	// forItemTypes: 当前各层 <For> render lambda 参数名 → 元素类型（B2）。
	// 嵌套 For 的内层 each（如 r.cells）需要按外层 item 类型 r:Row 查字段类型，
	// 否则退化成 []any。生成 render body 期间登记，退出恢复。
	forItemTypes map[string]string
	// signalFields: struct sig 字段表（类型名 → sig 字段名集合）。
	// item 逃逸检测用它区分：props 字段是 sig 字段时本身保鲜，无需回退值快照。
	signalFields signalFieldTable
	// diags: GX 语义诊断回投口（第十一轮 P3-F）。each 集合是 map 等非列表类型时
	// 追加明确诊断，避免生成 []map[...]/[]rune 这类误导性错误信息。nil 表示不收集。
	diags *[]GXDiagnostic
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

// signalFieldPropTokens 为 props 的 sig 字段生成值 token，档位表见 signalfields.go
// 文件顶部（单一事实来源），手写构造与 JSX 两处共用 buildSignalFieldValue。
// 返回 (tokens, true) 表示已处理；(nil, false) 表示非 sig 字段，走常规路径。
func signalFieldPropTokens(a jsxAttr, ctx *jsxSceneCtx, propsType string) ([]Token, bool) {
	if propsType == "" || a.name == "" || !ctx.signalFields.has(propsType, a.name) {
		return nil, false
	}
	p := a.pos
	T := ctx.signalFields.fieldType(propsType, a.name)

	// 字面量档：attrString / attrBool 直接 New；attrExpr 里的纯数字/字符串同样 New。
	switch a.kind {
	case attrString:
		return signalNewCall(tokString(normalizeStringLit(a.value), p)), true
	case attrBool:
		return signalNewCall(tokIdent(a.value, p)), true
	case attrExpr:
		if isPlainLiteralExpr(a.value) {
			return signalNewCall(lexExprScene(a.value, a.pos, ctx)...), true
		}
	}

	// 改写后的表达式 token（sig→title()、item getter→u().name）。
	vt := lexExprScene(a.value, a.pos, ctx)

	// 透传档：表达式恰好是单个 sig 读。两种形态：
	//   title()        —— sig 变量读，剥离 () 传 getter 本身；
	//   title          —— 已是 getter（forItemParam），无调用括号；
	//   other.name()   —— 别的 sig 字段读（selector），整体直通。
	if leaf, ok := pureSignalRead(a.value, vt, ctx); ok {
		return leaf, true
	}

	// For item getter（u() 形式的 item sig）是 sig 读，但 forItemParam 不是
	// BkSignal 绑定，统一分档的 exprHasSignalRead 认不出——这里补判并强制走 Computed
	// （档4），登记对 item sig 的依赖、item 变化时自动重算。
	if ctx.forItemParam != "" {
		for k := 0; k+2 < len(vt); k++ {
			if vt[k].Kind == Ident && vt[k].Text == ctx.forItemParam &&
				vt[k+1].Text == "(" && vt[k+2].Text == ")" {
				expr := make([]Token, 0, len(vt))
				for _, t := range vt {
					if t.Kind != EOF {
						expr = append(expr, t)
					}
				}
				return signalComputedCall(T, p, expr), true
			}
		}
	}
	// 去掉 EOF，得到纯表达式 token，交统一分档入口。
	expr := make([]Token, 0, len(vt))
	for _, t := range vt {
		if t.Kind != EOF {
			expr = append(expr, t)
		}
	}
	return buildSignalFieldValue(T, p, expr, 0, len(expr), ctx.bindings, ctx.signalFields, ctx.jsxPos), true
}

// signalNewCall 生成 Signal.New(<lit>) token 序列。
func signalNewCall(lit ...Token) []Token {
	if len(lit) == 0 {
		return nil
	}
	p := lit[0].Pos
	out := []Token{signalPkgToken(p), tokOp(".", p), tokIdent("New", p), tokOp("(", p)}
	out = append(out, lit...)
	out = append(out, tokOp(")", p))
	return out
}

// tokensForType 把一个类型文本（如 "string"、"[]int"、"*User"）词法化为 token。
func tokensForType(T string, p Pos) []Token {
	if strings.TrimSpace(T) == "" {
		return []Token{tokIdent("any", p)}
	}
	toks := New(strings.TrimSpace(T)).AllTokens()
	var out []Token
	for _, t := range toks {
		if t.Kind == EOF {
			continue
		}
		t.Pos = p
		t.IsGen = true
		out = append(out, t)
	}
	return out
}

// isPlainLiteralExpr 判断表达式原文是否为纯字面量（数字/字符串/bool/nil）。
func isPlainLiteralExpr(expr string) bool {
	e := strings.TrimSpace(expr)
	if e == "" {
		return false
	}
	toks := New(e).AllTokens()
	var sig []Token
	for _, t := range toks {
		if t.Kind != Space && t.Kind != Newline && t.Kind != EOF {
			sig = append(sig, t)
		}
	}
	if len(sig) != 1 {
		return false
	}
	switch sig[0].Kind {
	case Number, String, Rune:
		return true
	case Ident:
		return sig[0].Text == "true" || sig[0].Text == "false" || sig[0].Text == "nil"
	}
	return false
}

// pureSignalRead 判断改写后的表达式是否为「单个 sig 读」，是则返回可透传的
// getter token。识别形态（vt 为 lexExprScene 改写后的 token）：
//   - <sig>()        全局 sig/computed 读 → 剥离 () 传 getter（保鲜）；
//   - <itemGetter>   For item getter（无调用括号，forItemParam）→ 直通其本身；
//   - recv.chain.field()  sig 字段链读 → 交 signalChainGetter 统一分档（第九轮）：
//     中间有 sig 段生成读写 lambda / 裸 getter，单段链尾剥 () 裸透传。
func pureSignalRead(expr string, vt []Token, ctx *jsxSceneCtx) ([]Token, bool) {
	// 收集非空白 token
	var s []Token
	for _, t := range vt {
		if t.Kind != Space && t.Kind != Newline && t.Kind != EOF {
			s = append(s, t)
		}
	}
	if len(s) == 0 {
		return nil, false
	}

	// 形态：<ident>() —— sig 变量读，剥离括号。
	if len(s) == 3 && s[0].Kind == Ident && s[1].Text == "(" && s[2].Text == ")" {
		if ctx.sigs[s[0].Text] {
			return []Token{tokIdent(s[0].Text, s[0].Pos)}, true
		}
	}
	// 形态：<ident> —— 已是 getter（For item getter param），直通。
	if len(s) == 1 && s[0].Kind == Ident && ctx.forItemParam == s[0].Text {
		return []Token{tokIdent(s[0].Text, s[0].Pos)}, true
	}
	// 形态：recv.chain.field() —— sig 字段链读，统一交 signalChainGetter 分档。
	// 第八轮 P1-1：旧实现只看末尾 4 个 token + hasField 按字段名全表匹配，
	// 导致 p.a() + p.b() 这种派生表达式被误判为「单个 selector 读」。
	// 第九轮：改走 signalChainGetter——它内部用「跳过已补 () 」的链解析走到真正
	// 链尾，并做「整段恰好是链读」完整性校验，杜绝把派生表达式误判成链读。
	if n := len(s); n >= 5 && s[0].Kind == Ident && !isGoKeyword(s[0].Text) {
		if gen, ok := signalChainGetter(s, 0, n, ctx.bindings, ctx.signalFields, ctx.jsxPos); ok {
			return gen, true
		}
	}
	return nil, false
}

// compCallTyped 生成强类型契约组件调用：Card({ title: expr, ... })。
// 例外：props 的 sig 字段走 signalFieldPropTokens 统一生成——sig 透传、
// 字面量包 Signal.New、派生表达式包 Signal.Computed，语义与手写结构体赋值一致。
func compCallTyped(n *jsxNode, ctx *jsxSceneCtx) []Token {
	p := n.pos
	compName := n.name
	if idx := strings.LastIndex(compName, "."); idx >= 0 {
		compName = compName[idx+1:]
	}
	propsType := ctx.comps[compName]
	out := componentNameTokens(n, p)
	out = append(out, tokOp("(", p), tokOp("{", p))

	for _, a := range n.attrs {
		out = append(out, tokIdent(a.name, a.pos), tokOp(":", p))
		if val, ok := signalFieldPropTokens(a, ctx, propsType); ok {
			out = append(out, val...)
		} else {
			switch a.kind {
			case attrBool:
				out = append(out, tokIdent("true", a.pos))
			case attrString:
				out = append(out, tokString(normalizeStringLit(a.value), a.pos))
			case attrExpr:
				out = append(out, lexExprScene(a.value, a.pos, ctx)...)
			}
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
	// B11：零参数组件（func Counter() ...）用 <Counter/> 调用时，不应传 gxscene.Props。
	// 仅当调用不带任何 props（无 attrs、无 children）且签名确为零参数时省略。
	if len(n.attrs) == 0 && len(n.children) == 0 && ctx.bindings != nil {
		name := n.name
		if idx := strings.LastIndex(name, "."); idx >= 0 {
			name = name[idx+1:]
		}
		if sig := ctx.bindings.funcSigs[name]; sig != nil && len(sig.ParamTypes) == 0 && !sig.IsVoid {
			out := componentNameTokens(n, p)
			return append(out, tokOp("(", p), tokOp(")", p))
		}
	}
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
		// A5：高置信拼错的属性名给提示——bind:value 等冒号前缀、onClik 等事件名
		// 手误。只报明确模式，不设封闭白名单（Node 属性对后端扩展开放，见
		// docs/node-protocol.md），避免误伤后端自定义属性。
		if msg, _ := suspiciousAttr(a.name); msg != "" {
			ctx.report("attr-unknown", a.pos, msg)
		}
		// A2/A3：class/style 传对象字面量原本会降级成匿名 struct 静默错值（后端拿到
		// 无意义 struct）。现对 style 开一个特例：若对象字面量文本是**合法 JSON**
		// （key 带引号，如 style={{"&:hover":{"bg":"#eee"}}}），把它重写成
		// gxscene.parseStyle("...") ——返回受限 Style，由 host 展平为后端 props。
		// 非 JSON 的对象字面量（如 {{ color: "red" }}，key 无引号）仍报原诊断。
		// class 无样式表语义，保持原诊断不变。
		if a.kind == attrExpr && isObjectLitText(a.value) {
			if a.name == "style" {
				// React-native 风格：驼峰 key + 动态表达式值 → gxscene.Style{...}。
				// 整块作为一个动态属性插槽，runtime writeAttr 按 key 细粒度 diff。
				if styleToks, ok := styleObjToTokens(a.value, a.pos, ctx); ok {
					s := *slotCounter
					*slotCounter++
					*ops = append(*ops, sceneOp{code: "OpDynAttr", key: "style", slot: s})
					*getters = append(*getters, sceneGetter{pos: a.pos, call: styleToks})
					continue
				}
				// 兼容既有 JSON 形态（key 带引号）：静态 parseStyle。
				if json.Valid([]byte(a.value)) {
					a.value = "gxscene.parseStyle(" + strconv.Quote(a.value) + ")"
				} else {
					ctx.reportAttrUnsupported(a.pos, a.name,
						"style 对象字面量无法识别：key 请用驼峰（如 { fontSize: 12 }）或带引号 JSON（如 {\"&:hover\":{\"bg\":\"#eee\"}}）")
				}
			} else if a.name == "class" {
				ctx.reportAttrUnsupported(a.pos, a.name,
					"class 不支持对象字面量；请用字符串类名或条件表达式")
			}
		}
		// B10：双引号字符串里的 ${...} 不会被 GX 模板串展开（模板串用反引号），
		// 会原样进属性值且变量未被引用（declared and not used）。给明确诊断。
		if a.kind == attrString && strings.Contains(a.value, "${") {
			ctx.reportAttrUnsupported(a.pos, a.name,
				"属性值里的 ${...} 插值需用反引号模板串（如 `a ${n}`），双引号不会插值")
		}
		// 事件属性 on*：无条件透传为 OpEvent，前端不设白名单、不校验事件名，
		// 后端自定义事件（如 onFoo）同样原样进 Node.Handlers。静态字符串/bool 的
		// on* 也放行（编译为常量 getter，后端按需解释）。
		if isEventAttrName(a.name) {
			// B6：事件属性的箭头函数 body 是赋值语句时（() => n = n+1），
			// JSX 属性路径不展开箭头，=> 残留产非法 Go。给明确诊断 + 修法。
			if a.kind == attrExpr && isArrowAssignExpr(a.value) {
				ctx.report("event-arrow-statement", a.pos,
					"事件 handler 的箭头函数体是赋值语句，请用 func() { ... } 字面量（如 onClick={func() { n = n + 1 }}）")
			}
			s := *slotCounter
			*slotCounter++
			*ops = append(*ops, sceneOp{code: "OpEvent", key: a.name, slot: s})
			*getters = append(*getters, eventGetter(a))
			continue
		}
		// ref 模板引用（命令式逃生口）：ref={fn} 编成 OpRef + getter，fn 是
		// func(*gxscene.Element)（或 func(any) 兼容形态）。建树时该节点 ID 已分配，
		// 运行时回调一次 *Element 句柄，销毁时回调 nil。只认原生元素上的 attrExpr；
		// 字符串/bool 的 ref 无意义，按普通静态属性透传（后端自行解释）。
		if a.name == "ref" && a.kind == attrExpr {
			s := *slotCounter
			*slotCounter++
			*ops = append(*ops, sceneOp{code: "OpRef", slot: s})
			*getters = append(*getters, sceneGetter{expr: a.value, pos: a.pos})
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
			// A6：JSX 里写 Go 控制语句（for range / switch）会被当文本节点，
			// 且体内标识符未定义，产出坏 Go。明确报错并提示 GX 写法。
			if isGoControlInJSX(c.text) {
				ctx.report("jsx-control-statement", c.pos,
					"JSX 内不支持 for/switch 语句；列表请用 <For each={...}>，条件请用 if 块")
				continue
			}
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

// itemEscapesIntoComponentProps 检测 For 的 render body 里，item 参数是否被传入
// 组件 props（<Card name={u.name}/> / <Card u={u}/>）。
//
// 组件是值快照语义：slotComp 的 getter 只在挂载时调用一次，props 表达式里的
// u().name 会被立即求值成死字符串；item sig 后续变化，子组件静默停在旧值
// （静默功能回归）。这类场景必须回退到 ForGetterOf 值快照模式——整项重建时
// 重新调用 render，props 才是新值，行为与 item sig 引入前一致。
//
// 只检测组件 props 这一处确定的逃逸点；直接插值 {u.name} 是 sig 模式的主路径，
// 不算逃逸。item 被赋给局部变量等更边缘的逃逸由文档约束（见 language-spec）。
func itemEscapesIntoComponentProps(body, param string, ctx *jsxSceneCtx) bool {
	if param == "" {
		return false
	}
	root := newJSXParser(body, NoPos).parseElement()
	return nodeHasItemInCompProps(root, param, ctx)
}

// nodeHasItemInCompProps 递归检查节点树：item 进入组件的「非 sig 字段」props 即逃逸。
// sig 字段（struct sig name T）本身是 getter、值实时保鲜，item sig 能正常驱动，
// 不算逃逸——否则与用户显式声明的响应式字段语义冲突。
func nodeHasItemInCompProps(n *jsxNode, param string, ctx *jsxSceneCtx) bool {
	if n == nil {
		return false
	}
	if n.component {
		// 查组件 props 类型名（对齐 compCallTokens 的取名：取最后一段）。
		compName := n.name
		if idx := strings.LastIndex(compName, "."); idx >= 0 {
			compName = compName[idx+1:]
		}
		typeName := ctx.comps[compName]
		for _, a := range n.attrs {
			if a.kind != attrExpr || !exprMentionsIdent(a.value, param) {
				continue
			}
			// sig 字段保鲜，不需回退。
			if typeName != "" && ctx.signalFields.has(typeName, a.name) {
				continue
			}
			return true
		}
	}
	for _, c := range n.children {
		switch c.kind {
		case childElement:
			if nodeHasItemInCompProps(c.node, param, ctx) {
				return true
			}
		case childCond:
			for _, br := range c.cond.branches {
				for _, ch := range br.children {
					if ch.kind == childElement && nodeHasItemInCompProps(ch.node, param, ctx) {
						return true
					}
				}
			}
		}
	}
	return false
}

// exprMentionsIdent 判断表达式源码里是否出现标识符 name。
func exprMentionsIdent(expr, name string) bool {
	for _, t := range New(expr).AllTokens() {
		if t.Kind == Ident && t.Text == name {
			return true
		}
	}
	return false
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
// 元素类型推断优先级：render 参数显式注解 > each 集合类型（sig 声明字面量 /
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
		default:
			// A4：<For empty={...}> 及其它非 each/key 属性被静默丢弃（empty 节点
			// 既不进模板也不进 slot）。明确报未知属性，提示用条件分支实现空态。
			msg := "<For> 不支持属性 " + a.name
			if a.name == "empty" {
				msg += "：请用 if 块在列表为空时渲染占位（如 {len(rows) == 0 ? <li>none</li> : <For .../>}）"
			}
			ctx.reportAttrUnsupported(a.pos, a.name, msg)
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
		} else {
			// 第十三轮 P3-E：each 是 map / string 等非列表类型时先报明确诊断，
			// 不让 sliceElemType 把 map 当元素生成 []map[K]V、把 string 当 rune 列表。
			// 函数返回 map（load()）也覆盖：forEachCollType 已补查 FuncResult。
			coll := ctx.forEachCollType(eachExpr)
			collTrim := strings.TrimSpace(coll)
			if collTrim != "" && (strings.HasPrefix(collTrim, "map[") || collTrim == "string") {
				ctx.reportForNotList(n, coll)
			}
			if inferred := ctx.inferForElemType(eachExpr, ctx.jsxPos); inferred != "" {
				// 无类型注解时，从 each 集合表达式推断元素类型。
				elemType = inferred
			}
		}
	}

	// 逃逸检测：item 传入组件 props 时，组件插槽只求值一次、props 是值快照，
	// item sig 无法驱动它。回退到 ForGetterOf 值快照模式（整项重建），
	// 保持与 item sig 引入前一致的行为——静默旧值比重建更危险。
	escaped := itemEscapesIntoComponentProps(rBody, paramName, ctx)

	factory := "ForGetterOfSignal"
	if escaped {
		factory = "ForGetterOf"
	}
	out := []Token{
		tokIdent("gxscene", p), tokOp(".", p), tokIdent(factory, p), tokOp("[", p),
		tokIdent(strings.TrimSpace(elemType), p), tokOp("]", p), tokOp("(", p),
	}

	// each getter: func() []T { return <eachExpr> }
	out = append(out, tokIdent("func", p), tokOp("(", p), tokOp(")", p), tokSpace(" ", p),
		tokOp("[", p), tokOp("]", p), tokIdent(strings.TrimSpace(elemType), p), tokSpace(" ", p),
		tokOp("{", p), tokSpace(" ", p), tokIdent("return", p), tokSpace(" ", p))
	out = append(out, lexExprScene(eachExpr, p, ctx)...)
	// B3/B1：each={rows} 若 rows 是 getter 变量（`rows := func() []T{...}` 或
	// `rows := Get[T](...)`），绑定表记的是解包后类型 []T，直接 return rows 会
	// 返回函数值而非切片，go build 报 "cannot use rows (func() []T) as []T"。
	// 这里对裸 getter 标识符补一次调用 rows()。已是调用的形态（rows()/f(x)）
	// 与 sig 变量（走 sigs 改写）不受影响。
	if rootIdentOfExpr(strings.TrimSpace(eachExpr)) == strings.TrimSpace(eachExpr) &&
		ctx.bindings != nil && isGetterVarFromToks(ctx.bindings.toks, strings.TrimSpace(eachExpr)) {
		out = append(out, tokOp("(", p), tokOp(")", p))
	}
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
		// key body 是以 kName 为自由变量的表达式，需补 sig 读 ()。
		bodyToks := New(kBody).AllTokens()
		if len(ctx.sigs) > 0 {
			bodyToks = rewriteJSXInterpSignals(bodyToks, ctx.sigs, &ctx.explicitSigCallErr, "")
		}
		// Bug 01（处 2）：key lambda 读 item 的 sig 字段（如 key={(u)=>u.name}）
		// 也要补 ()，否则 key 取到函数地址、去重永远不相等。注入 kName 的静态
		// 类型并走 sig 字段 selector 改写（与 render body 同源）。
		restoreKeyType := ctx.bindings.WithTypeOverride(kName, kType)
		bodyToks = rewriteJSXSignalFieldSelectors(bodyToks, ctx.bindings, ctx.signalFields, ctx.jsxPos)
		restoreKeyType()
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

	// render 参数形态：
	//   - 常规（信号化）：func(u func() T) *gxscene.Instance，renderBody 里 u.name → u().name 登记 item sig 依赖；
	//   - 逃逸（item 进组件 props）：func(u T) *gxscene.Instance，值快照，整项重建时重跑 render 取新值。
	out = append(out, tokIdent("func", p), tokOp("(", p), tokIdent(paramName, p), tokSpace(" ", p))
	if !escaped {
		out = append(out, tokIdent("func", p), tokOp("(", p), tokOp(")", p), tokSpace(" ", p))
	}
	out = append(out,
		tokIdent(elemType, p), tokOp(")", p), tokSpace(" ", p),
		tokOp("*", p), tokIdent("gxscene", p), tokOp(".", p), tokIdent("Instance", p), tokSpace(" ", p),
		tokOp("{", p), tokSpace(" ", p), tokIdent("return", p), tokSpace(" ", p))
	// renderBody 是 JSX（如 <li>{u.name}</li>），走 scene 表达式转换。
	// 非逃逸时设置 forItemParam 让插值里的 u.name → u().name；保存旧值以支持嵌套 For。
	// 逃逸时保持 forItemParam 不变，u.name 保持值快照语义。
	oldItemParam := ctx.forItemParam
	oldLocked := ctx.jsxPosLocked
	oldItemTypes := ctx.forItemTypes
	if !escaped {
		ctx.forItemParam = paramName
	}
	// B2：无论是否逃逸都要登记本层 item 参数类型。逃逸只改变读语义
	// （值快照 ForGetterOf vs sig 化 ForGetterOfSignal），不改变 item 的静态
	// 类型；内层 <For each={r.cells}> 需按 r:Row 查字段推元素类型。
	// 之前把它放进 !escaped 分支，r.cells 传给内层 For 被判逃逸后，r 未登记，
	// 内层只能退化 any。
	if ctx.forItemTypes == nil {
		ctx.forItemTypes = map[string]string{}
	} else {
		cp := make(map[string]string, len(ctx.forItemTypes)+1)
		for k, v := range ctx.forItemTypes {
			cp[k] = v
		}
		ctx.forItemTypes = cp
	}
	ctx.forItemTypes[paramName] = elemType
	bodyToks := New(rBody).AllTokens()
	// rBody 来自重新词法化的片段，其内部 JSX 的 Pos 是相对假坐标（Index 很小）。
	// 锁定 jsxPos 强制用外层 <For> 的真实绝对坐标作锚点，否则 {n}/{d} 等插值查
	// 绑定表时落在文件开头，函数内 sig 被误判为局部遮蔽而不补 ()——列表里
	// 显示成函数地址（A1）。item 参数 r 仍由 forItemParam 正常识别，不受影响。
	// 保存/恢复旧锁值以支持嵌套 For（内层结束时不能解锁外层）。
	ctx.jsxPosLocked = true
	// Bug 01：把本层（含继承的外层）item 参数类型临时注入 Bindings，使 sig 字段
	// 链解析（recvTypeAt）能识别 u().field / u.field 的 receiver 类型并补 ()。
	// 这类 lambda 形参不在 Bindings 作用域表里，只能靠 forItemTypes 推断。
	var restoreItemTypes []func()
	for name, typ := range ctx.forItemTypes {
		restoreItemTypes = append(restoreItemTypes, ctx.bindings.WithTypeOverride(name, typ))
	}
	bodyToks = transformJSXScene(bodyToks, ctx)
	for i := len(restoreItemTypes) - 1; i >= 0; i-- {
		restoreItemTypes[i]()
	}
	ctx.jsxPosLocked = oldLocked
	ctx.forItemParam = oldItemParam
	ctx.forItemTypes = oldItemTypes
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
//   - 裸标识符：users（sig 或普通变量）
//   - sig 读调用：users()
//   - 函数调用：getUsers() / pkg.Items()
//
// 推断路径：取根标识符 → 先查 sigTypes（sig 声明原始类型），
// 再查 bindings（普通变量 / 函数形参 / 函数返回值）→ sliceElemType 提取元素。
// 无法判定时返回 ""，调用方降级为 any。
func (ctx *jsxSceneCtx) inferForElemType(eachExpr string, pos Pos) string {
	root := rootIdentOfExpr(eachExpr)
	if root == "" {
		return ""
	}
	// B2：嵌套 For 的内层 each 是外层 item 的字段（r.cells）。外层 item r 不在
	// bindings 里，按 forItemTypes[r]=Row 查 struct 字段 cells 的类型 → []Cell。
	if ctx.forItemTypes != nil && ctx.bindings != nil {
		if itemType, ok := ctx.forItemTypes[root]; ok && itemType != "" {
			t := strings.TrimSpace(eachExpr)
			if strings.HasPrefix(t, root+".") {
				field := strings.TrimPrefix(t, root+".")
				if dot := strings.IndexAny(field, ".[("); dot >= 0 {
					field = field[:dot]
				}
				// struct 字段绑定在 Binder 阶段只记 Kind、不记 Type（addBinding(...,"")），
				// 所以 structScopes[itemType].Bindings[field].Type 恒为空，走不到类型。
				// 改为直接查原始 token 流里的 `type <itemType> struct { field T }` 声明。
				if ft := structFieldTypeFromToks(ctx.bindings.toks, itemType, field); ft != "" {
					if s := sliceElemType(ft); s != "" {
						return s
					}
				}
			}
		}
	}
	// N-2（既有）：each 根标识符被索引时（rows["k"] / grouped[g]），根是 map，
	// TypeAt 常把 map 推断成 key 类型（string），sliceElemType 进而得到 rune，
	// 错误信息与真实元素类型毫无关联。改为按声明反推容器类型并逐层剥索引。
	// 第十二轮 P1-A：判据必须是「根标识符本身被索引」，不能用「表达式里有 [」——
	// 后者会把 filter(rows["k"])（方括号在实参里、根是 filter）也抢走推断权，
	// 且落空后 return "" 掐断下面的 FuncResult 兜底，导致退化成 any 编译失败。
	if n := strings.Count(eachExpr, "["); n > 0 && ctx.bindings != nil && indexedRoot(eachExpr) {
		et := containerElemTypeFromBinding(ctx.bindings, root, pos)
		for k := 1; k < n && et != ""; k++ {
			et = containerElemTypeOnce(et)
		}
		if s := sliceElemType(et); s != "" {
			return s
		}
		if et != "" {
			return et
		}
		// 落空 → 落到下面的旧路径（函数返回值兜底），不要 return ""。
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

// structFieldTypeFromToks 在原始 token 流中定位 `type <typeName> struct { ... }`，
// 返回其中名为 <field> 的字段类型文本（如 "[]Cell" / "map[string]int"）。
//
// 为什么需要它：Binder 收集 struct 字段时只登记 Kind=BkField、Type 传空串
// （见 bindings.go addBinding(..., "")），所以 structScopes 里的字段绑定查不到
// 类型。嵌套 <For each={r.cells}> 想从外层 item 类型 r:Row 推内层元素 Cell，
// 只能回到原始 token 流按声明文本反查。跨文件 struct 不在同一 token 流内，
// 返回 ""，调用方降级 any（不误报）。
func structFieldTypeFromToks(toks []Token, typeName, field string) string {
	n := len(toks)
	for i := 0; i < n; i++ {
		if !toks[i].is("type") {
			continue
		}
		j := nextSig(toks, i+1)
		if j < 0 || j >= n || toks[j].Text != typeName {
			continue
		}
		k := nextSig(toks, j+1)
		if k < 0 || k >= n || !toks[k].is("struct") {
			continue
		}
		br := nextSig(toks, k+1)
		if br < 0 || br >= n || toks[br].Text != "{" {
			continue
		}
		cl := matchDelim(toks, br, "{", "}")
		if cl < 0 || cl > n {
			continue
		}
		if t := fieldTypeInStruct(toks, br+1, cl, field); t != "" {
			return t
		}
	}
	return ""
}

// fieldTypeInStruct 在 struct body [from,to) 顶层按字段声明行查找 field 的类型。
// 按换行 / 分号切逻辑行，并在每个逻辑行内调用 fieldTypeInDecl。
func fieldTypeInStruct(toks []Token, from, to int, field string) string {
	depth := 0
	lineFrom := from
	for i := from; i <= to; i++ {
		if i < to {
			switch toks[i].Text {
			case "{", "[", "(":
				depth++
			case "}", "]", ")":
				depth--
			}
		}
		endLine := i == to || (depth == 0 && (toks[i].Kind == Newline || toks[i].Text == ";"))
		if !endLine {
			continue
		}
		if t := fieldTypeInDecl(toks, lineFrom, i, field); t != "" {
			return t
		}
		lineFrom = i + 1
	}
	return ""
}

// fieldTypeInDecl 解析单条字段声明 `name Type [tag]` / `a, b Type`：找到 field
// 后返回其类型文本（跳过共享名字与字符串 tag）。未命中返回 ""。
func fieldTypeInDecl(toks []Token, from, to int, field string) string {
	var idxs []int
	for i := from; i < to; i++ {
		if toks[i].Kind == Space || toks[i].Kind == Comment {
			continue
		}
		idxs = append(idxs, i)
	}
	for p, ix := range idxs {
		if toks[ix].Kind != Ident || toks[ix].Text != field {
			continue
		}
		ts := p + 1
		// 跳过 `, nextName`（a, b int 共享类型）。
		for ts+1 < len(idxs) && toks[idxs[ts]].Text == "," && toks[idxs[ts+1]].Kind == Ident {
			ts += 2
		}
		if ts >= len(idxs) {
			return ""
		}
		var sb strings.Builder
		for _, tix := range idxs[ts:] {
			t := toks[tix]
			if t.Kind == String || t.Kind == Template {
				break // struct tag
			}
			sb.WriteString(t.Text)
		}
		return sb.String()
	}
	return ""
}

// isGetterVarFromToks 在原始 token 流里判断 name 是否由 getter 表达式声明：
//   - `name := func(...) T { ... }`（函数字面量，B3）
//   - `name := Get[T](...)` / `name := pkg.Get[T](...)`（泛型 getter 工厂，B1）
//
// 绑定表把这类变量的 Type 记为解包后的返回类型（[]Row），编译产物里它是函数值，
// <For each={name}> 生成 each getter 时必须调用一次才能拿到切片。
func isGetterVarFromToks(toks []Token, name string) bool {
	n := len(toks)
	for i := 0; i < n; i++ {
		if toks[i].Kind != Ident || toks[i].Text != name {
			continue
		}
		op := nextSig(toks, i+1)
		if op < 0 || op >= n {
			continue
		}
		switch toks[op].Text {
		case ":=", "=":
			// 继续看右侧
		default:
			continue
		}
		rhs := nextSig(toks, op+1)
		if rhs < 0 || rhs >= n {
			continue
		}
		if toks[rhs].is("func") {
			lp := nextSig(toks, rhs+1)
			if lp >= 0 && lp < n && toks[lp].Text == "(" {
				return true
			}
		}
		if isGetterFactoryCall(toks, rhs) {
			return true
		}
	}
	return false
}

// isGetterFactoryCall 判断 toks[at] 起是否为 `Get[T](...)` / `pkg.Get[T](...)`
// 形态的泛型 getter 工厂调用。只认名字 Get/get，避免误伤普通泛型函数。
func isGetterFactoryCall(toks []Token, at int) bool {
	j := at
	if j+2 < len(toks) && toks[j+1].Text == "." && toks[j+2].Kind == Ident {
		j += 2
	}
	if j < 0 || j >= len(toks) || toks[j].Kind != Ident {
		return false
	}
	nm := toks[j].Text
	if nm != "Get" && nm != "get" {
		return false
	}
	k := nextSig(toks, j+1)
	if k < 0 || k >= len(toks) || toks[k].Text != "[" {
		return false
	}
	cl := matchDelim(toks, k, "[", "]")
	if cl < 0 || cl >= len(toks) {
		return false
	}
	p := nextSig(toks, cl+1)
	return p >= 0 && p < len(toks) && toks[p].Text == "("
}

// forEachCollType 返回 each 表达式根标识符的集合类型文本（第十二轮 P3-F），
// 用于区分「非列表」与「列表但元素类型推不出」。查不到返回空。
func (ctx *jsxSceneCtx) forEachCollType(eachExpr string) string {
	root := rootIdentOfExpr(eachExpr)
	if root == "" {
		return ""
	}
	if ctx.sigTypes != nil && ctx.sigTypes[root] != "" {
		return ctx.sigTypes[root]
	}
	if ctx.bindings != nil {
		t := ctx.bindings.TypeAt(root, ctx.jsxPos)
		// 第十三轮 P3-E：each 是函数调用（load() / getItems()）时 TypeAt 拿不到，
		// 补查函数返回值类型，使 map/string 等非列表形态也能命中 for-not-list。
		if t == "" && strings.Contains(eachExpr, "(") {
			t = ctx.bindings.FuncResult(root)
		}
		return t
	}
	return ""
}

// reportForNotList 对 each 是 map 等非列表类型追加 GX 诊断（第十二轮 P3-F）。
func (ctx *jsxSceneCtx) reportForNotList(n *jsxNode, collType string) {
	if ctx == nil || ctx.diags == nil {
		return
	}
	*ctx.diags = append(*ctx.diags, GXDiagnostic{
		Pos:     ctx.jsxPos,
		Code:    "for-not-list",
		Message: "<For> 需要列表，但 each 的类型 " + collType + " 不是列表；请先转成切片再用 <For> 渲染",
	})
}

// report 追加一条带 GX 坐标的诊断。diags 为空时静默（如 CompileStage 不收集诊断）。
func (ctx *jsxSceneCtx) report(code string, pos Pos, msg string) {
	if ctx == nil || ctx.diags == nil {
		return
	}
	*ctx.diags = append(*ctx.diags, GXDiagnostic{Pos: pos, Code: code, Message: msg})
}

// reportAttrUnsupported 对不支持的属性写法追加带坐标的 GX 诊断（A2/A3/B10）。
// 目的是把"编译通过 + 静默错值"变成带 GX 位置和修法的明确报错。
func (ctx *jsxSceneCtx) reportAttrUnsupported(pos Pos, attr, msg string) {
	ctx.report("attr-unsupported", pos, "属性 "+attr+"："+msg)
}

// gxStandardEvents 是标准事件名（去 on 前缀、首字母小写的驼峰值），仅用于 A5
// 高置信拼写提示。它不是白名单：on* 事件始终无条件透传，表外事件合法。
var gxStandardEvents = []string{
	"click", "mouseEnter", "mouseLeave", "mouseMove", "mouseDown", "mouseUp",
	"keyDown", "keyUp", "input", "change", "focus", "blur",
}

// suspiciousAttr 对「高置信拼错」的属性名给出提示（A5）。
// 只覆盖两类不会误伤后端自定义属性的模式：
//   - 冒号前缀（bind:value 等框架残留语法，GX 不支持）；
//   - onXxx 与某标准事件编辑距离 ≤1（onClik→onClick 等明显手误）。
//
// 返回空串表示不报；其余为建议写法。
func suspiciousAttr(name string) (msg, hint string) {
	if i := strings.IndexByte(name, ':'); i > 0 {
		return "不支持 " + name + " 这类冒号前缀属性（其他框架语法残留）；请用普通属性 + 事件回调",
			strings.Replace(name, ":", "", 1)
	}
	if !strings.HasPrefix(name, "on") || len(name) <= 2 {
		return "", ""
	}
	eve := name[2:]
	best, bestD := "", 3
	for _, std := range gxStandardEvents {
		if strings.EqualFold(eve, std) {
			return "", "" // 大小写差异不算拼错（GX 按原样透传）
		}
		if d := editDistanceLE2(strings.ToLower(eve), strings.ToLower(std), 2); d < bestD {
			best, bestD = std, d
		}
	}
	if bestD <= 1 {
		return "事件名 " + name + " 疑似拼写错误；标准事件为 on" + best + "（on* 仍会透传，请确认是否手误）", "on" + best
	}
	return "", ""
}

// editDistanceLE2 计算编辑距离，超过 limit 提前返回 limit+1（够用且省开销）。
func editDistanceLE2(a, b string, limit int) int {
	if len(a) > len(b)+limit || len(b) > len(a)+limit {
		return limit + 1
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		best := cur[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			v := prev[j-1] + cost
			if x := prev[j] + 1; x < v {
				v = x
			}
			if x := cur[j-1] + 1; x < v {
				v = x
			}
			cur[j] = v
		}
		for j := 0; j <= len(b); j++ {
			prev[j] = cur[j]
			if prev[j] < best {
				best = prev[j]
			}
		}
		if best > limit {
			return limit + 1
		}
	}
	return prev[len(b)]
}

// isObjectLitText 判断 readBraced 读出的表达式文本是否是一个对象字面量
// （trim 后以 '{' 开头且以 '}' 结尾，如 "{ active: true, big: true }"）。
func isObjectLitText(s string) bool {
	t := strings.TrimSpace(s)
	return len(t) >= 2 && t[0] == '{' && t[len(t)-1] == '}'
}

// isGoControlInJSX 判断 JSX 文本节点是否是一段误写的 Go 控制语句（A6）。
// JSX 里 for/switch 不是合法子节点，会被当文本并产出坏 Go。这里只认 trim 后
// 以 "for " / "switch " 开头的形态（关键字后须跟空白，避免误伤普通文本"format"）。
func isGoControlInJSX(s string) bool {
	t := strings.TrimSpace(s)
	for _, kw := range []string{"for", "switch"} {
		if strings.HasPrefix(t, kw) && len(t) > len(kw) && isHSpace(t[len(kw)]) {
			return true
		}
	}
	return false
}

// isArrowAssignExpr 判断事件属性值是否是"箭头函数 + 赋值 body"形态（B6）：
// `() => n = n + 1` / `(e) => n = e`。这类 lambda 在 JSX 属性路径不会被
// transformLambdas 展开，=> 会残留进 Go 源码导致语法错误。
func isArrowAssignExpr(s string) bool {
	arrow := strings.Index(s, "=>")
	if arrow < 0 {
		return false
	}
	body := strings.TrimSpace(s[arrow+2:])
	// body 以标识符/选择器开头且含单个 '='（赋值，非 ==/!=/<=/>=）。
	if body == "" {
		return false
	}
	for i := 0; i < len(body); i++ {
		if body[i] == '=' {
			prev := byte(0)
			next := byte(0)
			if i > 0 {
				prev = body[i-1]
			}
			if i+1 < len(body) {
				next = body[i+1]
			}
			if prev != '=' && prev != '!' && prev != '<' && prev != '>' && next != '=' {
				return true
			}
		}
	}
	return false
}

// indexedRoot 判断 each 表达式是否是「根标识符本身被索引」的形态（第十二轮 P1-A）。
// 判据：表达式里第一个 '[' 必须早于第一个 '('——
//
//	rows["k"]        → 第一个特殊字符是 '[' → true（根被索引）
//	groups[g][i]     → '[' → true
//	filter(rows["k"]) → 第一个特殊字符是 '(' → false（方括号在实参里，根是 filter）
//	getRows()[0]     → '(' → false
//
// 只有根被索引时才能按根标识符反推容器类型；否则应走函数返回值等旧路径。
func indexedRoot(expr string) bool {
	i := strings.IndexAny(expr, "[(")
	return i >= 0 && expr[i] == '['
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
// sigs 非空时对插值里的 sig 变量补读调用（{count} → count()），
// 弥补 transformSignals 无法穿透 JSX 整体 token 的盲区。
func lexExprScene(expr string, base Pos, ctx *jsxSceneCtx) []Token {
	toks := New(expr).AllTokens()
	// 需要补写 () 的名字集合：全局 sig + 当前 <For> 的 item getter 参数。
	// item 参数（信号化模式）是 func() T，读它必须写成 u() 才能让子树 getter
	// 登记对 item sig 的依赖——这是 For item sig 根治方案在编译器的落点。
	needRewrite := len(ctx.sigs) > 0 || ctx.forItemParam != ""
	if needRewrite {
		// 按 JSX 根位置解析绑定：被局部变量遮蔽的同名 sig 不应改写（{count}
		// 在 count := 3 作用域内读的是局部变量）。sigs 是全局名字集合，需先按
		// bindings+jsxPos 过滤出该位置真正指向 sig 的名字，再交回形态改写逻辑。
		effective := ctx.sigs
		if ctx.bindings != nil {
			effective = make(map[string]bool, len(ctx.sigs))
			for name := range ctx.sigs {
				if isSignalRefAt(ctx.bindings, name, ctx.jsxPos) {
					effective[name] = true
				}
			}
		} else if len(ctx.sigs) > 0 && ctx.forItemParam != "" {
			// 无绑定表但需补 item 参数：复制一份再改，避免污染全局 sigs map。
			cp := make(map[string]bool, len(ctx.sigs)+1)
			for name := range ctx.sigs {
				cp[name] = true
			}
			effective = cp
		}
		if ctx.forItemParam != "" {
			if effective == nil {
				effective = map[string]bool{}
			}
			effective[ctx.forItemParam] = true
		}
		toks = rewriteJSXInterpSignals(toks, effective, &ctx.explicitSigCallErr, ctx.forItemParam)
	}
	// sig 字段 selector 读补 ()（如组件内 {p.name} → p.name()）。
	// 与 rewriteJSXInterpSignals 独立：后者只认全局 sig 名，不管 sig 字段。
	toks = rewriteJSXSignalFieldSelectors(toks, ctx.bindings, ctx.signalFields, ctx.jsxPos)
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
	for i := 0; i < len(in); i++ {
		t := in[i]
		if t.Kind == JSX {
			// 记录根 JSX token 的绝对位置，供内部节点（如嵌套 <For>）查询绑定表。
			// 锁定期间（<For> render body 的重新词法化片段）保留外层锚点——内部
			// JSX 的 Pos 是相对假坐标，覆盖后插值里的函数内 sig 绑定查询会落空。
			if !ctx.jsxPosLocked {
				ctx.jsxPos = t.Pos
			}
			parser := newJSXParser(t.Text, t.Pos)
			node := parser.parseElement()
			out = append(out, genSceneExpr(node, ctx)...)

			// JSX 跨行缩进的排版换行（JSX 直接作为实参、未经 lambda 包装时）：
			// 闭合标签 `</div>` 后的换行再接宿主调用闭合符号 `)` / `]` / `,`，JSX 被替换
			// 成表达式后该换行夹在表达式与闭括号之间，Go 要求 trailing comma，否则报
			// "missing ',' before newline in argument list"。仅当换行后紧跟这些符号时
			// 吞掉（纯排版空白）；后接标识符或 `}`（函数体/语句块结束）则是语句结束换行，
			// 保留——否则会把 return 语句与函数闭合挤到同一行。
			j := i + 1
			hasNL := false
			// 第十三轮 P3-D：闭合标签后的排版区可能夹着行注释（`</div> // 尾注`
			// 再换行接 `)`）。Comment 必须一并视作排版并吞掉——保留它会把随后的
			// `)` 注释掉，生成代码丢闭合括号。
			for j < len(in) && (in[j].Kind == Space || in[j].Kind == Newline || in[j].Kind == Comment) {
				if in[j].Kind == Newline {
					hasNL = true
				}
				j++
			}
			if hasNL && j < len(in) {
				switch in[j].Text {
				case ")", "]", ",":
					i = j - 1 // 吞掉纯排版空白（Space/Newline/Comment）
				}
			}
			// B9：return <a/> <b/> 多根——return 后紧跟另一个 JSX 根，产出
			// 两个相邻 Render 表达式导致非法 Go。给明确诊断 + 修法。
			if prev := prevSig(in, i-1); prev >= 0 && in[prev].Text == "return" &&
				j < len(in) && in[j].Kind == JSX {
				ctx.report("jsx-multi-root", t.Pos,
					"组件 return 只能有一个根 JSX；请用 <div>...</div> 包裹多个根，或改写为 if 块返回单个根")
			}
			continue
		}
		// B9：return if v { <b/> } else { <i/> } —— Go 里 return 后不能跟 if 语句，
		// 产非法 Go。给明确诊断 + 修法（用条件表达式或包一层组件）。
		if t.Text == "return" {
			if n := nextSig(in, i+1); n >= 0 && in[n].Text == "if" {
				ctx.report("jsx-return-if", t.Pos,
					"return 后不能直接跟 if 块；请用条件表达式（cond ? <a/> : <b/>）或让组件返回单个根")
			}
		}
		out = append(out, t)
	}
	return out
}
