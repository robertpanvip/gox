package compiler

import (
	"strings"
	"testing"
)

func TestSignalDeclDesugar(t *testing.T) {
	// sig name = expr → var name = Signal.New(expr)，并注入 import
	src := `package main
sig count = 0
func main() { println(count) }
`
	got := compileOK(t, "sig1.gx", src)
	containsAll(t, got,
		`Signal.New(0)`,
		`import Signal "github.com/robertpanvip/gox/runtime/signal"`,
	)
}

func TestSignalReadRewrite(t *testing.T) {
	// 表达式中的 sig 裸读 → count()
	src := `package main
sig count = 0
func main() {
    let x = count + 1
    println(x)
}
`
	got := compileOK(t, "sig2.gx", src)
	containsAll(t, got, `x := count() + 1`)
}

func TestSignalWriteRewrite(t *testing.T) {
	// count = expr → count(expr)
	src := `package main
sig count = 0
func main() {
    count = 5
}
`
	got := compileOK(t, "sig3.gx", src)
	containsAll(t, got, `count(5)`)
	if strings.Contains(got, "count() = 5") {
		t.Errorf("写目标不应被读改写: %s", got)
	}
}

func TestSignalComputedRegistered(t *testing.T) {
	// Signal.computed 赋值的变量被登记为 sig，读取时加括号
	src := `package main
sig count = 0
func main() {
    let doubled = Signal.computed(() => count * 2)
    println(doubled)
}
`
	got := compileOK(t, "sig4.gx", src)
	containsAll(t, got,
		`doubled := Signal.Computed(func() int {return count() * 2})`,
		`println(doubled())`,
	)
}

func TestSignalEffectVoidLambda(t *testing.T) {
	// sig 提案里的effect：读改写 + void lambda，无非法 return
	src := `package main
sig count = 0
func main() {
    Signal.effect(() => println(count))
}
`
	got := compileOK(t, "sig5.gx", src)
	containsAll(t, got, `Signal.Effect(func() {println(count())})`)
}

func TestSignalScopeExclusions(t *testing.T) {
	// selector 右侧、复合字段键、函数参数、声明目标都不应被读改写
	src := `package main
sig count = 0
type Box struct { count int }
func use(count int) int { return count }
func main() {
    var b Box = Box{count: 1}
    println(b.count)
    println(use(2))
    println(count)
}
`
	got := compileOK(t, "sig6.gx", src)
	containsAll(t, got,
		`Box{count: 1}`,    // 复合字段键不改写
		`println(b.count)`, // selector 不改写
		`return count`,     // 函数参数不改写（保持裸标识符）
	)
}

func TestSignalNoFalseReimport(t *testing.T) {
	// 没有 sig 语法时不应注入 sig import
	src := `package main
func main() { println(1) }
`
	got := compileOK(t, "sig7.gx", src)
	if strings.Contains(got, "runtime/signal") {
		t.Errorf("无 sig 语法却注入了 sig import: %s", got)
	}
}

func TestSignalCompoundAndIncrement(t *testing.T) {
	// 复合赋值与自增自减展开为 读旧值+运算+写新值
	src := `package main
sig count = 0
func main() {
    count += 3
    count++
    count--
}
`
	got := compileOK(t, "sigcompound.gx", src)
	containsAll(t, got,
		`count(count() + 3)`,
		`count(count() + 1)`,
		`count(count() - 1)`,
	)
}

func TestSignalBitwiseCompoundAssign(t *testing.T) {
	// Go 全部位运算复合赋值都应展开为 name(name() op expr)
	src := `package main
sig n = 0
func main() {
    n &= 3
    n |= 8
    n ^= 2
    n <<= 1
    n >>= 1
    n &^= 1
}
`
	got := compileOK(t, "sigbit.gx", src)
	containsAll(t, got,
		`n(n() & 3)`,
		`n(n() | 8)`,
		`n(n() ^ 2)`,
		`n(n() << 1)`,
		`n(n() >> 1)`,
		`n(n() &^ 1)`,
	)
	if strings.Contains(got, "n() &=") || strings.Contains(got, "n() <<=") {
		t.Errorf("位复合赋值不应落到读改写: %s", got)
	}
}

func TestSignalImportReuseAlias(t *testing.T) {
	// 源码已用别名导入 sig 路径 → 生成代码复用该别名
	src := `package main
import S "github.com/robertpanvip/gox/runtime/signal"
sig count = 0
func main() { println(count) }
`
	got := compileOK(t, "sig8.gx", src)
	containsAll(t, got, `S.New(0)`)
	if strings.Count(got, "runtime/signal") != 1 {
		t.Errorf("sig 路径应只出现一次（复用已有 import）: %s", got)
	}
}

func TestSignalReadRewriteInJSXInterpolation(t *testing.T) {
	// vdom 后端同样要补齐 JSX 插值里的 sig 读
	res, err := CompileSource(`package main
func main() {
	sig n = 1
	_ = <p>{n}</p>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.GoSource, "n()") {
		t.Errorf("vdom JSX 插值里的 sig 读应补全 (), got:\n%s", res.GoSource)
	}
}

// TestSignalRHSReads 回归 H-3：sig 声明/简单赋值/复合赋值右侧的裸 sig
// 读取必须改写为调用式，否则类型/语义错误。
func TestSignalRHSReads(t *testing.T) {
	src := `package main
func main() {
	sig a = 1
	sig b = a
	sig c = 2
	b = c
	c += a
	println(b, c)
}
`
	got := compileOK(t, "sigrhs.gx", src)
	containsAll(t, got,
		`var b = Signal.New(a())`,
		`b(c())`,
		`c(c() + a())`,
	)
}

// TestSignalNoInitNotSwallowNext 回归 M-4：`sig x` 缺 '=' 时不得吞掉后续语句。
func TestSignalNoInitNotSwallowNext(t *testing.T) {
	src := `package main
func main() {
	sig x
	println(1)
}
`
	got := compileOK(t, "signoinit.gx", src)
	containsAll(t, got, `var x any`, `println(1)`)
	if strings.Contains(got, "Signal.New((") {
		t.Errorf("不应把后续语句吞进 Signal.New: %s", got)
	}
}

// TestSignalParamFirstInGroup 回归 L-6：`func f(a, b int)` 中逗号前的首参 a
// 是 sig 同名时不得被误改写为 a()。
func TestSignalParamFirstInGroup(t *testing.T) {
	src := `package main
func f(a, b int) { println(a, b) }
func main() { sig a = 1; f(a, 2) }
`
	got := compileOK(t, "sigparam.gx", src)
	if strings.Contains(got, "func f(a()") {
		t.Errorf("函数参数 a 被误改写为 a(): %s", got)
	}
	containsAll(t, got, `f(a(), 2)`)
}

// TestSignalNameShadowedByParam 回归 P1-1a：sig 名被函数参数遮蔽时，
// 函数体内对参数的引用必须保持普通变量，不能改写成 count()。
func TestSignalNameShadowedByParam(t *testing.T) {
	src := `package main
sig count = 0
func getCount(count int) int { return count + 1 }
func main() { println(getCount(1), count) }
`
	got := compileOK(t, "sigshadowparam.gx", src)
	if strings.Contains(got, "return count()") {
		t.Errorf("被遮蔽的参数 count 被误改写为 count(): %s", got)
	}
	containsAll(t, got, `return count + 1`, `var count = Signal.New(0)`, `count())`)
}

// TestSignalNameShadowedByLocal 回归 P1-1b：sig 名被局部变量遮蔽时，
// 局部作用域内的引用保持普通变量，函数外的全局 sig 读写仍正常改写。
func TestSignalNameShadowedByLocal(t *testing.T) {
	src := `package main
sig count = 0
func test() { count := 100; println(count) }
func main() { test(); println(count) }
`
	got := compileOK(t, "sigshadowlocal.gx", src)
	if strings.Contains(got, "count := 100; println(count())") {
		t.Errorf("被遮蔽的局部变量 count 被误改写: %s", got)
	}
	containsAll(t, got, `count := 100`, `println(count)`, `println(count())`)
}

// TestSignalReadInTemplate 回归：模板插值 ${a} 内的 sig 裸读必须改写为 a()。
// transformSignals 先于 transformTemplates 执行，插值彼时还是整块 Template token
// 未被识别，曾在展开后漏改，导致 fmt.Sprint(a) 把函数对象当值输出（函数地址）。
func TestSignalReadInTemplate(t *testing.T) {
	src := "package main\nsig a = 1\nsig b = 2\nfunc main() {\n\tprintln(`v=${a}`)\n\tprintln(`s=${a + b}`)\n\tprintln(`${a}-${b}`)\n}\n"
	got := compileOK(t, "sigtpl.gx", src)
	containsAll(t, got,
		`fmt.Sprint(a())`,
		`fmt.Sprint(a() + b())`,
		`fmt.Sprint(a())+"-"+fmt.Sprint(b())`,
	)
	// 不得残留裸 sig 名作为 fmt.Sprint 实参
	if strings.Contains(got, "fmt.Sprint(a)") || strings.Contains(got, "fmt.Sprint(b)") {
		t.Errorf("模板插值内 sig 未改写为 getter():\n%s", got)
	}
}

// TestSignalComputedInTemplate 回归：computed 派生 sig 在模板插值里也要改写。
// 同时验证 int 特化不受影响（strconv.Itoa(d()) 而非退化成 fmt.Sprint）。
func TestSignalComputedInTemplate(t *testing.T) {
	src := "package main\nsig a = 1\nfunc main() {\n\tlet d = Signal.computed(() => a * 2)\n\tprintln(`d=${d}`)\n}\n"
	got := compileOK(t, "sigtplcomp.gx", src)
	containsAll(t, got, `strconv.Itoa(d())`)
	if strings.Contains(got, "strconv.Itoa(d)") {
		t.Errorf("computed 在模板插值内未改写为 d():\n%s", got)
	}
}

// TestSignalTemplatePreservesExclusions 回归：模板插值里的 selector 右侧 /
// 函数实参 / 已是调用 / 局部遮蔽，不能被误改写。
func TestSignalTemplatePreservesExclusions(t *testing.T) {
	src := "package main\nsig a = 1\ntype Box struct { v int }\nfunc f(x int) int { return x }\nfunc main() {\n\tbox := Box{v: 3}\n\tprintln(`${box.v}`)\n\tprintln(`${f(a)}`)\n\tprintln(`${a}`)\n\ta := 9\n\tprintln(`${a}`)\n}\n"
	got := compileOK(t, "sigtplexcl.gx", src)
	containsAll(t, got,
		`fmt.Sprint(box.v)`,  // selector 不改写
		`fmt.Sprint(f(a()))`, // sig 作函数实参要改写
		`a := 9`,
	)
}

// TestSignalTemplateSelectorLHS 回归：sig 作为 selector 接收者 ${a.field}
// 应改写为 a().field（sig 本身是读），而不是整体跳过。
func TestSignalTemplateSelectorLHS(t *testing.T) {
	src := "package main\nsig p = struct{ v int }{v: 1}\nfunc main() { println(`${p.v}`) }\n"
	got := compileOK(t, "sigtplsel.gx", src)
	if !strings.Contains(got, "fmt.Sprint(p().v)") {
		t.Errorf("sig 作 selector 接收者应改写为 p().v:\n%s", got)
	}
}

// TestSignalReadInJSX 回归：JSX 插值里的 sig 裸读必须改写为 name()。
// JSX 在 lexer 阶段被整块吞成一个 token，transformSignals 扫不到内部的 sig
// 引用；曾在展开后漏改，导致把函数对象塞进 vdom（<span>{count}</span>）或直接
// 编译失败（{a+b} → a + b，函数相加）。展开后用 ResolveAt 按名字补改写。
func TestSignalReadInJSX(t *testing.T) {
	src := `package main
sig count = 0
sig name = "x"
sig a = 1
sig b = 2
func main() {
    _ = <div id={name}><span>{count}</span>{a + b}</div>
}
`
	got := compileOK(t, "sigjsx.gx", src)
	containsAll(t, got,
		`return name()`,
		`return count()`,
		`a() + b()`,
	)
	// 不得残留裸 sig 名作为 scene getter 返回值
	if strings.Contains(got, "return count }") || strings.Contains(got, "return name }") {
		t.Errorf("JSX 插值内 sig 未改写为 getter():\n%s", got)
	}
}

// TestSignalJSXShadowing 回归：JSX 插值里的局部遮蔽变量不得被误改写。
// sig count 被函数内 count := 3 遮蔽后，{count} 应读局部变量（原样 count）。
func TestSignalJSXShadowing(t *testing.T) {
	src := `package main
sig count = 0
func main() {
    count := 3
    _ = <div>{count}</div>
}
`
	got := compileOK(t, "sigjsxshadow.gx", src)
	containsAll(t, got, `count := 3`)
	if !strings.Contains(got, "return count }") {
		t.Errorf("被遮蔽的局部变量 count 不应改写为 count():\n%s", got)
	}
}

// TestSignalJSXNested 回归：嵌套 JSX 的每层插值里的 sig 都要改写。
func TestSignalJSXNested(t *testing.T) {
	src := `package main
sig a = 1
func main() {
    _ = <div>{a}<span>{a}</span></div>
}
`
	got := compileOK(t, "sigjsxnest.gx", src)
	// 两处 {a} 都应变成 a()
	if n := strings.Count(got, "a()"); n < 2 {
		t.Errorf("嵌套 JSX 两处 sig 读都应改写，实际 a() 出现 %d 次:\n%s", n, got)
	}
}

// TestSignalJSXComponentNameNotRewritten 回归：JSX 组件名（大写开头的引用）
// 即使与 sig 同名，也不应被改写成 sig 读取——组件名是要调用的组件引用，
// 不是 sig 读取。改写只应作用于插值片段。
func TestSignalJSXComponentNameNotRewritten(t *testing.T) {
	src := `package main
sig Card = 1
func main() {
    view := <Card/>
    _ = view
}
`
	got := compileOK(t, "sigjsxcomp.gx", src)
	containsAll(t, got, `Card(gxscene.Props{})`)
	if strings.Contains(got, "Card()(") {
		t.Errorf("JSX 组件名不应被误改写为 Card():\n%s", got)
	}
}

// §6b 回归（禁用底层 API 后重写）：文件里只有 sig 字段、没有 sig 变量
// 声明时，小写语法糖 Signal.computed(...) 不应触发 Signal2 别名——注入的 import
// 应复用 Signal，使小写还原后的 Signal.Computed 与生成代码指向同一包。
// 底层 Signal.New 已禁止手写，故用小写语法糖覆盖同一条别名路径。
func TestSignalImportNoSignal2Alias(t *testing.T) {
	src := `package main
type P struct { sig name string }
func main() {
	sig s = "x"
	c := Signal.computed(() => s)
	_ = P{name: c}
}
`
	got := compileOK(t, "sigalias.gx", src)
	containsAll(t, got,
		`import Signal "github.com/robertpanvip/gox/runtime/signal"`,
		`c := Signal.Computed(func() string {return s()})`,
	)
	if strings.Contains(got, "Signal2") {
		t.Errorf("不应生成 Signal2 别名:\n%s", got)
	}
}

// 底层 Signal API 禁用：首字母大写的运行时构造器是编译器内部 API，
// 用户源码直接调用必须报编译错误（源信号/派生/副作用只能走语法糖）。
func TestNoRawSignalAPIForbidden(t *testing.T) {
	cases := []struct {
		name, src, wantName string
	}{
		{"New", "package main\nfunc main() { n := Signal.New(1); _ = n() }", "Signal.New"},
		{"Computed", "package main\nfunc main() { c := Signal.Computed(func() int { return 1 }); _ = c() }", "Signal.Computed"},
		{"Effect", "package main\nfunc main() { Signal.Effect(func() {}) }", "Signal.Effect"},
		{"NewWithEquals", "package main\nfunc main() { n := Signal.NewWithEquals(1, nil); _ = n() }", "Signal.NewWithEquals"},
		{"CreateRoot", "package main\nfunc main() { Signal.CreateRoot(func() {}) }", "Signal.CreateRoot"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Compile("rawapi.gx", c.src)
			if err == nil {
				t.Fatalf("手写 %s 应被禁止", c.wantName)
			}
			if !strings.Contains(err.Error(), "不允许直接调用底层 API "+c.wantName) {
				t.Errorf("错误信息应点名 %s，got: %v", c.wantName, err)
			}
			// 诊断应指向用户写的成员名所在行列（第 2 行），而非 0 或残缺坐标。
			if !strings.HasPrefix(err.Error(), "2:") {
				t.Errorf("诊断应带源码坐标（第 2 行），got: %v", err)
			}
		})
	}
}

// 小写语法糖放行：computed/effect 调用与函数值传递、sig 声明、字符串内容
// 都不应触发禁用检查。小写由 transformExternal 在末尾还原成大写符号。
func TestNoRawSignalAPILowercaseAllowed(t *testing.T) {
	cases := []struct {
		name, src string
	}{
		{"computed", "package main\nfunc main() { sig n = 1; c := Signal.computed(() => n); _ = c }"},
		{"effect-call", "package main\nfunc main() { sig n = 1; Signal.effect(() => println(n)) }"},
		{"effect-as-value", "package main\nfunc main() { v := struct{}{}; _ = v; f := Signal.effect; _ = f }"},
		{"string-content", "package main\nfunc main() { s := \"Signal.New(1)\"; _ = s }"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := compileOK(t, "lower.gx", c.src)
			// 小写调用形态应还原为大写符号；字符串内容原样保留。
			if c.name == "computed" && !strings.Contains(got, "Signal.Computed(") {
				t.Errorf("小写 computed 应还原为 Signal.Computed:\n%s", got)
			}
		})
	}
}

// 禁用检查不得误伤编译器内部生成的 Signal.New / Signal.Computed。
// sig 声明降级、sig 字段三档包装都在检查之后发生，仍应正常生成。
func TestNoRawSignalAPIGeneratedStillWorks(t *testing.T) {
	src := `package main
type CardProps struct { sig name string; age int }
func main() {
	sig src = "x"
	p := CardProps{name: src, age: 3}
	c := Signal.computed(() => src)
	_ = p.name
	_ = c
}
`
	got := compileOK(t, "gen.gx", src)
	containsAll(t, got,
		`var src = Signal.New("x")`,
		`name: src`, // 透传档：裸 getter，不二次包装
		`age: 3`,
		`Signal.Computed(func() string {return src()})`, // 小写派生还原
		`import Signal "github.com/robertpanvip/gox/runtime/signal"`,
	)
}

// 回归：signal 包「类型名」出现在类型位置（函数参数/返回值/字段/断言）时，
// checkNoRawSignalAPI 不得误判为底层构造器。Context[T] 等泛型组件需要在
// 签名里写 `Signal.Signal[T]`。修复前该写法会被误拦，逼得组件只能用小写别名。
func TestNoRawSignalAPITypeNamesAllowed(t *testing.T) {
	cases := []struct {
		name, src string
	}{
		{"param-generic", "package main\nimport Signal \"github.com/robertpanvip/gox/runtime/signal\"\nfunc f(s Signal.Signal[int]) int { return s() }"},
		{"return-generic", "package main\nimport Signal \"github.com/robertpanvip/gox/runtime/signal\"\nfunc f() Signal.Signal[int] { return Signal.New(0) }"},
		{"param-plain-alias", "package main\nimport \"github.com/robertpanvip/gox/runtime/signal\"\nfunc f(s Signal.Signal[int]) int { return s() }"},
		{"owner-type", "package main\nimport Signal \"github.com/robertpanvip/gox/runtime/signal\"\nfunc f(o Signal.Owner) {}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := compileOK(t, "typename.gx", c.src)
			if !strings.Contains(got, "Signal.Signal[") && !strings.Contains(got, "Signal.Owner") {
				t.Errorf("类型引用应保留:\n%s", got)
			}
		})
	}
}

// 回归：白名单类型名若被当作函数调用（后跟 '('）仍须拦截，不能因白名单放行
// 而给底层构造器开后门。
func TestNoRawSignalAPIWhitelistTypeCallStillForbidden(t *testing.T) {
	_, err := Compile("typecall.gx", "package main\nimport \"github.com/robertpanvip/gox/runtime/signal\"\nfunc f() { Signal.Computation(0) }")
	if err == nil {
		t.Fatal("Signal.Computation(...) 作调用应被禁止")
	}
	if !strings.Contains(err.Error(), "不允许直接调用底层 API Signal.Computation") {
		t.Errorf("错误信息应点名 Signal.Computation，got: %v", err)
	}
}

// 回归：sig 字段一律按普通字段读写，禁止函数式读写。函数式读 s.x() 与
// 函数式写 s.x(2) 都必须拦截；裸读 s.x、赋值写 s.x = 2、复合赋值 s.x += 2
// 放行。此前检查器只覆盖 sig 变量，sig 字段的 () 写法会侥幸通过。
func TestNoExplicitSigFieldCall(t *testing.T) {
	forbidden := []struct{ name, expr string }{
		{"函数式读", "_ = s.x()"},
		{"函数式写", "s.x(2)"},
		{"函数式写-bool", "s.x(true)"},
	}
	for _, tc := range forbidden {
		t.Run("forbid_"+tc.name, func(t *testing.T) {
			src := "package main\ntype S struct{ sig x int }\nfunc main(){ s := S{x:1}; " + tc.expr + " }"
			_, err := Compile("sigfield.gx", src)
			if err == nil || !strings.Contains(err.Error(), "sig 字段") {
				t.Errorf("函数式 %s 应被拦截，got: %v", tc.expr, err)
			}
		})
	}
	allowed := []struct{ name, expr string }{
		{"裸读", "_ = s.x"},
		{"赋值写", "s.x = 2"},
		{"复合赋值", "s.x += 2"},
	}
	for _, tc := range allowed {
		t.Run("allow_"+tc.name, func(t *testing.T) {
			src := "package main\ntype S struct{ sig x int }\nfunc main(){ s := S{x:1}; " + tc.expr + "; _ = s }"
			got := compileOK(t, "sigfield.gx", src)
			if !strings.Contains(got, "x Signal.Signal[int]") {
				t.Errorf("sig 字段应降级:\n%s", got)
			}
		})
	}
}

// 回归：链式 selector 上的 sig 字段同样禁止函数式读写。此前检查器只从 '.'
// 左边紧邻 token 判定，链式 s.inner.x() 时那是字段名（推不出类型）而漏放。
// 现复用 resolveSignalChainEx 从链首沿链传播类型，覆盖多级链、索引链、
// 调用结果链；中间 sig 段显式写括号（s.leaf().v）亦拦截。
func TestNoExplicitSigFieldChainCall(t *testing.T) {
	forbidden := []struct{ name, src string }{
		{
			"多级链函数式读",
			"package main\ntype Inner struct{ sig x int }\ntype S struct{ inner Inner }\nfunc main(){ s := S{}; _ = s.inner.x() }",
		},
		{
			"多级链函数式写",
			"package main\ntype Inner struct{ sig x int }\ntype S struct{ inner Inner }\nfunc main(){ s := S{}; s.inner.x(2) }",
		},
		{
			"索引链函数式读",
			"package main\ntype Item struct{ sig x int }\nfunc main(){ m := map[string]Item{}; _ = m[\"a\"].x() }",
		},
		{
			"三层链函数式读",
			"package main\ntype Leaf struct{ sig x int }\ntype Mid struct{ b Leaf }\ntype S struct{ a Mid }\nfunc main(){ s := S{}; _ = s.a.b.x() }",
		},
		{
			"调用结果链函数式读",
			"package main\ntype P struct{ sig x int }\nfunc getP() P { return P{} }\nfunc main(){ _ = getP().x() }",
		},
		{
			"中间段显式括号",
			"package main\ntype Leaf struct{ sig v int }\ntype S struct{ sig leaf Leaf }\nfunc main(){ s := S{}; _ = s.leaf().v }",
		},
	}
	for _, tc := range forbidden {
		t.Run("forbid_"+tc.name, func(t *testing.T) {
			_, err := Compile("chain.gx", tc.src)
			if err == nil || !strings.Contains(err.Error(), "sig 字段") {
				t.Errorf("%s 应被拦截，got: %v", tc.name, err)
			}
		})
	}
	allowed := []struct{ name, src string }{
		{
			"多级链裸读",
			"package main\ntype Inner struct{ sig x int }\ntype S struct{ inner Inner }\nfunc main(){ s := S{}; _ = s.inner.x }",
		},
		{
			"索引链裸读",
			"package main\ntype Item struct{ sig x int }\nfunc main(){ m := map[string]Item{}; _ = m[\"a\"].x }",
		},
		{
			"多级链赋值写",
			"package main\ntype Inner struct{ sig x int }\ntype S struct{ inner Inner }\nfunc main(){ s := S{}; s.inner.x = 2; _ = s }",
		},
		{
			"普通方法链不误伤",
			"package main\ntype Leaf struct{}\nfunc (l Leaf) Method() int{return 1}\ntype Mid struct{ b Leaf }\ntype S struct{ a Mid }\nfunc main(){ s := S{}; _ = s.a.b.Method() }",
		},
		{
			"context getter 不误伤",
			"package main\nfunc main(){ f := func() int{return 1}; _ = f() }",
		},
	}
	for _, tc := range allowed {
		t.Run("allow_"+tc.name, func(t *testing.T) {
			compileOK(t, "chain.gx", tc.src)
		})
	}
}
