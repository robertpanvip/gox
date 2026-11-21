package compiler

import (
	"strings"
	"testing"
)

func TestLambdaSingleParamInfer(t *testing.T) {
	src := `package main
func main() {
	f := x => x + 1
	println(f(2))
}
`
	got := compileOK(t, "lambda.gx", src)
	containsAll(t, got, `func(x int) int {return x + 1}`)
}

func TestLambdaExplicitParams(t *testing.T) {
	src := `package main
func main() {
	add := (a int, b int) => a + b
	println(add(3, 4))
}
`
	got := compileOK(t, "lambda2.gx", src)
	containsAll(t, got, `func(a int,b int) int {return a + b}`)
}

func TestLambdaZeroParam(t *testing.T) {
	src := `package main
func main() {
	f := () => 42
	println(f())
}
`
	got := compileOK(t, "lambda3.gx", src)
	containsAll(t, got, `func() int {return 42}`)
}

func TestLambdaCompareBool(t *testing.T) {
	src := `package main
func main() {
	isBig := x => x > 10
	println(isBig(20))
}
`
	got := compileOK(t, "lambda4.gx", src)
	containsAll(t, got, `func(x int) bool {return x > 10}`)
}

func TestLambdaAsFuncArg(t *testing.T) {
	src := `package main
func apply(f func(int) int, v int) int { return f(v) }
func main() {
	println(apply(x => x * 2, 5))
}
`
	got := compileOK(t, "lambda5.gx", src)
	containsAll(t, got, `apply(func(x int) int {return x * 2}, 5)`)
}

func TestLambdaCollectionMap(t *testing.T) {
	src := `package main
func main() {
	xs := []int{1, 2, 3}
	ys := xs.map(x => x * 2)
	println(ys[0])
}
`
	got := compileOK(t, "lambda6.gx", src)
	containsAll(t, got, `Map(func(x int) int {return x * 2})`)
}

func TestLambdaCollectionFilter(t *testing.T) {
	src := `package main
func main() {
	xs := []int{1, 2, 3, 4}
	ys := xs.filter(x => x > 2)
	println(len(ys))
}
`
	got := compileOK(t, "lambda7.gx", src)
	containsAll(t, got, `Filter(func(x int) bool {return x > 2})`)
}

func TestLambdaUnionParamErased(t *testing.T) {
	src := `package main
func main() {
	f := (x int|string) => x
	_ = f
}
`
	got := compileOK(t, "lambda9.gx", src)
	containsAll(t, got, `func(x any) any {return x}`)
}

func TestLambdaUnusedParamAny(t *testing.T) {
	// 参数未被使用且 body 无依赖时降级 any，仍能产出合法 Go。
	src := `package main
func main() {
	f := x => "hi"
	println(f(1))
}
`
	got := compileOK(t, "lambda10.gx", src)
	containsAll(t, got, `func(x any) string {return "hi"}`)
}

func TestLambdaStringLen(t *testing.T) {
	src := `package main
func main() {
	xs := []string{"a", "bb"}
	ys := xs.map(s => len(s))
	println(ys[1])
}
`
	got := compileOK(t, "lambda8.gx", src)
	containsAll(t, got, `Map(func(s string) int {return len(s)})`)
}

func TestLambdaVoidBuiltinBody(t *testing.T) {
	// body 是无返回值调用 → 生成 func(){...}，不能是 return println(...)
	src := `package main
func main() {
	f := () => println(1)
	f()
}
`
	got := compileOK(t, "lambdavoid.gx", src)
	containsAll(t, got, `func() {println(1)}`)
}

func TestLambdaVoidEffectContext(t *testing.T) {
	// effect 回调上下文强制 void，即便 body 是有返回值的表达式
	src := `package main
func main() {
	Signal.effect(() => println(1))
}
`
	got := compileOK(t, "lambdavoid2.gx", src)
	containsAll(t, got, `func() {println(1)}`)
}

func TestLambdaVoidDeclaredFunc(t *testing.T) {
	// 本文件声明的无返回值函数作为 body → void
	src := `package main
func logIt(x int) { println(x) }
func main() {
	f := x => logIt(x)
	f(1)
}
`
	got := compileOK(t, "lambdavoid3.gx", src)
	containsAll(t, got, `func(x any) {logIt(x)}`)
}

// TestLambdaContextFuncArg 回归 H-2：恒等 lambda 作为函数实参时，
// 参数/返回类型必须从被调函数签名的对应参数位推断（body 无字面量可反推）。
func TestLambdaContextFuncArg(t *testing.T) {
	src := `package main
func apply(f func(int) int) { println(f(1)) }
func main() {
	apply(x => x)
}
`
	got := compileOK(t, "lambdactx1.gx", src)
	containsAll(t, got, `apply(func(x int) int {return x})`)
}

// TestLambdaContextFuncArgMulti 验证多参数函数类型 + 实参序号定位：
// 签名 func(func(int,int) int, string) 的第 0 位回调必须推断成 (int,int)->int。
func TestLambdaContextFuncArgMulti(t *testing.T) {
	src := `package main
func combine(f func(int,int) int, s string) { println(f(1,2), s) }
func main() {
	combine((a,b) => a+b, "x")
}
`
	got := compileOK(t, "lambdactx2.gx", src)
	containsAll(t, got, `func(a int,b int) int {return a+b}`)
}

// TestLambdaContextAssign 验证赋值初始化场景：var f func(int,string) bool = ...
func TestLambdaContextAssign(t *testing.T) {
	src := `package main
func main() {
	var f func(int,string) bool = (n,s) => true
	println(f(1,"a"))
}
`
	got := compileOK(t, "lambdactx3.gx", src)
	containsAll(t, got, `func(n int,s string) bool {return true}`)
}

// TestLambdaComputedGetterArrow 验证 SolidJS 风格：computed 的箭头函数 body 是
// 单点 signal getter 调用时，从改写前采集的 signal 声明类型反查返回类型，
// 不降级 any。这是 computed(() => count()) 正确工作的关键。
func TestLambdaComputedGetterArrow(t *testing.T) {
	src := `package main
func main() {
	signal count = 0
	signal name = "gx"
	signal ok = true
	_ = Signal.computed(() => count())
	_ = Signal.computed(() => name())
	_ = Signal.computed(() => ok())
	_ = Signal.computed(() => count() * 2)
}
`
	got := compileOK(t, "lambdacomp.gx", src)
	containsAll(t, got,
		`Computed(func() int {return count()})`,
		`Computed(func() string {return name()})`,
		`Computed(func() bool {return ok()})`,
		`Computed(func() int {return count() * 2})`,
	)
	if strings.Contains(got, `func() any {return count()}`) ||
		strings.Contains(got, `func() any {return name()}`) ||
		strings.Contains(got, `func() any {return ok()}`) {
		t.Fatalf("单点 signal getter 不应降级 any:\n%s", got)
	}
}
