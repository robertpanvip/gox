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
		"type Color int", "Red Color = iota", "Green Color = iota", "Blue Color = iota",
		"type code int", "ok code = 200", "fail code = 500")
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
		`import gxvdom "github.com/robertpanvip/gox/vdom"`,
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
    req, _ := http.newrequest("GET", "http://x", nil)
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

pub func f() { _, _ = h.newrequest("GET", "http://x", nil) }
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
		`import "github.com/robertpanvip/gox/gxcollections"`,
		"gxcollections.List[int]",
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
	containsAll(t, got, "make(gxcollections.List[int], 3)")
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

// ---- ExtSymbols 歧义兜底单元测试 ----

func TestExtSymbolsAmbiguityFallback(t *testing.T) {
	e := &ExtSymbols{
		byPkg:  map[string]map[string]string{},
		global: map[string]string{},
	}
	// 直接构造歧义场景：同名小写映射被丢弃（global 不收录）
	// 通过 loadExtSymbols 的 cand 逻辑验证：这里模拟「唯一才收录」
	e.global["method"] = "Method"
	if got, ok := e.restore("", "method"); !ok || got != "Method" {
		t.Errorf("唯一映射应还原: got %q ok=%v", got, ok)
	}
	if got, ok := e.restore("", "unknownsym"); ok || got != "unknownsym" {
		t.Errorf("未知符号应原样保留: got %q ok=%v", got, ok)
	}
	if got, ok := e.restore("", "Method"); ok || got != "Method" {
		t.Errorf("已大写应不动: got %q ok=%v", got, ok)
	}
	// 包名精确表缺失时，不退回 global（避免包内私有名被误改）
	e.byPkg["fake"] = map[string]string{}
	if got, ok := e.restore("fake", "method"); ok || got != "method" {
		t.Errorf("包名精确未命中不应回退 global: got %q ok=%v", got, ok)
	}
}
