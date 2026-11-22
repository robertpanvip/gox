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

// signalFieldTable 记录 struct 字段元数据，供所有「signal 字段读」判定共用
// （第八轮主线：统一判据入口）。两张表：
//   - sig：type → signal 字段名 → 字段类型文本（如 "string"），供 JSX props
//     生成 Signal.Computed(func() T {...}) 及判断"是否 signal 字段"；
//   - all：type → 所有字段名 → 字段类型文本（含普通字段），供链式 selector
//     沿链传播 receiver 类型——判断 t.mid.in.v 时逐级求 mid/in 的类型，
//     最终用 v 的直接 receiver 类型查 sig，取代按字段名全局猜测（P1-3）。
type signalFieldTable struct {
	sig map[string]map[string]string
	all map[string]map[string]string
}

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

// buildSignalFieldTable 从 token 流构建字段元数据表：sig（signal 字段）+ all（所有字段类型）。
// all 表从每个 `type X struct {...}` 的全部字段收集，用于链式 selector 沿链传播类型。
func buildSignalFieldTable(toks []Token, decls []signalFieldDecl) signalFieldTable {
	tbl := signalFieldTable{
		sig: map[string]map[string]string{},
		all: map[string]map[string]string{},
	}
	for _, d := range decls {
		if tbl.sig[d.typeName] == nil {
			tbl.sig[d.typeName] = map[string]string{}
		}
		tbl.sig[d.typeName][d.field] = d.typeText
	}
	// 收集所有 struct 字段类型（含普通字段）。signal 字段降级后类型是
	// Signal.Signal[T]，普通字段是其声明类型——沿链传播时据此逐级求 receiver 类型。
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("type") {
			continue
		}
		tn := nextSig(toks, i+1)
		if tn < 0 || toks[tn].Kind != Ident {
			continue
		}
		sk := nextSig(toks, tn+1)
		if sk < 0 || !toks[sk].is("struct") {
			continue
		}
		open := nextSig(toks, sk+1)
		if open < 0 || toks[open].Text != "{" {
			continue
		}
		close := matchDelimSkipSpace(toks, open, "{", "}")
		if close <= open {
			continue
		}
		typeName := toks[tn].Text
		if tbl.all[typeName] == nil {
			tbl.all[typeName] = map[string]string{}
		}
		for _, seg := range splitStructFields(toks, open+1, close) {
			first := nextSigIn(toks, seg[0], seg[1])
			if first < 0 || toks[first].Kind != Ident || isGoKeyword(toks[first].Text) {
				continue
			}
			if toks[first].is("signal") {
				first = nextSigIn(toks, first+1, seg[1])
				if first < 0 || toks[first].Kind != Ident {
					continue
				}
			}
			field := toks[first].Text
			tf := nextSigIn(toks, first+1, seg[1])
			if tf < 0 {
				continue
			}
			te := typeRangeEndIn(toks, tf, seg[1])
			if te <= tf {
				continue
			}
			tbl.all[typeName][field] = exprText(toks, tf, te)
		}
	}
	return tbl
}

// hasSignalFields 报告表里是否有任何 signal 字段（供管线早退）。
func (t signalFieldTable) hasSignalFields() bool { return len(t.sig) > 0 }

// has 判断 typeName 的 field 是否是 signal 字段。
func (t signalFieldTable) has(typeName, field string) bool {
	_, ok := t.sig[typeName][field]
	return ok
}

// fieldType 返回 typeName.field 的字段类型文本；非 signal 字段返回空串。
func (t signalFieldTable) fieldType(typeName, field string) string {
	return t.sig[typeName][field]
}

// fieldTypeOf 返回任意字段（含普通字段）的声明类型文本，供链式 selector 沿链
// 传播 receiver 类型；字段或类型未知返回空串。
func (t signalFieldTable) fieldTypeOf(typeName, field string) string {
	return t.all[typeName][field]
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
// 指针 receiver（*CardProps，如指针 props 组件）剥去前导 '*' 返回基类型名——
// signal 字段表按基类型名登记，解包 p.name → p.name() 对指针同样成立（第七轮 §6a）。
func recvTypeAt(b *Bindings, recvName string, recvPos Pos) string {
	if b == nil {
		return ""
	}
	if bd := b.ResolveAt(recvName, recvPos); bd != nil {
		return strings.TrimSpace(strings.TrimPrefix(bd.Type, "*"))
	}
	return ""
}

// signalSelector 是「一处 signal 字段 selector 读」的解析结果，由统一入口
// resolveSignalSel 产出。所有需要判断 signal 字段读的 transform 都应消费它，
// 避免各处自拟判据（第八轮主线：判据分裂是 4 个 P1 的共同根因）。
type signalSelector struct {
	field    string // 链尾字段名（如 name）
	fieldIdx int    // 字段名 token 下标（补 () / 剥 () 的定位点）
	recvIdx  int    // 链首 receiver token 下标
	recvType string // receiver 推断类型（可能为空：链式 receiver 退化判定）
	whole    bool   // [recvIdx, fieldIdx] 是否恰为完整 selector（无多余 token）
}

// resolveSignalSel 是 selector 「是否 signal 字段读」的统一判定入口。
//
// 输入 i 为 receiver 链首标识符下标（如 p.a 的 p、t.mid.in.v 的 t、getP().a 的
// getP）。向右解析完整的 '.' 字段链，取链尾字段名；判据分两级：
//
//  1. 二段式 recv.field：按 recvPos 处 receiver 的推断类型 + tbl.has(type, field)
//     精确判定（支持 *T 剥指针）；
//  2. 链式 / 非标识符 receiver（t.mid.in.v、getP().a）：链首类型无法沿字段链传播时，
//     退化为「链尾字段名在唯一一个已声明类型上是 signal 字段」判定——即只有恰好
//     一个 struct 声明了该名字的 signal 字段才命中；多类型同名则放弃（宁可少补，
//     不误伤普通字段）。这是前几轮报告建议的限定场景方案。
//
// 硬规则：绝不使用全表按名匹配（hasField）——它会让任意同名普通字段被误判。
//
// recvPos 是查询 Bindings 的位置锚点：普通 token 流传 toks[i].Pos；JSX 插值 token
// 的 Pos 是相对表达式偏移（lexExprScene 后统一被改成 base），须传 ctx.jsxPos。
// limit 为扫描右边界（开区间），<=0 表示用 len(toks)。
func resolveSignalSel(toks []Token, i int, recvPos Pos, b *Bindings, tbl signalFieldTable, limit int) (signalSelector, bool) {
	if !tbl.hasSignalFields() || i < 0 || i >= len(toks) || toks[i].Kind != Ident || isGoKeyword(toks[i].Text) {
		return signalSelector{}, false
	}
	if limit <= 0 || limit > len(toks) {
		limit = len(toks)
	}
	// 起点若是调用 getP(...)，把游标推进到 ')'——调用结果后面若紧跟 '.'，
	// 它就是 selector 的 receiver（getP().a）。chainStart 是链首 receiver 的下标。
	chainStart := i
	callRecv := false
	if lp := nextSigIn(toks, i+1, limit); lp >= 0 && toks[lp].Text == "(" {
		if rp := matchDelim(toks, lp, "(", ")"); rp > lp && rp < limit {
			if after := nextSigIn(toks, rp+1, limit); after >= 0 && toks[after].Text == "." {
				chainStart, callRecv = rp, true
			}
		}
	}
	// 第一遍：沿 '.' 链走，取链尾字段名与下标。
	lastField, lastFieldIdx := "", -1
	cur := chainStart
	for {
		dot := nextSigIn(toks, cur+1, limit)
		if dot < 0 || toks[dot].Text != "." {
			break
		}
		k := nextSigIn(toks, dot+1, limit)
		if k < 0 || toks[k].Kind != Ident || isGoKeyword(toks[k].Text) {
			break
		}
		lastField, lastFieldIdx = toks[k].Text, k
		cur = k
	}
	if lastFieldIdx < 0 {
		return signalSelector{}, false
	}
	// 第二遍：沿链逐段传播 receiver 类型，求"进入链尾字段前"的 receiver 类型。
	// 用户规则——只按 receiver 类型 + signalFieldTable 判定，不按字段名全局猜测。
	tailRecv := receiverTypeBeforeTail(toks, i, chainStart, callRecv, recvPos, b, tbl, lastFieldIdx, limit)
	if tailRecv == "" || !tbl.has(tailRecv, lastField) {
		return signalSelector{}, false
	}
	return signalSelector{field: lastField, fieldIdx: lastFieldIdx, recvIdx: i, recvType: tailRecv, whole: !callRecv}, true
}

// signalChain 描述一条 selector 链上所有 signal 字段段的解析结果。
// segs 是链上所有「signal 字段」的 token 下标（读时需在其后补 ()）；链上每段
// 都按「进入该段前的 receiver 真实类型」判定，中间段是 signal 也会被识别。
type signalChain struct {
	segs        []int // 链上所有 signal 字段的下标（按从左到右）
	tailIdx     int   // 链尾字段名下标
	tailIsSig   bool  // 链尾字段是不是 signal
	chainStart  int   // 链首 receiver 下标（ident 或调用结果 ')'）
	callRecv    bool  // 链首是否为调用结果（getP().a）
	recvIdx     int   // 链首 ident 下标
	fieldCount  int   // 链上字段段总数
}

// resolveSignalChain 沿链逐段解析：从 i 处的 receiver 出发，沿 '.' 字段链走到尾，
// 逐段按 receiver 真实类型判断该字段是不是 signal 字段，收集所有 signal 段下标。
// 这是"链条中每一段是 signal 就加括号"规则的统一实现（第八轮用户补充）。
//
// 类型沿链传播：每段 field 的声明类型从 tbl.all 取（signal 字段降级后是
// Signal.Signal[T]），bareFieldType 剥出元素类型 T 作为下一段的 receiver 类型；
// 任一段类型无法解析则停止传播（后续段不再判为 signal，保守不补）。
func resolveSignalChain(toks []Token, i int, recvPos Pos, b *Bindings, tbl signalFieldTable, limit int) (signalChain, bool) {
	if !tbl.hasSignalFields() || i < 0 || i >= len(toks) || toks[i].Kind != Ident || isGoKeyword(toks[i].Text) {
		return signalChain{}, false
	}
	if limit <= 0 || limit > len(toks) {
		limit = len(toks)
	}
	// 起点若是调用 getP(...)，其 ')' 是链首 receiver。
	chainStart := i
	callRecv := false
	if lp := nextSigIn(toks, i+1, limit); lp >= 0 && toks[lp].Text == "(" {
		if rp := matchDelim(toks, lp, "(", ")"); rp > lp && rp < limit {
			if after := nextSigIn(toks, rp+1, limit); after >= 0 && toks[after].Text == "." {
				chainStart, callRecv = rp, true
			}
		}
	}
	// 起始 receiver 类型：变量从 Bindings 查；调用结果用函数返回类型。
	curType := recvTypeAt(b, toks[i].Text, recvPos)
	if callRecv && b != nil && curType == "" {
		if rt := b.FuncResult(toks[i].Text); rt != "" {
			curType = strings.TrimSpace(strings.TrimPrefix(rt, "*"))
		}
	}
	if curType == "" {
		return signalChain{}, false
	}

	chain := signalChain{chainStart: chainStart, callRecv: callRecv, recvIdx: i}
	cur := chainStart
	for {
		dot := nextSigIn(toks, cur+1, limit)
		if dot < 0 || toks[dot].Text != "." {
			break
		}
		k := nextSigIn(toks, dot+1, limit)
		if k < 0 || toks[k].Kind != Ident || isGoKeyword(toks[k].Text) {
			break
		}
		field := toks[k].Text
		isSig := tbl.has(curType, field)
		if isSig {
			chain.segs = append(chain.segs, k)
			chain.tailIsSig = true
		} else {
			chain.tailIsSig = false
		}
		chain.tailIdx = k
		chain.fieldCount++
		// 传播到下一段：signal 字段用其信号元素类型，普通字段用声明类型。
		ft := tbl.fieldTypeOf(curType, field)
		if ft == "" && isSig {
			ft = tbl.fieldType(curType, field)
		}
		curType = bareFieldType(ft)
		if curType == "" {
			// 类型链断裂（如末尾是 int/string）：后面即使还有 '.' 也无法判定，
			// 保守停止、不按名猜测。
			break
		}
		cur = k
	}
	if chain.fieldCount == 0 {
		return signalChain{}, false
	}
	return chain, true
}

// receiverTypeBeforeTail 沿链从起点走到链尾字段的直接 receiver 类型：
// 解析 [start, tailFieldIdx) 之间的字段链，逐段把 receiver 类型推进到链尾字段的父类型。
// 与 resolveSignalSel 主循环同一套类型传播，独立成函数以便精确取"进入链尾前"的类型。
func receiverTypeBeforeTail(toks []Token, i, start int, callRecv bool, recvPos Pos, b *Bindings, tbl signalFieldTable, tailIdx, limit int) string {
	curType := recvTypeAt(b, toks[i].Text, recvPos)
	if callRecv && b != nil && curType == "" {
		if rt := b.FuncResult(toks[i].Text); rt != "" {
			curType = strings.TrimSpace(strings.TrimPrefix(rt, "*"))
		}
	}
	if curType == "" {
		return ""
	}
	seg := start
	for {
		dot := nextSigIn(toks, seg+1, limit)
		if dot < 0 || toks[dot].Text != "." {
			return ""
		}
		k := nextSigIn(toks, dot+1, limit)
		if k < 0 || toks[k].Kind != Ident {
			return ""
		}
		if k == tailIdx {
			return curType // 进入链尾字段前的 receiver 类型
		}
		ft := tbl.fieldTypeOf(curType, toks[k].Text)
		curType = bareFieldType(ft)
		if curType == "" {
			return ""
		}
		seg = k
	}
}

// bareFieldType 把字段声明类型文本归一为「可作 receiver 的具名类型」：
//   - Signal.Signal[T] → T（signal 字段读出来的是 T）
//   - *T → T；去掉可能的包前缀（pkg.T → T，具名类型在 signalFieldTable 按基名登记）
//
// 无法归一（如 int/string 等内建类型、匿名 struct）返回空串。
func bareFieldType(ft string) string {
	t := strings.TrimSpace(ft)
	i := strings.Index(t, "[")
	// Signal.Signal[T] → T
	if strings.HasPrefix(t, "Signal.Signal[") || strings.HasPrefix(t, "signal.Signal[") {
		inner := t[i+1 : strings.LastIndex(t, "]")]
		return bareFieldType(inner)
	}
	if i >= 0 {
		return "" // 其它泛型（如 list.List[T]）不作为字段链 receiver
	}
	t = strings.TrimSpace(strings.TrimPrefix(t, "*"))
	if dot := strings.LastIndex(t, "."); dot >= 0 {
		t = t[dot+1:]
	}
	if isBuiltinType(t) {
		return ""
	}
	return t
}

// isBuiltinType 判断是否为内建标量类型（不能作为 struct 字段链 receiver）。
func isBuiltinType(t string) bool {
	switch t {
	case "string", "bool", "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"byte", "rune", "float32", "float64", "complex64", "complex128",
		"any", "error", "interface{}":
		return true
	}
	return false
}

// isSignalSelector 判断位置 i 处的二段式 `recv . field` 是否是 signal 字段读。
// 统一入口 resolveSignalSel 的薄包装，保留给 rewriteSignalFieldRefs 使用。
func isSignalSelector(toks []Token, i int, b *Bindings, tbl signalFieldTable) (field string, ok bool, fieldIdx int) {
	sel, ok := resolveSignalSel(toks, i, toks[i].Pos, b, tbl, 0)
	if !ok {
		return "", false, -1
	}
	return sel.field, true, sel.fieldIdx
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
	// isSigSeg 判断 k 是否是链上的 signal 字段段。
	isSigSeg := func(segs []int, k int) bool {
		for _, s := range segs {
			if s == k {
				return true
			}
		}
		return false
	}
	// emitChain 原样输出 [from,to]，在 segs 中每个「尚未被调用」的 signal 段后补 ()。
	// writeIdx >= 0 时该段是写目标：不补 ()，由调用方在函数体外追加写载荷。
	// 段后已是 '('（用户已显式写调用式，如 o.mid.b.v() 的 v）→ 该段不重复补，
	// 但链上其它 signal 段（如 b）仍要补——这是"逐段判断"规则的关键。
	emitChain := func(from, to int, segs []int, writeIdx int) {
		for k := from; k <= to; k++ {
			out = append(out, toks[k])
			if k == writeIdx || !isSigSeg(segs, k) {
				continue
			}
			if n := nextSig(toks, k+1); n >= 0 && toks[n].Text == "(" {
				continue // 已是调用
			}
			out = append(out, tokOp("(", toks[k].Pos), tokOp(")", toks[k].Pos))
		}
	}
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		chain, ok := resolveSignalChain(toks, i, toks[i].Pos, b, tbl, 0)
		if !ok || len(chain.segs) == 0 {
			out = append(out, t)
			continue
		}
		fieldIdx := chain.tailIdx
		// 若链上所有 signal 段后都已是 '('（用户已显式写全调用式，如 p.name()），
		// 整条链无需改动。否则继续——中间 signal 段可能仍缺 ()（如 o.mid.b.v() 的 b）。
		allCalled := true
		for _, s := range chain.segs {
			n := nextSig(toks, s+1)
			if n < 0 || toks[n].Text != "(" {
				allCalled = false
				break
			}
		}
		if allCalled {
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

		if op == "" || !chain.tailIsSig {
			// 纯读，或链尾不是 signal（链式中间段是 signal 也要补 ()）：
			// 对整个链的每个 signal 段补 ()，消费整条链。
			emitChain(i, fieldIdx, chain.segs, -1)
			i = fieldIdx
			continue
		}

		// 写 / 复合写：链尾是 signal（写目标），整体消费 `recv.chain op expr`。
		// 中间 signal 段仍补 ()，链尾作为写目标不补、改写成 p.name(...)。
		end := opIdx + 1
		if op != "++" && op != "--" {
			end = signalExprEnd(toks, opIdx+1)
		}
		p := toks[fieldIdx].Pos
		emitChain(i, fieldIdx, chain.segs, fieldIdx)
		out = append(out, tokOp("(", p))

		switch {
		case op == "++" || op == "--":
			binop := "+"
			if op == "--" {
				binop = "-"
			}
			// p.name(p.name() + 1)：读回整条链。链尾由下面的 tokOp("()") 补，
			// 中间的 signal 段由 emitChain 补（writeIdx=fieldIdx 让链尾不重复补）。
			emitChain(i, fieldIdx, chain.segs, fieldIdx)
			out = append(out, tokOp("()", p), tokSpace(" ", p), tokOp(binop, p), tokSpace(" ", p), tokOp("1", p))
		case isAssignOp(op):
			for k := opIdx + 1; k < end; k++ {
				out = append(out, toks[k])
			}
		default:
			binop := strings.TrimSuffix(op, "=")
			emitChain(i, fieldIdx, chain.segs, fieldIdx)
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
// 识别四种形态：
//   - sigVar() 且 sigVar 是 signal/computed 绑定 → 直通 sigVar（剥离 ()）；
//   - sigVar 裸标识符且是 signal/computed 绑定 → 直通 sigVar；
//   - recv.field() 且 field 是 receiver 类型上的 signal 字段 → 直通 recv.field（剥离 ()）；
//   - Signal.New(...) / Signal.Computed(...) 等 Signal.xxx 调用 → 整段原样直通。
//
// 前置：本函数须在 rewriteSignalFieldRefs 之后运行——此时 signal 字段读已被改写成
// 调用式 p.a()，本函数剥离 () 还原 getter 本身；若在之前运行只会看到裸 p.a，
// 直通后会被随后的 rewrite 改成 p.a()（快照，静默冻结，第六轮遗留缺陷）。
func signalExprPassthrough(toks []Token, from, to int, b *Bindings, tbl signalFieldTable) (signalPassthrough, bool) {
	first := nextSigIn(toks, from, to)
	if first < 0 {
		return signalPassthrough{}, false
	}
	t := toks[first]

	// recv.field() → 剥离 () 直通 recv.field（signal 字段到字段透传，保鲜）。
	// 形态：[receiver 链] . field ( )，field 由统一入口按 receiver 类型判定
	// （支持链式 receiver 与 getP().field，见 resolveSignalSel）。
	// 与 JSX 侧 pureSignalRead 的 recv.field() 分支共用同一判据。
	if t.Kind == Ident {
		if sel, ok := resolveSignalSel(toks, first, t.Pos, b, tbl, to); ok {
			lp := nextSigIn(toks, sel.fieldIdx+1, to)
			if lp >= 0 && toks[lp].Text == "(" {
				rp := matchDelim(toks, lp, "(", ")")
				if rp > lp && nextSigIn(toks, rp+1, to) < 0 {
					return signalPassthrough{from: first, to: lp}, true // 去掉 ()
				}
			}
		}
	}

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

// exprHasSignalRead 判断 [from,to) 是否读取了任何 signal（signal 变量或 signal 字段），
// 用统一入口 resolveSignalSel 判定 selector、用 Bindings 判定 signal 变量。
// 用途：区分「纯普通变量/普通字段」（用裸 getter 每次读最新）与
// 「含 signal 读的派生表达式」（用 Signal.Computed 登记依赖并缓存）——
// Computed 只在其依赖的 signal 变化时重算，若表达式里没有 signal 读，
// 它会把首值永久缓存，普通变量后续改动就反映不出来（第八轮用户诉求）。
func exprHasSignalRead(toks []Token, from, to int, b *Bindings, tbl signalFieldTable, posOverride Pos) bool {
	posAt := func(t Token) Pos {
		if posOverride.valid() {
			return posOverride
		}
		return t.Pos
	}
	for i := from; i < to; i++ {
		if toks[i].Kind != Ident {
			continue
		}
		// signal 变量读 ident()
		if b != nil {
			lp := nextSigIn(toks, i+1, to)
			if lp >= 0 && toks[lp].Text == "(" {
				if bd := b.ResolveAt(toks[i].Text, posAt(toks[i])); bd != nil &&
					(bd.Kind == BkSignal || bd.Kind == BkComputed) {
					return true
				}
			}
		}
		// signal 字段 selector 读 recv.field()（调用式，field 后跟 '('）
		if sel, ok := resolveSignalSel(toks, i, posAt(toks[i]), b, tbl, to); ok {
			after := nextSigIn(toks, sel.fieldIdx+1, to)
			if after >= 0 && toks[after].Text == "(" {
				return true
			}
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

// signalGetterCall 生成裸 getter `func(...T) T { return <expr> }` token 序列。
// 用于「纯普通变量/普通字段」表达式：Signal[T] 底层是 func(...T) T，函数字面量
// 可直接赋值；每次读都重新求值，普通变量后续改动能反映——这正是用户诉求
// （普通变量用函数返回，不感知 signal 差异）。与 Computed 的区别：不缓存、不登记
// 依赖，适合没有 signal 读的表达式（这类表达式本就无依赖可登记）。
func signalGetterCall(T string, p Pos, expr []Token) []Token {
	out := []Token{
		tokIdent("func", p), tokOp("(", p), tokOp("...", p),
	}
	out = append(out, tokensForType(T, p)...)
	out = append(out, tokOp(")", p), tokSpace(" ", p))
	out = append(out, tokensForType(T, p)...)
	out = append(out, tokSpace(" ", p), tokOp("{", p), tokSpace(" ", p), tokIdent("return", p), tokSpace(" ", p))
	for _, t := range expr {
		if t.Kind == EOF {
			continue
		}
		out = append(out, t)
	}
	out = append(out, tokSpace(" ", p), tokOp("}", p))
	return out
}

// compositeLitInfo 描述一处 `TypeName{ ... }` 具名复合字面量。
type compositeLitInfo struct {
	typeName string
	open     int
	close    int
}

// wrapSignalFieldLiterals 对具名 struct 复合字面量里的 signal 字段值自动包装三档
// （透传 getter / 字面量 Signal.New / 派生 Signal.Computed），只对命中字段动手。
//
// 递归下降实现（第七轮 §5）：早期版本用「收集全局替换 + 原始下标从后往前应用」，
// 兄弟级替换没问题，但嵌套字面量时外层区间端点落在内层替换之后，内层改变长度后
// 外层端点错位，吞掉 ','、'}' 生成非法 Go。改为一次扫描就地构造新 token 切片：
// 每处理一个字段值先递归处理其内部嵌套字面量，得到 newVal 再据此生成载荷，
// 全程用局部切片下标，不再有跨层的原始下标漂移。
func wrapSignalFieldLiterals(toks []Token, b *Bindings, tbl signalFieldTable) []Token {
	if !tbl.hasSignalFields() {
		return toks
	}
	out, changed := wrapLitsInRange(toks, 0, len(toks), b, tbl)
	if !changed {
		return toks
	}
	return out
}

// wrapLitsInRange 递归扫描 [from,to)，构造性地返回处理后的 token 切片。
// changed 报告是否发生过任何包装（无变化则返回原切片，零分配）。
func wrapLitsInRange(toks []Token, from, to int, b *Bindings, tbl signalFieldTable) ([]Token, bool) {
	out := make([]Token, 0, to-from)
	changed := false
	i := from
	for i < to {
		// 定位下一个具名字面量起点 i..cursor（非字面量 token 原样搬运）。
		lit, ok := namedLitAt(toks, i, to)
		if !ok {
			out = append(out, toks[i])
			i++
			continue
		}
		// 搬运 类型名 与 '{'（含两者之间的空白）。
		for k := i; k <= lit.open; k++ {
			out = append(out, toks[k])
		}
		// 递归处理 body，逐字段包装。
		if tbl.hasSignalFieldsOf(lit.typeName) {
			body, ch := wrapLitBody(toks, lit, b, tbl)
			out = append(out, body...)
			changed = changed || ch
		} else {
			// 本类型无 signal 字段：body 里仍可能有别的具名字面量，递归下探。
			body, ch := wrapLitsInRange(toks, lit.open+1, lit.close, b, tbl)
			out = append(out, body...)
			changed = changed || ch
		}
		// 搬运 '}'。
		out = append(out, toks[lit.close])
		i = lit.close + 1
	}
	return out, changed
}

// wrapLitBody 处理一个已知含 signal 字段的具名字面量的 body（open+1 .. close），
// 返回处理后的 token 切片。段间的 ','/空白 原样保留；每个字段段的值先递归处理
// 嵌套字面量，再做三档判定（见 wrapLitSeg）。
func wrapLitBody(toks []Token, lit compositeLitInfo, b *Bindings, tbl signalFieldTable) ([]Token, bool) {
	out := make([]Token, 0, lit.close-lit.open-1)
	changed := false
	cursor := lit.open + 1
	for _, seg := range splitTopLevelSegs(toks, lit.open+1, lit.close) {
		// 段间分隔（前一段结束到本段起点，含顶层 ',' 与空白）原样搬运。
		if seg[0] > cursor {
			out = append(out, toks[cursor:seg[0]]...)
		}
		segToks, ch := wrapLitSeg(toks, lit.typeName, seg, b, tbl)
		out = append(out, segToks...)
		changed = changed || ch
		cursor = seg[1]
	}
	// 末段到 '}' 之间的分隔 token 原样搬运。
	if cursor < lit.close {
		out = append(out, toks[cursor:lit.close]...)
	}
	return out, changed
}

// wrapLitSeg 处理单个字段段 [s,e)（`field : value`）：先递归处理 value 内部嵌套
// 字面量得到 newVal，再按三档生成载荷；非 signal 字段只递归下探、原样搬运。
func wrapLitSeg(toks []Token, typeName string, seg [2]int, b *Bindings, tbl signalFieldTable) ([]Token, bool) {
	s, e := seg[0], seg[1]
	// 段内找顶层 ':'（字段值可能是复合字面量，须按深度跳过内部 ':'）。
	colon := -1
	depth := 0
	for k := s; k < e; k++ {
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
		return wrapLitsInRange(toks, s, e, b, tbl)
	}
	nameIdx := nextSigIn(toks, s, colon)
	if nameIdx < 0 || toks[nameIdx].Kind != Ident || !tbl.has(typeName, toks[nameIdx].Text) {
		return wrapLitsInRange(toks, s, e, b, tbl) // 非 signal 字段
	}
	field := toks[nameIdx].Text
	p := toks[nameIdx].Pos

	valFrom := nextSigIn(toks, colon+1, e)
	if valFrom < 0 || valFrom >= e {
		return toks[s:e], false
	}
	newVal, _ := wrapLitsInRange(toks, valFrom, e, b, tbl)
	// 段前缀 `field :`（含空白）原样搬运；载荷基于 newVal 生成。
	out := make([]Token, 0, len(newVal)+4)
	out = append(out, toks[s:valFrom]...)

	// 形态判定基于 newVal（递归已把内部嵌套字面量就地替换），下标 0..len。
	if pass, ok := signalExprPassthrough(newVal, 0, len(newVal), b, tbl); ok {
		out = append(out, newVal[:pass.to]...)
		// P3-1：直通档只取 pass.to 之前的 getter，但 pass.to 之后到段尾之间可能
		// 还有尾随注释（如 `P{a: src /* note */}`）。pass.to 指向被剥掉的 '('
		// （调用式如 src()）或第一个 token 之后（裸 getter），其后只可能是
		// '(' ')' 调用括号 + 空白/换行/注释。这里只补回空白/换行/注释，
		// 跳过调用括号（补回会破坏剥 () 的保鲜语义），保住注释又不引入语法。
		for k := pass.to; k < len(newVal); k++ {
			kind := newVal[k].Kind
			if kind == Space || kind == Newline || kind == Comment {
				out = append(out, newVal[k])
			}
		}
		return out, true
	}
	vs, ve := 0, len(newVal)
	for vs < ve && (newVal[vs].Kind == Space || newVal[vs].Kind == Newline) {
		vs++
	}
	for ve > vs && (newVal[ve-1].Kind == Space || newVal[ve-1].Kind == Newline) {
		ve--
	}
	// 用户规则（第八轮）：signal 字段的值按来源分档——
	//   1) signal（变量/字段读）→ 上面透传档，传 getter 本身；
	//   2) 纯字面量（字符串/数字/bool/nil）→ Signal.New(lit)（常量不会变）；
	//   3) 含 signal 读的派生表达式 → Signal.Computed(func() T { return expr })，
	//      登记依赖，signal 变化时自动重算；
	//   4) 纯普通变量/普通字段 → 裸 getter func(...T) T { return expr }，每次读
	//      重新求值——否则 Signal.New(v) 快照后 v 再改就丢了，用户不应感知
	//      signal 与普通变量的差别。
	T := tbl.fieldType(typeName, field)
	switch {
	case tokensArePlainLiteral(newVal[vs:ve]):
		out = append(out, signalNewCall(newVal[vs:ve]...)...)
	case exprHasSignalRead(newVal, vs, ve, b, tbl, Pos{}):
		out = append(out, signalComputedCall(T, p, newVal[vs:ve])...)
	default:
		out = append(out, signalGetterCall(T, p, newVal[vs:ve])...)
	}
	return out, true
}

// tokensArePlainLiteral 判断 token 区间是否为纯字面量常量（数字/字符串/rune/bool/nil，
// 可带前置一元 '-'/'+'）。这类值不会变化，构造 signal 字段时用 Signal.New 即可；
// 其余（变量、字段 selector、函数调用）一律用 Computed 包成函数保鲜。
func tokensArePlainLiteral(toks []Token) bool {
	var s []Token
	for _, t := range toks {
		if t.Kind == Space || t.Kind == Newline || t.Kind == Comment || t.Kind == EOF {
			continue
		}
		s = append(s, t)
	}
	if len(s) == 0 {
		return false
	}
	i := 0
	if (s[0].Text == "-" || s[0].Text == "+") && len(s) >= 2 {
		i = 1
	}
	if len(s)-i != 1 {
		return false
	}
	t := s[i]
	switch t.Kind {
	case Number, String, Rune:
		return true
	case Ident:
		return t.Text == "true" || t.Text == "false" || t.Text == "nil"
	}
	return false
}

// namedLitAt 判断位置 i 是否是一个具名复合字面量 TypeName{ 的起点，
// 是则返回其 info（open/close 为 '{'/'}' 下标），否则 ok=false。
// to 限定扫描边界，避免跨出当前递归区间。
func namedLitAt(toks []Token, i, to int) (compositeLitInfo, bool) {
	if i < 0 || i >= to || toks[i].Kind != Ident || isGoKeyword(toks[i].Text) {
		return compositeLitInfo{}, false
	}
	if p := prevSig(toks, i-1); p >= 0 && toks[p].Text == "." {
		return compositeLitInfo{}, false // pkg.Type{} 跳过
	}
	open := nextSigIn(toks, i+1, to)
	if open < 0 || toks[open].Text != "{" {
		return compositeLitInfo{}, false
	}
	close := matchDelimSkipSpace(toks, open, "{", "}")
	if close <= open || close >= to {
		return compositeLitInfo{}, false
	}
	return compositeLitInfo{typeName: toks[i].Text, open: open, close: close}, true
}

// hasSignalFieldsOf 判断某 typeName 是否有 signal 字段。
func (t signalFieldTable) hasSignalFieldsOf(typeName string) bool {
	return len(t.sig[typeName]) > 0
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
		// For item getter u() 后跟普通字段 → 不是 signal 字段链，跳过。
		// （u 是 lambda 参数，resolveSignalChain 推不出类型本就不会命中；此处显式排除更稳。）
		if prev := prevSig(toks, i-1); prev >= 0 && toks[prev].Text == ")" {
			continue
		}
		chain, ok := resolveSignalChain(toks, i, pos, b, tbl, 0)
		if !ok || len(chain.segs) == 0 {
			continue
		}
		// 逐段输出链：链首 t 已在上面 append，这里从 '.'（i+1）开始，避免重复。
		// 对象字面量 key（field 后跟 ':'）与已是调用（field 后跟 '('）不补 ()。
		for k := i + 1; k <= chain.tailIdx; k++ {
			out = append(out, toks[k])
			isSig := false
			for _, s := range chain.segs {
				if s == k {
					isSig = true
					break
				}
			}
			if !isSig {
				continue
			}
			n := nextSig(toks, k+1)
			if n >= 0 && (toks[n].Text == "(" || toks[n].Text == ":") {
				continue
			}
			out = append(out, tokOp("(", toks[k].Pos), tokOp(")", toks[k].Pos))
		}
		i = chain.tailIdx
	}
	return out
}
