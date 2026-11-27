// Package integration 提供 GX 端到端测试：GX 源码 → Compile → SourceMap →
// go build → go run → 校验真实输出。与 golden 测试（仅比对生成文本）互补，
// 这里验证组合特性能真正编译并产出正确运行结果。
package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robertpanvip/gox/internal/buildcheck"
	"github.com/robertpanvip/gox/pkg/compiler"
	"github.com/robertpanvip/gox/widgets"
)

const modulePath = "github.com/robertpanvip/gox"

// runGX 完整链路：转译 → 校验 sourcemap → 真实 go run → 返回 stdout。
func runGX(t *testing.T, gxSrc string) string {
	t.Helper()
	res, err := compiler.CompileWithModule("input.gx", gxSrc, modulePath)
	if err != nil {
		t.Fatalf("转译失败: %v", err)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("生成 Go 语法无效: %v\n---\n%s", err, res.GoSource)
	}
	if res.SourceMap == nil || res.SourceMap.Len() == 0 {
		t.Fatal("sourcemap 为空")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(res.GoSource), 0644); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	gomod := "module integration\n\ngo 1.27\n\nrequire " + modulePath + " v0.0.0\nreplace " + modulePath + " => " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=go1.27.4")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run 失败: %v\n--- output ---\n%s\n--- generated ---\n%s", err, out, res.GoSource)
	}
	return string(out)
}

func TestIntegrationPubEnumUnionTypeguardCollections(t *testing.T) {
	src := `package main

import "fmt"

pub enum status {
	active = 1,
	inactive = 2,
}

pub func describe(v int | string) string {
	if v is int {
		return fmt.Sprintf("int:%d", v)
	}
	return fmt.Sprintf("str:%s", v)
}

func main() {
	fmt.Println(describe(42))
	fmt.Println(describe("hi"))
	fmt.Println(statusActive.String())
	xs := []int{1, 2, 3}
	total := xs.map(func(x int) int { return x * 2 }).reduce(0, func(a int, b int) int { return a + b })
	fmt.Println(total)
}
`
	out := runGX(t, src)
	for _, want := range []string{"int:42", "str:hi", "active", "12"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestIntegrationJSXRender(t *testing.T) {
	// 默认 Compile 入口统一走 scene opcode 后端：JSX → gxscene.Render。
	src := `package main

import "fmt"

func main() {
	view := <div class="box">hello</div>
	fmt.print(view)
}
`
	out := runGX(t, src)
	for _, want := range []string{`<div class="box">`, `"hello"`, `</div>`} {
		if !strings.Contains(out, want) {
			t.Errorf("JSX 渲染输出应包含 %q, got:\n%s", want, out)
		}
	}
}

// runGXScene 与 runGX 相同，但用 opcode 后端（CompileScene）转译。
func runGXScene(t *testing.T, gxSrc string) string {
	t.Helper()
	res, err := compiler.CompileSceneWithModule("input.gx", gxSrc, modulePath)
	if err != nil {
		t.Fatalf("转译失败: %v", err)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("生成 Go 语法无效: %v\n---\n%s", err, res.GoSource)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(res.GoSource), 0644); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	gomod := "module integration\n\ngo 1.27\n\nrequire " + modulePath + " v0.0.0\nreplace " + modulePath + " => " + root + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=go1.27.4")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run 失败: %v\n--- output ---\n%s\n--- generated ---\n%s", err, out, res.GoSource)
	}
	return string(out)
}

func TestIntegrationJSXSceneFineGrained(t *testing.T) {
	// 路线三：JSX → gxscene opcode 模板；静态结构缓存，动态插值细粒度更新。
	src := `package main
import (
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"fmt"
)
func card(title func() string, n func() int) *gxscene.Instance {
	return <div class="card"><h1>{title()}</h1><span>{n()}</span></div>
}
func main() {
	title := "alpha"
	count := 1
	view := card(func() string { return title }, func() int { return count })
	fmt.Print(view)
	// 只改 title：应只写 1 个节点
	title = "beta"
	fmt.Println("writes:", view.Update())
	fmt.Print(view)
	// 值未变：应写 0 个节点
	fmt.Println("writes:", view.Update())
}
`
	out := runGXScene(t, src)
	for _, want := range []string{
		"alpha", "beta", `"1"`,
		"writes: 1", "writes: 0",
		`<div class="card">`, "<h1>", "<span>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestIntegrationBuildErrorMapsBackToGx(t *testing.T) {
	// 中文出现在使用错误符号之前；诊断须回投到 GX 第 4 行（字节列修复验证）。
	src := `package main
func main() {
	println("中文测试😀")
	undefinedFn()
}
`
	res, err := compiler.CompileWithModule("input.gx", src, modulePath)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.Abs(filepath.Join("..", ".."))
	checker := buildcheck.New(8)
	checker.SetToolchain("go", "GOTOOLCHAIN=go1.27.4")
	checker.AddReplace(modulePath, root)
	diags, err := checker.Check(context.Background(), res.GoSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) == 0 {
		t.Fatal("错误代码应产生构建诊断")
	}
	mapped := false
	for _, d := range diags {
		gx, ok := res.SourceMap.GoToGx(d.Line, d.Col)
		if !ok {
			continue
		}
		if gx.Line == 4 && strings.Contains(d.Message, "undefined") {
			mapped = true
		}
	}
	if !mapped {
		t.Fatalf("未找到回投到 GX 第 4 行的 undefined 诊断: %v", diags)
	}
}

func TestIntegrationStdlibSliceChaining(t *testing.T) {
	// 标准库返回原生 []T，链式调用应自动 list.Of 包装（架构问题 #13）。
	src := `package main

import (
	"fmt"
	"strings"
)

func main() {
	xs := strings.Fields("hello brave world")
	upper := xs.map(func(s string) string { return strings.ToUpper(s) })
	fmt.Println(upper[0], upper[1], upper[2])
	total := xs.map(func(s string) int { return len(s) }).reduce(0, func(a int, b int) int { return a + b })
	fmt.Println(total)
}
`
	out := runGX(t, src)
	for _, want := range []string{"HELLO", "BRAVE", "WORLD", "15"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestIntegrationObjectLiteral(t *testing.T) {
	// 对象字面量：匿名结构体（字段类型反推）+ 期望类型补全（var/arg/assign/return）。
	src := `package main
import "fmt"
type Point struct {
	name string
	age  int
}
func describe(p Point) string { return fmt.Sprint(p.name, ":", p.age) }
func makePoint() Point { return { name: "ret", age: 4 } }
func main() {
	anon := { name: "anon", age: 1 }
	fmt.Println(anon.name, anon.age)
	var p Point = { name: "var", age: 2 }
	fmt.Println(describe(p))
	fmt.Println(describe({ name: "arg", age: 3 }))
	p = { name: "assign", age: 5 }
	fmt.Println(describe(p))
	fmt.Println(describe(makePoint()))
	nested := { name: "n", inner: { a: 10, ok: true } }
	fmt.Println(nested.inner.a, nested.inner.ok)
}
`
	out := runGX(t, src)
	for _, want := range []string{
		"anon 1", "var:2", "arg:3", "assign:5", "ret:4", "10 true",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("对象字面量输出应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestIntegrationObjectLiteralAnyExpect(t *testing.T) {
	// 期望类型为 any / interface{} 时对象字面量按匿名结构体编译，
	// 真实 go run 验证能编译运行且字段值正确。
	src := `package main
import "fmt"
func make() any { return { name: "x", age: 7 } }
func pass(v any) { fmt.Println(v) }
func main() {
	fmt.Println(make())
	var i interface{} = { name: "y", age: 8 }
	fmt.Println(i)
	pass({ name: "z", age: 9 })
}
`
	out := runGX(t, src)
	for _, want := range []string{"{x 7}", "{y 8}", "{z 9}"} {
		if !strings.Contains(out, want) {
			t.Errorf("any 期望类型下匿名结构体输出应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestIntegrationObjectDestructure(t *testing.T) {
	// 构造后立刻解构：S₁={name,age} 构造、S₂={name,age} 解构，均 ⊆ fields(Student)。
	// 覆盖短声明 / var 形式 / 匿名 struct / 部分字段解构。
	src := `package main
import "fmt"
type Student struct {
	name string
	age  int
	city string
}
func main() {
	{
		var p Student = { name: "x", age: 1, city: "SZ" }
		{ name, age } := p
		fmt.Println(name, age)
	}
	{
		var q Student = { name: "y", age: 2, city: "BJ" }
		var { name, city } = q
		fmt.Println(name, city)
	}
	{
		anon := { name: "z", age: 3 }
		{ name, age } := anon
		fmt.Println(name, age)
	}
}
`
	out := runGX(t, src)
	for _, want := range []string{"x 1", "y BJ", "z 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("对象解构输出应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestIntegrationLambda(t *testing.T) {
	// Lambda 最简箭头：表达式反推（int/bool）、显式参数、集合回调、函数实参。
	src := `package main
import "fmt"
func apply(f func(int) int, v int) int { return f(v) }
func main() {
	double := x => x * 2
	isBig := x => x > 10
	add := (a int, b int) => a + b
	fmt.Println(double(7), isBig(20), add(3, 4))
	fmt.Println(apply(x => x + 1, 41))
	xs := []int{1, 2, 3, 4}
	ys := xs.map(x => x * 2).filter(x => x > 4)
	fmt.Println(len(ys), ys[0], ys[1])
}
`
	out := runGX(t, src)
	for _, want := range []string{"14 true 7", "42", "2 6 8"} {
		if !strings.Contains(out, want) {
			t.Errorf("Lambda 输出应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestIntegrationDefaultParams(t *testing.T) {
	// TS 语义核心：未传用默认值，显式传 0/"" 用零值而非默认值。
	src := `package main
import "fmt"
func greet(name string = "world", times int = 2) string {
	out := ""
	for i := 0; i < times; i++ { out += name + " " }
	return out
}
func main() {
	fmt.Println(greet())
	fmt.Println(greet("gx"))
	fmt.Println(greet("gx", 1))
	fmt.Println(greet("", 1))
	fmt.Println(greet("z", 0))
}
`
	out := runGX(t, src)
	// 每行以 \n 结尾（含最后的空串行），去掉末尾一个 \n 再切分。
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("应输出 5 行, got %d: %q", len(lines), out)
	}
	checks := [][2]string{
		{lines[0], "world world "},
		{lines[1], "gx gx "},
		{lines[2], "gx "},
		{lines[3], " "},
		{lines[4], ""},
	}
	for _, c := range checks {
		if c[0] != c[1] {
			t.Errorf("期望 %q, got %q (完整输出: %q)", c[1], c[0], out)
		}
	}
}

func TestIntegrationTemplateString(t *testing.T) {
	// 模板字符串：静态段 + 多插值 + 嵌套表达式，真实运行校验拼接结果。
	src := `package main
import "strconv"
func main() {
	name := "gx"
	score := 95
	msg := ` + "`hello ${name}, score=${score}, double=${strconv.itoa(score * 2)}`" + `
	println(msg)
}
`
	out := runGX(t, src)
	if !strings.Contains(out, "hello gx, score=95, double=190") {
		t.Errorf("模板字符串输出不符, got:\n%s", out)
	}
}

func TestIntegrationLocalSliceNoDoubleWrap(t *testing.T) {
	// 本地 []T 字面量已提升为 List，链式调用不应再 list.Of（golden 语义）。
	src := `package main
import "fmt"
func main() {
	xs := []int{1, 2, 3, 4}
	total := xs.map(func(x int) int { return x * 2 }).reduce(0, func(a int, b int) int { return a + b })
	fmt.Println(total)
}
`
	out := runGX(t, src)
	if !strings.Contains(out, "20") {
		t.Errorf("本地切片链式结果应为 20, got:\n%s", out)
	}
}

func TestIntegrationSignal(t *testing.T) {
	// sig 声明 + computed 派生 + effect 自动重跑 + 读/写改写 + 函数式运行时。
	src := `package main
sig count = 0
func main() {
    let doubled = Signal.computed(() => count * 2)
    Signal.effect(() => println("effect:", count))
    println("d0", doubled)
    count = 5
    println("d1", doubled)
}
`
	out := runGX(t, src)
	for _, want := range []string{
		"effect: 0", // effect 首次执行
		"d0 0",
		"effect: 5", // count=5 后 effect 自动重跑
		"d1 10",     // computed 派生值随 count 更新
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出应包含 %q, got:\n%s", want, out)
		}
	}
	// effect 首次 + 写入重跑，共两次
	if n := strings.Count(out, "effect:"); n != 2 {
		t.Errorf("effect 应执行 2 次, got %d:\n%s", n, out)
	}
}

func TestIntegrationSignalCompoundWrite(t *testing.T) {
	// 复合赋值/自增自减写 sig，computed 与 effect 应观察到最终结果。
	src := `package main
import "fmt"
sig n = 0
func main() {
    let v = Signal.computed(() => n)
    n += 3
    n++
    n--
    fmt.Println("v", v)
}
`
	out := runGX(t, src)
	if !strings.Contains(out, "v 3") {
		t.Errorf("n=0 +=3 ++ -- 应为 3, got:\n%s", out)
	}
}

func TestIntegrationArrowBlockCallbacks(t *testing.T) {
	// SolidJS 风格：sig 回调用箭头函数，多语句用花括号 body () => { ... }。
	// batch 内两条写语句、effect 花括号 body 嵌套调用。
	src := `package main
import "fmt"
sig n = 0
func main() {
    let doubled = Signal.computed(() => n * 2)
    Signal.effect(() => { fmt.Println("init", doubled) })
    Signal.batch(() => { n = 4; n = 5 })
    fmt.Println("final", n, doubled)
}
`
	out := runGX(t, src)
	for _, want := range []string{
		"init 0",  // effect 花括号 body 首次执行
		"init 10", // batch 写入 n=5 后 effect 重跑，doubled=10
		"final 5 10",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestIntegrationSignalBitwiseCompound(t *testing.T) {
	// 位运算复合赋值：&= / |= / ^= / <<= / >>= / &^= 真实运行结果。
	src := `package main
import "fmt"
sig n = 0
func main() {
    n |= 12        // 12
    n &= 10        // 8
    n ^= 3         // 11
    n <<= 1        // 22
    n >>= 2        // 5
    n &^= 1        // 4
    fmt.Println("n", n)
}
`
	out := runGX(t, src)
	if !strings.Contains(out, "n 4") {
		t.Errorf("位复合赋值最终结果应为 4, got:\n%s", out)
	}
}

func TestIntegrationSignalMultipleEffects(t *testing.T) {
	// 多个 sig、多个 effect，依赖追踪互不串扰。
	src := `package main
sig a = 1
sig b = 10
func main() {
    Signal.effect(() => println("A", a))
    Signal.effect(() => println("B", b))
    a = 2
    b = 20
}
`
	out := runGX(t, src)
	for _, want := range []string{"A 1", "B 10", "A 2", "B 20"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestIntegrationJSXSceneConditionalBranch(t *testing.T) {
	// 阶段2：JSX 条件分支 → OpBranch + BranchGetter。
	// 验证：selector 依赖切换分支、分支内 sig 只更新叶子、组件不重建。
	src := `package main
import (
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"fmt"
)
func main() {
	sig visible = true
	sig count = 10
	view := <div>
		if visible {
			<span>{count}</span>
		} else {
			<b>Hidden</b>
		}
	</div>
	view.BindReactive(Signal.effect)
	fmt.Print("INIT:", view)
	// 分支内 sig 更新：只改叶子，不切分支。
	count = 20
	fmt.Print("COUNT:", view)
	// selector 依赖切换分支。
	visible = false
	fmt.Print("HIDE:", view)
	visible = true
	fmt.Print("SHOW:", view)
}
`
	out := runGXScene(t, src)
	for _, want := range []string{
		`INIT:<div>`, `"10"`, "<span>",
		`COUNT:<div>`, `"20"`,
		`HIDE:<div>`, `"Hidden"`, "<b>",
		`SHOW:<div>`, `"20"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出应包含 %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Hidden") && !strings.Contains(out, `HIDE:<div>`) {
		t.Errorf("Hidden 只应出现在 HIDE 阶段: %s", out)
	}
}

func TestIntegrationJSXSceneFor(t *testing.T) {
	// 阶段3：<For> keyed 列表——增删/渲染，每元素独立 Owner。
	src := `package main
import (
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"fmt"
)
type User struct { id int; name string }
func main() {
	sig users = []User{{1,"Tom"},{2,"Jack"}}
	view := <ul>
		<For each={users} key={(u) => u.id}>
			{(u User) => <li>{u.name}</li>}
		</For>
	</ul>
	view.BindReactive(Signal.effect)
	fmt.Print("INIT:", view)
	users = []User{{2,"Jack"},{1,"Tom"},{3,"Ann"}}
	fmt.Print("ADD:", view)
	users = []User{{1,"Tom"}}
	fmt.Print("DEL:", view)
}
`
	out := runGXScene(t, src)
	for _, want := range []string{
		`INIT:<ul>`, `"Tom"`, `"Jack"`,
		`ADD:<ul>`, `"Ann"`,
		`DEL:<ul>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("输出应包含 %q, got:\n%s", want, out)
		}
	}
	// DEL 阶段应只剩 Tom，不含 Jack/Ann（用最后一次 DEL 输出段断言）。
	idx := strings.LastIndex(out, "DEL:")
	if idx >= 0 {
		seg := out[idx:]
		if strings.Contains(seg, "Jack") || strings.Contains(seg, "Ann") {
			t.Errorf("DEL 阶段应只剩 Tom:\n%s", seg)
		}
	}
}

func TestIntegrationJSXSceneForValueUpdate(t *testing.T) {
	// item sig 根治端到端：同 key（id 不变）但值变化（name 改写）时必须刷新该项。
	// 旧的值快照实现会显示旧值（除非销毁重建），这里通过 u().name 直接读 item sig。
	src := `package main
import (
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"fmt"
)
type User struct { id int; name string }
func main() {
	sig users = []User{{1,"Tom"},{2,"Jack"}}
	view := <ul>
		<For each={users} key={(u) => u.id}>
			{(u User) => <li>{u.name}</li>}
		</For>
	</ul>
	view.BindReactive(Signal.effect)
	users = []User{{1,"Thomas"},{2,"Jack"}}
	fmt.Print("UPD:", view)
}
`
	out := runGXScene(t, src)
	idx := strings.LastIndex(out, "UPD:")
	if idx < 0 {
		t.Fatalf("应有 UPD 输出, got:\n%s", out)
	}
	seg := out[idx:]
	if !strings.Contains(seg, `"Thomas"`) {
		t.Errorf("同 key 值变化应刷新为 Thomas, got:\n%s", seg)
	}
	if strings.Contains(seg, `"Tom"`) {
		t.Errorf("不应残留旧值 Tom, got:\n%s", seg)
	}
	if !strings.Contains(seg, `"Jack"`) {
		t.Errorf("未变更的项应保持 Jack, got:\n%s", seg)
	}
}

func TestIntegrationJSXSceneForItemIntoCompProps(t *testing.T) {
	// P1 回归：item 传给子组件 props 时，编译器逃逸检测应回退到值快照 ForGetterOf，
	// 同 key 值变化靠整项重建取新值，子组件不会静默停在旧值。
	src := `package main
import (
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"fmt"
)
type User struct { id int; name string }
type CardProps struct { text string }
func Card(props CardProps) *gxscene.Instance {
	return <b>{props.text}</b>
}
func main() {
	sig users = []User{{1,"Tom"}}
	view := <ul>
		<For each={users} key={(u) => u.id}>
			{(u User) => <li><Card text={u.name}/></li>}
		</For>
	</ul>
	view.BindReactive(Signal.effect)
	users = []User{{1,"Thomas"}}
	fmt.Print("UPD:", view)
}
`
	out := runGXScene(t, src)
	idx := strings.LastIndex(out, "UPD:")
	if idx < 0 {
		t.Fatalf("应有 UPD 输出, got:\n%s", out)
	}
	seg := out[idx:]
	if !strings.Contains(seg, `"Thomas"`) {
		t.Errorf("item 进组件 props 回退后应刷新为 Thomas, got:\n%s", seg)
	}
	if strings.Contains(seg, `"Tom"`) {
		t.Errorf("不应残留旧值 Tom, got:\n%s", seg)
	}
}

func TestIntegrationJSXSceneForInferElemType(t *testing.T) {
	// render 参数无类型注解：应从 each={users}（sig []User）推断元素类型 User，
	// 生成可编译的 gxscene.ForGetterOfSignal[User](...)（item sig 根治）。
	src := `package main
import (
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"fmt"
)
type User struct { id int; name string }
func main() {
	sig users = []User{{1,"Tom"},{2,"Jack"}}
	view := <ul>
		<For each={users} key={(u) => u.id}>
			{(u) => <li>{u.name}</li>}
		</For>
	</ul>
	view.BindReactive(Signal.effect)
	fmt.Print("INIT:", view)
	users = []User{{2,"Jack"},{1,"Tom"},{3,"Ann"}}
	fmt.Print("ADD:", view)
}
`
	out := runGXScene(t, src)
	for _, want := range []string{
		`INIT:<ul>`, `"Tom"`, `"Jack"`,
		`ADD:<ul>`, `"Ann"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("无类型 render 推断应正常运行，输出应包含 %q, got:\n%s", want, out)
		}
	}
}

// TestIntegrationBareWindowConfig 端到端验证：gxapp.open({ ... }) 裸对象字面量
// 从外部包 AST 推断为 gxapp.WindowConfig，app.render(() => jsx) 渲染场景树并可真实编译运行。
func TestIntegrationBareWindowConfig(t *testing.T) {
	src := `package main
import (
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	"github.com/robertpanvip/gox/runtime/signal"
	"fmt"
)
type user struct { id int; name string }
pub func Stat(props gxscene.Props) *gxscene.Instance {
	label := gxscene.get[string](props, "label")
	return <div>{label()}</div>
}
func main() {
	cfg := { title: "Gox", width: 800, height: 600 }
	{ title, width } := cfg
	sig users = []user{{1,"Tom"}}
	app, err := gxapp.open({ Title: title, Width: width, Height: cfg.height })
	if err != nil { panic(err) }
	defer app.close()
	app.render(() => <div><Stat label={title}/><ul><For each={users} key={(u)=>u.id}>{(u)=><li>{u.name}</li>}</For></ul></div>)
	fmt.println(title)
}
`
	out := runGXScene(t, src)
	if !strings.Contains(out, "Tom") {
		t.Errorf("render 后应渲染 Tom，got:\n%s", out)
	}
}

// TestIntegrationAppRenderAutoWiresReactive 端到端验证：app.render(() => jsx) 内部已完成
// bindReactive 接线——GX 源码无需手写 bindReactive，sig 写入即自动更新 + 重绘一帧。
func TestIntegrationAppRenderAutoWiresReactive(t *testing.T) {
	src := `package main
import (
	gxapp "github.com/robertpanvip/gox/runtime/app"
	"fmt"
)
func main() {
	sig count = 0
	app, err := gxapp.open({ Title: "Counter", Width: 800, Height: 600 })
	if err != nil { panic(err) }
	defer app.close()
	app.render(() => <div id="app"><span>count = {count}</span></div>)
	fmt.println("---AFTER---")
	count = 5
}
`
	out := runGXScene(t, src)
	parts := strings.SplitN(out, "---AFTER---", 2)
	if len(parts) != 2 {
		t.Fatalf("应输出分界标记, got:\n%s", out)
	}
	if !strings.Contains(parts[0], `"0"`) {
		t.Errorf("首帧应含初始值 0:\n%s", parts[0])
	}
	if !strings.Contains(parts[1], `"5"`) || strings.Contains(parts[1], `"0"`) {
		t.Errorf("sig 写入后应自动重绘为 5（无手写接线）:\n%s", parts[1])
	}
}

// TestIntegrationStatefulComponent 端到端验证 SolidJS 风格有状态组件：
// 组件内声明局部 sig + computed + effect + onCleanup，定时器自增驱动状态变化；
// app.render(() => <App/>) 建树时建立根 Owner，close 时级联执行组件 onCleanup。
func TestIntegrationStatefulComponent(t *testing.T) {
	src := `package main
import (
	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"fmt"
	"time"
)
pub func Counter(props gxscene.Props) *gxscene.Instance {
	sig count = 0
	let doubled = Signal.computed(() => count * 2)
	let same = Signal.computed(() => count)
	Signal.effect(() => { fmt.println("tick", count, doubled, same) })
	stop := make(chan struct{})
	Signal.onMount(() => {
		go func() {
			ticker := time.newTicker(10 * time.millisecond)
			defer ticker.stop()
			for {
				select {
				case <-ticker.c:
					if count >= 3 { return }
					count += 1
				case <-stop:
					return
				}
			}
		}()
	})
	Signal.onCleanup(() => { close(stop); fmt.println("cleanup", count) })
	return <div><b>{count}</b><i>{doubled}</i></div>
}
func main() {
	app, err := gxapp.open({ Title: "x", Width: 2, Height: 2 })
	if err != nil { panic(err) }
	app.render(() => <div><Counter/></div>)
	time.sleep(150 * time.millisecond)
	app.close()
}
`
	out := runGXScene(t, src)
	// 局部状态自增到 3、computed 派生到 6、单点 getter computed 恒等于 count=3
	for _, want := range []string{`"3"`, `"6"`, "tick 3 6 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出应包含 %q, got:\n%s", want, out)
		}
	}
	// 组件 onCleanup 在 close 时执行（生命周期归属 App）
	if !strings.Contains(out, "cleanup 3") {
		t.Errorf("close 时应执行组件 cleanup, got:\n%s", out)
	}
}

func TestIntegrationSignalFieldLiveProps(t *testing.T) {
	// struct sig 字段语言特性：组件/持有者函数读到的是外部 sig 的实时值，
	// 而非构造时的一次性快照（用户核心诉求："组件值不是一次性的"）。
	src := `package main
type CardProps struct {
    sig name string
    age int
}
func show(p CardProps) string { return p.name }
func main() {
    sig src = "x"
    p := CardProps{name: src, age: 3}
    println(show(p))
    src = "changed"
    println(show(p))
    p.name = "direct"
    println(p.name)
}
`
	out := runGX(t, src)
	lines := strings.Fields(strings.TrimSpace(out))
	// println 逐个输出：x / changed / direct（顺序严格）
	want := []string{"x", "changed", "direct"}
	if len(lines) < len(want) {
		t.Fatalf("输出行数不足: %q", out)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("第 %d 行应为 %q, got %q\n完整输出:\n%s", i+1, w, lines[i], out)
		}
	}
}

func TestIntegrationSignalFieldHandwrittenDerivedLive(t *testing.T) {
	// 第六轮 P2b 端到端：手写构造的派生表达式（含 sig 读）经 Computed 保鲜，
	// 源 sig 变化后读字段得到新值，而非构造时的快照。
	src := `package main
type CardProps struct { sig label string }
func show(p CardProps) string { return p.label }
func main() {
	sig n = 1
	p := CardProps{label: "n=" + Itoa(n)}
	println(show(p))
	n = 2
	println(show(p))
}
func Itoa(x int) string {
	if x == 0 { return "0" }
	digits := ""
	for x > 0 { digits = string(rune('0' + x%10)) + digits; x = x / 10 }
	return digits
}
`
	out := runGX(t, src)
	lines := strings.Fields(strings.TrimSpace(out))
	want := []string{"n=1", "n=2"}
	if len(lines) < len(want) {
		t.Fatalf("输出行数不足: %q", out)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("第 %d 行应为 %q, got %q\n完整输出:\n%s", i+1, w, lines[i], out)
		}
	}
}

func TestIntegrationSignalFieldHandwrittenFieldToFieldLive(t *testing.T) {
	// 第七轮 §4 端到端：手写字段间赋值 q.a = p.a 直通 getter，源 sig 变化后
	// 经 p.a 读到新值（JSX 侧 TestIntegrationSignalFieldLiveProps 的手写对应版）。
	src := `package main
type P struct { sig a string }
type Q struct { sig a string }
func main() {
	sig src = "x"
	p := P{a: src}
	q := Q{a: p.a}
	println(q.a)
	src = "y"
	println(q.a)
}
`
	out := runGX(t, src)
	lines := strings.Fields(strings.TrimSpace(out))
	want := []string{"x", "y"}
	if len(lines) < len(want) {
		t.Fatalf("输出行数不足: %q", out)
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("第 %d 行应为 %q, got %q\n完整输出:\n%s", i+1, w, lines[i], out)
		}
	}
}

func TestIntegrationSignalFieldNestedStruct(t *testing.T) {
	// sig 字段与复合写/自增的真实运行。
	src := `package main
type Counter struct { sig n int }
func main() {
    c := Counter{n: 1}
    c.n += 2
    c.n++
    println(c.n)
}
`
	out := runGX(t, src)
	if !strings.Contains(out, "4") {
		t.Errorf("复合写+自增结果应为 4, got:\n%s", out)
	}
}

func TestIntegrationSignalFieldJSXItemPropLive(t *testing.T) {
	// 第五轮根治端到端：sig 字段 props + For item 派生态。
	// <Card text={u.name}/> 中 text 是 sig 字段 → 生成 Signal.Computed(func() string{return u().name})，
	// item 值变化时 Computed 重新求值，组件内 {p.text()} 读到新值，无需整项重建。
	src := `package main
import (
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"fmt"
)
type User struct { id int; name string }
type CardProps struct { sig text string }
func Card(props CardProps) *gxscene.Instance {
	return <b>{props.text}</b>
}
func main() {
	sig users = []User{{1,"Tom"},{2,"Jack"}}
	view := <ul>
		<For each={users} key={(u) => u.id}>
			{(u User) => <li><Card text={u.name}/></li>}
		</For>
	</ul>
	view.BindReactive(Signal.effect)
	users = []User{{1,"Thomas"},{2,"Jack"}}
	fmt.Print("UPD:", view)
}
`
	out := runGXScene(t, src)
	idx := strings.LastIndex(out, "UPD:")
	if idx < 0 {
		t.Fatalf("应有 UPD 输出, got:\n%s", out)
	}
	seg := out[idx:]
	if !strings.Contains(seg, `"Thomas"`) {
		t.Errorf("sig 字段 props 应随 item 刷新为 Thomas, got:\n%s", seg)
	}
	if strings.Contains(seg, `"Tom"`) {
		t.Errorf("不应残留旧值 Tom, got:\n%s", seg)
	}
	if !strings.Contains(seg, `"Jack"`) {
		t.Errorf("未变更项应保持 Jack, got:\n%s", seg)
	}
}

// 第八轮回归：普通变量 / 普通字段作为 sig 字段的值，编译成裸 getter，
// 后续修改普通变量能读到新值——用户不应感知 sig 与普通变量的差别。
func TestIntegrationPlainVarSignalFieldStaysLive(t *testing.T) {
	src := `package main
import "fmt"
type Q struct { title string }
type P struct { sig a string; sig b string }
func main() {
	plain := "x"
	q := Q{title: "y"}
	p := P{a: plain, b: q.title}
	fmt.Println("before", p.a, p.b)
	plain = "X"
	q.title = "Y"
	fmt.Println("after", p.a, p.b)
}
`
	out := runGX(t, src)
	if !strings.Contains(out, "before x y") || !strings.Contains(out, "after X Y") {
		t.Errorf("普通变量应通过裸 getter 保鲜（before x y / after X Y）, got:\n%s", out)
	}
}

// 第八轮回归：含 sig 读的派生表达式仍走 Computed 保鲜；链式 sig 字段读
// 按 receiver 类型补 () 后能读到真实值（P1-2 / P1-3 的运行级验证）。
func TestIntegrationDerivedAndChainSignalFieldsLive(t *testing.T) {
	src := `package main
import "fmt"
type P struct { sig a string; sig b string }
type Inner struct { sig v string }
type Mid struct { in Inner }
type Outer struct { mid Mid }
func main() {
	p := P{a: "x", b: "y"}
	q := P{a: p.a + p.b}
	o := Outer{mid: Mid{in: Inner{v: "chain"}}}
	fmt.Println("before", q.a, o.mid.in.v)
	p.a = "X"
	p.b = "Y"
	fmt.Println("after", q.a)
}
`
	out := runGX(t, src)
	if !strings.Contains(out, "before xy chain") || !strings.Contains(out, "after XY") {
		t.Errorf("派生 Computed 与链式字段读应保鲜, got:\n%s", out)
	}
}

// 第九轮：链中间 sig 段的读写合一 getter 必须在 receiver 更换后保鲜。
// q.v 来自 o.mid.b.c（b 是 sig）；b 换成新 Leaf 后，q.v() 必须读到新 Leaf 的 c，
// 且 q.v = val 必须写到 b 当前指向的 Leaf——旧的「剥 () 裸透传」会固定初始 receiver。
func TestIntegrationChainRWGetterFreshReceiver(t *testing.T) {
	src := `package main
import "fmt"
type Leaf struct { sig c string }
type Mid struct { sig b Leaf }
type Outer struct { mid Mid }
type Q struct { sig v string }
func main() {
	sig lb = Leaf{c: "first"}
	o := Outer{mid: Mid{b: lb}}
	q := Q{v: o.mid.b.c}
	fmt.Println("read1", q.v)
	lb = Leaf{c: "second"}
	fmt.Println("read2", q.v)
	q.v = "written"
	fmt.Println("read3", q.v)
}
`
	out := runGX(t, src)
	want := []string{"read1 first", "read2 second", "read3 written"}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("链读写 getter 未保鲜, 期望包含 %q, got:\n%s", w, out)
		}
	}
}

// 第十轮 P1-A：sig 字段值是结构体构造（Signal.New 一次构造）时必须可写，
// 写入整对象后读到的应是新对象——旧「第四档裸 getter」会静默丢参数。
func TestIntegrationR10StructFieldWritable(t *testing.T) {
	src := `package main
import "fmt"
type Leaf struct { sig v string }
type Mid struct { sig leaf Leaf }
func main() {
	m := Mid{leaf: Leaf{v: "a"}}
	fmt.Println("before", m.leaf.v)
	m.leaf = Leaf{v: "b"}
	fmt.Println("after1", m.leaf.v)
	m.leaf.v = "c"
	fmt.Println("after2", m.leaf.v)
}
`
	out := runGX(t, src)
	want := []string{"before a", "after1 b", "after2 c"}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("结构体构造字段可写性失败, 期望 %q, got:\n%s", w, out)
		}
	}
}

// 第十轮 P1-2：普通变量构造的 sig 字段用 Signal.LockedGetter——
// 读跟随、写落回变量（跟随语义不变）。
func TestIntegrationR10PlainVarGetterWritable(t *testing.T) {
	src := `package main
import "fmt"
type P struct { sig a string }
func main() {
	v := "one"
	p := P{a: v}
	p.a = "two"
	fmt.Println("field", p.a, "var", v)
	v = "three"
	fmt.Println("follow", p.a)
}
`
	out := runGX(t, src)
	// p.a="two" 写回 v；之后 v="three" 读跟随。
	if !strings.Contains(out, "field two") || !strings.Contains(out, "var two") || !strings.Contains(out, "follow three") {
		t.Errorf("LockedGetter 写回/跟随失败, got:\n%s", out)
	}
}

// 第十一轮：中间 sig 段显式写调用括号的链尾写必须真正生效（与裸写一致）。
func TestIntegrationR11ExplicitParenMiddleWrite(t *testing.T) {
	src := `package main
import "fmt"
type Leaf struct { sig v string }
type Mid struct { sig leaf Leaf }
func main() {
	m := Mid{leaf: Leaf{v: "a"}}
	fmt.Println("before", m.leaf.v)
	m.leaf.v = "c"
	fmt.Println("after", m.leaf.v)
	m.leaf.v += "!"
	fmt.Println("compound", m.leaf.v)
}
`
	out := runGX(t, src)
	want := []string{"before a", "after c", "compound c!"}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("显式括号中间段写后读失败, 期望 %q, got:\n%s", w, out)
		}
	}
}

// 第十轮 P1-3：工厂函数返回 struct 的 sig 字段，读写必须正确改写并能运行。
// p := makeP() 若类型回填失败，p.a 会打印函数地址或写时编译失败。
func TestIntegrationR11FactoryResultField(t *testing.T) {
	src := `package main
import "fmt"
type P struct { sig a string }
func makeP(v string) P { return P{a: v} }
func makeP2(v string) *P { return &P{a: v} }
func main() {
	p := makeP("x")
	q := makeP2("y")
	fmt.Println("read", p.a, q.a)
	p.a = "z"
	q.a = "w"
	fmt.Println("write", p.a, q.a)
}
`
	out := runGX(t, src)
	if !strings.Contains(out, "read x y") || !strings.Contains(out, "write z w") {
		t.Errorf("工厂返回字段读写失败, got:\n%s", out)
	}
}

// 第十一轮 N-3：档5 跟随普通变量的字段改用按变量共享锁的 LockedGetterShared，
// 端到端验证生成代码可真实编译运行、写落回变量、读跟随最新值（跟随语义不变）。
func TestIntegrationR12SharedLockPlainVarField(t *testing.T) {
	src := `package main
import "fmt"
type P struct { sig a string; sig b string }
func main() {
	v := "x"
	p := P{a: v, b: v}
	fmt.Println("init", p.a, p.b)
	p.a = "y"
	fmt.Println("write-a", p.a, p.b, v)
	v = "z"
	fmt.Println("follow", p.a, p.b)
}
`
	out := runGX(t, src)
	want := []string{"init x x", "write-a y y y", "follow z z"}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("共享锁跟随语义失败, 期望 %q, got:\n%s", w, out)
		}
	}
}

// 第十二轮 P2-B+P2-C：m, err := load() 的多左值与 map 字面量类型推断联动——
// m["k"].a 必须被识别为 sig 字段并改写，否则静默打印函数地址、写生成非法 Go。
func TestIntegrationR12MultiLHSMapIndex(t *testing.T) {
	src := `package main
import "fmt"
type P struct { sig a string }
func load() (map[string]P, error) { return map[string]P{"k": {a: "x"}}, nil }
func main() {
	m, err := load()
	fmt.Println("read", m["k"].a, err == nil)
	m["k"].a = "y"
	fmt.Println("write", m["k"].a)
}
`
	out := runGX(t, src)
	if !strings.Contains(out, "read x true") || !strings.Contains(out, "write y") {
		t.Errorf("多左值 map 索引读写改写失败, got:\n%s", out)
	}
	if strings.Contains(out, "0x") {
		t.Errorf("不得打印函数地址（sig 字段读未改写）, got:\n%s", out)
	}
}

// 第十二轮 P2-D：结构体字段容器（map / slice）里的 sig 字段读写真实运行验证。
func TestIntegrationR12StructFieldContainerIndex(t *testing.T) {
	src := `package main
import "fmt"
type P struct { sig a string }
type S struct { m map[string]P; ps []P }
func main() {
	s := S{m: map[string]P{"k": {a: "x"}}, ps: []P{{a: "p"}}}
	fmt.Println("map", s.m["k"].a, "slice", s.ps[0].a)
	s.m["k"].a = "y"
	fmt.Println("write", s.m["k"].a)
}
`
	out := runGX(t, src)
	if !strings.Contains(out, "map x slice p") || !strings.Contains(out, "write y") {
		t.Errorf("结构体字段容器 sig 读写失败, got:\n%s", out)
	}
	if strings.Contains(out, "0x") {
		t.Errorf("不得打印函数地址, got:\n%s", out)
	}
}

// GX 语言层 Context/Provider 端到端：小写 .use()/.provider() 还原为 Go API、
// Provider 子树隔离、sig 驱动 value 响应式穿透 consumer。对应 examples/jsxapp-context.gx。
func TestIntegrationContextProvider(t *testing.T) {
	src := `package main

import (
    gxscene "github.com/robertpanvip/gox/runtime/scene"
    "github.com/robertpanvip/gox/runtime/signal"
    "github.com/robertpanvip/gox/widgets"
    "fmt"
)

var scoreCtx = widgets.NewContext[int](0)

// consumer：建树期 .use() 读 context，动态插槽 getter 读取即订阅
pub func Score(props gxscene.Props) *gxscene.Instance {
    score := scoreCtx.use()
    return <b class="score">{score()}</b>
}

// provider：包一层组件函数，Props 带类型变量后传 .provider()
pub func ScoreProvider(props gxscene.Props) *gxscene.Instance {
    p := gxscene.Props{
        "value": gxscene.Get[int](props, "v"),
        "children": gxscene.Get[any](props, "children"),
    }
    return scoreCtx.provider(p)
}

pub func Swatch(props gxscene.Props) *gxscene.Instance {
    accent := widgets.UseToken("accent")
    return <span class="swatch">{accent()}</span>
}

func main() {
    widgets.SetTheme(widgets.LightTheme)
    sig bonus = 2
    view := <div id="app">
        <ScoreProvider v={bonus}><Score/></ScoreProvider>
        <div class="panel">
            <widgets.ThemeProvider value={widgets.DarkTheme}>
                <Swatch/>
            </widgets.ThemeProvider>
        </div>
    </div>
    view.BindReactive(signal.Effect)
    fmt.println("INIT", view)
    bonus = 7
    fmt.println("CHANGED", view)
}
`
	out := runGX(t, src)
	// 初始：Score=2，子树主题=DarkTheme accent
	if !strings.Contains(out, "INIT") || !strings.Contains(out, "2") {
		t.Errorf("初始渲染应含分数 2, got:\n%s", out)
	}
	// 子树隔离：ThemeProvider 内 accent 必须是 DarkTheme.DarkBlue，而非全局
	// 浅色 LightBlue。断言引用 widgets 常量——主题换色（如曾短暂切到
	// Material Oceanic 青色）不会打破本用例。
	if !strings.Contains(out, widgets.DarkBlue) {
		t.Errorf("ThemeProvider 子树应读到 DarkTheme accent %s, got:\n%s", widgets.DarkBlue, out)
	}
	if strings.Contains(out, widgets.LightBlue) {
		t.Errorf("ThemeProvider 子树不应回落到全局浅色 %s, got:\n%s", widgets.LightBlue, out)
	}
	// 响应式：bonus=7 后 Score 应更新为 7（产物里出现两次分数，CHANGED 段为 7）
	if !strings.Contains(out, "CHANGED") || !strings.Contains(out, "7") {
		t.Errorf("bonus=7 后应重绘分数 7, got:\n%s", out)
	}
	// 不得打印函数地址（use 未还原会导致快照/地址）
	if strings.Contains(out, "0x") {
		t.Errorf("不得打印函数地址, got:\n%s", out)
	}
}

// 同一 Context、不同子树各有各的值：兄弟隔离 + 嵌套就近覆盖 + 无 Provider 读全局。
// 对应 examples/crossfile-context 的语义（单文件版，验证 GX 语法层还原与运行值）。
func TestIntegrationContextSubtreeIsolation(t *testing.T) {
	src := `package main

import (
    gxscene "github.com/robertpanvip/gox/runtime/scene"
    "github.com/robertpanvip/gox/runtime/signal"
    "github.com/robertpanvip/gox/widgets"
    "fmt"
)

var ctx = widgets.NewContext[string]("GLOBAL")

pub func Label(props gxscene.Props) *gxscene.Instance {
    v := ctx.use()
    return <b>{v()}</b>
}

pub func Provide(props gxscene.Props) *gxscene.Instance {
    p := gxscene.Props{
        "value": gxscene.Get[string](props, "v"),
        "children": gxscene.Get[any](props, "children"),
    }
    return ctx.provider(p)
}

func main() {
    view := <div>
        <span class="a"><Provide v="A"><Label/></Provide></span>
        <span class="b"><Provide v="B"><Label/></Provide></span>
        <span class="nest"><Provide v="OUT"><Label/><Provide v="IN"><Label/></Provide></Provide></span>
        <span class="g"><Label/></span>
    </div>
    view.BindReactive(signal.Effect)
    fmt.println(view)
}
`
	out := runGX(t, src)
	// 逐个片段断言各子树读到的值，确认互不串台
	checks := []struct {
		name, fragment, want string
	}{
		{"兄弟A", `class="a"`, `"A"`},
		{"兄弟B", `class="b"`, `"B"`},
		{"嵌套外层", `class="nest"`, `"OUT"`},
		{"嵌套内层", `"IN"`, `"IN"`},
		{"全局", `class="g"`, `"GLOBAL"`},
	}
	for _, c := range checks {
		idx := strings.Index(out, c.fragment)
		if idx < 0 {
			t.Errorf("%s: 输出缺少片段 %q, got:\n%s", c.name, c.fragment, out)
			continue
		}
		seg := out[idx:]
		if len(seg) > 400 {
			seg = seg[:400]
		}
		if !strings.Contains(seg, c.want) {
			t.Errorf("%s: 片段应读到 %s, got:\n%s", c.name, c.want, seg)
		}
	}
	if strings.Contains(out, "0x") {
		t.Errorf("不得打印函数地址, got:\n%s", out)
	}
}
