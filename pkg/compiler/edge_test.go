package compiler

import (
	"strings"
	"testing"
)

// assertNotContains 断言输出不含某片段。
func assertNotContains(t *testing.T, got, bad, reason string) {
	t.Helper()
	if strings.Contains(got, bad) {
		t.Errorf("%s: 输出不应含 %q\n---\n%s\n---", reason, bad, got)
	}
}

// ---------- 切片提升：不误伤边界 ----------

func TestSlicePromotionDoesNotTouchNonSlices(t *testing.T) {
	src := `package main

import "fmt"

type Box[T any] struct { v T }

pub func f() {
    xs := []int{1, 2, 3}
    arr := [3]int{1, 2, 3}
    x := xs[0]
    sub := xs[1:2]
    m := map[string]int{"a": 1}
    b := Box[int]{v: 3}
    _ = xs.map(func(v int) int { return v }) // 触发按需提升
    fmt.println(xs, arr, x, sub, m, b)
}
`
	got := compileOK(t, "noslice.gx", src)
	// 真正的切片被提升
	containsAll(t, got, "list.List[int]{1, 2, 3}")
	// 以下均不应被误伤
	assertNotContains(t, got, "list.List[3]int", "数组 [3]int 不应被提升")
	assertNotContains(t, got, "xs.List[", "索引中的 [0] 不应被当作切片类型")
	containsAll(t, got, "xs[0]", "xs[1:2]", "map[string]int", "Box[int]{v: 3}")
}

func TestSlicePromotionNestedAndPointer(t *testing.T) {
	src := `package main

type node struct{ n int }

pub func f() {
    var grid [][]int
    var ptrs []*node
    _ = grid.map(func(row []int) []int { return row }) // 触发按需提升
    _ = ptrs
}
`
	got := compileOK(t, "nested.gx", src)
	// 多维切片递归提升：外层 [] 内的 []int 也要提升
	containsAll(t, got, "list.List[list.List[int]]")
	// 指针元素：[]*node → list.List[*node]
	containsAll(t, got, "list.List[*node]")
}

func TestSlicePromotionQualifiedType(t *testing.T) {
	src := `package main

import "time"

pub func f() {
    var ts []time.Time
    _ = ts.map(func(t time.Time) time.Time { return t }) // 触发按需提升
}
`
	got := compileOK(t, "qual.gx", src)
	// 包限定类型 []time.Time → list.List[time.Time]
	containsAll(t, got, "list.List[time.Time]")
	assertNotContains(t, got, "list.List[time.List", "限定类型不应二次改写")
}

func TestNoSliceNoListImport(t *testing.T) {
	src := `package main

pub func f() { println("no slice here") }
`
	got := compileOK(t, "noslice2.gx", src)
	assertNotContains(t, got, "gox/runtime/list", "无切片时不应注入 list import")
	assertNotContains(t, got, "list.List", "无切片时不应出现 List 类型")
}

func TestCustomModuleListImport(t *testing.T) {
	src := `package main

pub func f() { xs := []int{1}; _ = xs.map(func(v int) int { return v }) }
`
	res, err := CompileWithModule("mod.gx", src, "example.com/myapp")
	if err != nil {
		t.Fatal(err)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("生成代码语法错误: %v\n%s", err, res.GoSource)
	}
	containsAll(t, res.GoSource, `import "example.com/myapp/runtime/list"`)
	assertNotContains(t, res.GoSource, "github.com/robertpanvip/gox/runtime/list", "自定义 module 不应使用默认路径")
}

func TestListImportNotDuplicated(t *testing.T) {
	// 源码已手写 list import，转译器不应重复注入
	src := `package main

import "github.com/robertpanvip/gox/runtime/list"

pub func f() { xs := []int{1}; _ = xs.map(func(v int) int { return v }) }
`
	got := compileOK(t, "dupimp.gx", src)
	if n := strings.Count(got, `import "github.com/robertpanvip/gox/runtime/list"`); n != 1 {
		t.Errorf("list import 应恰好出现 1 次, got %d\n%s", n, got)
	}
}

// ---------- pub 边界 ----------

func TestPubVarConstGroups(t *testing.T) {
	src := `package main

pub var (
    alpha = 1
    beta  = 2
)

pub const (
    gamma = 3
    delta = 4
)

func main() { _, _, _, _ = alpha, beta, gamma, delta }
`
	got := compileOK(t, "pubgroup.gx", src)
	containsAll(t, got, "Alpha = 1", "Beta  = 2", "Gamma = 3", "Delta = 4")
	// 引用还原
	containsAll(t, got, "Alpha, Beta, Gamma, Delta")
}

func TestPubMethodReceiver(t *testing.T) {
	src := `package main

type Point struct{ x int }

pub func (p Point) distance() int { return p.x }

func main() { p := Point{x: 1}; _ = p.distance() }
`
	got := compileOK(t, "pubmethod.gx", src)
	// pub 方法：接收者跳过，方法名大写
	containsAll(t, got, "func (p Point) Distance() int")
	// 调用处还原
	containsAll(t, got, "p.Distance()")
}

func TestPubAlreadyUpperUnchanged(t *testing.T) {
	src := `package main

pub func Already() {}

func main() { Already() }
`
	got := compileOK(t, "pubupper.gx", src)
	containsAll(t, got, "func Already()")
	assertNotContains(t, got, "func ALREADY", "已大写名不应重复改写")
}

// ---------- enum 边界 ----------

func TestEnumEmptyAndSingleLine(t *testing.T) {
	src := `package main

pub enum empty {}
pub enum single { only }
`
	got := compileOK(t, "enumbound.gx", src)
	containsAll(t, got, "type Empty int", "type Single int", "SingleOnly Single = iota")
	containsAll(t, got, "func ParseEmpty(", "func ParseSingle(")
}

func TestEnumStringNoValueDefaultsEmpty(t *testing.T) {
	src := `package main

pub enum method string { get = "GET", unset }
`
	got := compileOK(t, "enumstr.gx", src)
	// 字符串枚举无值成员给空字符串
	containsAll(t, got, `MethodGet Method = "GET"`, `MethodUnset Method = ""`)
}

func TestEnumParseZeroValue(t *testing.T) {
	src := `package main

pub enum color { red, green }
enum method string { get = "GET" }
`
	got := compileOK(t, "enumzero.gx", src)
	// 整数枚举 Parse 失败返回 0,false
	containsAll(t, got, "return 0, false")
	// 字符串枚举 Parse 失败返回 "",false
	containsAll(t, got, `return "", false`)
}

func TestEnumMemberRefRestore(t *testing.T) {
	// 使用处写首字母小写的全名，应还原为导出常量名
	src := `package main

pub enum color { red, green }

pub func f() colorGreen {
    return colorRed
}
`
	got := compileOK(t, "enumref.gx", src)
	// pub func f → F；成员全名 colorGreen/colorRed → ColorGreen/ColorRed
	containsAll(t, got, "func F() ColorGreen", "return ColorRed")
}

// ---------- external 边界 ----------

func TestExternalMultiwordCamel(t *testing.T) {
	// 小写形式照抄还原（兼容旧写法）：parse → Parse、host → Host、userinfo → Userinfo
	src := `package main

import "net/url"

pub func f() {
    u, _ := url.parse("http://x")
    _ = u.host
    _ = u.userinfo
}
`
	got := compileOK(t, "camel.gx", src)
	containsAll(t, got, "url.Parse", "u.Host")
	// Userinfo 的首字母小写形式是 userinfo，应还原为 Userinfo
	containsAll(t, got, "u.Userinfo")
}

func TestExternalAllCapsPreserved(t *testing.T) {
	// 全大写符号（缩写词）直接写大写，生成 Go 时原样保留，不被 upperFirst 改写。
	src := `package main

import "net/http"

pub func f(r *http.Request) int {
    _ = r.URL
    _ = r.Header
    return http.StatusOK
}
`
	got := compileOK(t, "allcaps.gx", src)
	containsAll(t, got, "r.URL", "r.Header", "http.StatusOK")
	assertNotContains(t, got, "r.Url", "全大写字段不应被改写成首字母大写")
}

func TestExternalPrivateFieldProtected(t *testing.T) {
	// 端到端：自己声明的私有字段 host 不应被当成外部字段大写
	src := `package main

type Config struct {
    host string
}

pub func f(c *Config) string {
    return c.host
}
`
	got := compileOK(t, "privfield.gx", src)
	containsAll(t, got, "type Config struct", "host string")
	// c.host 是自己的私有字段，必须保持小写
	containsAll(t, got, "return c.host")
	assertNotContains(t, got, "return c.Host", "自己的私有字段不应被大写")
}

func TestExternalKnownFieldWithUnexportedVarName(t *testing.T) {
	// 外部结构体字段（http.Request.Method）即便是小写书写也要还原
	src := `package main

import "net/http"

pub func f(req *http.Request) string {
    return req.method
}
`
	got := compileOK(t, "extfield.gx", src)
	containsAll(t, got, "return req.Method")
}

// ---------- 外部包复合字面量字段名还原 ----------

func TestExternalCompositeLiteralFields(t *testing.T) {
	src := `package main

import "example.com/lib"

func f() {
    _ = lib.box{value: 9, name: "x"}
    _ = lib.wrap{count: 1}
}
`
	got := compileOK(t, "extlit.gx", src)
	// 外部包类型的字段应按外部符号约定首字母还原
	containsAll(t, got, "lib.Box{Value: 9, Name: \"x\"}")
	containsAll(t, got, "lib.Wrap{Count: 1}")
}

func TestExternalCompositeLiteralNested(t *testing.T) {
	src := `package main

import "example.com/lib"

func f() {
    _ = lib.outer{inner: lib.inner{value: 1}, tag: "z"}
}
`
	got := compileOK(t, "extnest.gx", src)
	containsAll(t, got, "lib.Outer{Inner: lib.Inner{Value: 1}, Tag: \"z\"}")
}

func TestLocalCompositeLiteralFieldsUnchanged(t *testing.T) {
	// 本包类型字面量的字段名是私有字段，必须保持小写
	src := `package main

type Point struct{ x int; y int }
type Box struct{ v int }

func f() {
    _ = Point{x: 1, y: 2}
    _ = Box{v: 3}
    p := Point{x: 1}
    _ = p.x
}
`
	got := compileOK(t, "loclit.gx", src)
	containsAll(t, got, "Point{x: 1, y: 2}", "Box{v: 3}", "p.x")
	assertNotContains(t, got, "Point{X: 1", "本包私有字段不应大写")
}

func TestExternalCompositeGenericTypeFields(t *testing.T) {
	// 外部包泛型实例化类型 pkg.Type[T]{field:...}
	src := `package main

import "example.com/lib"

func f() {
    _ = lib.option[int]{value: 7, valid: true}
}
`
	got := compileOK(t, "extgen.gx", src)
	containsAll(t, got, "lib.Option[int]{Value: 7, Valid: true}")
}

func TestCompositeFieldRestoreDoesNotTouchMapOrAnon(t *testing.T) {
	// map 字面量的 key、匿名结构字段值不应被当作外部字段名改写
	src := `package main

import "example.com/lib"

type Pair struct{ k int; v int }

func f() {
    m := map[string]int{alpha: 1, beta: 2}
    _ = m
    _ = Pair{k: 1, v: 2}
    _ = lib.box{value: 3, opt: Pair{k: 4}}
}
`
	got := compileOK(t, "compmix.gx", src)
	containsAll(t, got, "lib.Box{Value: 3, Opt: Pair{k: 4}}")
	containsAll(t, got, "map[string]int{alpha: 1, beta: 2}", "Pair{k: 1, v: 2}")
}
