package compiler

// =============================================================================
// GX 语义层（desugar 之前运行）
//
// 职责：在 union 擦除 / is 改写之前，基于原始 token 流建立符号表与类型环境，
// 对 if / else-if / else 链做 is 收窄数据流分析，并产出 GX 原生坐标的诊断。
//
// 这一层是本编译器「自己的类型系统」的主战场：联合成员的合法性、收窄后的
// 有效类型都在这里判定；go/types（buildcheck）只兜底检查降级代码的合法性。
//
// 产物 Analysis 被 desugar 消费：
//   - typeguard 通过 GuardAt(pos) 拿到守卫的语义（recv / 目标类型 / token），
//     不再自己判定「什么是一个 is 守卫」。
//   - 未来穷尽性检查、诊断回投都复用同一份 Analysis。
// =============================================================================

import (
	"fmt"
	"strings"
)

// ---------- 类型表示 ----------

// Type 是 GX 类型层的最小类型表示。联合用 Members（长度 >=2）表达，
// 非联合用 Name 表达；"any" / "unknown" 是特殊名字，表示无约束或未能推断。
type Type struct {
	Name    string  // 非联合类型名；联合时为空
	Members []Type  // 非空表示联合类型
}

// IsUnion 报告是否为联合类型。
func (t Type) IsUnion() bool { return len(t.Members) >= 2 }

// String 返回可读类型文本（联合成员以 | 连接）。
func (t Type) String() string {
	if !t.IsUnion() {
		if t.Name == "" {
			return "unknown"
		}
		return t.Name
	}
	parts := make([]string, 0, len(t.Members))
	for _, m := range t.Members {
		parts = append(parts, m.String())
	}
	return strings.Join(parts, "|")
}

// Has 报告类型是否可容纳指定成员文本（精确匹配）。any 视作可容纳任意成员。
func (t Type) Has(member string) bool {
	if t.Name == "any" {
		return true
	}
	if t.IsUnion() {
		for _, m := range t.Members {
			if m.String() == member {
				return true
			}
		}
		return false
	}
	return t.Name == member
}

// Accepts 判断本类型（通常是联合）能否接受一个值。
// value 是值的类型文本，untyped 表示该值是 Go 无类型常量（字面量 / const）。
// 遵循 Go 可赋值规则：无类型常量可隐式转换到兼容的类型族。
func (t Type) Accepts(value string, untyped bool) bool {
	if t.Name == "any" || value == "" {
		return true // any 或未知类型放过，避免误报
	}
	candidates := []Type{t}
	if t.IsUnion() {
		candidates = t.Members
	}
	for _, c := range candidates {
		if assignableTo(c.String(), value, untyped) {
			return true
		}
	}
	return false
}

// assignableTo 判断 value 类型能否赋给 target 类型。
// untyped 时按 Go 无类型常量规则放宽到类型族；具类型时要求精确匹配。
func assignableTo(target, value string, untyped bool) bool {
	if target == value || target == "any" {
		return true
	}
	if !untyped {
		return false
	}
	// 无类型整数常量：1 可赋给所有整数/浮点/复数类型；
	// 负整数字面量（-1）不能赋给无符号整数。
	if value == "int-lit" {
		return isIntegerType(target) || isFloatType(target) || isComplexType(target)
	}
	if value == "neg-int-lit" {
		if isUnsignedType(target) {
			return false
		}
		return isSignedIntegerType(target) || isFloatType(target) || isComplexType(target)
	}
	// 无类型浮点常量：3.5 可赋给浮点/复数
	if value == "float-lit" {
		return isFloatType(target) || isComplexType(target)
	}
	// rune 常量：'a' 可赋给整数/浮点（rune 是 int32 的别名）
	if value == "rune-lit" {
		return isIntegerType(target) || isFloatType(target) || target == "rune"
	}
	// 无类型字符串常量：可赋给 string（及具名 string 类型，这里简化为 string 族）
	if value == "string-lit" {
		return isStringType(target)
	}
	// 无类型布尔常量
	if value == "bool-lit" {
		return target == "bool"
	}
	return false
}

func isIntegerType(s string) bool {
	return isSignedIntegerType(s) || isUnsignedType(s)
}

func isSignedIntegerType(s string) bool {
	switch s {
	case "int", "int8", "int16", "int32", "int64", "rune":
		return true
	}
	return false
}

func isUnsignedType(s string) bool {
	switch s {
	case "uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "byte":
		return true
	}
	return false
}

func isFloatType(s string) bool    { return s == "float32" || s == "float64" }
func isComplexType(s string) bool  { return s == "complex64" || s == "complex128" }
func isStringType(s string) bool   { return s == "string" }

// Without 从类型中排除一个成员（用于 else 分支收窄）。
// 非联合类型无法做有意义的排除，原样返回；联合排空后记为 never。
func (t Type) Without(member string) Type {
	if !t.IsUnion() {
		return t
	}
	var rest []Type
	for _, m := range t.Members {
		if m.String() != member {
			rest = append(rest, m)
		}
	}
	switch len(rest) {
	case 0:
		return Type{Name: "never"}
	case 1:
		return rest[0]
	default:
		return Type{Members: rest}
	}
}

// ---------- 符号、收窄事实、守卫、诊断 ----------

// Symbol 是变量声明的符号：名字、声明类型、声明下标与可见作用域（token 下标区间）。
type Symbol struct {
	Name       string
	Type       Type
	DeclIdx    int
	ScopeFrom  int
	ScopeTo    int
}

// NarrowFact 表示在 token 下标 [From, To) 内，变量 Var 的有效类型被收窄为 Type。
// Shadowed 表示 desugar 是否在该区间注入了 shadow（值确实被窄化，可参与运算）：
//   - then 守卫块内为 true（注入 x := __gx_v）；
//   - else 分支：剩余为单一具体类型且块内用到 x 时为 true（注入 x := x.(Narrow)），
//     否则为 false（剩余仍是联合或块内未用 x，仅保留类型供穷尽性分析）。
type NarrowFact struct {
	Var      string
	Type     Type
	From     int
	To       int
	Shadowed bool
}

// Guard 是一个已识别的 is 守卫，供 desugar 生成 comma-ok 断言使用。
// 所有位置均为原始源码位置（transforms 保留 Pos），因此不受 desugar 改写的下标漂移影响。
type Guard struct {
	Pos      Pos      // if token 位置（GuardAt 的键、临时变量命名锚点）
	EndPos   Pos      // 守卫块 '{' 位置（desugar 用它定位跳过点）
	Recv     string   // 被守卫变量
	RecvPos  Pos      // 被守卫变量位置（shadow 生成复用）
	Target       Type     // 目标类型
	TypeToks     []Token  // 目标类型原始 token（保留位置，供 emit / sourcemap）
	ThenUsesRecv bool     // then 块内是否引用了 recv；false 时不注入 shadow 避免未使用错误
}

// ElseShadow 描述一个 else 分支可注入的 shadow。
// 当 x 的联合类型经 is 守卫排除目标类型后，剩余类型 Narrow 若为单一具体类型
// （可作 Go 类型断言 x.(Narrow)），desugar 就在 else 块首注入
//
//	x := x.(Narrow)
//
// 使 else 内 x 具有收窄后的真实类型（而非擦除后的 any），从而可直接参与运算。
// 以 else 块 '{' 的 Pos 作为键，与 GuardAt 一样不受 desugar 下标漂移影响。
type ElseShadow struct {
	Recv    string
	RecvPos Pos
	Narrow  Type
}

// GXDiagnostic 是 GX 语义层产出的一条诊断，位置为 GX 源码坐标（1 基）。
type GXDiagnostic struct {
	Pos     Pos
	Code    string
	Message string
}

func (d GXDiagnostic) String() string {
	return fmt.Sprintf("%d:%d: %s: %s", d.Pos.Line, d.Pos.Col, d.Code, d.Message)
}

type posKey struct{ line, col int }

func keyOf(p Pos) posKey { return posKey{p.Line, p.Col} }

type funcRange struct{ from, to int }

// Analysis 是一份源码经 GX 语义层分析后的完整产物。
type Analysis struct {
	n         int
	symbols   []Symbol
	facts       []NarrowFact
	guards      map[posKey]Guard
	elseShadows map[posKey]ElseShadow // else 块 '{' 位置 → 可注入的排除法 shadow
	diags       []GXDiagnostic
	funcRange []funcRange
	// posToIdx 把 GX 坐标（行列）映射到 token 下标，供 AST 侧查询。
	// 构建时不覆盖：同一坐标保留最小下标（首个 token）。
	posToIdx map[posKey]int
	lineIdx  map[int][]lineEntry // 行号 → 该行的有效 token（按列升序）
	sigs       []FuncSig          // 函数签名索引（参数/返回值联合类型），供 AST 侧调用/返回规则使用
	namedTypes map[string]Type    // 命名类型别名表：type MyU = int|string → MyU: 联合
}

type lineEntry struct {
	col int
	idx int
}

// Analyze 在原始 token 流（union 擦除、is 改写之前）上做语义分析。
func Analyze(toks []Token) *Analysis {
	a := &Analysis{
		n:           len(toks),
		guards:      map[posKey]Guard{},
		elseShadows: map[posKey]ElseShadow{},
		posToIdx:    map[posKey]int{},
		lineIdx:    map[int][]lineEntry{},
		namedTypes: map[string]Type{},
	}
	a.funcRange = findFuncRanges(toks)
	a.collectNamedTypes(toks) // 必须先于符号收集：变量类型解析要查别名表
	a.collectSymbols(toks)
	a.collectSigs(toks)
	a.collectGuards(toks)
	a.indexPositions(toks)
	return a
}

// indexPositions 建立 GX 坐标 → token 下标的索引，供 AST 侧把节点坐标
// （经 sourcemap 映射回 GX 后）还原成 token 下标，进而复用精确作用域查询。
func (a *Analysis) indexPositions(toks []Token) {
	for i, t := range toks {
		if !t.Pos.valid() || t.Kind == Space || t.Kind == Newline || t.Kind == Comment {
			continue
		}
		k := keyOf(t.Pos)
		if _, ok := a.posToIdx[k]; !ok {
			a.posToIdx[k] = i
		}
		a.lineIdx[t.Pos.Line] = append(a.lineIdx[t.Pos.Line], lineEntry{col: t.Pos.Col, idx: i})
	}
}

// idxNearGx 返回最接近给定 GX 坐标的 token 下标：
// 精确命中优先；否则同行取列 <= col 的最后一个；再否则取最近的上一行行尾。
// 找不到返回 -1。
func (a *Analysis) idxNearGx(line, col int) int {
	if a == nil {
		return -1
	}
	if idx, ok := a.posToIdx[posKey{line, col}]; ok {
		return idx
	}
	if entries := a.lineIdx[line]; len(entries) > 0 {
		best := entries[0].idx
		for _, e := range entries {
			if e.col <= col {
				best = e.idx
			} else {
				break
			}
		}
		return best
	}
	// 没有同一行：找最近的上一行（有 token 的），取行尾
	prev := -1
	for ln := range a.lineIdx {
		if ln < line && ln > prev {
			prev = ln
		}
	}
	if prev > 0 {
		entries := a.lineIdx[prev]
		return entries[len(entries)-1].idx
	}
	return -1
}

// TypeAtGx 返回 GX 坐标 (line,col) 处变量 name 的有效类型。
// 这是 Analysis 供 AST 侧使用的核心接口：AST 节点 Go 坐标经 sourcemap 映射回
// GX 坐标后，用本方法从 side table 取回 desugar 中擦除的联合/收窄类型。
// ok=false 表示坐标附近没有可查询的 token 或该名字没有声明。
func (a *Analysis) TypeAtGx(name string, line, col int) (Type, bool) {
	idx := a.idxNearGx(line, col)
	if idx < 0 {
		return Type{}, false
	}
	t := a.TypeAtShadowed(name, idx) // 只认有 shadow 的收窄，与生成代码一致
	if t.Name == "unknown" && !t.IsUnion() {
		return Type{}, false
	}
	return t, true
}

// GuardAt 返回位于 Pos p 的 is 守卫。desugar 遍历 token 时以此获取守卫语义。
func (a *Analysis) GuardAt(p Pos) (Guard, bool) {
	if a == nil {
		return Guard{}, false
	}
	g, ok := a.guards[keyOf(p)]
	return g, ok
}

// ElseShadowAt 返回位于 Pos p 的 else 块应注入的排除法 shadow。
// desugar 遍历到 else 块的 '{' 时以此生成 `x := x.(Narrow)`。
func (a *Analysis) ElseShadowAt(p Pos) (ElseShadow, bool) {
	if a == nil {
		return ElseShadow{}, false
	}
	s, ok := a.elseShadows[keyOf(p)]
	return s, ok
}

// TypeAt 返回 token 下标 idx 处变量 name 的有效（收窄后）类型。
// 先取最近的声明类型，再叠加覆盖该下标的最窄收窄事实（嵌套时内层优先）。
func (a *Analysis) TypeAt(name string, idx int) Type {
	base := a.declaredTypeAt(name, idx)
	return a.narrowAt(name, idx, base, false)
}

// TypeAtShadowed 只叠加「有 shadow 注入」的收窄事实——即 desugar 真正把变量
// 窄化成具体值的区间（then 守卫块内，以及 else 排除法收窄到单一类型的块）。
// 供 AST 检查器判定「能否参与运算」，与生成代码保持一致：else 剩余仍是联合
// （无法注入单断言）或块内未用 x 时，即使分析层知道剩余类型也按未收窄处理。
func (a *Analysis) TypeAtShadowed(name string, idx int) Type {
	base := a.declaredTypeAt(name, idx)
	return a.narrowAt(name, idx, base, true)
}

// Diagnostics 返回 GX 语义层诊断的副本。
func (a *Analysis) Diagnostics() []GXDiagnostic {
	if a == nil {
		return nil
	}
	return append([]GXDiagnostic(nil), a.diags...)
}

// Symbols 返回收集到的符号副本（供测试 / LSP 消费）。
func (a *Analysis) Symbols() []Symbol {
	return append([]Symbol(nil), a.symbols...)
}

// NamedType 返回命名别名的展开类型。
// 供 AST 检查器判断 case 里的类型名是否为「擦除为 any 的联合别名」
// （如 type MyU = int|string → case MyU 在 type switch 中等价于 case any，覆盖全部）。
func (a *Analysis) NamedType(name string) (Type, bool) {
	if a == nil {
		return Type{}, false
	}
	t, ok := a.namedTypes[name]
	return t, ok && t.String() != "unknown"
}

// ---------- 作用域与符号查询 ----------

func (a *Analysis) scopeOf(idx int) (int, int) {
	from, to := 0, a.n
	best := a.n + 1
	for _, r := range a.funcRange {
		if idx > r.from && idx < r.to {
			if span := r.to - r.from; span < best {
				best = span
				from, to = r.from, r.to
			}
		}
	}
	return from, to
}

func (a *Analysis) declaredTypeAt(name string, idx int) Type {
	var best *Symbol
	for i := range a.symbols {
		s := &a.symbols[i]
		if s.Name != name || idx < s.ScopeFrom || idx >= s.ScopeTo {
			continue
		}
		if best == nil || s.DeclIdx > best.DeclIdx {
			best = s
		}
	}
	if best == nil {
		return Type{Name: "unknown"}
	}
	return best.Type
}

// narrowAt 叠加覆盖 idx 的最窄收窄事实。shadowedOnly=true 时只考虑注入了 shadow 的区间。
func (a *Analysis) narrowAt(name string, idx int, fallback Type, shadowedOnly bool) Type {
	best := a.n + 1
	found := false
	var res Type
	for _, f := range a.facts {
		if f.Var != name || idx < f.From || idx >= f.To {
			continue
		}
		if shadowedOnly && !f.Shadowed {
			continue
		}
		if span := f.To - f.From; span < best {
			best = span
			res = f.Type
			found = true
		}
	}
	if found {
		return res
	}
	return fallback
}

func (a *Analysis) addFact(v string, t Type, from, to int, shadowed bool) {
	if from >= to {
		return
	}
	for _, f := range a.facts {
		if f.Var == v && f.From == from && f.To == to && f.Type.String() == t.String() && f.Shadowed == shadowed {
			return
		}
	}
	a.facts = append(a.facts, NarrowFact{Var: v, Type: t, From: from, To: to, Shadowed: shadowed})
}

// ---------- 命名类型别名 ----------

// collectNamedTypes 收集 type 声明的底层类型，供变量/参数类型解析时展开。
// 两遍：先记录所有别名名字（含前向引用），再解析其底层类型。
func (a *Analysis) collectNamedTypes(toks []Token) {
	// 第一遍：记录 type 声明的名字与类型起点
	type decl struct {
		name string
		from int
		to   int
	}
	var decls []decl
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("type") {
			continue
		}
		j := nextSig(toks, i+1)
		if j < 0 || toks[j].Kind != Ident {
			continue
		}
		name := toks[j].Text
		// 单行声明：名字后是 = 或直接类型
		k := nextSig(toks, j+1)
		if k < 0 {
			continue
		}
		if toks[k].Text == "=" {
			k = nextSig(toks, k+1)
		}
		if k < 0 || !isTypeStartTok(toks[k]) {
			continue
		}
		from, to, ok := unionSpan(toks, k)
		if !ok {
			from, to = k, typeEnd(toks, k)
		}
		decls = append(decls, decl{name: name, from: from, to: to})
		// 先占位，避免自引用/前向引用解析时死循环
		a.namedTypes[name] = Type{Name: "unknown"}
	}

	// 分组 type 声明
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("type") {
			continue
		}
		j := nextSig(toks, i+1)
		if j < 0 || toks[j].Text != "(" {
			continue
		}
		close := matchDelim(toks, j, "(", ")")
		if close <= j {
			continue
		}
		lineStart := j + 1
		for k := j + 1; k <= close; k++ {
			if k == close || toks[k].Kind == Newline {
				nameIdx := nextSig(toks, lineStart)
				if nameIdx >= 0 && nameIdx < k && toks[nameIdx].Kind == Ident {
					t := nextSig(toks, nameIdx+1)
					if t < 0 || t >= k {
						lineStart = k + 1
						continue
					}
					if toks[t].Text == "=" {
						t = nextSig(toks, t+1)
					}
					if t >= 0 && t < k && isTypeStartTok(toks[t]) {
						from, to, ok := unionSpan(toks, t)
						if !ok {
							from, to = t, typeEnd(toks, t)
						}
						if to > k {
							to = k
						}
						decls = append(decls, decl{name: toks[nameIdx].Text, from: from, to: to})
						a.namedTypes[toks[nameIdx].Text] = Type{Name: "unknown"}
					}
				}
				lineStart = k + 1
			}
		}
	}

	// 第二遍：解析底层类型（展开已知别名，带访问栈防循环）
	visiting := map[string]bool{}
	var resolve func(d decl, depth int) Type
	resolve = func(d decl, depth int) Type {
		if depth > 16 {
			return Type{Name: "unknown"}
		}
		return typeFromTokensResolved(toks, d.from, d.to, func(name string) (Type, bool) {
			if underlying, ok := a.namedTypes[name]; ok && underlying.String() != "unknown" {
				return underlying, true
			}
			// 找目标别名的声明并递归解析
			for _, od := range decls {
				if od.name == name {
					if visiting[name] {
						return Type{Name: "unknown"}, true // 循环别名
					}
					visiting[name] = true
					t := resolve(od, depth+1)
					delete(visiting, name)
					a.namedTypes[name] = t
					return t, true
				}
			}
			return Type{}, false
		})
	}
	for _, d := range decls {
		if visiting[d.name] {
			continue
		}
		visiting[d.name] = true
		t := resolve(d, 0)
		delete(visiting, d.name)
		a.namedTypes[d.name] = t
	}
}

// typeFromTokensResolved 是 typeFromTokens 的可注入版本：resolveNamed 用于在
// 类型名匹配到命名别名时返回其展开类型。resolveNamed 返回 ok=false 时按普通类型名处理。
func typeFromTokensResolved(toks []Token, s, e int, resolveNamed func(string) (Type, bool)) Type {
	if s >= e || s < 0 || e > len(toks) {
		return Type{Name: "unknown"}
	}
	mems := unionMembersAt(toks, s, e)
	if len(mems) >= 2 {
		t := Type{}
		for _, m := range mems {
			t.Members = append(t.Members, resolveMember(m, resolveNamed))
		}
		return t
	}
	// 单类型：可能是别名
	if e-s == 1 && toks[s].Kind == Ident {
		if t, ok := resolveNamed(toks[s].Text); ok {
			return t
		}
	}
	return Type{Name: joinTypeText(toks, s, e)}
}

func resolveMember(m string, resolveNamed func(string) (Type, bool)) Type {
	// 成员文本是单个标识符时尝试展开别名（如 type U = MyU|bool 且 MyU=int|string）
	if isSimpleIdent(m) {
		if t, ok := resolveNamed(m); ok {
			return t
		}
	}
	return Type{Name: m}
}

func isSimpleIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// ---------- 符号收集 ----------

func findFuncRanges(toks []Token) []funcRange {
	var out []funcRange
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("func") {
			continue
		}
		j := nextSig(toks, i+1)
		if j < 0 || toks[j].Text != "(" {
			continue
		}
		c := matchDelim(toks, j, "(", ")")
		if c <= j {
			continue
		}
		b := findFuncBodyBrace(toks, c)
		if b < 0 {
			continue
		}
		e := matchDelim(toks, b, "{", "}")
		if e <= b {
			continue
		}
		out = append(out, funcRange{b, e})
	}
	return out
}

func (a *Analysis) collectSymbols(toks []Token) {
	// 函数参数 / 接收者：作用域为函数体。
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("func") {
			continue
		}
		j := nextSig(toks, i+1)
		if j < 0 {
			continue
		}
		// 先确定该函数体范围，作为参数符号的可见区间。
		ff, ft := 0, a.n
		if toks[j].Text == "(" {
			c := matchDelim(toks, j, "(", ")")
			if c > j {
				if b := findFuncBodyBrace(toks, c); b >= 0 {
					if e := matchDelim(toks, b, "{", "}"); e > b {
						ff, ft = b, e
					}
				}
			}
		}
		if toks[j].Text == "(" { // 接收者
			c := matchDelim(toks, j, "(", ")")
			if c <= j {
				continue
			}
			a.collectParams(toks, j, c, ff, ft)
			j = nextSig(toks, c+1)
			if j < 0 {
				continue
			}
		}
		if toks[j].Kind == Ident { // 函数名 / 方法名
			j = nextSig(toks, j+1)
		}
		if j >= 0 && toks[j].Text == "(" {
			c := matchDelim(toks, j, "(", ")")
			if c > j {
				a.collectParams(toks, j, c, ff, ft)
			}
		}
	}

	// var / const 声明（含分组）。
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("var") && !toks[i].is("const") {
			continue
		}
		j := nextSig(toks, i+1)
		if j < 0 {
			continue
		}
		if toks[j].Text == "(" {
			c := matchDelim(toks, j, "(", ")")
			if c <= j {
				continue
			}
			lineStart := j + 1
			for k := j + 1; k <= c; k++ {
				if k == c || toks[k].Kind == Newline {
					a.addVarSymbol(toks, lineStart, k)
					lineStart = k + 1
				}
			}
		} else {
			a.addVarSymbol(toks, j, -1)
		}
	}

	// := 短变量：类型未知（联合通常来自显式声明 / 参数，够用）。
	for i := 0; i < len(toks); i++ {
		if toks[i].Text != ":=" {
			continue
		}
		p := prevSig(toks, i-1)
		if p >= 0 && toks[p].Kind == Ident {
			ff, ft := a.scopeOf(i)
			a.symbols = append(a.symbols, Symbol{
				Name: toks[p].Text, Type: Type{Name: "unknown"}, DeclIdx: p,
				ScopeFrom: ff, ScopeTo: ft,
			})
		}
	}
}

func (a *Analysis) collectParams(toks []Token, open, close, ff, ft int) {
	segStart := open + 1
	for k := open + 1; k <= close; k++ {
		if k == close || toks[k].Text == "," {
		seg := parseParamSeg(toks, segStart, k)
		if seg.nameIdx >= 0 {
			typ := a.typeFromTokens(toks, seg.typeStart, typeRangeEnd(toks, seg.typeStart))
				a.symbols = append(a.symbols, Symbol{
					Name: toks[seg.nameIdx].Text, Type: typ, DeclIdx: seg.nameIdx,
					ScopeFrom: ff, ScopeTo: ft,
				})
			}
			segStart = k + 1
		}
	}
}

func (a *Analysis) addVarSymbol(toks []Token, start, limit int) {
	nameIdx := nextSig(toks, start)
	if nameIdx < 0 || (limit > 0 && nameIdx >= limit) || toks[nameIdx].Kind != Ident {
		return
	}
	ts := oneDeclRoot(toks, start, limit, false)
	if ts < 0 { // 推断初始化，无显式类型
		return
	}
	te := typeRangeEnd(toks, ts)
	if limit > 0 && te > limit {
		te = limit
	}
	typ := a.typeFromTokens(toks, ts, te)
	ff, ft := a.scopeOf(nameIdx)
	a.symbols = append(a.symbols, Symbol{
		Name: toks[nameIdx].Text, Type: typ, DeclIdx: nameIdx,
		ScopeFrom: ff, ScopeTo: ft,
	})
}

// typeRangeEnd 返回类型的完整结束下标：若顶层带联合 A|B|C，取 unionSpan 的
// 结束（typeEnd 遇到 '|' 会提前停止，必须用 unionSpan 才能吃到完整联合）。
func typeRangeEnd(toks []Token, s int) int {
	_, e, ok := unionSpan(toks, s)
	if ok {
		return e
	}
	return typeEnd(toks, s)
}

// typeFromTokens 把 token 区间解析为 Type：顶层 A|B|C → 联合，否则单类型。
// 不展开命名别名（供无 Analysis 上下文的工具函数使用）。
func typeFromTokens(toks []Token, s, e int) Type {
	return typeFromTokensResolved(toks, s, e, func(string) (Type, bool) { return Type{}, false })
}

// a.typeFromTokens 在 Analysis 上下文中解析类型，展开已知命名别名。
func (a *Analysis) typeFromTokens(toks []Token, s, e int) Type {
	return typeFromTokensResolved(toks, s, e, func(name string) (Type, bool) {
		t, ok := a.namedTypes[name]
		if !ok || t.String() == "unknown" {
			return Type{}, false
		}
		return t, true
	})
}

// ---------- is 收窄数据流 ----------

// parsedGuard 是语法守卫（typeGuard）的分析视图：原始语法信息 + 解析出的目标类型。
// 由 parseGuardAt 构造，仅分析层内部使用；把「类型解析」与「语法识别」解耦。
type parsedGuard struct {
	start    int
	end      int
	recv     string
	recvPos  Pos
	typeToks []Token
	target   Type
}

func parseGuardAt(toks []Token, i int) (parsedGuard, bool) {
	g, ok := parseTypeGuard(toks, i)
	if !ok {
		return parsedGuard{}, false
	}
	return parsedGuard{
		start: g.start, end: g.end, recv: g.recv, recvPos: g.recvPos,
		typeToks: g.typeToks,
		target:   typeFromTokens(g.typeToks, 0, len(g.typeToks)),
	}, true
}

// checkStrandedIs 检测 desugar 后仍残留的中缀 `x is T` 形态。
// 这类 is 出现在复合条件（如 `if x is int && x > 0`）中，parseTypeGuard 会
// 正确拒绝改写，但残留的 is 关键字会让 Go 源码非法。与其静默产出非法码，
// 不如在这里报出明确诊断，引导用户拆成简单守卫。
func checkStrandedIs(toks []Token) error {
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("is") {
			continue
		}
		p := prevSig(toks, i-1)
		n := nextSig(toks, i+1)
		// 中缀形态：前是标识符、后是类型起始 token
		if p >= 0 && toks[p].Kind == Ident && n >= 0 && isTypeStartTok(toks[n]) {
			return fmt.Errorf("%d:%d: is 守卫仅支持 `if x is T` 简单形态，复合条件请拆分为独立守卫", toks[i].Pos.Line, toks[i].Pos.Col)
		}
	}
	return nil
}

func (a *Analysis) collectGuards(toks []Token) {
	processed := map[int]bool{}
	for i := 0; i < len(toks); i++ {
		if !toks[i].is("if") || processed[i] {
			continue
		}
		if _, ok := parseGuardAt(toks, i); !ok {
			continue
		}
		// else-if 的 if 会在链首那次循环里一并处理，跳过。
		if p := prevSig(toks, i-1); p >= 0 && toks[p].is("else") {
			processed[i] = true
			continue
		}
		a.processChain(toks, i, processed)
	}
}

// processChain 处理一条 if / else-if / else 链：
// 每个 then 分支把 recv 收窄到守卫目标；else-if 继承「已排除前面成员」的类型；
// else 分支收窄到剩余联合。
func (a *Analysis) processChain(toks []Token, start int, processed map[int]bool) {
	g, ok := parseGuardAt(toks, start)
	if !ok {
		return
	}
	cur := a.narrowAt(g.recv, start, a.declaredTypeAt(g.recv, start), false)
	covered := 0 // 链已覆盖的联合成员数（仅统计确实是成员的守卫）
	i := start
	for {
		processed[i] = true
		target := g.target

		// 诊断 1：守卫目标不是联合成员（仅当 recv 声明为已知联合时）。
		if cur.IsUnion() && !cur.Has(target.String()) {
			a.diags = append(a.diags, GXDiagnostic{
				Pos: toks[i].Pos, Code: "union-member",
				Message: fmt.Sprintf("类型 %s 不属于 %s 的联合类型 %s", target.String(), g.recv, cur.String()),
			})
		} else if !cur.IsUnion() && cur.String() == target.String() && cur.String() != "unknown" && cur.String() != "any" {
			// 诊断 2：已收窄到同类型的守卫冗余（嵌套守卫）。
			a.diags = append(a.diags, GXDiagnostic{
				Pos: toks[i].Pos, Code: "redundant-guard",
				Message: fmt.Sprintf("冗余守卫：%s 已收窄为 %s", g.recv, target.String()),
			})
		}

		// then 分支收窄事实。
		thenBrace := g.end
		thenClose := matchDelim(toks, thenBrace, "{", "}")
		if thenClose <= thenBrace {
			return
		}
		thenUses := blockUsesVar(toks, thenBrace+1, thenClose, g.recv)
		a.addFact(g.recv, target, thenBrace+1, thenClose, true) // if 块内：有 shadow
		a.guards[keyOf(toks[i].Pos)] = Guard{
			Pos: toks[i].Pos, EndPos: toks[thenBrace].Pos,
			Recv: g.recv, RecvPos: g.recvPos,
			Target: target, TypeToks: g.typeToks,
			ThenUsesRecv: thenUses,
		}

		// 为后续 else-if / else 排除当前成员。
		if cur.IsUnion() && cur.Has(target.String()) {
			covered++
		}
		cur = cur.Without(target.String())

		// 寻找 else 分支。
		k := nextSig(toks, thenClose+1)
		if k < 0 || !toks[k].is("else") {
			// 链尾无 else 兜底：覆盖 >=2 个成员且仍有剩余时，报穷尽性。
			// 单分支守卫（只判断一种情况）是常见写法，不报以免噪音。
			if covered >= 2 && cur.String() != "never" && cur.String() != "unknown" {
				a.diags = append(a.diags, GXDiagnostic{
					Pos: toks[start].Pos, Code: "non-exhaustive",
					Message: fmt.Sprintf("if 守卫链未穷尽 %s 的联合类型，缺少：%s", g.recv, cur.String()),
				})
			}
			return
		}
		m := nextSig(toks, k+1)
		if m < 0 {
			return
		}
		if toks[m].is("if") {
			g2, ok2 := parseGuardAt(toks, m)
			if !ok2 {
				return
			}
			g = g2
			i = m
			continue
		}
		if toks[m].Text == "{" {
			ec := matchDelim(toks, m, "{", "}")
			if ec > m {
				// 排除法收窄：else 块内 x 的有效类型是「联合减去已守卫成员」的剩余。
				// 若剩余为单一具体类型，且 else 块内确实引用了 x，则可注入
				// `x := x.(Narrow)` shadow，让 else 内 x 拥有真实类型（而非 any）。
				// 否则（剩余仍是联合 / never / 块内未用 x）保持无 shadow，避免
				// 生成非法的 `x.(A|B)` 断言或未使用变量。
				if canAssertSingle(cur) && blockUsesVar(toks, m+1, ec, g.recv) {
					a.elseShadows[keyOf(toks[m].Pos)] = ElseShadow{
						Recv: g.recv, RecvPos: g.recvPos, Narrow: cur,
					}
					a.addFact(g.recv, cur, m+1, ec, true) // 有 shadow：可参与运算
				} else {
					a.addFact(g.recv, cur, m+1, ec, false) // 无 shadow：仅用于穷尽性
				}
			}
		}
		return
	}
}

// canAssertSingle 报告类型 t 能否作为 Go 单个类型断言 x.(T) 的目标：
// 必须是单一具体类型，不能是联合、never、any、unknown 或空。
func canAssertSingle(t Type) bool {
	if t.IsUnion() {
		return false
	}
	switch t.String() {
	case "", "never", "any", "unknown":
		return false
	}
	return true
}

// blockUsesVar 扫描 token 下标 [from,to) 内是否引用了标识符 name。
// 用于判断 else 块是否真的用到被守卫变量——没用到就不注入 shadow，
// 避免生成 `x := x.(T)` 后 Go 报「declared and not used」。
func blockUsesVar(toks []Token, from, to int, name string) bool {
	for k := from; k < to; k++ {
		if toks[k].Kind == Ident && toks[k].Text == name {
			return true
		}
	}
	return false
}
