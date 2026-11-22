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
func main() { p := CardProps{name: Signal.New("x")}; println(p.name) }
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

func TestSignalFieldConstructWithExplicitSignalNew(t *testing.T) {
	// 字段值直接写 Signal.New(...)：整段直通，不得把复合字面量的 '}' 吞进值区间
	// （曾因 prevSigIn 边界越界导致产物多一个 '}'）。
	src := `package main
type CardProps struct { signal name string }
func main() { p := CardProps{name: Signal.New("x")}; println(p.name) }
`
	got := compileOK(t, "sf11.gx", src)
	containsAll(t, got, `name: Signal.New("x")`, `println(p.name())`)
	// 函数体内不应出现多余的 '}'（CardProps{...} 一个 + main{...} 一个）
	if strings.Contains(got, `"x")}}`) {
		t.Errorf("构造字面量后被吞入多余的 '}':\n%s", got)
	}
}
