package compiler

import (
	"strings"
	"testing"
)

func TestJSXSceneStaticOnly(t *testing.T) {
	out, err := CompileStage(`package main
func main() { _ = <div class="box">hello</div> }
`, "jsxscene")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"gxscene.Render",
		"gxscene.OpElem", `Arg:"div"`,
		"gxscene.OpAttr", `Key:"class"`, `Arg:"box"`,
		"gxscene.OpChildText", `Arg:"hello"`,
		"gxscene.OpClose", "N:0",
		"runtime/scene",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物应包含 %q, got:\n%s", want, out)
		}
	}
	// 无动态插值时不应出现 getter
	if strings.Contains(out, "func() any") {
		t.Errorf("静态模板不应生成 getter, got:\n%s", out)
	}
}

func TestJSXSceneDynamicSlots(t *testing.T) {
	out, err := CompileStage(`package main
func main() {
	title := "x"
	_ = <div><h1>{title}</h1><b>{1 + 2}</b></div>
}
`, "jsxscene")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"gxscene.OpDynChild", "Slot:0", "Slot:1", "N:2",
		"func() any{ return title }", "func() any{ return 1 + 2 }",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestJSXSceneDynamicAttr(t *testing.T) {
	out, err := CompileStage(`package main
func main() {
	url := "/home"
	_ = <a href={url}>home</a>
}
`, "jsxscene")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"gxscene.OpDynAttr", `Key:"href"`, "Slot:0", "N:1",
		"func() any{ return url }",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestJSXSceneCompositeChild(t *testing.T) {
	out, err := CompileStage(`package main
func main() {
	title := "hi"
	_ = <div><Card title={title} class="x">hello</Card></div>
}
`, "jsxscene")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"gxscene.OpComp", `Arg:"Card"`, "Slot:0", "N:1",
		"Card(gxscene.Props{",
		`"title":func() any{ return title }`,
		`"class":"x"`,
		`"children":func() any{ return gxscene.Render`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物应包含 %q, got:\n%s", want, out)
		}
	}
	// Card 的静态属性不应变成 OpAttr（组件属性走 Props，不落在场景节点上）
	if strings.Contains(out, `gxscene.OpAttr`) {
		t.Errorf("组件不应生成 OpAttr, got:\n%s", out)
	}
}

func TestJSXSceneCompositeRoot(t *testing.T) {
	out, err := CompileStage(`package main
func main() { _ = <ui.Card n={1}/> }
`, "jsxscene")
	if err != nil {
		t.Fatal(err)
	}
	// 根是组件：直接产出一次组件调用，不再包一层 gxscene.Render
	if !strings.Contains(out, "ui.Card(gxscene.Props{") {
		t.Errorf("根组件应产出限定名调用, got:\n%s", out)
	}
	if !strings.Contains(out, `"n":func() any{ return 1 }`) {
		t.Errorf("动态属性应为 getter, got:\n%s", out)
	}
}

func TestJSXSceneNested(t *testing.T) {
	out, err := CompileStage(`package main
func main() { _ = <ul><li>a</li><li>b</li></ul> }
`, "jsxscene")
	if err != nil {
		t.Fatal(err)
	}
	// 嵌套元素应展开为多个 OpElem / OpClose，且栈平衡（各 3 个：ul + 两个 li）
	if c := strings.Count(out, "gxscene.OpElem"); c != 3 {
		t.Errorf("应有 3 个 OpElem, got %d:\n%s", c, out)
	}
	if c := strings.Count(out, "gxscene.OpClose"); c != 3 {
		t.Errorf("应有 3 个 OpClose, got %d:\n%s", c, out)
	}
	if !strings.Contains(out, `Arg:"ul"`) || !strings.Contains(out, `Arg:"li"`) {
		t.Errorf("嵌套标签名缺失:\n%s", out)
	}
}

// 以下用例走完整管线（CompileScene），验证 signal 语法糖与 JSX 插值交汇：
// transformSignals 无法穿透 JSX 整体 token，插值里的 signal 读应由 JSX 转换补 ()。

func TestJSXSceneSignalInterpolation(t *testing.T) {
	res, err := CompileScene("sig.gx", `package main
func main() {
	signal count = 0
	_ = <div><span>{count}</span></div>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	for _, want := range []string{
		"var count = Signal.New(0)",
		"func() any{ return count() }", // 关键：插值里的 signal 读补全 ()
		"gxscene.OpDynChild",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物应包含 %q, got:\n%s", want, out)
		}
	}
	// 不应把 signal 函数对象本身当值（return count 缺 ()）
	if strings.Contains(out, "func() any{ return count }") {
		t.Errorf("signal 插值漏了读调用 (), got:\n%s", out)
	}
}

func TestJSXSceneSignalAttrAndComputed(t *testing.T) {
	res, err := CompileScene("sig.gx", `package main
func main() {
	signal title = "hi"
	let upper = Signal.computed(func() string { return title() })
	_ = <a href={title}>{upper}</a>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	for _, want := range []string{
		"func() any{ return title() }", // 动态属性里的 signal 读
		"func() any{ return upper() }", // 派生 signal 插值读
		"Signal.Computed",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("产物应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestJSXSceneSignalInCompositeProps(t *testing.T) {
	res, err := CompileScene("sig.gx", `package main
func main() {
	signal n = 1
	_ = <Card count={n}/>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	if !strings.Contains(out, `"count":func() any{ return n() }`) {
		t.Errorf("组件属性里的 signal 读应补全 (), got:\n%s", out)
	}
}

// 强类型组件契约：func Card(props CardProps) 命中后，JSX 调用生成裸对象字面量，
// 由管线第二次 objectlit 从签名补全 Card(CardProps{...}) 并按字段校验。
func TestJSXSceneTypedPropsContract(t *testing.T) {
	res, err := CompileScene("typed.gx", `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type CardProps struct {
	title string
	n     int
}
func Card(props CardProps) *gxscene.Instance { return nil }
func main() {
	_ = <div><Card title="Gox" n={42}/></div>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	if !strings.Contains(out, "Card(CardProps{") {
		t.Errorf("应补全为 Card(CardProps{...}), got:\n%s", out)
	}
	for _, want := range []string{`title:"Gox"`, `n:42`} {
		if !strings.Contains(out, want) {
			t.Errorf("产物应包含 %q, got:\n%s", want, out)
		}
	}
	// 强类型路径不应出现 getter-map / OpComp getter 包装
	if strings.Contains(out, `gxscene.Props{`) {
		t.Errorf("强类型组件不应生成 gxscene.Props getter-map, got:\n%s", out)
	}
}

func TestJSXSceneTypedPropsValidation(t *testing.T) {
	cases := []struct {
		name string
		jsx  string
		want string
	}{
		{"未知字段", `<Card title="Gox" n={1} bad={2}/>`, `未知字段 "bad"`},
		{"类型不匹配", `<Card title={123} n={1}/>`, `字段 "title" 类型不匹配`},
		{"缺字段", `<Card title="Gox"/>`, `缺少字段 "n"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := CompileScene("typed.gx", `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type CardProps struct {
	title string
	n     int
}
func Card(props CardProps) *gxscene.Instance { return nil }
func main() { _ = <div>`+c.jsx+`</div> }
`)
			if err == nil {
				t.Fatalf("应校验失败: %s", c.jsx)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("错误应包含 %q, got: %v", c.want, err)
			}
		})
	}
}

// 老式 gxscene.Props 组件不受影响：仍走 getter-map（保鲜语义）。
func TestJSXScenePropsComponentUnchanged(t *testing.T) {
	res, err := CompileScene("old.gx", `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
func Card(props gxscene.Props) *gxscene.Instance { return nil }
func main() {
	v := "x"
	_ = <div><Card title={v}/></div>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.GoSource, `gxscene.Props{`) {
		t.Errorf("老式组件应保留 gxscene.Props getter-map, got:\n%s", res.GoSource)
	}
	if !strings.Contains(res.GoSource, `func() any{ return v }`) {
		t.Errorf("老式组件动态属性应包 getter, got:\n%s", res.GoSource)
	}
}

// 条件分支：JSX 内的 if/else 编译成 gxscene.OpBranch + BranchGetter。
func TestJSXSceneConditional(t *testing.T) {
	res, err := CompileScene("cond.gx", `package main
func main() {
	signal visible = true
	signal count = 10
	_ = <div>
		if visible {
			<span>{count}</span>
		} else {
			<b>Hidden</b>
		}
	</div>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	for _, want := range []string{
		"gxscene.OpBranch", "Slot:0", "N:1",
		"gxscene.BranchGetter",
		"if visible()", "return 0", "return 1",
		`Arg:"span"`, `Arg:"b"`, `Arg:"Hidden"`,
		"func() any{ return count() }", // 分支内 signal 读补 ()
	} {
		if !strings.Contains(out, want) {
			t.Errorf("条件分支产物应包含 %q, got:\n%s", want, out)
		}
	}
}

func TestJSXSceneConditionalElseIf(t *testing.T) {
	res, err := CompileScene("cond.gx", `package main
func main() {
	signal n = 1
	_ = <div>
		if n == 1 {
			<span>one</span>
		} else if n == 2 {
			<span>two</span>
		} else {
			<span>many</span>
		}
	</div>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	for _, want := range []string{
		"gxscene.BranchGetter",
		"if n() == 1", "return 0",
		"if n() == 2", "return 1",
		"return 2", // else 兜底索引
		`Arg:"one"`, `Arg:"two"`, `Arg:"many"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("else-if 产物应包含 %q, got:\n%s", want, out)
		}
	}
}
