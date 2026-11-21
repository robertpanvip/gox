package main

import (
	"fmt"
	"strings"

	"github.com/robertpanvip/gox/pkg/compiler"
)

// selftestCase 是一条内联转译断言，无需外部文件。
type selftestCase struct {
	name string
	src  string
	want []string // 全部须包含
	none []string // 全部须不包含
}

// cmdSelfTest 运行 24 项转译自检，覆盖 GX 全部核心特性与边界。
func cmdSelfTest(_ []string) error {
	tests := []selftestCase{
		// ---- pub 可见性（4）----
		{"pub func", "package main\npub func greet() {}\nfunc main(){greet()}", []string{"func Greet()", "Greet()"}, nil},
		{"pub type", "package main\npub type point struct{ x int }\nvar p point", []string{"type Point struct", "var p Point"}, nil},
		{"pub var group", "package main\npub var (\n a int\n b string\n)", []string{"A int", "B string"}, nil},
		{"私有保持小写", "package main\nfunc helper() {}\nvar c = 1", []string{"func helper()", "var c = 1"}, []string{"Helper", " Var C"}},

		// ---- enum（5）----
		{"整数 enum", "package main\npub enum color { red, green, blue }", []string{"type Color int", "ColorRed Color = iota", "ColorGreen Color = iota"}, nil},
		{"enum String", "package main\npub enum color { red, green }", []string{"func (x Color) String() string", `return "red"`}, nil},
		{"enum Parse", "package main\npub enum color { red, green }", []string{"func ParseColor(s string) (Color, bool)", "return ColorRed, true"}, nil},
		{"字符串 enum", `package main
pub enum method string { get = "GET", post = "POST" }`, []string{`type Method string`, `MethodGet Method = "GET"`, "func ParseMethod"}, nil},
		{"位标志 enum", "package main\npub enum perm { read = 1 << iota, write, exec }", []string{"type Perm int", "PermRead Perm = 1 << iota", "PermWrite Perm = 1 << iota"}, nil},

		// ---- JSX（3）----
		{"JSX 元素", `package main
pub func v() any { return <div class="box">hi</div> }`, []string{"gxvdom.CreateElement", `"div"`, `"class"`, `"box"`, `"hi"`}, nil},
		{"JSX 嵌套+表达式", `package main
pub func v(n string) any { return <div><span id={n}/></div> }`, []string{"gxvdom.CreateElement(\"div\"", "gxvdom.CreateElement(\"span\"", "n"}, nil},
		{"JSX 自闭合", `package main
pub func v() any { return <br/> }`, []string{`gxvdom.CreateElement("br"`}, nil},

		// ---- 外部符号（5）----
		{"包级还原", `package main
import "fmt"
pub func f(){ fmt.println("x") }`, []string{"fmt.Println"}, nil},
		{"多词驼峰照抄", `package main
import "net/http"
pub func f(){ http.newRequest("GET","/",nil) }`, []string{"http.NewRequest"}, nil},
		{"URL 风格", `package main
import "net/url"
pub func f(){ url.parse("/") }`, []string{"url.Parse"}, nil},
		{"别名导入", `package main
import h "net/http"
pub func f(){ h.newRequest("GET","/",nil) }`, []string{"h.NewRequest"}, nil},
		{"裸内置不误改", "package main\npub func f(){ println(\"x\"); len(\"a\") }", []string{`println("x")`, "len(\"a\")"}, []string{`Println("x")`}},

		// ---- 集合链式与提升（4）----
		{"切片提升", "package main\npub func f(){ xs := []int{1,2}; _ = xs }", []string{"list.List[int]"}, nil},
		// 相邻 .map(f).filter(g) 由 fuseMapFilter 融合为单次遍历的 MapFilter
		// （见 compiler_test.go TestMapFilterFusion 与 runtime/list.MapFilter）。
		{"链式换型", "package main\npub func f(){ xs:=[]int{1}; ys:=xs.map(fn).filter(g) }\nfunc fn(x int)int{return x}\nfunc g(x int)bool{return true}", []string{"xs.MapFilter(fn, g)"}, []string{"xs.Map(fn)", ".Filter(g)"}},
		{"reduce", "package main\npub func f(){ xs:=[]int{1}; _=xs.reduce(0,add) }\nfunc add(a,b int)int{return a+b}", []string{"xs.Reduce(0,add)"}, nil},
		{"边界不误伤", "package main\npub func f(){ a:=[3]int{1,2,3}; x:=a[0]; s:=a[1:2]; m:=map[string]int{}; _,_,_,_=x,s,m,a }", nil, []string{"[3]list.List", "a[list.List"}},

		// ---- sourcemap / 综合（3）----
		{"sourcemap 生成", "package main\npub func f() {}", []string{}, nil},
		{"混合程序", `package main
import "fmt"
pub enum color { red, green }
pub func show(c color) string { return "x" }
func main(){ fmt.println(show(ColorRed)) }`, []string{"type Color int", "func Show(c Color) string", "fmt.Println(Show(ColorRed))"}, nil},
		{"pub 引用多处", "package main\npub func greet() {}\nfunc main(){ greet(); greet() }", []string{"Greet()"}, []string{" greet("}},
	}

	failed := 0
	for _, tt := range tests {
		res, err := compiler.Compile(tt.name+".gx", tt.src)
		if err != nil {
			fmt.Printf("FAIL %-18s 转译错误: %v\n", tt.name, err)
			failed++
			continue
		}
		missing := []string{}
		for _, w := range tt.want {
			if !strings.Contains(res.GoSource, w) {
				missing = append(missing, "expected:"+w)
			}
		}
		for _, n := range tt.none {
			if strings.Contains(res.GoSource, n) {
				missing = append(missing, "unexpected:"+n)
			}
		}
		if tt.name == "sourcemap 生成" {
			if res.SourceMap == nil || res.SourceMap.Len() == 0 {
				missing = append(missing, "empty sourcemap")
			}
		}
		if len(missing) > 0 {
			fmt.Printf("FAIL %-18s %v\n---\n%s\n---\n", tt.name, missing, res.GoSource)
			failed++
		} else {
			fmt.Printf("PASS %-18s\n", tt.name)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d/%d 自检失败", failed, len(tests))
	}
	fmt.Printf("全部 %d 项自检通过\n", len(tests))
	return nil
}
