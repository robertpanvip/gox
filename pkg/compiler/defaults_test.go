package compiler

import "testing"

func TestDefaultsBasic(t *testing.T) {
	src := `package main
func greet(name string = "world", times int = 1) string {
	return name
}
func main() {
	println(greet())
	println(greet("gx"))
	println(greet("gx", 3))
}
`
	got := compileOK(t, "def.gx", src)
	containsAll(t, got,
		`func greet(name string , times int ) string`, // 声明处默认值被剥离
		`greet("world",1)`,
		`greet("gx",1)`,
		`greet("gx", 3)`,
	)
}

func TestDefaultsPartialLeadingRequired(t *testing.T) {
	src := `package main
func f(a int, b string = "x", c int = 3) string {
	return b
}
func main() {
	println(f(1))
	println(f(1, "y"))
	println(f(1, "y", 9))
}
`
	got := compileOK(t, "def2.gx", src)
	containsAll(t, got, `f(1,"x",3)`, `f(1, "y",3)`, `f(1, "y", 9)`)
}

func TestDefaultsExprWithGXSyntax(t *testing.T) {
	// 默认值表达式含 GX 语法：小写外部符号调用 + 模板字符串，均需被后续 transform 处理。
	src := `package main
import "strconv"
func f(x string = ` + "`n=${strconv.itoa(42)}`" + `) string {
	return x
}
func main() {
	println(f())
}
`
	got := compileOK(t, "def3.gx", src)
	containsAll(t, got, `f("n="+fmt.Sprint(strconv.Itoa(42)))`)
}

func TestDefaultsPubName(t *testing.T) {
	// pub 函数：调用点用小写原名匹配补全，pub transform 之后统一改大写。
	src := `package main
pub func greet(name string = "pub") string { return name }
func main() { println(greet()) }
`
	got := compileOK(t, "def4.gx", src)
	containsAll(t, got, `Greet("pub")`)
}

func TestDefaultsNoSelectorCall(t *testing.T) {
	// 方法调用/selector 不补全（避免同名误伤）。
	src := `package main
type obj struct{}
func (o obj) greet(x int = 1) int { return x }
func greet(x int = 2) int { return x }
func main() {
	o := obj{}
	println(o.greet())
	println(greet())
}
`
	got := compileOK(t, "def5.gx", src)
	containsAll(t, got, `greet(2)`) // 仅顶层函数补全
}

func TestDefaultsUnionParam(t *testing.T) {
	// 联合类型参数 + 默认值：默认值随 token 流经过 union 擦除与守卫。
	src := `package main
func f(v int | string = 5) any { return v }
func main() { println(f()) }
`
	got := compileOK(t, "def6.gx", src)
	containsAll(t, got, `f(5)`)
}
