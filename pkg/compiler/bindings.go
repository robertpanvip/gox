package compiler

// bindings.go — 统一符号表 + 词法作用域 + 绑定解析（改造方案 P0）。
//
// 背景：同一份「名字 → 声明 / 作用域」知识曾在各 transform 里被重复实现 5+ 次，
// 坐标基准不一（token 下标 vs Pos）、规则不一，导致 sig 遮蔽（P1-1）、pub 遮蔽
// （M-5）、selector 误保护（L-7）一类「同名冲突」缺陷反复出现。
//
// 本文件把这份知识收敛成一套贯穿管线的绑定解析服务：
//   - 稳定身份用 Pos.Offset（lex 时确定、单调、不受 desugar 增删 token 影响）；
//   - 一次构建（Bind 在 lex 之后、任何 transform 之前调用一次），全程查询；
//   - ResolveAt(name, pos) 沿作用域链从内到外解析，天然处理遮蔽。
//
// P0 阶段：纯新增，不改动任何现有 transform；先用单元测试对拍解析正确性，
// 后续阶段（P1 sig / P2 pub / P3 类型查询 / P4 external）再逐个把消费者切到本 API。

import (
	"sort"
	"strings"
)

// BindingKind 是一个名字被声明的语义类别。
// 它取代各 transform 里零散的「这是不是 sig / 参数 / pub」判断。
type BindingKind uint8

const (
	BkParam      BindingKind = iota // 函数/方法形参
	BkReceiver                      // 方法接收者
	BkLocal                         // := 短变量
	BkVar                           // var 声明（局部或包级）
	BkConst                         // const 声明
	BkFunc                          // 函数名 / 方法名
	BkType                          // type 声明（含别名）
	BkField                         // struct 字段（含匿名 struct）
	BkEnumMember                    // enum 成员
	BkSignal                        // sig 声明 → 运行期 Signal
	BkComputed                      // Signal.computed 派生的只读 sig
	BkImport                        // import 包名 / 别名
	BkRangeVar                      // for x := range 的迭代变量
	BkLabel                         // goto / break / continue 标签
)

func (k BindingKind) String() string {
	switch k {
	case BkParam:
		return "param"
	case BkReceiver:
		return "receiver"
	case BkLocal:
		return "local"
	case BkVar:
		return "var"
	case BkConst:
		return "const"
	case BkFunc:
		return "func"
	case BkType:
		return "type"
	case BkField:
		return "field"
	case BkEnumMember:
		return "enum"
	case BkSignal:
		return "sig"
	case BkComputed:
		return "computed"
	case BkImport:
		return "import"
	case BkRangeVar:
		return "range"
	case BkLabel:
		return "label"
	}
	return "?"
}

// Binding 是一个声明的规范化表示。DeclPos 是它在原始源码中的稳定身份。
type Binding struct {
	ID      int    // 绑定表内唯一编号，便于生成唯一临时名
	Name    string // 声明名（保留源码大小写）
	Kind    BindingKind
	Type    string     // 类型文本（尽力推断，未知为 ""）
	DeclPos Pos        // 声明 token 的位置（稳定身份，Offset 单调）
	Scope   *BindScope // 所属作用域
	IsPub   bool       // pub/enum 阶段回填：导出名映射的镜像
	PubName string     // 导出后的名字（如 foo → Foo）

	// VisibleFrom 是绑定「真正开始可见」的位置（按 Offset 比较）。
	// 多数声明可见起点就是 DeclPos；但 Go 的语言规则是短变量声明/带初始化
	// 的 var 声明其作用域从「声明语句结束」开始，因此 `x := x + 1` 右侧的 x
	// 应引用外层绑定。这类声明会把 VisibleFrom 设到语句右端，ResolveAt 据此排除。
	VisibleFrom Pos
}

// setVisibleFrom 设置绑定的可见起点。nil 安全、NoPos 忽略。
func (bd *Binding) setVisibleFrom(pos Pos) {
	if bd != nil && pos.valid() {
		bd.VisibleFrom = pos
	}
}

// ScopeKind 区分作用域种类；selector 保护等规则依赖它（字段/方法只在 BkField/BkFunc 有效）。
type ScopeKind uint8

const (
	SkGlobal ScopeKind = iota // 包级
	SkFunc                    // 函数体（含接收者/参数）
	SkBlock                   // 裸 {} 块
	SkStruct                  // struct 定义体 / 匿名 struct（字段名空间）
	SkFor                     // for 语句（含 init/条件/post/body）
	SkIf                      // if 语句（含 init/条件/body/else 链）
	SkSwitch                  // switch 语句（含 init/tag/body）
	SkSelect                  // select 语句
)

// BindScope 是词法作用域树的一个节点。边界用 Pos.Offset 表示，不受下标漂移影响。
type BindScope struct {
	ID       int
	Kind     ScopeKind
	Parent   *BindScope
	Children []*BindScope
	From, To Pos                   // 覆盖的 token 区间（半开），按 Offset 比较
	Bindings map[string][]*Binding // 同名可多次声明（不同块），按 DeclPos.Offset 排序
}

// FuncSigRaw 是函数/方法声明的原始文本签名（供 B 类「调用点反查被调函数签名」）。
// 类型文本用 exprText 原样拼接、不展开别名，与 Analysis.FuncSig 的语义展开互补。
type FuncSigRaw struct {
	Name       string
	ParamTypes []string // 按参数位置；无名/共享类型以 "" 占位
	ResultType string   // 首个返回类型文本；"" 表示无返回值
	IsVoid     bool     // 无返回值（'(' 后直接 '{'）
}

// Bindings 是贯穿管线的绑定解析服务。一次构建，全局查询。
type Bindings struct {
	scopes       []*BindScope
	byPos        map[int]*Binding // DeclPos.Offset → Binding，声明点 O(1) 命中
	all          []*Binding
	root         *BindScope             // SkGlobal
	structScopes map[string]*BindScope  // type 名 → 对应 struct 的 SkStruct scope（供 SelAt）
	toks         []Token                // 原始 token 流（供 SelAt 反查接收者名）
	funcSigs     map[string]*FuncSigRaw // 具名函数的原始文本签名（供签名反查）
	// typeOverrides 是临时「标识符→类型名」覆盖表，供 lambda 形参（典型：<For>
	// 的 item 参数）在 JSX 场景转换期间使用——这类参数不在 Bindings 作用域表里，
	// 但 sig 字段链解析（recvTypeAt）需要知道其 receiver 类型才能补 ()。
	// 由 WithTypeOverride 成对设置/恢复，支持嵌套。
	typeOverrides map[string]string
}

// WithTypeOverride 临时把 name 的类型设为 typ，返回恢复闭包。
// 用于 For item 参数等 lambda 形参：它们的类型由 forItemTypes 推断得到，
// 不在 Bindings 作用域表中，但 sig 字段链解析需要按 receiver 类型查表。
// 恢复闭包必须在使用结束后调用，以支持嵌套 For 的同名参数。
func (b *Bindings) WithTypeOverride(name, typ string) func() {
	if b == nil || name == "" || typ == "" {
		return func() {}
	}
	if b.typeOverrides == nil {
		b.typeOverrides = map[string]string{}
	}
	old, had := b.typeOverrides[name]
	b.typeOverrides[name] = typ
	return func() {
		if had {
			b.typeOverrides[name] = old
		} else {
			delete(b.typeOverrides, name)
		}
	}
}

// ---------- 构建（Binder）----------

// Bind 在 lex 之后、任何 transform 之前对原始 token 流做一次线性扫描，构建绑定表。
// 传入的 toks 必须是未被 desugar 改写的原始流（Pos.Offset 与扫描下标单调对应）。
func Bind(toks []Token) *Bindings {
	b := &Bindings{
		byPos:        map[int]*Binding{},
		structScopes: map[string]*BindScope{},
		funcSigs:     map[string]*FuncSigRaw{},
		toks:         toks,
	}
	root := b.newScope(SkGlobal, nil, Pos{Offset: 0, Line: 1, Col: 1}, maxTokenEnd(toks))
	b.root = root
	// 预扫函数签名：让 `p := makeP()` 这类工厂调用在建绑定时就能用 FuncResult
	// 回填左值类型（第十轮 P1-3）。单遍扫描时若 makeP 定义在 main 之后会拿不到，
	// 故先独立走一遍只收 funcSigs（collectFuncSig 有去重保护，主扫描再填不冲突）。
	b.prescanFuncSigs(toks)
	b.scan(toks, 0, len(toks), root)

	// 同名声明按声明位置排序，ResolveAt 取最近一个。
	for _, s := range b.scopes {
		for name := range s.Bindings {
			list := s.Bindings[name]
			sort.SliceStable(list, func(i, j int) bool {
				return list[i].DeclPos.Offset < list[j].DeclPos.Offset
			})
		}
	}
	return b
}

// maxTokenEnd 返回覆盖整个文件的上界 Offset（半开区间的 To）。
func maxTokenEnd(toks []Token) Pos {
	max := 0
	for _, t := range toks {
		if t.Pos.Offset > max {
			max = t.Pos.Offset
		}
	}
	return Pos{Offset: max + 1, Line: 1 << 30, Col: 1 << 30}
}

func (b *Bindings) newScope(kind ScopeKind, parent *BindScope, from, to Pos) *BindScope {
	s := &BindScope{
		ID:       len(b.scopes),
		Kind:     kind,
		Parent:   parent,
		From:     from,
		To:       to,
		Bindings: map[string][]*Binding{},
	}
	b.scopes = append(b.scopes, s)
	if parent != nil {
		parent.Children = append(parent.Children, s)
	}
	return s
}

// addBinding 在 scope 中登记一个声明。同一 Pos 已有绑定时升级为更具体的 Kind
// （如 `:= Signal.computed(...)` 先按 BkLocal 收集，再升级为 BkComputed）。
func (b *Bindings) addBinding(t Token, kind BindingKind, scope *BindScope, typ string) *Binding {
	if t.Kind != Ident || t.Text == "_" || isGoKeyword(t.Text) {
		return nil
	}
	off := t.Pos.Offset
	if ex, ok := b.byPos[off]; ok {
		if kind > ex.Kind {
			ex.Kind = kind
		}
		if typ != "" && ex.Type == "" {
			ex.Type = typ
		}
		return ex
	}
	bd := &Binding{
		ID:          len(b.all),
		Name:        t.Text,
		Kind:        kind,
		Type:        typ,
		DeclPos:     t.Pos,
		VisibleFrom: t.Pos, // 默认：声明即可见；:= 场景由调用方改写为语句结束处
		Scope:       scope,
	}
	scope.Bindings[t.Text] = append(scope.Bindings[t.Text], bd)
	b.byPos[off] = bd
	b.all = append(b.all, bd)
	return bd
}

// scan 线性扫描 [start,end) 区间内的 token，在 scope 中登记声明、按需创建子作用域。
func (b *Bindings) scan(toks []Token, start, end int, scope *BindScope) {
	for i := start; i < end; i++ {
		t := toks[i]
		switch {
		case t.is("func"):
			i = b.scanFunc(toks, i, end, scope)
		case t.is("type"):
			i = b.scanType(toks, i, end, scope)
		case t.is("sig"):
			if j := nextSig(toks, i+1); j >= 0 && j < end && toks[j].Kind == Ident && !isGoKeyword(toks[j].Text) {
				b.addBinding(toks[j], BkSignal, scope, "")
			}
		case t.is("var"), t.is("const"):
			i = b.scanVarConst(toks, i, end, scope)
		case t.is("import"):
			i = b.scanImport(toks, i, end, scope)
		case t.is("enum"):
			b.scanEnum(toks, i, end, scope)
		case t.Text == "computed" || t.Text == "Computed":
			if name := computedAssignTarget(toks, i); name != "" {
				// computedAssignTarget 返回名字文本；定位其 token 以升级 Kind。
				if idx := bindingIdxBefore(toks, i, name); idx >= 0 {
					b.addBinding(toks[idx], BkComputed, scope, "")
				}
			}
		case t.Text == ":=":
			b.bindLHS(toks, i, scope, BkLocal)
		case t.Text == "range":
			// for k, v := range m：把刚收集的左侧升级为 BkRangeVar。
			b.upgradeRangeVars(toks, i, scope)
		case t.is("struct"):
			// 匿名 struct {...}：建字段名空间。
			if br := nextSig(toks, i+1); br >= 0 && br < end && toks[br].Text == "{" {
				close := matchDelim(toks, br, "{", "}")
				if close > br && close <= end {
					fs := b.newScope(SkStruct, scope, toks[br].Pos, toks[close].Pos)
					for _, idx := range structFieldIdxs(toks, br, close) {
						b.addBinding(toks[idx], BkField, fs, "")
					}
					i = close
				}
			}
		case t.is("for"), t.is("if"), t.is("switch"), t.is("select"):
			// 控制语句：init 变量（for i := 0 / if x := f()）的作用域仅限本语句，
			// 不得泄漏到外层。为整条语句建独立作用域 [关键字, body右括号)，
			// 在其中扫描 init + 条件 + body，返回 body 右括号让外层跳过。
			i = b.scanControl(toks, i, end, scope)
		case t.Text == "{":
			// 普通块：建 SkBlock 并递归扫描其内容，跳过整个块。
			close := matchDelim(toks, i, "{", "}")
			if close > i && close <= end {
				blk := b.newScope(SkBlock, scope, toks[i].Pos, toks[close].Pos)
				b.scan(toks, i+1, close, blk)
				i = close
			}
		}
	}
}

// scanControl 扫描一条 for/if/switch/select 语句，为其建立独立词法作用域。
// 作用域覆盖从关键字 token 到 body 右括号（含 init / 条件 / post / body / else 链），
// 使 `for i := 0` / `if x := f()` 声明的变量在语句外不可见（P2-1）。
// 返回 body 右括号下标（若有 else 链则到链尾），供外层 scan 跳过整条语句。
func (b *Bindings) scanControl(toks []Token, ci, end int, parent *BindScope) int {
	body := controlBodyBrace(toks, ci+1, end)
	if body < 0 || body >= end {
		return ci // 定位失败：退化为普通扫描，至少 init 仍登记在 parent（旧行为）
	}
	close := matchDelim(toks, body, "{", "}")
	if close <= body || close > end {
		return ci
	}

	var kind ScopeKind
	switch {
	case toks[ci].is("for"):
		kind = SkFor
	case toks[ci].is("if"):
		kind = SkIf
	case toks[ci].is("switch"):
		kind = SkSwitch
	default:
		kind = SkSelect
	}
	// 作用域从关键字覆盖到 body 右括号；else 链稍后并入同一作用域。
	last := close
	cs := b.newScope(kind, parent, toks[ci].Pos, toks[close].Pos)

	// init + 条件 + post（关键字之后到 body 之前）：在 cs 中扫描，使 := 落入 cs。
	b.scan(toks, ci+1, body, cs)
	// body：其内部第一个 '{' 会由普通块规则再建子 SkBlock；初值变量经父链 cs 可见。
	b.scan(toks, body, close+1, cs)

	// else / else if：并入同一 if 作用域，共享 init 变量。
	if kind == SkIf {
		last = b.scanElseChain(toks, close, end, cs)
		cs.To = toks[last].Pos
	}
	return last
}

// scanElseChain 扫描 if 的 else/else-if 链，全部并入 scope（与 if init 共享作用域）。
// 返回链尾 token 下标（最后一个右括号）。
func (b *Bindings) scanElseChain(toks []Token, thenClose, end int, scope *BindScope) int {
	last := thenClose
	for {
		j := nextSig(toks, last+1)
		if j < 0 || j >= end || !toks[j].is("else") {
			break
		}
		k := nextSig(toks, j+1)
		if k < 0 || k >= end {
			break
		}
		if toks[k].is("if") {
			// else if：作为新的 if 语句扫描，但 parent 用当前 scope 以共享 init。
			last = b.scanControl(toks, k, end, scope)
		} else if toks[k].Text == "{" {
			close := matchDelim(toks, k, "{", "}")
			if close > k && close <= end {
				b.scan(toks, k, close+1, scope) // else body 的 '{' 再建子块
				last = close
			} else {
				break
			}
		} else {
			break
		}
	}
	return last
}

// controlBodyBrace 定位控制语句的 body '{'：从 start 起找第一个处于括号/方括号/
// 花括号嵌套深度为 0 的 '{'。Go 规定 header 里的复合字面量（[]T{...}）必须
// 被括号包裹，因此顶层的第一个 '{' 必是 body，不会被 `range []int{1,2}` 误伤。
func controlBodyBrace(toks []Token, start, end int) int {
	depth := 0
	for i := start; i < end; i++ {
		switch toks[i].Text {
		case "{":
			if depth != 0 {
				depth++ // 嵌套在括号/复合字面量里的 '{'
				continue
			}
			// depth==0 的 '{' 候选：可能是 body，也可能是 header 里无括号的
			// 复合字面量（如 `for _, v := range []int{1,2} {`）。Go 允许 range
			// 表达式里的复合字面量不加括号，此时其配对 '}' 后紧跟 body 的 '{'。
			// 用「配对后是否紧跟另一个 '{'」区分二者。
			if close := matchDelim(toks, i, "{", "}"); close > i && close < end {
				if nxt := nextSig(toks, close+1); nxt > 0 && nxt < end && toks[nxt].Text == "{" {
					i = close // 当前是复合字面量，跳到其后继续找 body
					continue
				}
			}
			return i
		case "(":
			depth++
		case "[":
			depth++
		case ")", "]":
			if depth > 0 {
				depth--
			}
		case "}":
			if depth > 0 {
				depth--
			}
		}
	}
	return -1
}

// scanFunc 处理一个 func 声明/字面量：建 SkFunc 作用域，登记接收者、参数、函数名，
// 递归扫描函数体（函数体本身不再额外建 SkBlock）。返回函数体右花括号下标。
func (b *Bindings) scanFunc(toks []Token, fi, end int, parent *BindScope) int {
	open, close, ok := funcParamParens(toks, fi)
	if !ok {
		return fi
	}
	brace := findFuncBodyBrace(toks, close)
	if brace < 0 || brace >= end {
		return fi // 函数类型 / 接口方法签名，无函数体
	}
	bodyEnd := matchDelim(toks, brace, "{", "}")
	if bodyEnd <= brace {
		return fi
	}
	// 作用域从参数 '(' 开始覆盖到函数体 '}'：参数声明 token 在 '{' 之前，
	// 必须从 '(' 起才能落在 SkFunc 内，ResolveAt 才会命中参数而非外层 sig。
	fn := b.newScope(SkFunc, parent, toks[open].Pos, toks[bodyEnd].Pos)

	// 接收者（方法）：func (p point) m(...) — funcParamParens 的第一个 ( 是接收者。
	if recOpen, recClose, isMethod := funcReceiverParens(toks, fi); isMethod {
		for _, pb := range parseParamBindings(toks, recOpen, recClose) {
			b.addBinding(toks[pb.nameIdx], BkReceiver, fn, paramsTypeText(toks, recOpen, recClose, pb))
		}
	}
	// 参数
	for _, pb := range parseParamBindings(toks, open, close) {
		b.addBinding(toks[pb.nameIdx], BkParam, fn, paramsTypeText(toks, open, close, pb))
	}
	// 函数名 / 方法名：绑定到外层作用域
	if ni := funcNameAt(toks, fi); ni >= 0 {
		b.addBinding(toks[ni], BkFunc, parent, "")
		// B 类：收集原始文本签名（保留别名，供调用点反查参数/返回类型）。
		b.collectFuncSig(toks, fi, ni, open, close, brace)
	}
	b.scan(toks, brace+1, bodyEnd, fn)
	return bodyEnd
}

// calledFuncResult 识别 [from,to) 是否以 `ident(...)` 函数调用开头，返回其首个返回类型。
// 用于 `p := makeP()` 的工厂返回类型回填（第十轮 P1-3）。指针返回剥 '*'。
// 保守：只认表达式起始就是函数调用；多值/方法链/运算表达式不认，交回调用方降级。
func calledFuncResult(toks []Token, from, to int, b *Bindings) string {
	if b == nil || from < 0 || from >= to || toks[from].Kind != Ident || isGoKeyword(toks[from].Text) {
		return ""
	}
	lp := nextSigIn(toks, from+1, to)
	if lp < 0 || toks[lp].Text != "(" {
		return ""
	}
	if rp := matchDelim(toks, lp, "(", ")"); rp < 0 || rp >= to {
		return ""
	}
	rt := b.FuncResult(toks[from].Text)
	if rt == "" {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(rt, "*"))
}

// prescanFuncSigs 在建绑定前独立走一遍，只收集具名函数的文本签名到 funcSigs。
// 目的：`p := makeP()` 的 bindLHS 在建绑定时即可用 FuncResult("makeP") 回填 p 的
// 类型，无需 makeP 定义在调用点之前（第十轮 P1-3）。不建任何 scope/binding。
// collectFuncSig 对同名声明有去重保护，主 scan 阶段重复收集不会覆盖。
func (b *Bindings) prescanFuncSigs(toks []Token) {
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("func") {
			continue
		}
		open, close, ok := funcParamParens(toks, i)
		if !ok {
			continue
		}
		brace := findFuncBodyBrace(toks, close)
		if brace < 0 {
			continue // 函数类型 / 接口方法签名，无函数体
		}
		ni := funcNameAt(toks, i)
		if ni < 0 {
			continue
		}
		b.collectFuncSig(toks, i, ni, open, close, brace)
	}
}

// collectFuncSig 为一个具名函数记录原始文本签名。
// 参数类型按位置存（共享类型 / 无名参数留空），返回类型取首个。
func (b *Bindings) collectFuncSig(toks []Token, fi, nameIdx, open, close, brace int) {
	sig := &FuncSigRaw{Name: toks[nameIdx].Text}
	pbs := parseParamBindings(toks, open, close)
	// 参数位置数 = 顶层逗号分段数；用 parseParamBindings 的名字顺序近似。
	// 无名参数场景 parseParamBindings 也可能给出 typeStart，故按段数补齐。
	segs := splitTopLevelSegs(toks, open+1, close)
	for range segs {
		sig.ParamTypes = append(sig.ParamTypes, "")
	}
	for _, pb := range pbs {
		if pb.typeStart >= 0 && pb.typeStart < pb.typeEnd {
			t := exprText(toks, pb.typeStart, pb.typeEnd)
			// 按参数名在段中的位置填回（共享类型会被多个 pb 指向同一类型区间）。
			for si, seg := range segs {
				f := nextSigIn(toks, seg[0], seg[1])
				if f == pb.nameIdx && si < len(sig.ParamTypes) {
					sig.ParamTypes[si] = t
				}
			}
		}
	}
	// 返回类型：')' 之后到 '{' 之间。
	k := nextSig(toks, close+1)
	if k < 0 || k >= brace || toks[k].Text == "{" {
		sig.IsVoid = true
	} else {
		te := typeRangeEnd(toks, k)
		if te > brace {
			te = brace
		}
		if te > k {
			sig.ResultType = exprText(toks, k, te)
		} else {
			sig.IsVoid = true
		}
	}
	// 同名函数取首个声明（与 FuncSig 一致）。
	if _, exists := b.funcSigs[sig.Name]; !exists {
		b.funcSigs[sig.Name] = sig
	}
}

// funcReceiverParens 判定 func 声明是否带接收者；是则返回接收者括号。
func funcReceiverParens(toks []Token, fi int) (open, close int, ok bool) {
	j := nextSig(toks, fi+1)
	if j < 0 || toks[j].Text != "(" {
		return 0, 0, false
	}
	rc := matchDelim(toks, j, "(", ")")
	if rc <= j {
		return 0, 0, false
	}
	// 接收者括号之后应有方法名，再之后才是参数括号（否则 j 就是匿名函数的参数）。
	after := nextSig(toks, rc+1)
	if after < 0 || toks[after].Kind != Ident || isGoKeyword(toks[after].Text) {
		return 0, 0, false
	}
	nxt := nextSig(toks, after+1)
	if nxt < 0 || toks[nxt].Text != "(" {
		return 0, 0, false
	}
	return j, rc, true
}

// paramsTypeText 提取参数的类型文本（[typeStart,typeEnd)）。
// 与 exprText 一致：token 原样拼接、不插入空格，保证 "[]int"/"map[string]int"
// 这类类型文本与 structs 表 / fieldsFromStructText 的键格式一致。
func paramsTypeText(toks []Token, open, close int, pb paramBinding) string {
	if pb.typeStart < 0 || pb.typeStart >= pb.typeEnd {
		return ""
	}
	return exprText(toks, pb.typeStart, pb.typeEnd)
}

// scanType 处理 type 声明：命名类型 + struct 字段空间 + 分组 type (...)。
func (b *Bindings) scanType(toks []Token, ti, end int, scope *BindScope) int {
	j := nextSig(toks, ti+1)
	if j < 0 || j >= end {
		return ti
	}
	if toks[j].Text == "(" {
		close := matchDelim(toks, j, "(", ")")
		if close > j && close <= end {
			lineStart := true
			for k := j + 1; k < close; k++ {
				t := toks[k]
				if t.Kind == Newline {
					lineStart = true
					continue
				}
				if t.Kind == Space || t.Kind == Comment {
					continue
				}
				if lineStart && t.Kind == Ident && !isGoKeyword(t.Text) {
					b.addBinding(t, BkType, scope, "")
					lineStart = false
				}
			}
			return close
		}
		return ti
	}
	if toks[j].Kind != Ident || isGoKeyword(toks[j].Text) {
		return ti
	}
	b.addBinding(toks[j], BkType, scope, "")
	// type Name struct { fields }
	st := nextSig(toks, j+1)
	if st >= 0 && st < end && toks[st].is("struct") {
		br := nextSig(toks, st+1)
		if br >= 0 && br < end && toks[br].Text == "{" {
			close := matchDelim(toks, br, "{", "}")
			if close > br && close <= end {
				fs := b.newScope(SkStruct, scope, toks[br].Pos, toks[close].Pos)
				for _, idx := range structFieldIdxs(toks, br, close) {
					b.addBinding(toks[idx], BkField, fs, "")
				}
				b.structScopes[toks[j].Text] = fs
				return close
			}
		}
	}
	return ti
}

// declTypeAfterName 在名字下标之后提取声明的类型文本：
//   - `var name TYPE [= expr]`：TYPE（到 `=` / 行尾）；
//   - `name := expr`：从右侧字面量推断（复用 inferExprLitType）。
//
// 返回 "" 表示无法推断。仅在 Binder 阶段（原始 token 流）调用。
func declTypeAfterName(toks []Token, nameIdx, limit int) string {
	n := nextSig(toks, nameIdx+1)
	if n < 0 || n >= limit {
		return ""
	}
	switch toks[n].Text {
	case "=":
		return "" // var name = expr：无显式类型
	case ":=":
		exprFrom := nextSig(toks, n+1)
		if exprFrom < 0 || exprFrom >= limit {
			return ""
		}
		exprTo := exprEndInStmt(toks, exprFrom, limit)
		return inferExprLitType(toks, exprFrom, exprTo)
	default:
		// var name TYPE：类型起点是 n，到 `=` 或语句结束。
		te := typeRangeEnd(toks, n)
		if te > limit {
			te = limit
		}
		// 停在 '=' 之前。
		for k := n; k < te; k++ {
			if toks[k].Text == "=" {
				te = k
				break
			}
		}
		if te <= n {
			return ""
		}
		return exprText(toks, n, te)
	}
}

// scanVarConst 处理 var/const 声明（单条或分组）。
func (b *Bindings) scanVarConst(toks []Token, vi, end int, scope *BindScope) int {
	kind := BkVar
	if toks[vi].is("const") {
		kind = BkConst
	}
	j := nextSig(toks, vi+1)
	if j < 0 || j >= end {
		return vi
	}
	if toks[j].Text == "(" {
		close := matchDelim(toks, j, "(", ")")
		if close > j && close <= end {
			lineStart := true
			for k := j + 1; k < close; k++ {
				t := toks[k]
				if t.Kind == Newline {
					lineStart = true
					continue
				}
				if t.Kind == Space || t.Kind == Comment {
					continue
				}
				if lineStart && t.Kind == Ident && !isGoKeyword(t.Text) {
					b.addBinding(t, kind, scope, declTypeAfterName(toks, k, close))
					lineStart = false
				}
			}
			return close
		}
		return vi
	}
	// 单条：var name [type] [= expr]，含 `var a, b int` 多名字。
	for k := j; k < end; {
		if toks[k].Kind != Ident || isGoKeyword(toks[k].Text) {
			break
		}
		b.addBinding(toks[k], kind, scope, declTypeAfterName(toks, k, end))
		n := nextSig(toks, k+1)
		if n >= 0 && n < end && toks[n].Text == "," {
			k = nextSig(toks, n+1)
			if k < 0 || k >= end {
				break
			}
			continue
		}
		break
	}
	return vi
}

// scanImport 登记 import 包名 / 别名（包名 token 定位到路径字符串，供 external 查询）。
func (b *Bindings) scanImport(toks []Token, ii, end int, scope *BindScope) int {
	for _, sp := range collectImportSpecs(toks[ii:end]) {
		if sp.pkg == "_" || sp.pkg == "." {
			continue
		}
		// 找到该 spec 的路径字符串 token 作为稳定身份锚点。
		quoted := sp.path
		for k := ii; k < end; k++ {
			if toks[k].Kind == String {
				lit := stringsTrim(toks[k].Text, "\"`")
				if lit == quoted {
					b.addBinding(tokIdent(sp.pkg, toks[k].Pos), BkImport, scope, "")
					break
				}
			}
		}
	}
	return ii
}

// scanEnum 登记 enum 成员名（GX 语法：enum Color { red, green }）。
func (b *Bindings) scanEnum(toks []Token, ei, end int, scope *BindScope) {
	br := nextSig(toks, ei+1)
	for br >= 0 && br < end && toks[br].Text != "{" && toks[br].Kind != Newline {
		br = nextSig(toks, br+1)
	}
	if br < 0 || br >= end || toks[br].Text != "{" {
		return
	}
	close := matchDelim(toks, br, "{", "}")
	if close <= br || close > end {
		return
	}
	expect := true
	for k := br + 1; k < close; k++ {
		t := toks[k]
		switch {
		case t.Kind == Newline || t.Text == "," || t.Text == ";":
			expect = true
		case t.Kind == Space || t.Kind == Comment:
			continue
		case expect && t.Kind == Ident && !isGoKeyword(t.Text):
			b.addBinding(t, BkEnumMember, scope, "")
			expect = false
		}
	}
}

// bindLHS 收集 `:=` 左侧的标识符（可能多个：`a, b := ...`）。
// 类型从 `:=` 右侧首个字面量推断（多值场景与 inferIdentFromDecl 一致，取首个）。
//
// 可见性：Go 规定短变量声明的作用域从声明语句结束处开始，故 `x := x + 1` 右侧
// 的 x 应引用外层绑定。这里把新绑定的 VisibleFrom 设到右侧表达式最后一个 token；
// `range` 迭代变量例外——它们在循环体里立即生效，保持默认 DeclPos 即可。
func (b *Bindings) bindLHS(toks []Token, op int, scope *BindScope, kind BindingKind) {
	exprFrom := nextSig(toks, op+1)
	exprTo := -1
	typ := ""
	visibleFrom := NoPos
	isRange := exprFrom >= 0 && toks[exprFrom].Text == "range"
	if exprFrom >= 0 {
		exprTo = exprEndInStmt(toks, exprFrom, len(toks))
		typ = inferExprLitType(toks, exprFrom, exprTo)
		// 工厂/构造函数返回：`p := makeP("x")` 的字面量扫描会误命中参数里的字符串，
		// 推断成 string；函数调用形态的类型应以 FuncResult 为准（第十轮 P1-3），
		// 优先级高于字面量扫描。指针返回剥 '*'，与 signalfields 调用链类型传播一致。
		if !isRange {
			if ft := calledFuncResult(toks, exprFrom, exprTo, b); ft != "" {
				typ = ft
			}
		}
		if !isRange {
			if exprTo > exprFrom && exprTo <= len(toks) {
				visibleFrom = toks[exprTo-1].Pos // 表达式最后一个 token 处生效
			} else if exprFrom < len(toks) {
				visibleFrom = toks[exprFrom].Pos
			}
		}
	}
	// 第十二轮 P2-C：多左值时按位置分别推断，不再把首个结果套给所有左值。
	// 先按源码从左到右收集左侧标识符下标。
	var lhsIdx []int
	for p := prevSig(toks, op-1); p >= 0; {
		if toks[p].Kind == Ident && !isGoKeyword(toks[p].Text) {
			lhsIdx = append(lhsIdx, p)
		}
		q := prevSig(toks, p-1)
		if q >= 0 && toks[q].Text == "," {
			p = prevSig(toks, q-1)
			continue
		}
		break
	}
	// lhsIdx 是逆序（从右往左），翻转为源码顺序。
	for i, j := 0, len(lhsIdx)-1; i < j; i, j = i+1, j-1 {
		lhsIdx[i], lhsIdx[j] = lhsIdx[j], lhsIdx[i]
	}
	// 逐位置推断类型（仅多左值且非 range 时启用；单左值沿用上面的 typ）。
	types := make([]string, len(lhsIdx))
	for i := range types {
		types[i] = typ
	}
	if len(lhsIdx) > 1 && exprFrom >= 0 && !isRange {
		if inferred := inferMultiLHSTypes(toks, exprFrom, len(toks), b, len(lhsIdx)); inferred != nil {
			types = inferred
		}
	}
	for i, p := range lhsIdx {
		bd := b.addBinding(toks[p], kind, scope, types[i])
		bd.setVisibleFrom(visibleFrom)
	}
}

// indexElemTypeAt 推断 toks[from:to] 是否为单索引表达式 `x[...]`，是则返回其
// 元素类型（第十三轮 P2-C，供 comma-ok `v, ok := m[k]` 的第一左值）。
// 只认「标识符 + 单个 [...] + 纯空白尾巴」的窄形态；x[i].f / x[i][j] /
// 函数调用等一律返回空，避免误伤其它多左值场景。元素类型优先取 Binding.Type，
// 回退到声明 token 反推；二者都推不出则沉默。
func indexElemTypeAt(toks []Token, from, to int, b *Bindings) string {
	// 收紧到语句尾，避免传入 len(toks) 时把后续的 `}` / EOF 当作索引尾巴。
	to = exprEndOfLine(toks, from, to)
	i := from
	for i < to && toks[i].Kind == Space {
		i++
	}
	if i >= to || toks[i].Kind != Ident || isGoKeyword(toks[i].Text) {
		return ""
	}
	lb := nextSigIn(toks, i+1, to)
	if lb < 0 || toks[lb].Text != "[" {
		return ""
	}
	rb := matchDelim(toks, lb, "[", "]")
	if rb < 0 || rb >= to {
		return ""
	}
	// 索引闭合后到语句尾只能是空白/换行；有 '.' 或后续 '[' 说明不是单索引取值。
	for k := rb + 1; k < to; k++ {
		if toks[k].Kind != Space && toks[k].Kind != Newline {
			return ""
		}
	}
	name, pos := toks[i].Text, toks[i].Pos
	if b != nil {
		bd := b.ResolveAt(name, pos)
		if bd != nil && bd.Type != "" {
			if et := containerElemTypeOnce(bd.Type); et != "" {
				return et
			}
		}
	}
	return containerElemTypeFromDecl(toks, i, pos)
}

// inferMultiLHSTypes 为多左值 `a, b := <rhs>` 按位置推断类型（第十二轮 P2-C）。
// 覆盖三种形态：
//   - 单函数调用返回元组：`m, err := load()` → FuncResult 得 "(map[string]P, error)"，
//     拆元组按位置分配 → ["map[string]P", "error"]；
//   - 逗号分隔多表达式：`a, n := map[K]V{...}, 1` → 每段独立推断；
//   - comma-ok 索引取值：`v, ok := m[k]` → 第一左值取元素类型、第二为 bool
//     （第十三轮 P2-C，避免 v 只拿到 key 类型导致 v.a 打印 Signal 函数地址）。
//
// 无法可靠分解时返回 nil，调用方沿用「首个结果套全部」的旧行为（保守）。
func inferMultiLHSTypes(toks []Token, from, to int, b *Bindings, n int) []string {
	if from < 0 || from >= to || n < 2 {
		return nil
	}
	// 收紧到语句结尾（只认换行/分号，不在顶层逗号处截断）——
	// 调用方传入的边界可能是 exprEndInStmt 的结果（在第一个逗号处停），
	// 对 a, n := e1, e2 这种形态必须先扩回完整右侧才能分段。
	to = exprEndOfLine(toks, from, to)
	// 形态三（第十三轮 P2-C）：comma-ok 索引取值 `v, ok := m[k]` —— 第一左值
	// 取容器元素类型，第二左值 bool。必须放在形态二前：单索引表达式不含顶层
	// 逗号，形态二分段数为 1 ≠ n=2 不会命中；但显式前置更清晰、更稳。
	if n == 2 {
		if et := indexElemTypeAt(toks, from, to, b); et != "" {
			return []string{et, "bool"}
		}
	}
	// 形态一：单函数调用返回元组。
	if toks[from].Kind == Ident && !isGoKeyword(toks[from].Text) {
		lp := nextSigIn(toks, from+1, to)
		if lp >= 0 && toks[lp].Text == "(" {
			if rp := matchDelim(toks, lp, "(", ")"); rp >= 0 && funcCallEndsExpr(toks, rp+1, to) {
				if rt := b.FuncResult(toks[from].Text); strings.HasPrefix(strings.TrimSpace(rt), "(") {
					parts := splitTypeTuple(rt)
					if len(parts) == n {
						return parts
					}
				}
			}
		}
	}
	// 形态二：逗号分隔多表达式，段数须与左值数一致。
	segs := splitTopLevelSegs(toks, from, to)
	if len(segs) == n {
		out := make([]string, n)
		for i, seg := range segs {
			out[i] = inferExprLitType(toks, seg[0], seg[1])
			if out[i] == "" {
				if ft := calledFuncResult(toks, seg[0], seg[1], b); ft != "" {
					out[i] = ft
				}
			}
		}
		return out
	}
	return nil
}

// exprEndOfLine 返回 [from,limit) 内到换行或分号为止的下标，不在顶层逗号截断。
// 供多左值 := 推断使用——右侧 a, n := e1, e2 的逗号是表达式分隔符而非语句结尾。
func exprEndOfLine(toks []Token, from, limit int) int {
	depth := 0
	for i := from; i < limit; i++ {
		t := toks[i]
		switch t.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth > 0 {
				depth--
			}
		case ";":
			if depth == 0 {
				return i
			}
		}
		if t.Kind == Newline && depth == 0 {
			return i
		}
	}
	return limit
}

// funcCallEndsExpr 判断 rp+1..to 是否只有空白/换行（即调用是完整右侧表达式）。
func funcCallEndsExpr(toks []Token, from, to int) bool {
	for i := from; i < to; i++ {
		switch toks[i].Kind {
		case Space, Newline, Comment:
			continue
		default:
			return false
		}
	}
	return true
}

// splitTypeTuple 把 "(T1, T2, ...)" 类型元组文本拆成各元素类型。
// 用 lexer 重新分词 + splitTopLevelSegs，保留嵌套类型（map[K]V / []T / func(...)）。
func splitTypeTuple(text string) []string {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "(") || !strings.HasSuffix(t, ")") {
		return nil
	}
	inner := t[1 : len(t)-1]
	toks := New(inner).AllTokens()
	segs := splitTopLevelSegs(toks, 0, len(toks))
	out := make([]string, 0, len(segs))
	for _, seg := range segs {
		s := exprText(toks, seg[0], seg[1])
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// upgradeRangeVars 把 range 之前的 `:=` 左侧从 BkLocal 升级为 BkRangeVar。
func (b *Bindings) upgradeRangeVars(toks []Token, ri int, scope *BindScope) {
	// 向前找最近的 :=（应在同一 for 语句内、未跨行到别的语句）。
	op := -1
	for j := ri - 1; j >= 0; j-- {
		t := toks[j]
		if t.Kind == Space || t.Kind == Comment {
			continue
		}
		if t.Text == ":=" {
			op = j
		}
		if t.Kind == Newline || t.Text == ";" || t.Text == "{" {
			break
		}
	}
	if op < 0 {
		return
	}
	// range xs：xs 的类型 → 元素类型（[]T/数组/string→rune）。
	elemType := ""
	xsIdx := nextSig(toks, ri+1)
	if xsIdx >= 0 && toks[xsIdx].Kind == Ident {
		if xsBD := b.ResolveAt(toks[xsIdx].Text, toks[xsIdx].Pos); xsBD != nil {
			elemType = sliceElemType(xsBD.Type)
		}
	}
	p := prevSig(toks, op-1)
	for p >= 0 {
		if bd, ok := b.byPos[toks[p].Pos.Offset]; ok && bd.Kind == BkLocal {
			bd.Kind = BkRangeVar
			// 与旧 inferIdentFromRange 对齐：range 左侧变量统一填元素类型，
			// 不区分 k/v（旧逻辑对 k、v 都返回 sliceElemType）。保持迁移零行为差异。
			if elemType != "" {
				bd.Type = elemType
			}
		}
		q := prevSig(toks, p-1)
		if q >= 0 && toks[q].Text == "," {
			p = prevSig(toks, q-1)
			continue
		}
		break
	}
}

// bindingIdxBefore 从 idx 向左找名字为 name 的标识符 token 下标（用于 computed 目标升级）。
func bindingIdxBefore(toks []Token, idx int, name string) int {
	for j := idx - 1; j >= 0; j-- {
		t := toks[j]
		if t.Kind == Space || t.Kind == Comment {
			continue
		}
		if t.Kind == Newline || t.Text == ";" || t.Text == "{" {
			return -1
		}
		if t.Kind == Ident && t.Text == name {
			return j
		}
	}
	return -1
}

// structFieldIdxs 收集 struct {...} 顶层字段名下标（支持 `a, b int`，跳过嵌套）。
func structFieldIdxs(toks []Token, open, close int) []int {
	var out []int
	depth, expect := 0, true
	for i := open + 1; i <= close; i++ {
		t := toks[i]
		switch t.Text {
		case "{", "[", "(":
			depth++
		case "}", "]", ")":
			depth--
		}
		if depth > 0 {
			continue
		}
		if t.Kind == Newline || t.Text == ";" || t.Text == "," {
			expect = true
			continue
		}
		if t.Kind == Space || t.Kind == Comment {
			continue
		}
		if expect && t.Kind == Ident && !isGoKeyword(t.Text) {
			out = append(out, i)
			expect = false
		}
	}
	return out
}

// ---------- 查询 API ----------

// ResolveAt 解析 pos 处对 name 的引用，沿作用域链从内到外返回词法可见的绑定。
// 这是消除遮蔽类 bug 的核心：sig/pub/typeinfer 不再自己扫 token 判断遮蔽。
func (b *Bindings) ResolveAt(name string, pos Pos) *Binding {
	off := pos.Offset
	for s := b.ScopeAt(pos); s != nil; s = s.Parent {
		cands := s.Bindings[name]
		var best *Binding
		for _, c := range cands {
			// 声明点自身总是可见：pub/sig 等 transform 在声明 token 位置
			// 查询绑定以判断「此声明是否遮蔽外层符号」，必须命中新声明本身。
			if c.DeclPos.Offset == off {
				return c
			}
			// 引用点：VisibleFrom 为声明真正生效的偏移（:= 场景是语句结束处），
			// 未设置时回退到 DeclPos。这样 `x := x + 1` 右侧的 x 不命中新绑定。
			from := c.VisibleFrom.Offset
			if !c.VisibleFrom.valid() {
				from = c.DeclPos.Offset
			}
			if from <= off {
				best = c
			}
		}
		if best != nil {
			return best
		}
		// 包级允许前向引用（func/type/var/const/sig 可先使用后声明）。
		if s.Kind == SkGlobal && len(cands) > 0 {
			return cands[0]
		}
	}
	return nil
}

// TypeAt 返回 name 在 pos 处的推断类型文本；"" 表示未知或被遮蔽后无类型。
// 供类型查询从「每次重扫 token」切换为「按稳定 Pos 查绑定表」（P3）。
func (b *Bindings) TypeAt(name string, pos Pos) string {
	if bd := b.ResolveAt(name, pos); bd != nil {
		return bd.Type
	}
	return ""
}

// TypeAtIdx 是 TypeAt 的下标适配：用 toks[useIdx].Pos 查询绑定表。
// 供 desugar 后 token 流上的调用点使用——Pos 是跨 desugar 稳定的锚点，
// 而 useIdx 会随 token 增删漂移。b 为 nil 或越界时返回 ""（调用方降级）。
func (b *Bindings) TypeAtIdx(toks []Token, name string, useIdx int) string {
	if b == nil || useIdx < 0 || useIdx >= len(toks) {
		return ""
	}
	return b.TypeAt(name, toks[useIdx].Pos)
}

// DeclAt 返回 pos 处的声明绑定（供「声明目标不改写」判断，取代 isSignalDeclTarget）。
func (b *Bindings) DeclAt(pos Pos) *Binding {
	return b.byPos[pos.Offset]
}

// ScopeAt 返回包含 pos 的最深作用域。
func (b *Bindings) ScopeAt(pos Pos) *BindScope {
	off := pos.Offset
	s := b.root
	for {
		var next *BindScope
		for _, c := range s.Children {
			if off >= c.From.Offset && off < c.To.Offset {
				next = c
				break
			}
		}
		if next == nil {
			return s
		}
		s = next
	}
}

// SelAt 解析 selector `recv.sel` 中 sel 位置 selPos 的绑定，供 selector 保护判断。
// 先按 recvPos 处的标识符解析接收者绑定，取其类型名对应的 struct 作用域，再查字段。
// 返回 nil 表示 sel 无本地绑定（可能是外部符号 / 需大写的 pub 字段）。
func (b *Bindings) SelAt(sel string, recvPos, selPos Pos) *Binding {
	recvName := ""
	for _, t := range b.toks {
		if t.Pos.Offset == recvPos.Offset && t.Kind == Ident {
			recvName = t.Text
			break
		}
	}
	if recvName != "" {
		if recv := b.ResolveAt(recvName, recvPos); recv != nil && recv.Type != "" {
			if fs := b.structScopes[recv.Type]; fs != nil {
				for _, bd := range fs.Bindings[sel] {
					return bd
				}
			}
		}
	}
	// 兜底：在 selPos 所在链里找同名字段/方法（覆盖未推断出类型的情形）。
	for s := b.ScopeAt(selPos); s != nil; s = s.Parent {
		for _, bd := range s.Bindings[sel] {
			if bd.Kind == BkField || bd.Kind == BkFunc {
				return bd
			}
		}
	}
	return nil
}

// FuncParamAt 返回具名函数 name 第 idx 个参数的原始类型文本；"" 表示无/未知。
func (b *Bindings) FuncParamAt(name string, idx int) string {
	if b == nil || b.funcSigs == nil {
		return ""
	}
	sig := b.funcSigs[name]
	if sig == nil || idx < 0 || idx >= len(sig.ParamTypes) {
		return ""
	}
	return sig.ParamTypes[idx]
}

// FuncResult 返回具名函数 name 的首个返回类型文本；"" 表示无返回值/未知。
func (b *Bindings) FuncResult(name string) string {
	if b == nil || b.funcSigs == nil {
		return ""
	}
	if sig := b.funcSigs[name]; sig != nil {
		return sig.ResultType
	}
	return ""
}

// FuncIsVoid 判断具名函数 name 是否声明为无返回值。
// 第三返回值 ok 表示是否找到该函数（未找到时调用方应回退旧扫描）。
func (b *Bindings) FuncIsVoid(name string) (isVoid, ok bool) {
	if b == nil || b.funcSigs == nil {
		return false, false
	}
	if sig := b.funcSigs[name]; sig != nil {
		return sig.IsVoid, true
	}
	return false, false
}

// AllBindings 返回全部绑定（调试/对拍用）。不要修改返回值。
func (b *Bindings) AllBindings() []*Binding { return b.all }

// Scopes 返回全部作用域（调试/对拍用）。不要修改返回值。
func (b *Bindings) Scopes() []*BindScope { return b.scopes }
