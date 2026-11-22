package compiler

import (
	"strings"
	"testing"
)

func TestSignalFieldDeclDesugar(t *testing.T) {
	// struct 内 signal 字段 → Signal.Signal[T] 字段，普通字段不变，注入 import。
	src := `package main
type CardProps struct {
    signal name string
    signal count int
    age int
}
func main() { _ = CardProps{} }
`
	got := compileOK(t, "sf1.gx", src)
	containsAll(t, got,
		`name Signal.Signal[string]`,
		`count Signal.Signal[int]`,
		`age int`,
		`import Signal "github.com/robertpanvip/gox/runtime/signal"`,
	)
	if strings.Contains(got, "var name any") || strings.Contains(got, "signal name") {
		t.Errorf("signal 字段不应残留旧降级形态:\n%s", got)
	}
}

func TestSignalFieldReadRewrite(t *testing.T) {
	// 读 p.name → p.name()
	src := `package main
type CardProps struct { signal name string }
func main() { p := CardProps{name: "x"}; println(p.name) }
`
	got := compileOK(t, "sf2.gx", src)
	containsAll(t, got, `println(p.name())`)
	if strings.Contains(got, `p.name)`) {
		t.Errorf("裸读应改写为调用式: %s", got)
	}
}

func TestSignalFieldWriteRewrite(t *testing.T) {
	// 写 p.name = expr → p.name(expr)
	src := `package main
type CardProps struct { signal name string }
func main() { p := CardProps{name: "x"}; p.name = "y" }
`
	got := compileOK(t, "sf3.gx", src)
	containsAll(t, got, `p.name(`)
	if !strings.Contains(got, `"y"`) {
		t.Errorf("写值丢失: %s", got)
	}
	if strings.Contains(got, "p.name() =") || strings.Contains(got, "p.Name") {
		t.Errorf("写目标不应读改写或被大写: %s", got)
	}
}

func TestSignalFieldCompoundAndIncDec(t *testing.T) {
	// 复合写与自增：c.n += 2 → c.n(c.n() + 2)，c.n++ → c.n(c.n() + 1)
	src := `package main
type Counter struct { signal n int }
func main() { c := Counter{n: 1}; c.n += 2; c.n++ }
`
	got := compileOK(t, "sf4.gx", src)
	gotCompact := strings.Join(strings.Fields(got), "")
	containsAll(t, gotCompact, `c.n(c.n()+2)`, `c.n(c.n()+1)`)
}

func TestSignalFieldConstructWrap(t *testing.T) {
	// 构造位置：字面量值包 Signal.New，普通字段不动。
	src := `package main
type CardProps struct { signal name string; age int }
func main() { p := CardProps{name: "x", age: 3}; _ = p }
`
	got := compileOK(t, "sf5.gx", src)
	containsAll(t, got, `name: Signal.New("x")`, `age: 3`)
	if strings.Contains(got, `age: Signal.New`) {
		t.Errorf("普通字段不应被包装: %s", got)
	}
}

func TestSignalFieldPassthroughKeepsLiveSignal(t *testing.T) {
	// 保鲜关键：signal 变量 src 传给 signal 字段时，传 getter 本身（src），
	// 不能是 Signal.New(src())（那会快照当前值、断开后续更新）。
	src := `package main
type CardProps struct { signal name string }
func main() { signal src = "x"; p := CardProps{name: src}; _ = p }
`
	got := compileOK(t, "sf6.gx", src)
	containsAll(t, got, `var src = Signal.New("x")`, `name: src`)
	if strings.Contains(got, `name: Signal.New(src())`) || strings.Contains(got, `name: src()`) {
		t.Errorf("signal 变量应直通 getter 本身（保鲜），产物: %s", got)
	}
}

func TestSignalFieldParamReceiver(t *testing.T) {
	// 函数参数（组件函数）receiver 也能解包 —— 组件值保鲜的核心场景。
	src := `package main
type CardProps struct { signal name string }
func show(p CardProps) string { return p.name }
func main() { _ = show }
`
	got := compileOK(t, "sf7.gx", src)
	containsAll(t, got, `return p.name()`)
}

func TestSignalFieldDoesNotTouchPlainFields(t *testing.T) {
	// 无 signal 字段的 struct：所有字段/读写保持原样（零回归）。
	src := `package main
type Point struct { x int; y int }
func main() { p := Point{x: 1, y: 2}; println(p.x, p.y) }
`
	got := compileOK(t, "sf8.gx", src)
	containsAll(t, got, `x int`, `p.x`, `p.y`)
	if strings.Contains(got, "Signal") {
		t.Errorf("无 signal 字段不应注入 Signal: %s", got)
	}
}

func TestSignalFieldSelectorShadowNotTouched(t *testing.T) {
	// 不同 struct 有同名普通字段：不属于 signal 字段表，不得误改。
	src := `package main
type Box struct { signal value int }
type Other struct { value int }
func main() {
    b := Box{value: 1}
    o := Other{value: 2}
    println(b.value, o.value)
}
`
	got := compileOK(t, "sf9.gx", src)
	containsAll(t, got, `println(b.value(), o.value)`)
	if strings.Contains(got, `o.value()`) {
		t.Errorf("Other.value 是普通字段，不应被解包: %s", got)
	}
}

func TestSignalFieldWithLocalSignalCoexist(t *testing.T) {
	// struct signal 字段与局部 signal 变量共存，互不干扰。
	src := `package main
type Box struct { signal value int }
func main() {
    signal local = 5
    b := Box{value: 1}
    b.value = local
    println(b.value, local)
}
`
	got := compileOK(t, "sf10.gx", src)
	containsAll(t, got, `var local = Signal.New(5)`, `value: Signal.New(1)`, `b.value(`)
	if !strings.Contains(got, `local()`) {
		t.Errorf("局部 signal 读取应改写为 local(): %s", got)
	}
}

func TestSignalFieldConstructLiteralNoSwallowedBrace(t *testing.T) {
	// 字段值走字面量档（生成 Signal.New("x")）：不得把复合字面量的 '}' 吞进值区间
	// （曾因 prevSigIn 边界越界导致产物多一个 '}'）。底层 API 已禁止手写，故用
	// 字面量触发同一生成形态，继续守住这条边界。
	src := `package main
type CardProps struct { signal name string }
func main() { p := CardProps{name: "x"}; println(p.name) }
`
	got := compileOK(t, "sf11.gx", src)
	containsAll(t, got, `name: Signal.New("x")`, `println(p.name())`)
	// 函数体内不应出现多余的 '}'（CardProps{...} 一个 + main{...} 一个）
	if strings.Contains(got, `"x")}}`) {
		t.Errorf("构造字面量后被吞入多余的 '}':\n%s", got)
	}
}

// ===== 第五轮：signal 字段 + JSX 统一语义回归 =====

// 2.1：组件体内 JSX 插值读 signal 字段 p.name 必须补 ()，
// 否则把 Signal 函数当值插入界面（编译通过但显示函数地址）。
func TestSignalFieldJSXSelectorReadAddsCall(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type CardProps struct { signal name string }
func Card(p CardProps) *gxscene.Instance { return <div>{p.name}</div> }
func main() {
	signal s = "x"
	_ = <div><Card name={s}/></div>
}
`
	got := compileOK(t, "sfjsx_sel.gx", src)
	containsAll(t, got, `return p.name()`, `Card(CardProps{name:s,})`)
}

// 2.2：signal 变量传给 signal 字段 → 直通 getter（保鲜），不再生成 title() 非法 Go。
func TestSignalFieldJSXPropPassthrough(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type CardProps struct { signal name string }
func Card(p CardProps) *gxscene.Instance { return nil }
func main() {
	signal title = "hello"
	_ = <div><Card name={title}/></div>
}
`
	got := compileOK(t, "sfjsx_pass.gx", src)
	containsAll(t, got, `Card(CardProps{name:title,})`)
	if strings.Contains(got, "name:title()") {
		t.Errorf("signal 应透传 getter 而非求值:\n%s", got)
	}
}

// 2.3：字面量传给 signal 字段 → 自动包 Signal.New，不再编译期类型错。
func TestSignalFieldJSXPropLiteralWrapped(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type CardProps struct { signal name string }
func Card(p CardProps) *gxscene.Instance { return nil }
func main() { _ = <div><Card name="lit"/></div> }
`
	got := compileOK(t, "sfjsx_lit.gx", src)
	containsAll(t, got, `Card(CardProps{name:Signal.New("lit"),})`)
}

// 2.5/2.6：<For> item 派生态传给 signal 字段 → 包 Signal.Computed(func() T {...})
// 保鲜，且该 For 仍走 ForGetterOfSignal 零重建（逃逸豁免语义成立）。
func TestSignalFieldJSXForItemDerivedComputed(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type User struct { id int; name string }
type CardProps struct { signal name string }
func Card(p CardProps) *gxscene.Instance { return nil }
func main() {
	signal users = []User{{1,"Tom"}}
	_ = <ul><For each={users} key={(u) => u.id}>{(u User) => <li><Card name={u.name}/></li>}</For></ul>
}
`
	got := compileOK(t, "sfjsx_for.gx", src)
	containsAll(t, got,
		`ForGetterOfSignal[User]`,
		`Signal.Computed(func() string { return u().name })`,
	)
}

// signal 字段之间透传：一个 signal 字段传给另一个组件的 signal 字段 →
// 直通 getter（不 New、不 Computed），与手写结构体赋值语义一致（用户要求）。
func TestSignalFieldJSXFieldToFieldPassthrough(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type CardProps struct { signal name string }
type WrapProps struct { signal inner string }
func Card(p CardProps) *gxscene.Instance { return <div>{p.name}</div> }
func Wrap(p WrapProps) *gxscene.Instance { return <div><Card name={p.inner}/></div> }
func main() { _ = <Wrap inner="hello"/> }
`
	got := compileOK(t, "sfjsx_ff.gx", src)
	containsAll(t, got,
		`Card(CardProps{name:p.inner,})`,
		`Wrap(WrapProps{inner:Signal.New("hello"),})`,
		`return p.name()`,
	)
}

// 第六轮 P2 回归：JSX selector 补 () 必须按 receiver 推断类型判定，不能全文件
// 按字段名猜。同文件里 CardProps 有 signal name、User 有普通 name 时，
// {u.name} 不得被补成 u.name()（那是硬编译错误：string is not a function）。
func TestJSXSelectorNotTouchSameNamePlainField(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type CardProps struct { signal name string }
type User struct { id int; name string }
func main() {
	u := User{1, "plain"}
	_ = <div>{u.name}</div>
}
`
	got := compileOK(t, "sfjsx_plain.gx", src)
	if strings.Contains(got, `u.name()`) {
		t.Errorf("User.name 是普通字段，不应补调用括号:\n%s", got)
	}
	containsAll(t, got, `return u.name`)
}

// 二级 selector 同样按 receiver 类型判定：signal 字段结构体嵌套的普通结构体
// 同名字段不得误补；而真正的 signal 字段 selector 仍要补 ()。
func TestJSXSelectorNestedFieldByReceiverType(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type Addr struct { city string }
type User struct { addr Addr }
type CardProps struct { signal name string }
func Card(p CardProps) *gxscene.Instance { return <div>{p.name}</div> }
func main() {
	u := User{Addr{"SZ"}}
	_ = <div>{u.addr.city}</div>
}
`
	got := compileOK(t, "sfjsx_nested.gx", src)
	if strings.Contains(got, `u.addr.city()`) {
		t.Errorf("User.addr.city 是普通字段，不应补调用括号:\n%s", got)
	}
	containsAll(t, got, `return p.name()`)
}

// 第六轮 P2b 回归：手写结构体构造里的派生表达式（含 signal 变量读取）必须包
// Signal.Computed 保鲜，与 JSX 侧 signalFieldPropTokens 语义一致。旧产物是
// Signal.New(fmt.Sprint(n()))（快照，n 后续变化不反映到字段）。
func TestSignalFieldHandwrittenDerivedUsesComputed(t *testing.T) {
	src := `package main
import "fmt"
type CardProps struct { signal name string }
func main() {
	signal n = 1
	p := CardProps{name: fmt.Sprint(n())}
	_ = p
}
`
	got := compileOK(t, "sfhw_derived.gx", src)
	containsAll(t, got, `Signal.Computed(func() string { return fmt.Sprint(n()) })`)
	if strings.Contains(got, `Signal.New(fmt.Sprint(n()))`) {
		t.Errorf("派生表达式不应退化为快照:\n%s", got)
	}
}

// 第七轮 §6a 回归：指针 props 组件（func Card(p *CardProps)）必须可用——
// 调用点生成 &CardProps{...}（而非匿名 struct），组件体内 signal 字段读补 ()。
func TestPointerPropsComponent(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type CardProps struct { signal name string }
func Card(p *CardProps) *gxscene.Instance { return <div>{p.name}</div> }
func main() { _ = <Card name="x"/> }
`
	got := compileOK(t, "sfptr.gx", src)
	containsAll(t, got,
		`Card(&CardProps{name:Signal.New("x"),})`,
		`return p.name()`,
	)
	if strings.Contains(got, `Card(struct{`) {
		t.Errorf("指针 props 不应回退匿名 struct:\n%s", got)
	}
}

// 第七轮 §5 回归：嵌套 signal 字段复合字面量必须生成合法 Go。
// 早期「全局替换 + 原始下标从后往前应用」在嵌套时外层端点错位，吞掉 ','/'}' 生成
// 非法代码。递归下降重写后，内层先就地处理，外层载荷基于已处理结果构造。
func TestSignalFieldNestedCompositeLiteralValid(t *testing.T) {
	src := `package main
type Inner struct { signal v string }
type Outer struct { signal inner Inner; signal other string }
func main() {
	signal n = "x"
	p := Outer{inner: Inner{v: n}, other: n}
	_ = p
}
`
	// compileOK 内含 Validate()：产物语法非法会直接 fail。
	// 第八轮规则：复合字面量 Inner{v: n} 不是纯字面量，用裸 getter
	// func(...Inner) Inner { return ... } 每次读重新构造（内部的 v 是活 getter），
	// 而非 Signal.New 快照。此处仍验证嵌套下标不漂移、产物合法。
	got := compileOK(t, "sfhw_nested.gx", src)
	containsAll(t, got, `inner: func(...Inner) Inner { return Inner{v: n} }`, `other: n`)
}

// 三层嵌套 + 混合字段，进一步压测递归下标的正确性。
func TestSignalFieldDeepNestedCompositeLiteralValid(t *testing.T) {
	src := `package main
type L3 struct { signal v string; plain int }
type L2 struct { signal inner L3; tag string }
type L1 struct { signal child L2; signal title string }
func main() {
	signal n = "x"
	p := L1{child: L2{inner: L3{v: n, plain: 1}, tag: "t"}, title: n}
	_ = p
}
`
	got := compileOK(t, "sfhw_deep.gx", src)
	containsAll(t, got, `child: func(...L2) L2 { return L2{inner: func(...L3) L3 { return L3{v: n, plain: 1} }, tag: "t"} }`)
}

// 第七轮 §4 回归：手写字段间赋值 p.a → q.a 必须直通裸 getter（保鲜），
// 不能包 Signal.New(p.a())（那会静默冻结，源 signal 后续变化不反映到字段）。
// 与 JSX 侧 TestSignalFieldJSXFieldToFieldPassthrough 对齐。
func TestSignalFieldHandwrittenFieldToFieldPassthrough(t *testing.T) {
	src := `package main
type P struct { signal a string }
type Q struct { signal a string }
func f(p P) Q { return Q{a: p.a} }
`
	got := compileOK(t, "sfhw_ff.gx", src)
	containsAll(t, got, `return Q{a: p.a}`)
	if strings.Contains(got, `Q{a: Signal.New(p.a())}`) || strings.Contains(got, `Q{a: p.a()}`) {
		t.Errorf("字段间赋值应直通 getter 保鲜，不得快照或二次调用:\n%s", got)
	}
}

// 字面量/普通常量表达式仍是 Signal.New 快照（与 JSX isPlainLiteralExpr 档一致）。
func TestSignalFieldHandwrittenLiteralStaysNew(t *testing.T) {
	src := `package main
type CardProps struct { signal name string }
func main() {
	p := CardProps{name: "lit"}
	_ = p
}
`
	got := compileOK(t, "sfhw_lit.gx", src)
	containsAll(t, got, `name: Signal.New("lit")`)
}

// ============ 第八轮回归 ============

// P1-1/P1-2 邻居用例：派生表达式里出现两个 signal 字段读，必须整体包 Computed，
// 且两个字段读都补 ()——不能只根据末尾 token 判定导致 p.b 丢 ()、也不能走 New 快照。
func TestSignalFieldTwoSignalReadsDerived(t *testing.T) {
	src := `package main
type P struct { signal a string; signal b string }
type Q struct { signal v string }
func main() {
	p := P{a: "x", b: "y"}
	q := Q{v: p.a + p.b}
	_ = q
}
`
	got := compileOK(t, "sf8_two.gx", src)
	containsAll(t, got, `v: Signal.Computed(func() string { return p.a() + p.b() })`)
	if strings.Contains(got, `return p.a() + p.b}`) {
		t.Errorf("末尾字段读 p.b 必须补 ():\n%s", got)
	}
}

// P1-3 邻居：链式 receiver + 同名字段不同类型——链尾按真实 receiver 类型判定，
// 链上普通字段不补 ()，链尾 signal 字段补 ()；同名非 signal 字段不被误伤。
func TestSignalFieldChainReceiverTyped(t *testing.T) {
	src := `package main
type Inner struct { signal v string }
type Mid struct { in Inner }
type Outer struct { mid Mid; v string }
func main() {
	o := Outer{mid: Mid{in: Inner{v: "x"}}, v: "plain"}
	println(o.mid.in.v, o.v)
}
`
	got := compileOK(t, "sf8_chain.gx", src)
	containsAll(t, got, `println(o.mid.in.v(), o.v)`)
	if strings.Contains(got, `o.v()`) {
		t.Errorf("Outer.v 是普通字段，不得补 ()：\n%s", got)
	}
}

// 用户核心规则：普通变量 / 普通字段作为 signal 字段的值 → 裸 getter func(...T) T，
// 每次读重新求值；Signal.New 快照会在变量后续改动时冻结（错误）。
func TestSignalFieldPlainVarUsesGetter(t *testing.T) {
	src := `package main
type Q struct { title string }
type P struct { signal a string; signal b string }
func main() {
	plain := "x"
	q := Q{title: "y"}
	p := P{a: plain, b: q.title}
	_ = p
}
`
	got := compileOK(t, "sf8_plain.gx", src)
	containsAll(t, got,
		`a: func(...string) string { return plain }`,
		`b: func(...string) string { return q.title }`,
	)
	if strings.Contains(got, `Signal.New(plain)`) || strings.Contains(got, `Signal.New(q.title)`) {
		t.Errorf("普通变量/字段不得用 Signal.New 快照：\n%s", got)
	}
}

// 普通变量 vs signal 读分档：字面量仍 New、signal 派生仍 Computed、普通变量裸 getter。
func TestSignalFieldThreeTiersCoexist(t *testing.T) {
	src := `package main
type P struct { signal a string; signal b string; signal c string }
func main() {
	signal n = "n"
	plain := "p"
	p := P{a: "lit", b: n + "!", c: plain}
	_ = p
}
`
	got := compileOK(t, "sf8_tiers.gx", src)
	containsAll(t, got,
		`a: Signal.New("lit")`,
		`b: Signal.Computed(func() string { return n() + "!" })`,
		`c: func(...string) string { return plain }`,
	)
}

// P2-1 邻居：&{...} 省略类型名 + 取址，指针 props 补全单个 &、signal 字段透传 getter。
func TestSignalFieldAddrOmittedTypeLit(t *testing.T) {
	src := `package main
type CardProps struct { signal name string; title string }
func Card(p *CardProps) { println(p.name(), p.title) }
func main() {
	signal n = "nn"
	Card(&{name: n, title: "t"})
}
`
	got := compileOK(t, "sf8_addr.gx", src)
	containsAll(t, got, `Card(&CardProps{name: n, title: "t"})`)
	if strings.Contains(got, "&&") {
		t.Errorf("不得生成双取址 &&：\n%s", got)
	}
}

// P2-2 邻居：map value / slice 元素字面量的期望类型下推 + signal 字段包装。
func TestSignalFieldContainerElemExpectedType(t *testing.T) {
	src := `package main
type P struct { signal a string }
func main() {
	signal n = "x"
	m := map[string]P{"k": {a: n}}
	s := []P{{a: n}, {a: "lit"}}
	_, _ = m, s
}
`
	got := compileOK(t, "sf8_container.gx", src)
	compact := strings.Join(strings.Fields(got), "")
	containsAll(t, compact,
		`m:=map[string]P{"k":P{a:n}}`,
		`s:=list.List[P]{P{a:n},P{a:Signal.New("lit")}}`, // []P 经集合提升为 list.List[P]
	)
	if strings.Contains(compact, `struct{a any}`) {
		t.Errorf("容器元素不应退化为匿名 struct：\n%s", got)
	}
}

// P2-3 邻居：显式类型名的指针 props 补全 &。
func TestSignalFieldPtrExplicitLitGetsAmp(t *testing.T) {
	src := `package main
type CardProps struct { signal name string }
func Card(p *CardProps) { println(p.name()) }
func main() {
	signal n = "nn"
	Card(CardProps{name: n})
}
`
	got := compileOK(t, "sf8_ptrexplicit.gx", src)
	containsAll(t, got, `Card(&CardProps{name: n})`)
}

// P3-1 邻居：普通变量直通档保留尾随注释。
func TestSignalFieldPassthroughKeepsTrailingComment(t *testing.T) {
	src := `package main
type P struct { signal a string }
func main() {
	signal src = "x"
	p := P{a: src /* note */}
	_ = p
}
`
	got := compileOK(t, "sf8_comment.gx", src)
	containsAll(t, got, `p := P{a: src /* note */}`)
}

// 第八轮用户补充：链条 a.b.c 的"中间段" b 若是 signal 也要加括号。
// 旧实现只判链尾，导致 o.mid.b.c（b 是 signal）生成非法 Go。
func TestSignalFieldChainMiddleSegmentSignal(t *testing.T) {
	src := `package main
type Inner struct { c string; signal d string }
type Mid struct { signal b Inner }
type Outer struct { mid Mid; v string }
func main() {
	o := Outer{mid: Mid{b: Inner{c: "x", d: "y"}}, v: "plain"}
	println(o.mid.b.c, o.mid.b.d, o.v)
}
`
	got := compileOK(t, "sf8_mid.gx", src)
	containsAll(t, got, `println(o.mid.b().c, o.mid.b().d(), o.v)`)
	if strings.Contains(got, `o.mid.b.c`) {
		t.Errorf("中间 signal 段 b 必须补 ():\n%s", got)
	}
	if strings.Contains(got, `o.v()`) {
		t.Errorf("普通字段 o.v 不得补 ():\n%s", got)
	}
}

// 链式两段都是 signal + 已是调用式：每段各补一次，不重复补（防 c.n()()）。
func TestSignalFieldChainTwoSignalNoDoubleCall(t *testing.T) {
	src := `package main
type Leaf struct { signal v string }
type Mid struct { signal b Leaf }
type Outer struct { mid Mid }
func main() {
	o := Outer{mid: Mid{b: Leaf{v: "hi"}}}
	println(o.mid.b().v(), o.mid.b.v)
}
`
	got := compileOK(t, "sf8_twochain.gx", src)
	gotCompact := strings.Join(strings.Fields(got), "")
	containsAll(t, gotCompact,
		`println(o.mid.b().v(),o.mid.b().v())`,
	)
	if strings.Contains(gotCompact, `()()`) {
		t.Errorf("不得出现重复调用 ()()：\n%s", got)
	}
}

// ============ 第九轮回归：链中间含 signal 段作为表达式 → 读写合一 getter ============

// 链中间有 signal 段 + 链尾 signal（手写结构体字段值）：必须生成读写合一 lambda，
// 每次读写重新走链——旧的「剥 () 裸透传」会把中间 signal 段的 receiver 固定，b 变化后腐烂。
func TestSignalFieldChainRWGetterHandwritten(t *testing.T) {
	src := `package main
type Leaf struct { signal c string }
type Mid struct { signal b Leaf }
type Outer struct { mid Mid }
type Q struct { signal v string }
func main() {
	o := Outer{mid: Mid{b: Leaf{c: "x"}}}
	q := Q{v: o.mid.b.c}
	_ = q; _ = o
}
`
	got := compileOK(t, "sf9_rw.gx", src)
	containsAll(t, got,
		`func(arg ...string) string`,
		`if len(arg) > 0`,
		`return o.mid.b().c(arg...)`,
		`return o.mid.b().c()`,
	)
}

// 同上，JSX signal prop 走同一入口，产物一致（判据不分裂）。
func TestSignalFieldChainRWGetterJSX(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type Leaf struct { signal c string }
type Mid struct { signal b Leaf }
type Outer struct { mid Mid }
type CardProps struct { signal title string }
func Card(p CardProps) *gxscene.Instance { return <div>{p.title}</div> }
func main() {
	o := Outer{mid: Mid{b: Leaf{c: "x"}}}
	_ = <Card title={o.mid.b.c}/>
	_ = o
}
`
	got := compileOK(t, "sf9_rwjsx.gx", src)
	containsAll(t, got,
		`func(arg ...string) string`,
		`if len(arg) > 0`,
		`return o.mid.b().c(arg...)`,
		`return o.mid.b().c()`,
	)
}

// 链中间有 signal 段 + 链尾普通字段：退回裸 getter（无写分支），不生成 Computed 快照。
func TestSignalFieldChainGetterTailPlain(t *testing.T) {
	src := `package main
type Leaf struct { c string }
type Mid struct { signal b Leaf }
type Outer struct { mid Mid }
type Q struct { signal v string }
func main() {
	o := Outer{mid: Mid{b: Leaf{c: "x"}}}
	q := Q{v: o.mid.b.c}
	_ = q; _ = o
}
`
	got := compileOK(t, "sf9_tailplain.gx", src)
	containsAll(t, got, `func(...string) string { return o.mid.b().c }`)
	if strings.Contains(got, `Signal.Computed(func() string { return o.mid.b().c })`) {
		t.Errorf("链尾普通字段应走裸 getter，不生成 Computed 快照:\n%s", got)
	}
}

// 单段链（仅链尾 signal）保持裸透传、零开销，不包 lambda。
func TestSignalFieldChainSingleStillPassthrough(t *testing.T) {
	src := `package main
type Leaf struct { signal c string }
type Q struct { signal v string }
func main() {
	o := Leaf{c: "x"}
	q := Q{v: o.c}
	_ = q; _ = o
}
`
	got := compileOK(t, "sf9_single.gx", src)
	containsAll(t, got, `q := Q{v: o.c}`)
	if strings.Contains(got, `func(arg ...string) string`) {
		t.Errorf("单段链不应包读写 lambda:\n%s", got)
	}
}

// 派生表达式里即使含链式 signal 读，也不得被误判成单条链读（完整性校验）。
func TestSignalFieldChainGetterRejectsDerivedExpr(t *testing.T) {
	src := `package main
type Leaf struct { signal c string }
type Mid struct { signal b Leaf }
type Outer struct { mid Mid }
type Q struct { signal v string }
func main() {
	o := Outer{mid: Mid{b: Leaf{c: "x"}}}
	q := Q{v: o.mid.b().c() + "!"}
	_ = q; _ = o
}
`
	got := compileOK(t, "sf9_derived.gx", src)
	containsAll(t, got, `Signal.Computed(func() string { return o.mid.b().c() + "!" })`)
	if strings.Contains(got, `func(arg ...string) string`) {
		t.Errorf("派生表达式不得走读写 lambda:\n%s", got)
	}
}
