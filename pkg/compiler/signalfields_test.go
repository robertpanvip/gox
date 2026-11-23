package compiler

import (
	"regexp"
	"strings"
	"testing"
)

func TestSignalFieldDeclDesugar(t *testing.T) {
	// struct 内 sig 字段 → Signal.Signal[T] 字段，普通字段不变，注入 import。
	src := `package main
type CardProps struct {
    sig name string
    sig count int
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
	if strings.Contains(got, "var name any") || strings.Contains(got, "sig name") {
		t.Errorf("sig 字段不应残留旧降级形态:\n%s", got)
	}
}

func TestSignalFieldReadRewrite(t *testing.T) {
	// 读 p.name → p.name()
	src := `package main
type CardProps struct { sig name string }
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
type CardProps struct { sig name string }
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
type Counter struct { sig n int }
func main() { c := Counter{n: 1}; c.n += 2; c.n++ }
`
	got := compileOK(t, "sf4.gx", src)
	gotCompact := strings.Join(strings.Fields(got), "")
	containsAll(t, gotCompact, `c.n(c.n()+2)`, `c.n(c.n()+1)`)
}

func TestSignalFieldConstructWrap(t *testing.T) {
	// 构造位置：字面量值包 Signal.New，普通字段不动。
	src := `package main
type CardProps struct { sig name string; age int }
func main() { p := CardProps{name: "x", age: 3}; _ = p }
`
	got := compileOK(t, "sf5.gx", src)
	containsAll(t, got, `name: Signal.New("x")`, `age: 3`)
	if strings.Contains(got, `age: Signal.New`) {
		t.Errorf("普通字段不应被包装: %s", got)
	}
}

func TestSignalFieldPassthroughKeepsLiveSignal(t *testing.T) {
	// 保鲜关键：sig 变量 src 传给 sig 字段时，传 getter 本身（src），
	// 不能是 Signal.New(src())（那会快照当前值、断开后续更新）。
	src := `package main
type CardProps struct { sig name string }
func main() { sig src = "x"; p := CardProps{name: src}; _ = p }
`
	got := compileOK(t, "sf6.gx", src)
	containsAll(t, got, `var src = Signal.New("x")`, `name: src`)
	if strings.Contains(got, `name: Signal.New(src())`) || strings.Contains(got, `name: src()`) {
		t.Errorf("sig 变量应直通 getter 本身（保鲜），产物: %s", got)
	}
}

func TestSignalFieldParamReceiver(t *testing.T) {
	// 函数参数（组件函数）receiver 也能解包 —— 组件值保鲜的核心场景。
	src := `package main
type CardProps struct { sig name string }
func show(p CardProps) string { return p.name }
func main() { _ = show }
`
	got := compileOK(t, "sf7.gx", src)
	containsAll(t, got, `return p.name()`)
}

func TestSignalFieldDoesNotTouchPlainFields(t *testing.T) {
	// 无 sig 字段的 struct：所有字段/读写保持原样（零回归）。
	src := `package main
type Point struct { x int; y int }
func main() { p := Point{x: 1, y: 2}; println(p.x, p.y) }
`
	got := compileOK(t, "sf8.gx", src)
	containsAll(t, got, `x int`, `p.x`, `p.y`)
	if strings.Contains(got, "Signal") {
		t.Errorf("无 sig 字段不应注入 Signal: %s", got)
	}
}

func TestSignalFieldSelectorShadowNotTouched(t *testing.T) {
	// 不同 struct 有同名普通字段：不属于 sig 字段表，不得误改。
	src := `package main
type Box struct { sig value int }
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
	// struct sig 字段与局部 sig 变量共存，互不干扰。
	src := `package main
type Box struct { sig value int }
func main() {
    sig local = 5
    b := Box{value: 1}
    b.value = local
    println(b.value, local)
}
`
	got := compileOK(t, "sf10.gx", src)
	containsAll(t, got, `var local = Signal.New(5)`, `value: Signal.New(1)`, `b.value(`)
	if !strings.Contains(got, `local()`) {
		t.Errorf("局部 sig 读取应改写为 local(): %s", got)
	}
}

func TestSignalFieldConstructLiteralNoSwallowedBrace(t *testing.T) {
	// 字段值走字面量档（生成 Signal.New("x")）：不得把复合字面量的 '}' 吞进值区间
	// （曾因 prevSigIn 边界越界导致产物多一个 '}'）。底层 API 已禁止手写，故用
	// 字面量触发同一生成形态，继续守住这条边界。
	src := `package main
type CardProps struct { sig name string }
func main() { p := CardProps{name: "x"}; println(p.name) }
`
	got := compileOK(t, "sf11.gx", src)
	containsAll(t, got, `name: Signal.New("x")`, `println(p.name())`)
	// 函数体内不应出现多余的 '}'（CardProps{...} 一个 + main{...} 一个）
	if strings.Contains(got, `"x")}}`) {
		t.Errorf("构造字面量后被吞入多余的 '}':\n%s", got)
	}
}

// ===== 第五轮：sig 字段 + JSX 统一语义回归 =====

// 2.1：组件体内 JSX 插值读 sig 字段 p.name 必须补 ()，
// 否则把 Signal 函数当值插入界面（编译通过但显示函数地址）。
func TestSignalFieldJSXSelectorReadAddsCall(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type CardProps struct { sig name string }
func Card(p CardProps) *gxscene.Instance { return <div>{p.name}</div> }
func main() {
	sig s = "x"
	_ = <div><Card name={s}/></div>
}
`
	got := compileOK(t, "sfjsx_sel.gx", src)
	containsAll(t, got, `return p.name()`, `Card(CardProps{name:s,})`)
}

// 2.2：sig 变量传给 sig 字段 → 直通 getter（保鲜），不再生成 title() 非法 Go。
func TestSignalFieldJSXPropPassthrough(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type CardProps struct { sig name string }
func Card(p CardProps) *gxscene.Instance { return nil }
func main() {
	sig title = "hello"
	_ = <div><Card name={title}/></div>
}
`
	got := compileOK(t, "sfjsx_pass.gx", src)
	containsAll(t, got, `Card(CardProps{name:title,})`)
	if strings.Contains(got, "name:title()") {
		t.Errorf("sig 应透传 getter 而非求值:\n%s", got)
	}
}

// 2.3：字面量传给 sig 字段 → 自动包 Signal.New，不再编译期类型错。
func TestSignalFieldJSXPropLiteralWrapped(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type CardProps struct { sig name string }
func Card(p CardProps) *gxscene.Instance { return nil }
func main() { _ = <div><Card name="lit"/></div> }
`
	got := compileOK(t, "sfjsx_lit.gx", src)
	containsAll(t, got, `Card(CardProps{name:Signal.New("lit"),})`)
}

// 2.5/2.6：<For> item 派生态传给 sig 字段 → 包 Signal.Computed(func() T {...})
// 保鲜，且该 For 仍走 ForGetterOfSignal 零重建（逃逸豁免语义成立）。
func TestSignalFieldJSXForItemDerivedComputed(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type User struct { id int; name string }
type CardProps struct { sig name string }
func Card(p CardProps) *gxscene.Instance { return nil }
func main() {
	sig users = []User{{1,"Tom"}}
	_ = <ul><For each={users} key={(u) => u.id}>{(u User) => <li><Card name={u.name}/></li>}</For></ul>
}
`
	got := compileOK(t, "sfjsx_for.gx", src)
	containsAll(t, got,
		`ForGetterOfSignal[User]`,
		`Signal.Computed(func() string { return u().name })`,
	)
}

// sig 字段之间透传：一个 sig 字段传给另一个组件的 sig 字段 →
// 直通 getter（不 New、不 Computed），与手写结构体赋值语义一致（用户要求）。
func TestSignalFieldJSXFieldToFieldPassthrough(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type CardProps struct { sig name string }
type WrapProps struct { sig inner string }
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
// 按字段名猜。同文件里 CardProps 有 sig name、User 有普通 name 时，
// {u.name} 不得被补成 u.name()（那是硬编译错误：string is not a function）。
func TestJSXSelectorNotTouchSameNamePlainField(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type CardProps struct { sig name string }
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

// 二级 selector 同样按 receiver 类型判定：sig 字段结构体嵌套的普通结构体
// 同名字段不得误补；而真正的 sig 字段 selector 仍要补 ()。
func TestJSXSelectorNestedFieldByReceiverType(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type Addr struct { city string }
type User struct { addr Addr }
type CardProps struct { sig name string }
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

// 第六轮 P2b 回归：手写结构体构造里的派生表达式（含 sig 变量读取）必须包
// Signal.Computed 保鲜，与 JSX 侧 signalFieldPropTokens 语义一致。旧产物是
// Signal.New(fmt.Sprint(n()))（快照，n 后续变化不反映到字段）。
func TestSignalFieldHandwrittenDerivedUsesComputed(t *testing.T) {
	src := `package main
import "fmt"
type CardProps struct { sig name string }
func main() {
	sig n = 1
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
// 调用点生成 &CardProps{...}（而非匿名 struct），组件体内 sig 字段读补 ()。
func TestPointerPropsComponent(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type CardProps struct { sig name string }
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

// 第七轮 §5 回归：嵌套 sig 字段复合字面量必须生成合法 Go。
// 早期「全局替换 + 原始下标从后往前应用」在嵌套时外层端点错位，吞掉 ','/'}' 生成
// 非法代码。递归下降重写后，内层先就地处理，外层载荷基于已处理结果构造。
func TestSignalFieldNestedCompositeLiteralValid(t *testing.T) {
	src := `package main
type Inner struct { sig v string }
type Outer struct { sig inner Inner; sig other string }
func main() {
	sig n = "x"
	p := Outer{inner: Inner{v: n}, other: n}
	_ = p
}
`
	// compileOK 内含 Validate()：产物语法非法会直接 fail。
	// 第九轮 P1-A：复合构造 Inner{v: n} 用 Signal.New 一次构造（可写、可响应），
	// 不再包成「每次读重建」的裸 getter。此处验证嵌套下标不漂移、产物合法。
	got := compileOK(t, "sfhw_nested.gx", src)
	containsAll(t, got, `inner: Signal.New(Inner{v: n})`, `other: n`)
}

// 三层嵌套 + 混合字段，进一步压测递归下标的正确性。
func TestSignalFieldDeepNestedCompositeLiteralValid(t *testing.T) {
	src := `package main
type L3 struct { sig v string; plain int }
type L2 struct { sig inner L3; tag string }
type L1 struct { sig child L2; sig title string }
func main() {
	sig n = "x"
	p := L1{child: L2{inner: L3{v: n, plain: 1}, tag: "t"}, title: n}
	_ = p
}
`
	got := compileOK(t, "sfhw_deep.gx", src)
	containsAll(t, got, `child: Signal.New(L2{inner: Signal.New(L3{v: n, plain: 1}), tag: "t"})`)
}

// 第七轮 §4 回归：手写字段间赋值 p.a → q.a 必须直通裸 getter（保鲜），
// 不能包 Signal.New(p.a())（那会静默冻结，源 sig 后续变化不反映到字段）。
// 与 JSX 侧 TestSignalFieldJSXFieldToFieldPassthrough 对齐。
func TestSignalFieldHandwrittenFieldToFieldPassthrough(t *testing.T) {
	src := `package main
type P struct { sig a string }
type Q struct { sig a string }
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
type CardProps struct { sig name string }
func main() {
	p := CardProps{name: "lit"}
	_ = p
}
`
	got := compileOK(t, "sfhw_lit.gx", src)
	containsAll(t, got, `name: Signal.New("lit")`)
}

// ============ 第八轮回归 ============

// P1-1/P1-2 邻居用例：派生表达式里出现两个 sig 字段读，必须整体包 Computed，
// 且两个字段读都补 ()——不能只根据末尾 token 判定导致 p.b 丢 ()、也不能走 New 快照。
func TestSignalFieldTwoSignalReadsDerived(t *testing.T) {
	src := `package main
type P struct { sig a string; sig b string }
type Q struct { sig v string }
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
// 链上普通字段不补 ()，链尾 sig 字段补 ()；同名非 sig 字段不被误伤。
func TestSignalFieldChainReceiverTyped(t *testing.T) {
	src := `package main
type Inner struct { sig v string }
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

// 第十轮 P1-2：普通变量 / 普通字段作为 sig 字段的值 → Signal.LockedGetter，
// 读跟随、写落回变量（跟随语义不变），外层实例级互斥锁消除裸变量访问竞态。
func TestSignalFieldPlainVarUsesGetter(t *testing.T) {
	src := `package main
type Q struct { title string }
type P struct { sig a string; sig b string }
func main() {
	plain := "x"
	q := Q{title: "y"}
	p := P{a: plain, b: q.title}
	_ = p
}
`
	got := compileOK(t, "sf8_plain.gx", src)
	containsAll(t, got, `Signal.LockedGetterShared("sf:`)
	gotCompact := strings.Join(strings.Fields(got), "")
	containsAll(t, gotCompact,
		`func()string{returnplain}`,
		`func(__vstring){plain=__v}`,
		`func()string{returnq.title}`,
		`func(__vstring){q.title=__v}`,
	)
	if strings.Contains(got, `Signal.New(plain)`) || strings.Contains(got, `Signal.New(q.title)`) {
		t.Errorf("普通变量/字段应保持跟随语义，不得用 Signal.New 快照：\n%s", got)
	}
}

// 普通变量 vs sig 读分档：字面量 New、sig 派生 Computed、普通变量 LockedGetter。
func TestSignalFieldThreeTiersCoexist(t *testing.T) {
	src := `package main
type P struct { sig a string; sig b string; sig c string }
func main() {
	sig n = "n"
	plain := "p"
	p := P{a: "lit", b: n + "!", c: plain}
	_ = p
}
`
	got := compileOK(t, "sf8_tiers.gx", src)
	containsAll(t, got,
		`a: Signal.New("lit")`,
		`b: Signal.Computed(func() string { return n() + "!" })`,
	)
	containsAll(t, strings.Join(strings.Fields(got), ""),
		`c:Signal.LockedGetterShared("sf:`,
		`func()string{returnplain}`,
		`func(__vstring){plain=__v}`,
	)
}

// P2-1 邻居：&{...} 省略类型名 + 取址，指针 props 补全单个 &、sig 字段透传 getter。
func TestSignalFieldAddrOmittedTypeLit(t *testing.T) {
	src := `package main
type CardProps struct { sig name string; title string }
func Card(p *CardProps) { println(p.name(), p.title) }
func main() {
	sig n = "nn"
	Card(&{name: n, title: "t"})
}
`
	got := compileOK(t, "sf8_addr.gx", src)
	containsAll(t, got, `Card(&CardProps{name: n, title: "t"})`)
	if strings.Contains(got, "&&") {
		t.Errorf("不得生成双取址 &&：\n%s", got)
	}
}

// P2-2 邻居：map value / slice 元素字面量的期望类型下推 + sig 字段包装。
func TestSignalFieldContainerElemExpectedType(t *testing.T) {
	src := `package main
type P struct { sig a string }
func main() {
	sig n = "x"
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
type CardProps struct { sig name string }
func Card(p *CardProps) { println(p.name()) }
func main() {
	sig n = "nn"
	Card(CardProps{name: n})
}
`
	got := compileOK(t, "sf8_ptrexplicit.gx", src)
	containsAll(t, got, `Card(&CardProps{name: n})`)
}

// P3-1 邻居：普通变量直通档保留尾随注释。
func TestSignalFieldPassthroughKeepsTrailingComment(t *testing.T) {
	src := `package main
type P struct { sig a string }
func main() {
	sig src = "x"
	p := P{a: src /* note */}
	_ = p
}
`
	got := compileOK(t, "sf8_comment.gx", src)
	containsAll(t, got, `p := P{a: src /* note */}`)
}

// 第八轮用户补充：链条 a.b.c 的"中间段" b 若是 sig 也要加括号。
// 旧实现只判链尾，导致 o.mid.b.c（b 是 sig）生成非法 Go。
func TestSignalFieldChainMiddleSegmentSignal(t *testing.T) {
	src := `package main
type Inner struct { c string; sig d string }
type Mid struct { sig b Inner }
type Outer struct { mid Mid; v string }
func main() {
	o := Outer{mid: Mid{b: Inner{c: "x", d: "y"}}, v: "plain"}
	println(o.mid.b.c, o.mid.b.d, o.v)
}
`
	got := compileOK(t, "sf8_mid.gx", src)
	containsAll(t, got, `println(o.mid.b().c, o.mid.b().d(), o.v)`)
	if strings.Contains(got, `o.mid.b.c`) {
		t.Errorf("中间 sig 段 b 必须补 ():\n%s", got)
	}
	if strings.Contains(got, `o.v()`) {
		t.Errorf("普通字段 o.v 不得补 ():\n%s", got)
	}
}

// 链式两段都是 sig + 已是调用式：每段各补一次，不重复补（防 c.n()()）。
func TestSignalFieldChainTwoSignalNoDoubleCall(t *testing.T) {
	src := `package main
type Leaf struct { sig v string }
type Mid struct { sig b Leaf }
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

// ============ 第九轮回归：链中间含 sig 段作为表达式 → 读写合一 getter ============

// 链中间有 sig 段 + 链尾 sig（手写结构体字段值）：必须生成读写合一 lambda，
// 每次读写重新走链——旧的「剥 () 裸透传」会把中间 sig 段的 receiver 固定，b 变化后腐烂。
func TestSignalFieldChainRWGetterHandwritten(t *testing.T) {
	src := `package main
type Leaf struct { sig c string }
type Mid struct { sig b Leaf }
type Outer struct { mid Mid }
type Q struct { sig v string }
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

// 同上，JSX sig prop 走同一入口，产物一致（判据不分裂）。
func TestSignalFieldChainRWGetterJSX(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type Leaf struct { sig c string }
type Mid struct { sig b Leaf }
type Outer struct { mid Mid }
type CardProps struct { sig title string }
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

// 链中间有 sig 段 + 链尾普通字段：退回裸 getter（无写分支），不生成 Computed 快照。
func TestSignalFieldChainGetterTailPlain(t *testing.T) {
	src := `package main
type Leaf struct { c string }
type Mid struct { sig b Leaf }
type Outer struct { mid Mid }
type Q struct { sig v string }
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

// 单段链（仅链尾 sig）保持裸透传、零开销，不包 lambda。
func TestSignalFieldChainSingleStillPassthrough(t *testing.T) {
	src := `package main
type Leaf struct { sig c string }
type Q struct { sig v string }
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

// 派生表达式里即使含链式 sig 读，也不得被误判成单条链读（完整性校验）。
func TestSignalFieldChainGetterRejectsDerivedExpr(t *testing.T) {
	src := `package main
type Leaf struct { sig c string }
type Mid struct { sig b Leaf }
type Outer struct { mid Mid }
type Q struct { sig v string }
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

// ==================== 第十轮评审回归 ====================

// P1-A：sig 字段值是结构体构造时必须 Signal.New（一次构造、可写），
// 不能包成忽略参数的裸 getter（写入丢失、依赖链断、每次读重建）。
func TestR10StructLitUsesNewNotBareGetter(t *testing.T) {
	src := `package main
type Leaf struct { sig v string }
type Mid struct { sig leaf Leaf }
func main() {
	m := Mid{leaf: Leaf{v: "a"}}
	_ = m
}
`
	got := compileOK(t, "r10_structlit.gx", src)
	containsAll(t, got, `leaf: Signal.New(Leaf{v: Signal.New("a")})`)
	if strings.Contains(got, `func(...Leaf) Leaf`) {
		t.Errorf("结构体构造不得包成裸 getter:\n%s", got)
	}
}

// 第十轮 P1-2：普通变量构造的 sig 字段用 Signal.LockedGetter——跟随+写落回变量，
// 外层实例级互斥锁消除裸变量读写竞态。
func TestR10PlainVarGetterWritable(t *testing.T) {
	src := `package main
type P struct { sig a string }
func main() {
	v := "one"
	p := P{a: v}
	p.a = "two"
	_ = p; _ = v
}
`
	got := compileOK(t, "r10_writable.gx", src)
	containsAll(t, got,
		`a: Signal.LockedGetterShared("sf:`,
		`func() string { return v }`,
		`func(__v string) { v = __v }`,
	)
}

// P2-A：局部同名变量 Signal 的字段访问按绑定放行，不算手写底层 API。
func TestR10LocalSignalVarFieldAllowed(t *testing.T) {
	src := `package main
import "fmt"
type Holder struct { New string }
func main() {
	Signal := Holder{New: "v"}
	fmt.Println(Signal.New)
}
`
	_ = compileOK(t, "r10_local.gx", src)
}

// P2-B：从 map/slice 索引取值后读 sig 字段必须补 ()。
func TestR10IndexReceiverRead(t *testing.T) {
	src := `package main
import "fmt"
type P struct { sig a string }
func main() {
	sig n = "x"
	m := map[string]P{"k": {a: n}}
	s := []P{{a: n}}
	fmt.Println(m["k"].a, s[0].a)
}
`
	got := compileOK(t, "r10_index.gx", src)
	containsAll(t, got, `m["k"].a()`, `s[0].a()`)
}

// P2-C：嵌套容器 map[string][]P 的元素补全为具名 P，不退化匿名 struct。
func TestR10NestedContainerElemType(t *testing.T) {
	src := `package main
type P struct { sig a string }
func main() {
	sig n = "x"
	m := map[string][]P{"k": {{a: n}}}
	_ = m
}
`
	got := compileOK(t, "r10_nested_container.gx", src)
	containsAll(t, got, `P{a: n}`)
	if strings.Contains(got, `struct{`) {
		t.Errorf("嵌套容器元素不得退化匿名 struct:\n%s", got)
	}
}

// P2-D：&{...} 内层省略类型字面量补全为具名类型 + sig 字段包 New。
func TestR10AddrLitNestedFieldType(t *testing.T) {
	src := `package main
type Inner struct { sig v string }
type Out struct { inner Inner }
func Card(p *Out) {}
func main() { Card(&{inner: {v: "x"}}) }
`
	got := compileOK(t, "r10_addr_nested.gx", src)
	containsAll(t, got, `&Out{inner: Inner{v: Signal.New("x")}}`)
}

// 第十一轮：中间 sig 段用户显式写调用括号（m.leaf().v）时，读写改写必须
// 跳过该括号走到真正链尾，而不是在 '(' 处断链漏改写。与裸写 m.leaf.v 产物一致。
func TestR11ExplicitParenMiddleSegment(t *testing.T) {
	write := `package main
type Leaf struct { sig v string }
type Mid struct { sig leaf Leaf }
func main() {
	m := Mid{leaf: Leaf{v: "a"}}
	m.leaf().v = "c"
	_ = m
}
`
	got := compileOK(t, "r11_write.gx", write)
	containsAll(t, got, `m.leaf().v( "c")`)

	bare := `package main
type Leaf struct { sig v string }
type Mid struct { sig leaf Leaf }
func main() {
	m := Mid{leaf: Leaf{v: "a"}}
	m.leaf.v = "c"
	_ = m
}
`
	gotBare := compileOK(t, "r11_bare.gx", bare)
	if !strings.Contains(gotBare, `m.leaf().v( "c")`) {
		t.Errorf("裸写链尾写产物异常:\n%s", gotBare)
	}

	compound := `package main
type Leaf struct { sig n int }
type Mid struct { sig leaf Leaf }
func main() {
	m := Mid{leaf: Leaf{n: 1}}
	m.leaf().n += 2
	_ = m
}
`
	gotC := compileOK(t, "r11_compound.gx", compound)
	containsAll(t, gotC, `m.leaf().n(m.leaf().n() +  2)`)
	if strings.Contains(gotC, `()()`) {
		t.Errorf("复合写不得产生重复调用:\n%s", gotC)
	}
}

// 第十一轮：JSX props / 插值里的显式括号中间段同样走到链尾。
func TestR11JSXExplicitParenMiddleSegment(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type Leaf struct { sig v string }
type Mid struct { sig leaf Leaf }
type CardProps struct { sig title string }
func Card(p CardProps) *gxscene.Instance { return <div>{p.title}</div> }
func main() {
	m := Mid{leaf: Leaf{v: "a"}}
	_ = <Card title={m.leaf().v}/>
	_ = <div>{m.leaf().v}</div>
	_ = m
}
`
	got := compileOK(t, "r11_jsx.gx", src)
	containsAll(t, got,
		`return m.leaf().v(arg...)`,
		`return m.leaf().v()`,
	)
}

// 第十轮 P1-3：工厂函数返回的 struct，其 sig 字段读写必须正确改写。
// p := makeP() 时 Bindings 用 FuncResult 回填 p 的类型为 P，p.a 才能识别为 sig 字段。
func TestR11FactoryResultTypeBackfill(t *testing.T) {
	src := `package main
import "fmt"
type P struct { sig a string }
func makeP(v string) P { return P{a: v} }
func makeP2(v string) *P { return &P{a: v} }
func main() {
	p := makeP("x")
	q := makeP2("y")
	fmt.Println(p.a, q.a)
	p.a = "z"
}
`
	got := compileOK(t, "r11_factory.gx", src)
	containsAll(t, got, `p.a()`, `q.a()`)
	compact := strings.Join(strings.Fields(got), "")
	containsAll(t, compact, `p.a("z")`)
}

// 第十轮 P1-4：多层索引 m["k"][0][0].a / s[0][0].a 必须逐层推进元素类型并补 ()。
func TestR11MultiLevelIndexChain(t *testing.T) {
	src := `package main
import "fmt"
type P struct { sig a string }
func main() {
	sig n = "x"
	m := map[string][][]P{"k": {{{a: n}}}}
	s := [][]P{{{a: n}}}
	fmt.Println(m["k"][0][0].a, s[0][0].a)
}
`
	got := compileOK(t, "r11_multi_index.gx", src)
	containsAll(t, got, `m["k"][0][0].a()`, `s[0][0].a()`)
}

// 第十轮 P1-5：JSX props 里索引 receiver m["k"].v 必须作为 sig 透传（不双重包装）。
func TestR11JSXIndexReceiver(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type P struct { sig v string }
type CardProps struct { sig v string }
func Card(props CardProps) *gxscene.Instance { return nil }
func main() {
	m := map[string]P{"k": P{v: "x"}}
	_ = <div><Card v={m["k"].v}/></div>
}
`
	res, err := CompileScene("r11_jsx_index.gx", src)
	if err != nil { t.Fatal(err) }
	out := res.GoSource
	// 透传档：m["k"].v 本身是 sig，应作为 getter 透传，不得双重包 Signal.New。
	if strings.Contains(out, `Signal.New(m["k"].v)`) {
		t.Errorf("索引 receiver 被双重包装：\n%s", out)
	}
	containsAll(t, out, `m["k"].v`)
}

// 第十轮 P1-4+P1-5：JSX props 里多层索引 receiver 也应正确透传。
func TestR11JSXMultiLevelIndexReceiver(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type P struct { sig v string }
type CardProps struct { sig v string }
func Card(props CardProps) *gxscene.Instance { return nil }
func main() {
	m := map[string][][]P{"k": {{{v: "x"}}}}
	_ = <div><Card v={m["k"][0][0].v}/></div>
}
`
	res, err := CompileScene("r11_jsx_multi_index.gx", src)
	if err != nil { t.Fatal(err) }
	out := res.GoSource
	if strings.Contains(out, `Signal.New(m["k"]`) {
		t.Errorf("多层索引 receiver 被双重包装：\n%s", out)
	}
	containsAll(t, out, `m["k"][0][0].v`)
}

// 第十轮 P2-1：sig 字段值是省略类型的复合字面量，须剥 Signal[...] 取元素类型补全。
func TestR11SignalFieldOmittedInnerType(t *testing.T) {
	src := `package main
type Inner struct { sig v string }
type Out struct { sig inner Inner }
func main() {
	o := Out{inner: {v: "x"}}
	_ = o
}
`
	got := compileOK(t, "r11_sig_inner.gx", src)
	// 不得生成 Signal.Signal[Inner]{V:"x"} 这种非法复合字面量。
	if strings.Contains(got, `Signal.Signal[Inner]{`) || strings.Contains(got, `Signal[Inner]{`) {
		t.Errorf("sig 字段内层省略类型未剥包装：\n%s", got)
	}
	containsAll(t, got, `Signal.New(Inner{v: Signal.New("x")})`)
}

// 第十轮 P2-2：JSX props 传复合字面量，内层 sig 字段必须递归包装。
func TestR11JSXCompositePropInnerWrap(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type Leaf struct { sig v string }
type CardProps struct { sig label Leaf }
func Card(props CardProps) *gxscene.Instance { return nil }
func main() {
	_ = <div><Card label={Leaf{v: "a"}}/></div>
}
`
	res, err := CompileScene("r11_jsx_comp_inner.gx", src)
	if err != nil { t.Fatal(err) }
	out := res.GoSource
	// 内层 v 应被包 Signal.New("a")；不得是裸 Leaf{v:"a"} 进 Signal.New 后内层没包。
	containsAll(t, out, `Signal.New(Leaf{v: Signal.New("a")})`)
}

// 第十轮 P3：三元 ?: 不支持，应在编译期报清晰诊断而非生成非法 Go。
func TestR11TernaryRejected(t *testing.T) {
	src := `package main
type P struct { sig a string }
func main() {
	flag := true
	p := P{a: flag ? "y" : "n"}
	_ = p
}
`
	_, err := Compile("r11_ternary.gx", src)
	if err == nil {
		t.Fatal("三元表达式应报编译错误")
	}
	if !strings.Contains(err.Error(), "不支持三元") {
		t.Errorf("应给出三元不支持的诊断，got: %v", err)
	}
}

// 第十一轮 N-1（本轮回归）：同一个 map 变量索引链的重复访问，每次都必须改写。
// 回归点：containerElemTypeFromDecl 文本回溯在第二次访问时命中上一次引用 m["k"]
// 的 '['，曾误判成 []T 类型起点并提前返回，导致第二次读静默失效（打印函数地址）、
// 先读后写生成非法的 map struct field 赋值。
func TestR12RepeatedMapIndexReadAndWrite(t *testing.T) {
	src := `package main
import "fmt"
type P struct { sig a string }
func main() {
	m := map[string]P{"k": {a: "x"}}
	fmt.Println(m["k"].a)
	fmt.Println(m["k"].a)
	m["k"].a = "y"
	fmt.Println(m["k"].a)
}
`
	got := compileOK(t, "r12_map_repeat.gx", src)
	// 三次读都必须是 getter() 调用；少一次即第二次被静默漏改。
	if c := strings.Count(got, `m["k"].a()`); c != 3 {
		t.Errorf("期望 3 处 m[\"k\"].a() getter 读，实际 %d 处:\n%s", c, got)
	}
	// 写必须改写成调用式，否则 Validate 阶段就因 map struct field 赋值失败。
	compact := strings.Join(strings.Fields(got), "")
	containsAll(t, compact, `m["k"].a("y")`)
}

// 第十一轮 N-1：多层索引 m["k"][0][0].a 同样要支持重复访问。
func TestR12RepeatedNestedMapIndexRead(t *testing.T) {
	src := `package main
import "fmt"
type P struct { sig a string }
func main() {
	m := map[string][][]P{"k": {{{a: "x"}}}}
	fmt.Println(m["k"][0][0].a)
	fmt.Println(m["k"][0][0].a)
}
`
	got := compileOK(t, "r12_nested_map_repeat.gx", src)
	if c := strings.Count(got, `m["k"][0][0].a()`); c != 2 {
		t.Errorf("期望 2 处 getter 读，实际 %d 处:\n%s", c, got)
	}
}

// 第十一轮 N-3：同一变量被多个 fields / 多个实例引用时，生成的共享锁 key 必须相同，
// 否则运行时各持一把锁，同实例多字段/跨实例并发仍 race。
func TestR12SharedLockKeySameVariable(t *testing.T) {
	src := `package main
type P struct { sig a string; sig b string }
func main() {
	v := "x"
	p1 := P{a: v, b: v}
	p2 := P{a: v, b: v}
	_, _ = p1, p2
}
`
	got := compileOK(t, "r12_share_key.gx", src)
	// 用正则跨行提取所有 LockedGetterShared("key"...) 的 key，不依赖产物排版。
	keys := extractLockKeys(t, got)
	if len(keys) != 4 {
		t.Fatalf("期望 4 个档5 getter（p1.a/p1.b/p2.a/p2.b），实际 %d:\n%s", len(keys), got)
	}
	for _, k := range keys[1:] {
		if k != keys[0] {
			t.Errorf("同一变量 v 的 getter 必须共享 key，期望 %q，另有 %q:\n%s", keys[0], k, got)
		}
	}
}

// 第十一轮 N-3：不同变量必须生成不同 key，避免无关变量被强制串行化。
func TestR12SharedLockKeyDistinctVariables(t *testing.T) {
	src := `package main
type P struct { sig a string; sig b string }
func main() {
	v := "x"
	w := "y"
	p := P{a: v, b: w}
	_ = p
}
`
	got := compileOK(t, "r12_distinct_key.gx", src)
	keys := extractLockKeys(t, got)
	if len(keys) != 2 {
		t.Fatalf("期望 2 个档5 getter，实际 %d:\n%s", len(keys), got)
	}
	// 两个 key 必须不同：v 与 w 是不同声明。
	if keys[0] == keys[1] {
		t.Errorf("不同变量 v/w 不应共享 key，都是 %q:\n%s", keys[0], got)
	}
}

// extractLockKeys 用正则从生成 Go 中提取所有 LockedGetterShared 的 key 字面量，
// 跨行匹配、不依赖产物排版（第十二轮测试质量修正）。
func extractLockKeys(t *testing.T, goSrc string) []string {
	t.Helper()
	re := regexp.MustCompile(`LockedGetterShared\("([^"]*)"`)
	matches := re.FindAllStringSubmatch(goSrc, -1)
	keys := make([]string, 0, len(matches))
	for _, m := range matches {
		keys = append(keys, m[1])
	}
	return keys
}

// 第十二轮 P2-D：结构体字段容器 s.m["k"] / s.ps[0] 的 sig 字段读必须改写。
// 旧实现在 .m 段用 bareFieldType("map[string]P") 得空串，类型链断裂，链尾 a 漏改。
func TestR12StructFieldContainerIndex(t *testing.T) {
	src := `package main
import "fmt"
type P struct { sig a string }
type S struct { m map[string]P; ps []P }
func main() {
	s := S{m: map[string]P{"k": {a: "x"}}, ps: []P{{a: "p"}}}
	fmt.Println(s.m["k"].a, s.ps[0].a)
	s.m["k"].a = "y"
	fmt.Println(s.m["k"].a)
}
`
	got := compileOK(t, "r12_struct_container.gx", src)
	containsAll(t, got,
		`s.m["k"].a()`,
		`s.ps[0].a()`,
	)
	compact := strings.Join(strings.Fields(got), "")
	containsAll(t, compact, `s.m["k"].a("y")`)
}

// 第十二轮 §4.5 立规矩：silentIndexedSignalField 的判据正确性——
// 含索引 + 链尾字段名唯一命中 sig 字段 → 报；普通字段名/多类型同名 → 不报。
func TestR12SilentIndexedSignalFieldDetect(t *testing.T) {
	src := `package main
type P struct { sig a string; b int }
type Q struct { a int }
func main() {
	m := map[string]P{"k": {a: "x", b: 1}}
	_ = m["k"].a
	_ = m["k"].b
}
`
	toks := New(src).AllTokens()
	decls := collectSignalFieldDecls(toks)
	tbl := buildSignalFieldTable(toks, decls)

	// 找 m["k"].a 的 m 下标（第 2 个 m：声明后第一次使用）。
	mIdx := -1
	cnt := 0
	for i, tk := range toks {
		if tk.Text == "m" && tk.Kind == Ident {
			cnt++
			if cnt == 2 {
				mIdx = i
				break
			}
		}
	}
	if mIdx < 0 {
		t.Fatal("找不到 m 使用点")
	}
	field, hit := silentIndexedSignalField(toks, mIdx, 0, tbl, nil)
	if !hit || field != "a" {
		t.Errorf("m[\"k\"].a 应命中 sig 字段 a，got field=%q hit=%v", field, hit)
	}
	// 用独立源码验证普通字段 b 不触发（b 不是任何类型的 sig 字段）。
	src2 := `package main
type P struct { sig a string; b int }
func main() {
	m := map[string]P{"k": {b: 1}}
	_ = m["k"].b
}
`
	toks2 := New(src2).AllTokens()
	tbl2 := buildSignalFieldTable(toks2, collectSignalFieldDecls(toks2))
	m2 := -1
	cnt2 := 0
	for i, tk := range toks2 {
		if tk.Text == "m" && tk.Kind == Ident {
			cnt2++
			if cnt2 == 2 { m2 = i; break }
		}
	}
	_, hit2 := silentIndexedSignalField(toks2, m2, 0, tbl2, nil)
	if hit2 {
		t.Error("普通字段 b 不应命中 sig 字段检测")
	}
}

// 第十三轮 P1-A：诊断按「元素类型确为 sig 字段的 owner」判定，不再按字段名
// 唯一命中。普通字段与别处 sig 字段撞名不得误报（否则项目模式构建被阻断）；
// 真阳性（map 元素类型的 sig 字段读未改写）仍须命中。
func TestR13SilentIndexedFieldNoFalsePositive(t *testing.T) {
	// 误报场景：Item.title 是 sig 字段，Row.title 是普通字段——撞名不应报错。
	srcFalse := `package main
type Item struct { sig title string }
type Row  struct { title string }
func main() {
	rows := []Row{{title: "t"}}
	_ = rows[0].title
}
`
	res, err := Compile("r13_false.gx", srcFalse)
	if err != nil {
		t.Fatal(err)
	}
	if hasDiagCode(res.Analysis.Diagnostics(), "signal-field-index-unknown") {
		t.Errorf("普通字段撞名不应误报 signal-field-index-unknown:\n%+v", res.Analysis.Diagnostics())
	}
	if !strings.Contains(res.GoSource, `rows[0].title`) {
		t.Errorf("普通字段读应保持原样, got:\n%s", res.GoSource)
	}

	// 元素类型可推断时，正常路径应直接改写成 a()，不挂诊断（无需沉默兜底）。
	srcOK := `package main
type P struct { sig a string }
func main() {
	m := map[string]P{"k": {a: "x"}}
	_ = m["k"].a
}
`
	res2, err := Compile("r13_ok.gx", srcOK)
	if err != nil {
		t.Fatal(err)
	}
	if hasDiagCode(res2.Analysis.Diagnostics(), "signal-field-index-unknown") {
		t.Errorf("类型可推断且已改写时不应挂诊断:\n%+v", res2.Analysis.Diagnostics())
	}
	if !strings.Contains(res2.GoSource, `m["k"].a()`) {
		t.Errorf("map 元素 sig 字段应被正常改写为 a(), got:\n%s", res2.GoSource)
	}
}

// 判据层：元素类型推不出时保持沉默（不猜测、不按字段名撞车误报）。
func TestR13SilentIndexedFieldUnknownTypeStaysSilent(t *testing.T) {
	// rows 声明为函数参数且无元素类型信息时，回退用不到声明、Binding.Type 也为空，
	// silentIndexedSignalField 应返回 false（宁可沉默也不误报）。
	src := `package main
type P struct { sig x string }
func main() {
	rows := getRows()
	_ = rows[0].x
}
func getRows() []any { return nil }
`
	toks := New(src).AllTokens()
	tbl := buildSignalFieldTable(toks, collectSignalFieldDecls(toks))
	// 找 rows[0].x 里的 rows（使用点，非声明点）。
	idx := -1
	cnt := 0
	for i, tk := range toks {
		if tk.Text == "rows" && tk.Kind == Ident {
			cnt++
			if cnt == 2 { idx = i; break }
		}
	}
	if idx < 0 { t.Fatal("找不到 rows 使用点") }
	if _, hit := silentIndexedSignalField(toks, idx, 0, tbl, nil); hit {
		t.Error("元素类型推不出时应保持沉默，不得按字段名撞车命中")
	}
}

// ===== sig 字段核心语义规则（三条）的集中回归 =====
// 规则见 docs/language-spec.md §11.1 的规则引用块：
//  1. 谁被 sig 修饰谁响应式（读补 ()、写转发）；
//  2. 语义沿 struct 嵌套访问链逐段保留（每个 sig 段各补一次 ()）；
//  3. 不自动传染给普通字段 / 同名的非 sig 字段（一律按普通 Go 字段读）。
func TestSignalFieldSemanticRules(t *testing.T) {
	src := `package main
type Leaf struct { sig v string; plain string }
type Mid struct { sig mid Leaf; tag string }
type Other struct { v string }
type Root struct { root Mid; v string; other Other }
func main() {
	r := Root{}
	println(r.root.mid.v)
	println(r.root.mid.plain)
	println(r.root.tag)
	println(r.v)
	println(r.other.v)
	r.v = "x"
	l := Leaf{}
	l.v = "y"
	l.plain = "z"
}
`
	got := compileOK(t, "sf_rules.gx", src)

	// 规则 1 + 2：sig 段的读与写都响应式，嵌套链上每个 sig 段各补一次 ()。
	gotCompact := strings.Join(strings.Fields(got), "")
	containsAll(t, got,
		`r.root.mid().v()`, // 链上 mid(sig)、v(sig) 各补 ()；root 普通段不补
	)
	containsAll(t, gotCompact,
		`l.v("y")`, // sig 字段写 → 转发给 getter
	)

	// 规则 3：普通字段、同名但非 sig 的字段，都不补 ()、写保持普通赋值。
	for _, bad := range []string{
		`r.root()`,       // root 是普通字段
		`r.root.mid.plain()`, // 链尾 plain 是普通字段
		`r.root.tag()`,   // tag 是普通字段
		`r.v()`,          // Root.v 是普通字段（与 Leaf.v 同名）
		`r.other.v()`,    // Other.v 是普通字段（与 Leaf.v 同名）
		`l.plain("z")`,   // 普通字段写不应转发
		`r.v("x")`,       // 普通字段写不应转发
	} {
		if strings.Contains(got, bad) {
			t.Errorf("普通/非 sig 字段被误加响应式语义 %q：\n%s", bad, got)
		}
	}

	// 规则 1（写）：普通字段写仍是标准 Go 赋值，sig 字段写是调用式。
	containsAll(t, got, `r.v = "x"`, `l.plain = "z"`)
}
