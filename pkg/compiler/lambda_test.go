package compiler

import "testing"

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
