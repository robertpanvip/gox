package compiler

import (
	"strings"
	"testing"
)

func TestSignalDeclDesugar(t *testing.T) {
	// signal name = expr → var name = Signal.New(expr)，并注入 import
	src := `package main
signal count = 0
func main() { println(count()) }
`
	got := compileOK(t, "sig1.gx", src)
	containsAll(t, got,
		`Signal.New(0)`,
		`import Signal "github.com/robertpanvip/gox/runtime/signal"`,
	)
}

func TestSignalReadRewrite(t *testing.T) {
	// 表达式中的 signal 裸读 → count()
	src := `package main
signal count = 0
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
signal count = 0
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
	// Signal.computed 赋值的变量被登记为 signal，读取时加括号
	src := `package main
signal count = 0
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
	// signal 提案里的effect：读改写 + void lambda，无非法 return
	src := `package main
signal count = 0
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
signal count = 0
type Box struct { count int }
func use(count int) int { return count }
func main() {
    var b Box = Box{count: 1}
    println(b.count)
    println(use(2))
    println(count())
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
	// 没有 signal 语法时不应注入 signal import
	src := `package main
func main() { println(1) }
`
	got := compileOK(t, "sig7.gx", src)
	if strings.Contains(got, "runtime/signal") {
		t.Errorf("无 signal 语法却注入了 signal import: %s", got)
	}
}

func TestSignalCompoundAndIncrement(t *testing.T) {
	// 复合赋值与自增自减展开为 读旧值+运算+写新值
	src := `package main
signal count = 0
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
signal n = 0
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
	// 源码已用别名导入 signal 路径 → 生成代码复用该别名
	src := `package main
import S "github.com/robertpanvip/gox/runtime/signal"
signal count = 0
func main() { println(count()) }
`
	got := compileOK(t, "sig8.gx", src)
	containsAll(t, got, `S.New(0)`)
	if strings.Count(got, "runtime/signal") != 1 {
		t.Errorf("signal 路径应只出现一次（复用已有 import）: %s", got)
	}
}

// TestSignalRHSReads 回归 H-3：signal 声明/简单赋值/复合赋值右侧的裸 signal
// 读取必须改写为调用式，否则类型/语义错误。
func TestSignalRHSReads(t *testing.T) {
	src := `package main
func main() {
	signal a = 1
	signal b = a
	signal c = 2
	b = c
	c += a
	println(b(), c())
}
`
	got := compileOK(t, "sigrhs.gx", src)
	containsAll(t, got,
		`var b = Signal.New(a())`,
		`b(c())`,
		`c(c() + a())`,
	)
}

// TestSignalNoInitNotSwallowNext 回归 M-4：`signal x` 缺 '=' 时不得吞掉后续语句。
func TestSignalNoInitNotSwallowNext(t *testing.T) {
	src := `package main
func main() {
	signal x
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
// 是 signal 同名时不得被误改写为 a()。
func TestSignalParamFirstInGroup(t *testing.T) {
	src := `package main
func f(a, b int) { println(a, b) }
func main() { signal a = 1; f(a, 2) }
`
	got := compileOK(t, "sigparam.gx", src)
	if strings.Contains(got, "func f(a()") {
		t.Errorf("函数参数 a 被误改写为 a(): %s", got)
	}
	containsAll(t, got, `f(a(), 2)`)
}
