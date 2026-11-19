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
type box struct { count int }
func use(count int) int { return count }
func main() {
    var b box = box{count: 1}
    println(b.count)
    println(use(2))
    println(count())
}
`
	got := compileOK(t, "sig6.gx", src)
	containsAll(t, got,
		`box{count: 1}`,       // 复合字段键不改写
		`println(b.count)`,    // selector 不改写
		`return count`,        // 函数参数不改写（保持裸标识符）
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
