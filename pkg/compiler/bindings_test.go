package compiler

import (
	"testing"
)

// posAtNthIdent 返回源码中第 n 个（1 基）名为 name 的标识符的 Pos。
func posAtNthIdent(t *testing.T, toks []Token, name string, n int) Pos {
	t.Helper()
	seen := 0
	for _, tk := range toks {
		if tk.Kind == Ident && tk.Text == name {
			seen++
			if seen == n {
				return tk.Pos
			}
		}
	}
	t.Fatalf("未找到第 %d 个标识符 %q", n, name)
	return NoPos
}

// findBinding 在绑定表中按「名字 + 声明行」定位绑定。
func findBinding(t *testing.T, b *Bindings, name string, line int) *Binding {
	t.Helper()
	for _, bd := range b.all {
		if bd.Name == name && bd.DeclPos.Line == line {
			return bd
		}
	}
	t.Fatalf("未找到绑定 %q @line %d", name, line)
	return nil
}

func TestBindParamShadowsPackageSignal(t *testing.T) {
	// P1-1 根因场景：包级 signal count，但函数参数也叫 count。
	// 函数体内 ResolveAt("count") 必须解析到参数（BkParam），而非 signal。
	src := `package main
signal count = 0
func getCount(count int) int {
    return count
}
`
	toks := New(src).AllTokens()
	b := Bind(toks)
	usePos := posAtNthIdent(t, toks, "count", 3) // 第 3 个 count：return count
	bd := b.ResolveAt("count", usePos)
	if bd == nil {
		t.Fatal("count 引用未解析到任何绑定")
	}
	if bd.Kind != BkParam {
		t.Errorf("函数体内 count 应解析为参数 BkParam，实际 %s", bd.Kind)
	}
	if bd.DeclPos.Line != 3 {
		t.Errorf("参数 count 声明应在第 3 行，实际第 %d 行", bd.DeclPos.Line)
	}
}

func TestBindLocalShadowsPackageSignal(t *testing.T) {
	// 函数内局部 count := 遮蔽包级 signal。
	src := `package main
signal count = 0
func f() int {
    count := 5
    return count
}
`
	toks := New(src).AllTokens()
	b := Bind(toks)
	usePos := posAtNthIdent(t, toks, "count", 3) // return count
	bd := b.ResolveAt("count", usePos)
	if bd == nil || bd.Kind != BkLocal {
		t.Fatalf("局部 count 应解析为 BkLocal，实际 %v", bd)
	}
	if bd.DeclPos.Line != 4 {
		t.Errorf("局部 count 声明应在第 4 行，实际第 %d 行", bd.DeclPos.Line)
	}
}

func TestBindPackageSignalVisibleInFunc(t *testing.T) {
	// 无遮蔽时，函数体内引用解析到包级 signal（BkSignal）。
	src := `package main
signal count = 0
func f() int {
    return count
}
`
	toks := New(src).AllTokens()
	b := Bind(toks)
	usePos := posAtNthIdent(t, toks, "count", 2) // return count
	bd := b.ResolveAt("count", usePos)
	if bd == nil || bd.Kind != BkSignal {
		t.Fatalf("无遮蔽时 count 应解析为 BkSignal，实际 %v", bd)
	}
}

func TestBindForwardReferenceGlobal(t *testing.T) {
	// 包级符号允许前向引用：main 在 helper 之前，仍解析到 BkFunc。
	src := `package main
func main() { println(helper()) }
func helper() int { return 1 }
`
	toks := New(src).AllTokens()
	b := Bind(toks)
	usePos := posAtNthIdent(t, toks, "helper", 1) // 调用点
	bd := b.ResolveAt("helper", usePos)
	if bd == nil || bd.Kind != BkFunc {
		t.Fatalf("前向引用的 helper 应解析为 BkFunc，实际 %v", bd)
	}
}

func TestBindComputedKindUpgrade(t *testing.T) {
	// let doubled := Signal.computed(...) 应先按 BkLocal 收集，再升级为 BkComputed。
	src := `package main
func main() {
    let doubled = Signal.computed(() => 1)
    println(doubled)
}
`
	toks := New(src).AllTokens()
	b := Bind(toks)
	bd := findBinding(t, b, "doubled", 3)
	if bd.Kind != BkComputed {
		t.Errorf("doubled 应为 BkComputed，实际 %s", bd.Kind)
	}
}

func TestBindStructFieldResolve(t *testing.T) {
	// struct 字段收集到 SkStruct 作用域，并登记到 structScopes。
	src := `package main
type point struct {
    x int
    y int
}
`
	toks := New(src).AllTokens()
	b := Bind(toks)
	fs := b.structScopes["point"]
	if fs == nil {
		t.Fatal("未登记 point 的 struct 作用域")
	}
	for _, name := range []string{"x", "y"} {
		found := false
		for _, bd := range fs.Bindings[name] {
			if bd.Kind == BkField {
				found = true
			}
		}
		if !found {
			t.Errorf("struct point 缺少字段 %q 的 BkField 绑定", name)
		}
	}
}

func TestBindMultiParamSharedType(t *testing.T) {
	// func f(a, b int)：a、b 都应解析为参数（共享类型）。
	src := `package main
func f(a, b int) int {
    return a + b
}
`
	toks := New(src).AllTokens()
	b := Bind(toks)
	for _, p := range []struct {
		name string
		nth  int
	}{{"a", 2}, {"b", 2}} {
		usePos := posAtNthIdent(t, toks, p.name, p.nth)
		bd := b.ResolveAt(p.name, usePos)
		if bd == nil || bd.Kind != BkParam {
			t.Errorf("参数 %q 应解析为 BkParam，实际 %v", p.name, bd)
		}
	}
}

func TestBindDeclAtStableIdentity(t *testing.T) {
	// DeclAt 用稳定 Pos.Offset 命中声明点。
	src := `package main
signal count = 0
func main() { println(count) }
`
	toks := New(src).AllTokens()
	b := Bind(toks)
	declPos := posAtNthIdent(t, toks, "count", 1) // signal count
	bd := b.DeclAt(declPos)
	if bd == nil || bd.Kind != BkSignal {
		t.Fatalf("DeclAt 应命中 signal 声明，实际 %v", bd)
	}
}

func TestBindNestedBlockScope(t *testing.T) {
	// 内层块的局部声明不泄漏到外层。
	src := `package main
func f() {
    x := 1
    {
        x := 2
        _ = x
    }
    _ = x
}
`
	toks := New(src).AllTokens()
	b := Bind(toks)
	// 第 3 个 x（内层块内）应解析到第 4 行的内层 x（BkLocal，line 5）。
	innerPos := posAtNthIdent(t, toks, "x", 3)
	bd := b.ResolveAt("x", innerPos)
	if bd == nil || bd.Kind != BkLocal || bd.DeclPos.Line != 5 {
		t.Errorf("内层 x 应解析到 line 5 的 BkLocal，实际 %v @line %d", bd, bd.DeclPos.Line)
	}
	// 第 4 个 x（块外）应解析到第 3 行的外层 x。
	outerPos := posAtNthIdent(t, toks, "x", 4)
	bd = b.ResolveAt("x", outerPos)
	if bd == nil || bd.Kind != BkLocal || bd.DeclPos.Line != 3 {
		t.Errorf("外层 x 应解析到 line 3 的 BkLocal，实际 %v @line %d", bd, bd.DeclPos.Line)
	}
}

func TestBindBuildDeterministic(t *testing.T) {
	// 同一源码多次构建结果一致（身份稳定）。
	src := `package main
signal count = 0
func f(count int) int { return count }
`
	toks := New(src).AllTokens()
	b1 := Bind(toks)
	b2 := Bind(toks)
	if len(b1.AllBindings()) != len(b2.AllBindings()) {
		t.Fatalf("两次构建绑定数不同：%d vs %d", len(b1.AllBindings()), len(b2.AllBindings()))
	}
	for i, a := range b1.AllBindings() {
		c := b2.AllBindings()[i]
		if a.Name != c.Name || a.Kind != c.Kind || a.DeclPos != c.DeclPos {
			t.Errorf("第 %d 个绑定不一致：%+v vs %+v", i, a, c)
		}
	}
}

func TestBindTypeAt(t *testing.T) {
	// TypeAt 应与现有 inferIdentType 在参数/var/:= 场景给出一致的类型文本。
	src := `package main
func f(name string) {
    var age int = 1
    ratio := 3.14
    println(name, age, ratio)
}
`
	toks := New(src).AllTokens()
	b := Bind(toks)
	cases := []struct {
		name  string
		usage string
		nth   int
		want  string
	}{
		{"name", "name", 2, "string"},    // 参数（第1个是声明）
		{"age", "age", 2, "int"},         // var 显式类型（第1个是声明）
		{"ratio", "ratio", 2, "float64"}, // := 字面量
	}
	for _, c := range cases {
		usePos := posAtNthIdent(t, toks, c.usage, c.nth)
		got := b.TypeAt(c.name, usePos)
		if got != c.want {
			t.Errorf("TypeAt(%q) = %q, want %q", c.name, got, c.want)
		}
		// 对拍：inferIdentType 用下标，TypeAt 用 Pos——结果应一致。
		useIdx := -1
		for i, tk := range toks {
			if tk.Pos == usePos {
				useIdx = i
				break
			}
		}
		if useIdx >= 0 {
			legacy := inferIdentType(toks, c.name, useIdx)
			if legacy != got && legacy != "" {
				t.Errorf("TypeAt(%q)=%q 与 inferIdentType=%q 不一致", c.name, got, legacy)
			}
		}
	}
}

func TestBindTypeAtShadow(t *testing.T) {
	// 遮蔽场景下，TypeAt 应返回遮蔽变量的类型而非外层 signal 的。
	src := `package main
signal flag = true
func f() {
    flag := 5
    println(flag)
}
`
	toks := New(src).AllTokens()
	b := Bind(toks)
	usePos := posAtNthIdent(t, toks, "flag", 3) // println(flag)
	if got := b.TypeAt("flag", usePos); got != "int" {
		t.Errorf("遮蔽后 flag 类型应为 int，实际 %q", got)
	}
}

// ===== 全面接入对拍测试：验证绑定表路径与旧扫描逻辑等价 =====

func TestBindTypeQueryParity(t *testing.T) {
	// 覆盖 lookupIdentType 的全部 A 类场景：参数 / var 显式 / := 字面量 /
	// range 元素 / 链式赋值 / 遮蔽。逐一比对「绑定表命中」与「旧扫描」结果。
	src := `package main
func f(nums []int, name string) {
    var age int = 1
    score := 3.14
    flag := true
    j := 0
    i := j
    for _, v := range nums {
        p := point{x: v}
        println(p, name, age, score, flag, i)
    }
}
type point struct { x int }
`
	toks := New(src).AllTokens()
	b := Bind(toks)
	// 只比使用点（跳过声明点：声明点旧扫描返回 "" 而绑定表返回类型，属预期）。
	cases := []string{"nums", "name", "age", "score", "flag", "j", "i", "v"}
	for i, tk := range toks {
		if tk.Kind != Ident {
			continue
		}
		matched := false
		for _, c := range cases {
			if tk.Text == c {
				matched = true
				break
			}
		}
		if !matched || b.DeclAt(tk.Pos) != nil {
			continue
		}
		viaBind := b.TypeAtIdx(toks, tk.Text, i)
		viaOld := inferIdentType(toks, tk.Text, i)
		if viaBind != viaOld {
			t.Errorf("TypeParity %q @line%d: 绑定表=%q 旧扫描=%q", tk.Text, tk.Pos.Line, viaBind, viaOld)
		}
	}
}

func TestBindFuncSigParity(t *testing.T) {
	// B 类：函数签名索引与旧扫描等价（参数原始文本 / 返回类型 / isVoid）。
	src := `package main
func apply(f func(int) int, n int) int { return f(n) }
func draw(p point) point { return p }
func notify(msg string) { println(msg) }
type point struct { x int }
`
	toks := New(src).AllTokens()
	b := Bind(toks)
	params := []struct {
		name string
		idx  int
	}{{"apply", 0}, {"apply", 1}, {"draw", 0}, {"notify", 0}}
	for _, p := range params {
		viaBind := b.FuncParamAt(p.name, p.idx)
		viaOld := declaredParamTypeAt(toks, p.name, p.idx)
		if viaBind != viaOld {
			t.Errorf("FuncParamParity %s[%d]: 绑定表=%q 旧扫描=%q", p.name, p.idx, viaBind, viaOld)
		}
	}
	for _, name := range []string{"apply", "draw", "notify"} {
		viaBind, ok := b.FuncIsVoid(name)
		if !ok {
			t.Errorf("FuncIsVoid %s: 绑定表未收录签名", name)
			continue
		}
		viaOld := declaredVoidFunc(toks, name, nil)
		if viaBind != viaOld {
			t.Errorf("FuncVoidParity %s: 绑定表=%v 旧扫描=%v", name, viaBind, viaOld)
		}
	}
}

func TestBindShadowTypePrefersBinding(t *testing.T) {
	// 关键对拍：遮蔽场景下绑定表给出正确类型，旧扫描给出错误类型。
	// 证明确实由绑定表接管（而非回退路径碰巧一致）。
	src := `package main
func f(n int) {
    n := "shadow"
    p := point{x: 0}
    println(n, p)
}
type point struct { x int }
`
	toks := New(src).AllTokens()
	b := Bind(toks)
	usePos := posAtNthIdent(t, toks, "n", 3) // println(n) —— 遮蔽后应为 string
	if got := b.TypeAt("n", usePos); got != "string" {
		t.Errorf("遮蔽后 n 类型应为 string（绑定表），实际 %q", got)
	}
	// 旧扫描取「最近声明」会在某些下标下误报 int；这里断言绑定表正确性即可。
}

// ===== P1-2 / P2-1 回归：短变量声明显式可见性 + 控制语句作用域 =====

func TestBindShortDeclRHSReferencesOuter(t *testing.T) {
	// 回归 P1-2：`x := x + 1` 右侧的 x 必须引用外层 signal，而非刚声明的局部 x。
	// 旧实现用 DeclPos.Offset <= off，右侧 x 会误命中新绑定，导致类型推断 /
	// signal 读取改写出错。
	src := `package main
signal count = 10
func f() {
    count := count + 1
    println(count)
}
`
	toks := New(src).AllTokens()
	b := Bind(toks)
	// count 出现次序：1=signal声明 2=右侧(:=的RHS) 3=左侧声明 4=println
	// 注意 lexer 先扫到 := 左侧还是 RHS 取决于文本顺序：源码是 "count := count + 1"，
	// 故次序为 1=signal, 2=左侧声明, 3=右侧, 4=println。
	rhsPos := posAtNthIdent(t, toks, "count", 3)
	bd := b.ResolveAt("count", rhsPos)
	if bd == nil {
		t.Fatal("右侧 count 未解析到绑定")
	}
	if bd.Kind != BkSignal {
		t.Errorf("`:=` 右侧 count 应解析到外层 BkSignal，实际 %s @line %d", bd.Kind, bd.DeclPos.Line)
	}
	if bd.DeclPos.Line != 2 {
		t.Errorf("右侧 count 应引用第 2 行的 signal 声明，实际第 %d 行", bd.DeclPos.Line)
	}
	// 而 println(count) 应解析到新声明的局部变量（语句结束后可见）。
	lhsUsePos := posAtNthIdent(t, toks, "count", 4)
	bd2 := b.ResolveAt("count", lhsUsePos)
	if bd2 == nil || bd2.Kind != BkLocal {
		t.Errorf("println 的 count 应解析到 BkLocal，实际 %v", bd2)
	}
}

func TestBindForInitDoesNotLeak(t *testing.T) {
	// 回归 P2-1：`for i := 0; ...` 的 i 不得泄漏到循环外。
	// 循环内引用解析到 SkFor 作用域的 init；循环外同名引用解析到外层。
	src := `package main
func f() {
    for i := 0; i < 10; i++ {
        println(i)
    }
    i := 5
    println(i)
}
`
	toks := New(src).AllTokens()
	b := Bind(toks)
	// i 次序：1=init声明 2=cond 3=post 4=body println 5=外层声明 6=外层 println
	bodyPos := posAtNthIdent(t, toks, "i", 4)
	bd := b.ResolveAt("i", bodyPos)
	if bd == nil || bd.Scope == nil || bd.Scope.Kind != SkFor {
		t.Fatalf("循环体内 i 应解析到 SkFor init，实际 %v scope=%v", bd, scopeKindOf(bd))
	}
	outerPos := posAtNthIdent(t, toks, "i", 6)
	bd2 := b.ResolveAt("i", outerPos)
	if bd2 == nil || bd2.Scope == nil || bd2.Scope.Kind == SkFor {
		t.Fatalf("循环外 i 不应解析到 SkFor init，实际 %v scope=%v", bd2, scopeKindOf(bd2))
	}
	if bd2.DeclPos.Line != 6 {
		t.Errorf("循环外 i 应解析到第 6 行的局部声明，实际第 %d 行", bd2.DeclPos.Line)
	}
}

func TestBindIfInitScopedToChain(t *testing.T) {
	// 回归 P2-1：`if x := f(); x > 0 {}` 的 x 在 if/else 链内可见，链外不可见。
	src := `package main
func get() int { return 1 }
func f() {
    if x := get(); x > 0 {
        println(x)
    } else {
        println(x)
    }
    x := 9
    println(x)
}
`
	toks := New(src).AllTokens()
	b := Bind(toks)
	// x 次序：1=if init 2=cond 3=then println 4=else println 5=外层声明 6=外层 println
	elsePos := posAtNthIdent(t, toks, "x", 4)
	bd := b.ResolveAt("x", elsePos)
	if bd == nil || bd.Scope == nil || bd.Scope.Kind != SkIf {
		t.Fatalf("else 分支 x 应解析到 SkIf init，实际 %v scope=%v", bd, scopeKindOf(bd))
	}
	outerPos := posAtNthIdent(t, toks, "x", 6)
	bd2 := b.ResolveAt("x", outerPos)
	if bd2 == nil || bd2.Scope == nil || bd2.Scope.Kind == SkIf {
		t.Fatalf("if 链外 x 不应解析到 SkIf init，实际 %v scope=%v", bd2, scopeKindOf(bd2))
	}
}

func TestBindRangeCompositeLiteralBody(t *testing.T) {
	// 回归：`for _, v := range []int{1,2}` 的无括号复合字面量不得干扰 body 定位。
	// body 内 println(v) 必须解析到 range 迭代变量（BkRangeVar）。
	src := `package main
func f() {
    for _, v := range []int{1, 2} {
        println(v)
    }
}
`
	toks := New(src).AllTokens()
	b := Bind(toks)
	// v 次序：1=range 左侧声明 2=body println
	usePos := posAtNthIdent(t, toks, "v", 2)
	bd := b.ResolveAt("v", usePos)
	if bd == nil {
		t.Fatal("body 内 v 未解析到绑定")
	}
	if bd.Kind != BkRangeVar {
		t.Errorf("body 内 v 应为 BkRangeVar，实际 %s", bd.Kind)
	}
}

func scopeKindOf(bd *Binding) string {
	if bd == nil || bd.Scope == nil {
		return "<nil>"
	}
	switch bd.Scope.Kind {
	case SkGlobal:
		return "SkGlobal"
	case SkFunc:
		return "SkFunc"
	case SkBlock:
		return "SkBlock"
	case SkStruct:
		return "SkStruct"
	case SkFor:
		return "SkFor"
	case SkIf:
		return "SkIf"
	case SkSwitch:
		return "SkSwitch"
	case SkSelect:
		return "SkSelect"
	}
	return "?"
}
