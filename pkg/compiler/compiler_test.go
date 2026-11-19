package compiler

import (
	"strings"
	"testing"
)

func compileOK(t *testing.T, name, src string) string {
	t.Helper()
	res, err := Compile(name, src)
	if err != nil {
		t.Fatalf("compile %s: %v", name, err)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("generated Go invalid for %s: %v\n---\n%s\n---", name, err, res.GoSource)
	}
	return res.GoSource
}

func containsAll(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("输出缺少 %q\n---\n%s\n---", w, got)
		}
	}
}

func TestPubAndRefRestore(t *testing.T) {
	src := `package main

pub type point struct {
    pub x int
    y int
}

pub func greet() {}
func helper() {}
func main() { greet(); helper(); _ = point{} }
`
	got := compileOK(t, "pub.gx", src)
	containsAll(t, got,
		"type Point struct", "X int", "y int",
		"func Greet()", "func helper()", "Greet()", "helper()", "_ = Point{}")
}

func TestEnum(t *testing.T) {
	src := `package main

pub enum color { red, green, blue }
enum code { ok = 200, fail = 500 }
`
	got := compileOK(t, "enum.gx", src)
	containsAll(t, got,
		"type Color int", "ColorRed Color = iota", "ColorGreen Color = iota", "ColorBlue Color = iota",
		"func (x Color) String() string", "func ParseColor(s string) (Color, bool)",
		"type code int", "CodeOk code = 200", "CodeFail code = 500",
		"func ParseCode(s string) (code, bool)")
}

func TestEnumStringAndFlags(t *testing.T) {
	src := `package main

pub enum method string { get = "GET", post = "POST" }
pub enum perm { read = 1 << iota, write, exec }
`
	got := compileOK(t, "enum2.gx", src)
	containsAll(t, got,
		"type Method string", `MethodGet Method = "GET"`, `MethodPost Method = "POST"`,
		"func (x Method) String() string", "func ParseMethod(s string) (Method, bool)",
		"type Perm int", "PermRead Perm = 1 << iota", "PermWrite Perm = 1 << iota",
		"case \"GET\":", "return MethodGet, true")
}

func TestJSX(t *testing.T) {
	src := `package main

pub func view(name string) any {
    return <div class="box" id={name} disabled>
        hello {name}
        <span>world</span>
        <br/>
    </div>
}
`
	got := compileOK(t, "jsx.gx", src)
	containsAll(t, got,
		`import gxvdom "github.com/robertpanvip/gox/runtime/vdom"`,
		"gxvdom.CreateElement", `"div"`, `"class"`, `"box"`,
		`"id": name`, `"disabled": true`, `"hello"`,
		"gxvdom.CreateElement(\"span\"", "gxvdom.CreateElement(\"br\"")
}

func TestExternalPreciseAndGlobal(t *testing.T) {
	src := `package main

import (
    "fmt"
    "net/http"
    "net/url"
)

pub func f() {
    req, _ := http.newRequest("GET", "http://x", nil)
    u, _ := url.parse("http://x")
    fmt.println(req.method, u.host, u.path)
}
`
	got := compileOK(t, "ext.gx", src)
	containsAll(t, got,
		"http.NewRequest", "url.Parse", "fmt.Println",
		"req.Method", "u.Host", "u.Path")
}

func TestExternalAliasImport(t *testing.T) {
	src := `package main

import h "net/http"

pub func f() { _, _ = h.newRequest("GET", "http://x", nil) }
`
	got := compileOK(t, "alias.gx", src)
	containsAll(t, got, "h.NewRequest")
}

func TestExternalAlreadyUpperUnchanged(t *testing.T) {
	src := `package main

import "fmt"

pub func f() { fmt.Println("x") }
`
	got := compileOK(t, "upper.gx", src)
	containsAll(t, got, "fmt.Println")
	if strings.Contains(got, "fmt.Printlin") {
		t.Errorf("不应二次改写已大写符号")
	}
}

func TestBareBuiltinNotTouched(t *testing.T) {
	// 裸标识符 println 不是 selector，不应被改成 Println
	src := `package main

pub func f() { println("x") }
`
	got := compileOK(t, "bare.gx", src)
	containsAll(t, got, "println(\"x\")")
	if strings.Contains(got, "Println(\"x\")") {
		t.Errorf("裸内置函数 println 被误改")
	}
}

func TestCollectionsChain(t *testing.T) {
	src := `package main

import "strconv"

func fn(x int) int { return x }
func keep(x int) bool { return x > 0 }

pub func f() {
    nums := []int{1, 2, 3}
    labels := nums.map(strconv.itoa).filter(func(s string) bool { return s != "" })
    total := nums.filter(keep).reduce(0, func(acc int, x int) int { return acc + x })
    _ = labels
    _ = total
}
`
	got := compileOK(t, "coll.gx", src)
	containsAll(t, got,
		`import "github.com/robertpanvip/gox/runtime/list"`,
		"list.List[int]",
		"nums.MapFilter(strconv.Itoa,",
		"nums.Filter(keep).Reduce(0,",
		"func(acc int, x int) int { return acc + x }")
}

func TestMapFilterFusion(t *testing.T) {
	src := `package main

func keep(x int) bool { return x > 0 }
func double(x int) int { return x * 2 }

pub func f() {
    xs := []int{1, 2, 3}
    ys := xs.map(double).filter(keep)
    _ = ys
}
`
	got := compileOK(t, "fuse.gx", src)
	containsAll(t, got, "xs.MapFilter(double, keep)")
	if strings.Contains(got, "xs.Map(double)") || strings.Contains(got, ".Filter(keep)") {
		t.Errorf("融合后仍残留独立的 Map/Filter 链:\n%s", got)
	}
}

func TestMapFilterFusionNonAdjacent(t *testing.T) {
	src := `package main

func keep(x int) bool { return x > 0 }
func double(x int) int { return x * 2 }

pub func f() {
    xs := []int{1, 2, 3}
    ys := xs.filter(keep).map(double)
    _ = ys
}
`
	got := compileOK(t, "nofuse.gx", src)
	containsAll(t, got, "xs.Filter(keep).Map(double)")
	if strings.Contains(got, "MapFilter") {
		t.Errorf("非相邻的 filter().map() 不应融合:\n%s", got)
	}
}

func TestCollectionsMake(t *testing.T) {
	src := `package main

func keep(x int) bool { return x > 0 }
pub func f() { xs := make([]int, 3); _ = xs.filter(keep) }
`
	got := compileOK(t, "collmake.gx", src)
	containsAll(t, got, "make(list.List[int], 3)")
}

func TestSourcemapRoundTrip(t *testing.T) {
	res, err := Compile("sm.gx", "package main\n\npub func f() {}\n")
	if err != nil {
		t.Fatal(err)
	}
	if res.SourceMap.Len() == 0 {
		t.Fatal("sourcemap 为空")
	}
	// 取第一条映射做往返
	m := res.SourceMap.Mappings()[0]
	gx, ok := res.SourceMap.GoToGx(m.Generated.Line, m.Generated.Column)
	if !ok {
		t.Fatal("GoToGx 失败")
	}
	if gx.Line != m.Original.Line {
		t.Errorf("往返行号不一致: got %d want %d", gx.Line, m.Original.Line)
	}
}

// ---- 外部符号「首字母小写」机械还原 + 作用域防误伤单测 ----

func TestMechanicalExternalRestore(t *testing.T) {
	// 只首字母小写、其余驼峰照抄 → upperFirst 精确还原
	cases := []struct{ in, want string }{
		{"newRequest", "NewRequest"},
		{"uRL", "URL"},
		{"sprintf", "Sprintf"},
		{"itoa", "Itoa"},
	}
	for _, c := range cases {
		if got := upperFirst(c.in); got != c.want {
			t.Errorf("upperFirst(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// 已大写 / 未知小写标识符保持
	if isLowerStart("Method") || isLowerStart("URL") {
		t.Errorf("isLowerStart 对大写名应返回 false")
	}
	if !isLowerStart("method") {
		t.Errorf("method 应为小写开头")
	}
}

func TestScopePrivateProtection(t *testing.T) {
	src := `package main
type config struct {
    host string
}
func demo(req *config) {
    x := req.host
    _ = x
}
`
	toks := New(src).AllTokens()
	s := collectScope(toks)
	if !s.IsPrivate("host") {
		t.Errorf("私有字段 host 应被收集")
	}
	if !s.IsPrivate("x") {
		t.Errorf("局部变量 x 应被收集")
	}
	if !s.IsPrivate("req") {
		t.Errorf("参数 req 应被收集")
	}
	if s.IsPrivate("config") {
		t.Errorf("类型名 config 不应进私有表（它不是 selector 右侧会引用的字段）")
	}
}

func TestJSXComponentGeneratesCallableFirstArg(t *testing.T) {
	src := `package main
func Foo(props map[string]any) any { return nil }
func main() { _ = <Foo title="hi"/> }
`
	res, err := CompileSource(src)
	if err != nil {
		t.Fatal(err)
	}
	// 组件名 Foo 应作为标识符（可调用的函数）传入，而不是被引号包成字符串标签。
	if !strings.Contains(res.GoSource, "CreateElement(Foo") {
		t.Fatalf("组件应生成 CreateElement(Foo, ...):\n%s", res.GoSource)
	}
	// 原生标签仍应是字符串字面量。
	native, _ := CompileSource(`package main
func main() { _ = <div/> }
`)
	if !strings.Contains(native.GoSource, `CreateElement("div"`) {
		t.Fatalf("原生标签应生成字符串 tag:\n%s", native.GoSource)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("生成代码语法无效: %v", err)
	}
}

func TestEnumDuplicateValues(t *testing.T) {
	// 整数枚举重复值：String() 只生成一个 case，编译合法，取第一个为规范名
	intSrc := `package main
pub enum status {
    pending = 1,
    processing = 1,
}
func main() { println(statusPending.String()) }
`
	res, err := CompileSource(intSrc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(res.GoSource, "case StatusPending:") != 1 {
		t.Errorf("规范名 case 应恰好一个:\n%s", res.GoSource)
	}
	if strings.Contains(res.GoSource, "case StatusProcessing:") {
		t.Errorf("重复值别名不应生成重复 case:\n%s", res.GoSource)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("整数枚举重复值生成代码语法无效: %v", err)
	}

	// 字符串枚举重复值：Parse() 只生成一个 case，取第一个
	strSrc := `package main
pub enum method string {
    get = "GET",
    retrieve = "GET",
}
func main() { println(ParseMethod("GET")) }
`
	res2, err := CompileSource(strSrc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(res2.GoSource, `case "GET":`) != 1 {
		t.Errorf("重复字符串值的 case 应恰好一个:\n%s", res2.GoSource)
	}
	if err := res2.Validate(); err != nil {
		t.Fatalf("字符串枚举重复值生成代码语法无效: %v", err)
	}
}

func TestEnumNoValueMembersDeduplicated(t *testing.T) {
	// 无值成员靠 iota 自增，值各不相同，不能被误判为重复而丢失 case
	src := `package main
enum color { red, green, blue }
func main() { println(colorRed.String()) }
`
	res, err := CompileSource(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"case ColorRed:", "case ColorGreen:", "case ColorBlue:"} {
		if !strings.Contains(res.GoSource, c) {
			t.Errorf("无值成员 %s 不应被去重:\n%s", c, res.GoSource)
		}
	}
}

func TestImportAliasReuse(t *testing.T) {
	// 源码已用别名导入运行时，生成代码应复用该别名而非注入新的
	src := `package main
import mylist "github.com/robertpanvip/gox/runtime/list"
func main() {
	xs := []int{1, 2, 3}
	_ = xs.map(func(x int) int { return x })
}
`
	res, err := CompileSource(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.GoSource, "mylist.List[int]") {
		t.Errorf("生成代码应复用别名 mylist:\n%s", res.GoSource)
	}
	if strings.Contains(res.GoSource, "undefined") || strings.Count(res.GoSource, "runtime/list") != 1 {
		t.Errorf("不应重复注入 list import:\n%s", res.GoSource)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("生成代码语法无效: %v", err)
	}
}

func TestImportAliasConflict(t *testing.T) {
	// 占位名 list 被用户 import 占用，运行时应分配无冲突别名 list2
	src := `package main
import list "strings"
func main() {
	xs := []int{1, 2, 3}
	_ = xs.map(func(x int) int { return x })
	_ = list.ToUpper("hi")
}
`
	res, err := CompileSource(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.GoSource, "import list2 \"github.com/robertpanvip/gox/runtime/list\"") {
		t.Errorf("占位名冲突应分配 list2:\n%s", res.GoSource)
	}
	if !strings.Contains(res.GoSource, "list2.List[int]") {
		t.Errorf("生成代码应使用 list2 别名:\n%s", res.GoSource)
	}
	if !strings.Contains(res.GoSource, `list.ToUpper`) {
		t.Errorf("用户的 list(strings) 引用不应被改写:\n%s", res.GoSource)
	}
	if err := res.Validate(); err != nil {
		t.Fatalf("生成代码语法无效: %v", err)
	}
}

func TestImportOnlyWhenUsed(t *testing.T) {
	// 只用 map、不用 JSX/enum，不应注入 vdom/strconv import
	src := `package main
func main() {
	xs := []int{1, 2}
	_ = xs.map(func(x int) int { return x })
}
`
	res, err := CompileSource(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, unwanted := range []string{"runtime/vdom", `"strconv"`, "strconv."} {
		if strings.Contains(res.GoSource, unwanted) {
			t.Errorf("未使用的特性不应注入 %q:\n%s", unwanted, res.GoSource)
		}
	}
	if !strings.Contains(res.GoSource, "runtime/list") {
		t.Errorf("使用的 map 应注入 list import:\n%s", res.GoSource)
	}
}

func TestSourcemapUnicodeByteColumns(t *testing.T) {
	// 含多字节字符时，GX 输入列与 Go 输出列都应是「UTF-8 字节列」。
	// 若 emit 按 rune 计列、lexer 按字节计列，则中文之后的 token 映射会错位。
	src := "package main\nfunc main() { message := \"中文😀\"; println(message) }\n"
	res, err := CompileSource(src)
	if err != nil {
		t.Fatal(err)
	}
	// 找到 println token 对应的映射：它的输入列应按字节计算。
	// 直接逐映射核对：未被 desugar 改写的 token，输出列应等于输入列。
	for _, mp := range res.SourceMap.Mappings() {
		if mp.Generated.Line != mp.Original.Line {
			continue
		}
		if mp.Generated.Column != mp.Original.Column {
			t.Errorf("第 %d 行未改写 token 的列应一致: go=%d gx=%d\n(疑似 Unicode 列按 rune 计)",
				mp.Generated.Line, mp.Generated.Column, mp.Original.Column)
		}
	}
}

func TestChainWrapExternalSlice(t *testing.T) {
	// 外部变量（未提升）的链式调用应插入 list.Of 包装
	src := `package main
import "strings"
func main() {
	xs := strings.Fields("a b")
	_ = xs.map(func(s string) string { return s })
}
`
	res, err := CompileSource(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.GoSource, "list.Of(xs).Map") {
		t.Errorf("外部切片应插入 list.Of 包装:\n%s", res.GoSource)
	}
	if err := res.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestChainNoWrapPromotedSlice(t *testing.T) {
	// 本地提升的切片变量不应重复包装
	src := `package main
func main() {
	xs := []int{1, 2}
	_ = xs.map(func(x int) int { return x })
}
`
	res, err := CompileSource(src)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.GoSource, "list.Of(xs)") {
		t.Errorf("已提升切片不应重复 list.Of 包装:\n%s", res.GoSource)
	}
	if !strings.Contains(res.GoSource, "xs.Map") {
		t.Errorf("应保留 xs.Map 链式调用:\n%s", res.GoSource)
	}
}
