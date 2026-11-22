package compiler

import "strings"

// signalfields.go — struct signal 字段（语言特性）。
//
// GX 写法：
//
//	type CardProps struct {
//	    signal name string   // 响应式字段：编译器生成 getter 字段
//	    age int              // 普通字段
//	}
//
// 降级为 Go：
//
//	type CardProps struct {
//	    name Signal.Signal[string]  // getter 字段：无参调用读，带参调用写
//	    age  int
//	}
//
// 配套读写改写（持有该 struct 类型的地方）：
//
//	p := CardProps{name: Signal.New("x")}  // 对象字面量里 signal 字段自动包 Signal.New
//	println(p.name())                      // 读：p.name → p.name()
//	p.name("y")                            // 写：p.name = "y" → p.name("y")
//
// 这样字段值可由外部 Signal 驱动，组件/持有者读到的是实时值而非一次性快照。
//
// 关键点：降级必须在 Bind（绑定表构建）之前完成——否则 struct 里的 `signal`
// 关键字会被局部 signal 逻辑误判为 `signal name = expr` 声明（旧行为生成非法
// 的 `var name any string`）。降级后所有下游（Bindings/scope/objectlit/pub/
// external）看到的都是标准 Go 的 `Signal.Signal[T]` 字段，天然享受既有保护。

// signalFieldTable 记录每个具名 struct 的 signal 字段名 → 字段类型文本。
// 类型文本（如 "string"）供 JSX props 生成 Signal.Computed(func() T {...}) 使用。
// 值非空即表示该字段是 signal 字段；空串不应出现（signal 字段必有类型）。
type signalFieldTable map[string]map[string]string

// signalFieldDecl 描述一处 `signal name TYPE` 字段声明在 token 流中的位置。
type signalFieldDecl struct {
	typeName string // struct 类型名
	field    string // 字段名
	typeText string // 字段类型文本（如 "string"），JSX Computed 包装需要
	sigIdx   int    // `signal` token 下标
	nameIdx  int    // 字段名 token 下标
	typeFrom int    // 类型起始下标
	typeTo   int    // 类型结束下标（开区间）
}

// collectSignalFieldDecls 扫描原始 token 流里的 `type X struct { ... }`，
// 找出其中形如 `signal name TYPE` 的字段声明。
//
// 用 splitStructFields 切字段段（换行/分号分隔），每段首 token 是 `signal`
// 且第二个有效 token 是标识符（字段名）、其后是类型，即识别为 signal 字段。
func collectSignalFieldDecls(toks []Token) []signalFieldDecl {
	var out []signalFieldDecl
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("type") {
			continue
		}
		j := nextSig(toks, i+1)
		if j < 0 || toks[j].Kind != Ident || isGoKeyword(toks[j].Text) {
			continue
		}
		typeName := toks[j].Text
		k := nextSig(toks, j+1)
		if k < 0 || !toks[k].is("struct") {
			continue
		}
		open := nextSig(toks, k+1)
		if open < 0 || toks[open].Text != "{" {
			continue
		}
		close := matchDelimSkipSpace(toks, open, "{", "}")
		if close <= open {
			continue
		}
		for _, seg := range splitStructFields(toks, open+1, close) {
			first := nextSigIn(toks, seg[0], seg[1])
			if first < 0 || !toks[first].is("signal") {
				continue
			}
			nameIdx := nextSigIn(toks, first+1, seg[1])
			if nameIdx < 0 || toks[nameIdx].Kind != Ident || isGoKeyword(toks[nameIdx].Text) {
				continue
			}
			typeFrom := nextSigIn(toks, nameIdx+1, seg[1])
			if typeFrom < 0 {
				continue // `signal name` 无类型：不降级，交 Go 报错
			}
			typeTo := typeRangeEndIn(toks, typeFrom, seg[1])
			if typeTo <= typeFrom {
				continue
			}
			out = append(out, signalFieldDecl{
				typeName: typeName,
				field:    toks[nameIdx].Text,
				typeText: exprText(toks, typeFrom, typeTo),
				sigIdx:   first,
				nameIdx:  nameIdx,
				typeFrom: typeFrom,
				typeTo:   typeTo,
			})
		}
	}
	return out
}

// typeRangeEndIn 在 [start, limit) 内求类型 token 区间的结束下标。
// 复用 typeRangeEnd 的括号/泛型层次判断，再把结果夹到 limit 内。
func typeRangeEndIn(toks []Token, start, limit int) int {
	end := typeRangeEnd(toks, start)
	if end > limit {
		end = limit
	}
	return end
}

// buildSignalFieldTable 从字段声明构建「type → signal 字段名」表。
func buildSignalFieldTable(decls []signalFieldDecl) signalFieldTable {
	tbl := signalFieldTable{}
	for _, d := range decls {
		if tbl[d.typeName] == nil {
			tbl[d.typeName] = map[string]string{}
		}
		tbl[d.typeName][d.field] = d.typeText
	}
	return tbl
}

// hasSignalFields 报告表里是否有任何 signal 字段（供管线早退）。
func (t signalFieldTable) hasSignalFields() bool { return len(t) > 0 }

// has 判断 typeName 的 field 是否是 signal 字段。
func (t signalFieldTable) has(typeName, field string) bool {
	_, ok := t[typeName][field]
	return ok
}

// fieldType 返回 typeName.field 的字段类型文本；非 signal 字段返回空串。
func (t signalFieldTable) fieldType(typeName, field string) string {
	return t[typeName][field]
}

// hasField 判断任意 struct 是否声明了名为 field 的 signal 字段（宽松兜底）。
func (t signalFieldTable) hasField(field string) bool {
	for _, fields := range t {
		if _, ok := fields[field]; ok {
			return true
		}
	}
	return false
}

// downgradeSignalFields 把 `signal name TYPE` 降级为 `name Signal.Signal[TYPE]`。
// 从后往前替换，避免下标漂移。Signal token 标记 IsGen 由 ReconcileImports 注入 import。
func downgradeSignalFields(toks []Token, decls []signalFieldDecl) []Token {
	if len(decls) == 0 {
		return toks
	}
	out := append([]Token{}, toks...)
	// 按下标降序替换，前面的替换不影响后面的下标。
	for d := len(decls) - 1; d >= 0; d-- {
		decl := decls[d]
		p := toks[decl.sigIdx].Pos
		var repl []Token
		repl = append(repl, tokIdent(decl.field, p), tokSpace(" ", p))
		repl = append(repl, signalPkgToken(p), tokOp(".", p), tokIdent("Signal", p), tokOp("[", p))
		// 类型 token 原样保留（保留原始 Pos 便于 sourcemap），去掉尾随空白。
		for k := decl.typeFrom; k < decl.typeTo; k++ {
			if toks[k].Kind == Space || toks[k].Kind == Comment || toks[k].Kind == Newline {
				continue
			}
			repl = append(repl, toks[k])
		}
		repl = append(repl, tokOp("]", p))
		out = append(out[:decl.sigIdx], append(repl, out[decl.typeTo:]...)...)
	}
	return out
}

// ---------- 读写解包 ----------

// recvTypeAt 返回 selector 左侧 receiver 在 recvPos 处的推断类型名。
// receiver 可能是局部变量 / 函数参数 / 方法接收者，都由 Bindings 统一解析。
func recvTypeAt(b *Bindings, recvName string, recvPos Pos) string {
	if b == nil {
		return ""
	}
	if bd := b.ResolveAt(recvName, recvPos); bd != nil {
		return bd.Type
	}
	return ""
}

// isSignalSelector 判断位置 i 处的 `recv . field` 是否是「已知 struct 类型的
// signal 字段」。返回 (字段名, 是否命中, selector 结束下标)。
// i 必须是 receiver 标识符下标；field 下标与 '.' 下标在内部重新定位。
func isSignalSelector(toks []Token, i int, b *Bindings, tbl signalFieldTable) (field string, ok bool, fieldIdx int) {
	if i < 0 || i >= len(toks) || toks[i].Kind != Ident {
		return "", false, -1
	}
	j := nextSig(toks, i+1)
	if j < 0 || toks[j].Text != "." {
		return "", false, -1
	}
	k := nextSig(toks, j+1)
	if k < 0 || toks[k].Kind != Ident || isGoKeyword(toks[k].Text) {
		return "", false, -1
	}
	recvName := toks[i].Text
	field = toks[k].Text
	typeName := recvTypeAt(b, recvName, toks[i].Pos)
	return field, tbl.has(typeName, field), k
}

// rewriteSignalFieldRefs 对持有 signal 字段 struct 的 selector 做读写解包：
//   - 读 `p.name` → `p.name()`
//   - 写 `p.name = expr` → `p.name(expr)`
//   - 复合写 `p.name += expr` → `p.name(p.name() + expr)`
//   - 自增 `p.name++` → `p.name(p.name() + 1)`
//
// 判定基于 receiver 的推断类型 + signalFieldTable；普通字段、外部包符号、
// 已是调用 `p.name(...)` 都不改。this transform 在 transformSignals 之后、
// external 之前运行——此时字段仍是小写、类型已是 Signal.Signal[T]。
func rewriteSignalFieldRefs(toks []Token, b *Bindings, tbl signalFieldTable) []Token {
	if !tbl.hasSignalFields() {
		return toks
	}
	out := make([]Token, 0, len(toks))
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		_, ok, fieldIdx := isSignalSelector(toks, i, b, tbl)
		if !ok {
			out = append(out, t)
			continue
		}

		// 已是调用 p.name(...) → 不改（用户已显式读写 getter）
		n := nextSig(toks, fieldIdx+1)
		if n >= 0 && toks[n].Text == "(" {
			out = append(out, t)
			continue
		}

		// 找 selector 之后紧跟的运算符（跳过空白）
		opIdx := nextSig(toks, fieldIdx+1)
		op := ""
		if opIdx >= 0 {
			candidate := toks[opIdx].Text
			if isAssignOp(candidate) || compoundAssignOps[candidate] || candidate == "++" || candidate == "--" {
				op = candidate
			}
		}

		if op == "" {
			// 纯读：p.name → p.name()
			// 输出 receiver 到 field 之间的原 token（含 '.' 与可能的空白）
			for k := i; k <= fieldIdx; k++ {
				out = append(out, toks[k])
			}
			out = append(out, tokOp("(", toks[fieldIdx].Pos), tokOp(")", toks[fieldIdx].Pos))
			// i 之后的 token（'.'、field）已被消费
			i = fieldIdx
			continue
		}

		// 写 / 复合写：整体消费 `recv . field op expr`
		end := opIdx + 1
		if op != "++" && op != "--" {
			end = signalExprEnd(toks, opIdx+1)
		}
		p := toks[fieldIdx].Pos
		// p.name(
		for k := i; k <= fieldIdx; k++ {
			out = append(out, toks[k])
		}
		out = append(out, tokOp("(", p))

		switch {
		case op == "++" || op == "--":
			// p.name(p.name() + 1) / p.name(p.name() - 1)
			binop := "+"
			if op == "--" {
				binop = "-"
			}
			for k := i; k <= fieldIdx; k++ {
				out = append(out, toks[k])
			}
			out = append(out, tokOp("()", p), tokSpace(" ", p), tokOp(binop, p), tokSpace(" ", p), tokOp("1", p))
		case isAssignOp(op):
			// p.name(expr)：右侧表达式里的裸 signal 读取已由前置 transformSignals
			// 处理成调用式，这里原样拷贝即可。
			for k := opIdx + 1; k < end; k++ {
				out = append(out, toks[k])
			}
		default:
			// 复合写 p.name += expr → p.name(p.name() + expr)
			binop := strings.TrimSuffix(op, "=")
			for k := i; k <= fieldIdx; k++ {
				out = append(out, toks[k])
			}
			out = append(out, tokOp("()", p), tokSpace(" ", p), tokOp(binop, p), tokSpace(" ", p))
			for k := opIdx + 1; k < end; k++ {
				out = append(out, toks[k])
			}
		}
		out = append(out, tokOp(")", p))
		i = end - 1
	}
	return out
}

// ---------- 构造包装 ----------

// signalPassthrough 描述一个「已是 signal、构造时直通」的值表达式区间。
type signalPassthrough struct {
	from, to int // 直通 token 区间（开区间）
}

// signalExprPassthrough 判断 [from,to) 的值表达式是否已是 signal，返回直通区间。
//
// 关键场景（保鲜语义）：源 signal 变量 src 传给 signal 字段时，transformSignals
// 会先把读取 src 改写成 src()；此时必须「剥离 ()、传 getter 本身」，否则
// Signal.New(src()) 只是把当前值快照一遍，src 后续变化不再反映到字段。
//
// 识别三种形态：
//   - sigVar() 且 sigVar 是 signal/computed 绑定 → 直通 sigVar（剥离 ()）；
//   - sigVar 裸标识符且是 signal/computed 绑定 → 直通 sigVar；
//   - Signal.New(...) / Signal.Computed(...) 等 Signal.xxx 调用 → 整段原样直通。
func signalExprPassthrough(toks []Token, from, to int, b *Bindings) (signalPassthrough, bool) {
	first := nextSigIn(toks, from, to)
	if first < 0 {
		return signalPassthrough{}, false
	}
	t := toks[first]

	// src() → 剥离 () 直通 src（保鲜的核心）
	if t.Kind == Ident && b != nil {
		lp := nextSigIn(toks, first+1, to)
		if lp >= 0 && toks[lp].Text == "(" {
			rp := matchDelim(toks, lp, "(", ")")
			// 必须恰好是 sigVar()（右括号后到 to 无其他有效 token）
			if rp > lp && nextSigIn(toks, rp+1, to) < 0 {
				if bd := b.ResolveAt(t.Text, t.Pos); bd != nil &&
					(bd.Kind == BkSignal || bd.Kind == BkComputed) {
					return signalPassthrough{from: first, to: lp}, true // 去掉 ()
				}
			}
		}
	}

	// 裸 sigVar 标识符
	if t.Kind == Ident && nextSigIn(toks, first+1, to) < 0 && b != nil {
		if bd := b.ResolveAt(t.Text, t.Pos); bd != nil &&
			(bd.Kind == BkSignal || bd.Kind == BkComputed) {
			return signalPassthrough{from: first, to: first + 1}, true
		}
	}

	// Signal.New(...) / Signal.Computed(...) / Signal.xxx 调用 → 整段直通
	if t.is("Signal") {
		if dot := nextSigIn(toks, first+1, to); dot >= 0 && toks[dot].Text == "." {
			// prevSigIn 的 end 是「包含」端点：传 to-1，避免把区间外的 token
			// （如复合字面量的 '}'）误计入直通区间导致产物多出一个 '}'。
			last := prevSigIn(toks, to-1, from)
			if last >= from && last < to {
				return signalPassthrough{from: first, to: last + 1}, true
			}
		}
	}
	return signalPassthrough{}, false
}

// exprHasSignalRead 判断 [from,to) 内是否含「signal 变量读取」——即形如 ident()
// 且 ident 经 bindings 解析为 signal/computed 绑定。transformSignals 在前置阶段已把
// 裸 signal 读改写成调用式，故这是判定「派生表达式」的可靠依据。
//
// 只认 signal 变量，不认 signal 字段 selector（w.inner()）：后者属字段到字段透传
// 的既有语义范畴，不在手写派生档内。
func exprHasSignalRead(toks []Token, from, to int, b *Bindings) bool {
	if b == nil {
		return false
	}
	for i := from; i < to; i++ {
		if toks[i].Kind != Ident {
			continue
		}
		lp := nextSigIn(toks, i+1, to)
		if lp < 0 || toks[lp].Text != "(" {
			continue
		}
		if bd := b.ResolveAt(toks[i].Text, toks[i].Pos); bd != nil &&
			(bd.Kind == BkSignal || bd.Kind == BkComputed) {
			return true
		}
	}
	return false
}

// signalComputedCall 生成 Signal.Computed(func() T { return <expr> }) token 序列，
// 用于手写构造的派生档（与 JSX signalFieldPropTokens 的派生档语义一致：保鲜）。
func signalComputedCall(T string, p Pos, expr []Token) []Token {
	out := []Token{
		signalPkgToken(p), tokOp(".", p), tokIdent("Computed", p), tokOp("(", p),
		tokIdent("func", p), tokOp("(", p), tokOp(")", p), tokSpace(" ", p),
	}
	out = append(out, tokensForType(T, p)...)
	out = append(out, tokSpace(" ", p), tokOp("{", p), tokSpace(" ", p), tokIdent("return", p), tokSpace(" ", p))
	for _, t := range expr {
		if t.Kind == EOF {
			continue
		}
		out = append(out, t)
	}
	out = append(out, tokSpace(" ", p), tokOp("}", p), tokOp(")", p))
	return out
}

// compositeLitInfo 描述一处 `TypeName{ ... }` 具名复合字面量。
type compositeLitInfo struct {
	typeName string
	open     int
	close    int
}

// detectNamedCompositeLits 扫描当前 token 流里的具名复合字面量 TypeName{...}。
// 只识别顶层类型名是标识符 + '{' 的形态（排除 map[K]V{}/[]T{}/struct{}{}）。
func detectNamedCompositeLits(toks []Token) []compositeLitInfo {
	var out []compositeLitInfo
	for i := 0; i < len(toks); i++ {
		if toks[i].Kind != Ident || isGoKeyword(toks[i].Text) {
			continue
		}
		// 跳过 `pkg.Type{...}`：类型名是 selector 右侧时，取 selector 整体也不影响
		// （signal 字段是同包类型，直接写 TypeName{}）。这里仅处理裸类型名。
		if p := prevSig(toks, i-1); p >= 0 && toks[p].Text == "." {
			continue
		}
		open := nextSig(toks, i+1)
		if open < 0 || toks[open].Text != "{" {
			continue
		}
		close := matchDelimSkipSpace(toks, open, "{", "}")
		if close <= open {
			continue
		}
		out = append(out, compositeLitInfo{typeName: toks[i].Text, open: open, close: close})
	}
	return out
}

// wrapSignalFieldLiterals 对具名 struct 复合字面量里的 signal 字段值自动包装：
//   - 值已是 signal（变量/调用）→ 原样；
//   - 否则 → 包成 Signal.New(expr)。
//
// 用 signalFieldTable 判定「typeName.field 是否 signal 字段」，只对命中字段动手，
// 普通字段与其它 struct 的同名字段都不受影响。
func wrapSignalFieldLiterals(toks []Token, b *Bindings, tbl signalFieldTable) []Token {
	if !tbl.hasSignalFields() {
		return toks
	}
	lits := detectNamedCompositeLits(toks)
	if len(lits) == 0 {
		return toks
	}
	// 收集所有替换，从后往前应用避免下标漂移。
	type repl struct {
		from, to int
		toks     []Token
	}
	var reps []repl
	for _, lit := range lits {
		if !tbl.hasSignalFieldsOf(lit.typeName) {
			continue
		}
		for _, seg := range splitTopLevelSegs(toks, lit.open+1, lit.close) {
			colon := -1
			depth := 0
			for k := seg[0]; k < seg[1]; k++ {
				switch toks[k].Text {
				case "(", "[", "{":
					depth++
				case ")", "]", "}":
					if depth > 0 {
						depth--
					}
				case ":":
					if depth == 0 {
						colon = k
					}
				}
			}
			if colon < 0 {
				continue
			}
			nameIdx := nextSigIn(toks, seg[0], colon)
			if nameIdx < 0 || toks[nameIdx].Kind != Ident {
				continue
			}
			field := toks[nameIdx].Text
			if !tbl.has(lit.typeName, field) {
				continue
			}
			valFrom := nextSigIn(toks, colon+1, seg[1])
			if valFrom < 0 || valFrom >= seg[1] {
				continue
			}
			valTo := seg[1]
			if pass, ok := signalExprPassthrough(toks, valFrom, valTo, b); ok {
				// 已是 signal：直通到 pass.to 为止。src() 的 pass.to 落在 '(' 前，
				// 从而剥离调用括号、传 getter 本身（保鲜关键）；Signal.New(...)
				// 的 pass.to 落在末尾，整段原样直通。pass.to 之后的 token（如
				// src() 的 "()"）被有意丢弃。
				reps = append(reps, repl{from: valFrom, to: valTo, toks: append([]Token{}, toks[valFrom:pass.to]...)})
				continue
			}
			// 剥掉值表达式首尾空白
			vs := valFrom
			ve := valTo
			for vs < ve && (toks[vs].Kind == Space || toks[vs].Kind == Newline) {
				vs++
			}
			for ve > vs && (toks[ve-1].Kind == Space || toks[ve-1].Kind == Newline) {
				ve--
			}
			p := toks[nameIdx].Pos
			// 派生档（第六轮 P2b）：值表达式含 signal 变量读取 → 包 Computed 保鲜，
			// 与 JSX 侧 signalFieldPropTokens 语义一致。否则（字面量/普通常量表达式）
			// 仍是 Signal.New 快照。signalExprPassthrough 已先处理单 signal 读直通。
			if exprHasSignalRead(toks, vs, ve, b) {
				T := tbl.fieldType(lit.typeName, field)
				reps = append(reps, repl{from: valFrom, to: valTo, toks: signalComputedCall(T, p, toks[vs:ve])})
				continue
			}
			var toks2 []Token
			toks2 = append(toks2, signalPkgToken(p), tokOp(".", p), tokIdent("New", p), tokOp("(", p))
			for k := vs; k < ve; k++ {
				toks2 = append(toks2, toks[k])
			}
			toks2 = append(toks2, tokOp(")", p))
			reps = append(reps, repl{from: valFrom, to: valTo, toks: toks2})
		}
	}
	for k := len(reps) - 1; k >= 0; k-- {
		r := reps[k]
		toks = append(toks[:r.from], append(r.toks, toks[r.to:]...)...)
	}
	return toks
}

// hasSignalFieldsOf 判断某 typeName 是否有 signal 字段。
func (t signalFieldTable) hasSignalFieldsOf(typeName string) bool {
	return len(t[typeName]) > 0
}

// rewriteJSXSignalFieldSelectors 在 JSX 插值 token 里给「signal 字段 selector 读」
// 补上调用括号：p.name → p.name()。JSX 生成的 token 是 transform 新产物，没经过
// rewriteSignalFieldRefs，故需在此补——否则 {p.name} 会把 Signal 函数当值插入界面。
//
// 识别 X.field：field 是 receiver 推断类型上的 signal 字段、field 后不是 '('、
// 且 receiver X 不是紧邻 ')' 的调用结果（排除 For item getter `u().name` 这类普通字段读取）。
//
// 第六轮 P2 修复：早期版本用 tbl.hasField(field) 全文件按字段名猜测，导致同文件
// 里任意 struct 只要声明过 `signal name`，普通 `User.name` 也会被补成 u.name()
// （硬编译错误：string is not a function）。现改为 receiver 类型限定——只有 receiver
// 的推断类型确为某 struct、且该字段在该类型上是 signal 字段时才补 ()，兑现
// language-spec §11.1「同名但非 signal 的字段不受影响」的承诺。
//
// pos 是 JSX 在原始源码中的绝对位置：插值 token 的 Pos 是相对表达式的偏移，
// 不能用于 bindings 查询；以 JSX 根位置为锚点解析 receiver 绑定与 isSignalRefAt 一致。
func rewriteJSXSignalFieldSelectors(toks []Token, b *Bindings, tbl signalFieldTable, pos Pos) []Token {
	if !tbl.hasSignalFields() {
		return toks
	}
	out := make([]Token, 0, len(toks)+4)
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		out = append(out, t)
		if t.Kind != Ident {
			continue
		}
		dot := nextSig(toks, i+1)
		if dot < 0 || toks[dot].Text != "." {
			continue
		}
		fieldIdx := nextSig(toks, dot+1)
		if fieldIdx < 0 || toks[fieldIdx].Kind != Ident {
			continue
		}
		field := toks[fieldIdx].Text
		// receiver 类型限定：推断 receiver 的静态类型，只有该类型上的 signal 字段才补 ()。
		// 类型未知（推断失败）时保守不补——宁可少补让 Go 编译器暴露，也不误伤普通字段。
		recvType := recvTypeAt(b, t.Text, pos)
		if recvType == "" || !tbl.has(recvType, field) {
			continue
		}
		// field 后已是 '('（调用/字段写）→ 不补。
		if n := nextSig(toks, fieldIdx+1); n >= 0 && toks[n].Text == "(" {
			continue
		}
		// field 后是 ':'（对象字面量 key）→ 不补。
		if n := nextSig(toks, fieldIdx+1); n >= 0 && toks[n].Text == ":" {
			continue
		}
		// receiver 是紧邻 ')' 的调用结果（如 For item getter u()）→ 普通字段，不补。
		if prev := prevSig(toks, i-1); prev >= 0 && toks[prev].Text == ")" {
			continue
		}
		// 把 .field 原样搬运，再补 ()。
		for k := dot; k <= fieldIdx; k++ {
			out = append(out, toks[k])
		}
		out = append(out, tokOp("(", t.Pos), tokOp(")", t.Pos))
		i = fieldIdx
	}
	return out
}
