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
		"nums.Map(strconv.Itoa).Filter(",
		"nums.Filter(keep).Reduce(0,",
		"func(acc int, x int) int { return acc + x }")
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
