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

// For item 逃逸检测：item 传给组件 props 时，必须回退到值快照 ForGetterOf，
// 而非信号化 ForGetterOfSignal——组件 props 是挂载时求值一次的值快照，item signal
// 无法驱动它，否则 item 变化后子组件静默停在旧值。
func TestJSXSceneForItemEscapesIntoCompProps(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type User struct { id int; name string }
type CardProps struct { name string }
func Card(props CardProps) *gxscene.Instance { return nil }
func main() {
	signal users = []User{{1,"Tom"}}
	_ = <ul><For each={users} key={(u) => u.id}>{(u User) => <li><Card name={u.name}/></li>}</For></ul>
}
`
	res, err := CompileScene("for-escape.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	if !strings.Contains(out, "gxscene.ForGetterOf[User]") {
		t.Errorf("item 进组件 props 应回退 ForGetterOf（值快照）, got:\n%s", out)
	}
	if strings.Contains(out, "ForGetterOfSignal") {
		t.Errorf("逃逸场景不应使用 ForGetterOfSignal, got:\n%s", out)
	}
	if !strings.Contains(out, "func(u User) *gxscene.Instance") {
		t.Errorf("render 应为值参数 func(u User), got:\n%s", out)
	}
	if strings.Contains(out, "u().name") {
		t.Errorf("props 里应保持值快照 u.name，不应改写为 u().name, got:\n%s", out)
	}
}

// struct signal 字段的 props 本身保鲜（getter），item 传入时不应回退值快照：
// 显式声明响应式字段的用户应享受 item signal 零重建，而非被强制整项重建。
func TestJSXSceneForItemIntoSignalFieldStaysSignal(t *testing.T) {
	src := `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type User struct { id int; name string }
type CardProps struct { signal name string }
func Card(props CardProps) *gxscene.Instance { return nil }
func main() {
	signal users = []User{{1,"Tom"}}
	_ = <ul><For each={users} key={(u) => u.id}>{(u User) => <li><Card name={u.name}/></li>}</For></ul>
}
`
	res, err := CompileScene("for-sigfield.gx", src)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	if !strings.Contains(out, "gxscene.ForGetterOfSignal[User]") {
		t.Errorf("item 进 signal 字段 props 应保持 ForGetterOfSignal（保鲜）, got:\n%s", out)
	}
	if strings.Contains(out, "gxscene.ForGetterOf[") {
		t.Errorf("signal 字段场景不应回退值快照 ForGetterOf, got:\n%s", out)
	}
	if !strings.Contains(out, "func(u func() User) *gxscene.Instance") {
		t.Errorf("render 应保持 getter 参数 func(u func() User), got:\n%s", out)
	}
}

// 直接插值（不经过组件边界）仍应走信号化，保持 item signal 零重建。
func TestJSXSceneForDirectInterpStaysSignal(t *testing.T) {
	res, err := CompileScene("for-direct.gx", `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type User struct { id int; name string }
func main() {
	signal users = []User{{1,"Tom"}}
	_ = <ul><For each={users} key={(u) => u.id}>{(u User) => <li>{u.name}</li>}</For></ul>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	if !strings.Contains(out, "gxscene.ForGetterOfSignal[User]") {
		t.Errorf("直接插值应走 ForGetterOfSignal, got:\n%s", out)
	}
	if !strings.Contains(out, "u().name") {
		t.Errorf("直接插值应改写为 u().name 登记 item signal 依赖, got:\n%s", out)
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

// Keyed For（item signal 根治）：JSX <For> 编译成 OpFor + ForGetterOfSignal[T]，
// render 参数是 item getter func() T，插值 u.name 改写为 u().name 登记依赖。
func TestJSXSceneFor(t *testing.T) {
	res, err := CompileScene("for.gx", `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type User struct { id int; name string }
func main() {
	signal users = []User{{1,"Tom"}}
	_ = <ul><For each={users} key={(u) => u.id}>{(u User) => <li>{u.name}</li>}</For></ul>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	for _, want := range []string{
		"gxscene.OpFor", "Slot:0", "N:1",
		"gxscene.ForGetterOfSignal[User]",
		"func() []User", "users()",
		"func(u User) any", "u.id", // key 仍是值参数
		"func(u func() User) *gxscene.Instance", // render 参数是 item getter
		"u().name",                              // item getter 读补 ()
		`Arg:"li"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("For 产物应包含 %q, got:\n%s", want, out)
		}
	}
}

// Keyed For 元素类型推断：render 参数无类型注解时，从 each 集合表达式推断元素类型。
func TestJSXSceneForInferElemType(t *testing.T) {
	res, err := CompileScene("for.gx", `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type User struct { id int; name string }
func main() {
	signal users = []User{{1,"Tom"}}
	_ = <ul><For each={users} key={(u) => u.id}>{(u) => <li>{u.name}</li>}</For></ul>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	for _, want := range []string{
		"gxscene.ForGetterOfSignal[User]", // 关键：从 signal users 推断出 User，而非 any
		"func() []User", "users()",
		"func(u User) any", "u.id",
		"func(u func() User) *gxscene.Instance",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("无类型 render 应推断为 User，期望产物包含 %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ForGetterOfSignal[any]") {
		t.Errorf("不应降级为 any, got:\n%s", out)
	}
}

// 普通变量（非 signal）也应能从 each 推断元素类型，走 bindings 查询路径。
func TestJSXSceneForInferFromPlainVar(t *testing.T) {
	res, err := CompileScene("for.gx", `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type User struct { id int; name string }
func main() {
	users := []User{{1,"Tom"}}
	_ = <ul><For each={users} key={(u) => u.id}>{(u) => <li>{u.name}</li>}</For></ul>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	if !strings.Contains(out, "gxscene.ForGetterOfSignal[User]") {
		t.Errorf("普通变量应推断为 User, got:\n%s", out)
	}
	if strings.Contains(out, "ForGetterOfSignal[any]") {
		t.Errorf("不应降级为 any, got:\n%s", out)
	}
}

// 显式类型注解仍优先于推断（回归保证）。
func TestJSXSceneForExplicitTypeWins(t *testing.T) {
	res, err := CompileScene("for.gx", `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type User struct { id int; name string }
func main() {
	signal users = []User{{1,"Tom"}}
	_ = <ul><For each={users} key={(u) => u.id}>{(u User) => <li>{u.name}</li>}</For></ul>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	if !strings.Contains(out, "gxscene.ForGetterOfSignal[User]") {
		t.Errorf("显式注解应生成 ForGetterOfSignal[User], got:\n%s", out)
	}
}

// 事件属性无条件透传为 OpEvent（前端不设白名单、不校验事件名）。
func TestJSXSceneEventsTranspass(t *testing.T) {
	res, err := CompileScene("evt.gx", `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
func handle() {}
func main() {
	_ = <button id="b" onClick={handle} onCustomThing={func(){}} onMouseEnter={handle}>hi</button>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	// 标准事件 + 自定义事件都编成 OpEvent，Key 原样保留
	for _, key := range []string{`"onClick"`, `"onMouseEnter"`, `"onCustomThing"`} {
		if !strings.Contains(out, "gxscene.OpEvent,Key:"+key) {
			t.Errorf("应生成 OpEvent Key=%s, got:\n%s", key, out)
		}
	}
	// 事件不能是 OpDynAttr（否则会进 Props 被当普通属性）
	if strings.Contains(out, "OpDynAttr,Key:\"on") {
		t.Errorf("事件不应编成 OpDynAttr, got:\n%s", out)
	}
	// 普通静态属性仍是 OpAttr，不受影响
	if !strings.Contains(out, `gxscene.OpAttr,Arg:"b",Key:"id"`) {
		t.Errorf("普通属性应仍是 OpAttr, got:\n%s", out)
	}
}

// 恰好以 on 开头的普通属性（第 3 字符小写）不被误判为事件。
func TestJSXSceneEventNameNotOverreach(t *testing.T) {
	res, err := CompileScene("attr.gx", `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
func main() {
	_ = <div once="x" only="y">hi</div>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	if strings.Contains(out, "OpEvent") {
		t.Errorf("once/only 不应被当事件, got:\n%s", out)
	}
	if !strings.Contains(out, `gxscene.OpAttr,Arg:"x",Key:"once"`) || !strings.Contains(out, `gxscene.OpAttr,Arg:"y",Key:"only"`) {
		t.Errorf("once/only 应编成普通 OpAttr, got:\n%s", out)
	}
}

// 第十一轮 N-2（既有）：each 是索引表达式 rows["k"] 时，元素类型不能退化为 rune。
// 根标识符 rows 是 map[string][]Row，TypeAt 只给出 key 类型 string → 旧逻辑推成 rune。
func TestJSXSceneForInferFromIndexedMap(t *testing.T) {
	res, err := CompileScene("for_indexed.gx", `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type Row struct { id int; name string }
func main() {
	rows := map[string][]Row{"k": {{1, "a"}}}
	_ = <ul><For each={rows["k"]} key={(r) => r.id}>{(r) => <li>{r.name}</li>}</For></ul>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	// 直接断言元素类型 Row 与 item getter 形态，不再自我打折地只查一个子串。
	for _, want := range []string{
		"gxscene.ForGetterOfSignal[Row]",
		"func() Row) *gxscene.Instance",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("each 索引应推断为 Row，期望 %q, got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "ForGetterOfSignal[rune]") || strings.Contains(out, "[]rune") {
		t.Errorf("不应退化为 rune, got:\n%s", out)
	}
}

// 第十二轮 P1-A（本轮回归）：each 是函数调用、实参含索引时，不能因为表达式里
// 出现 '[' 就抢走推断权——根是 filter，方括号在实参 rows["k"] 里。
// 旧实现 strings.Count("[")>0 命中索引路径，推断落空 return "" 掐断 FuncResult
// 兜底，退化成 ForGetterOfSignal[any] 编译失败。
func TestJSXSceneForInferCallWithIndexedArg(t *testing.T) {
	res, err := CompileScene("for_call_idx.gx", `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type Row struct { id int; name string }
func filter(xs []Row) []Row { return xs }
func main() {
	rows := map[string][]Row{"k": {{1, "a"}}}
	_ = <ul><For each={filter(rows["k"])} key={(r) => r.id}>{(r) => <li>{r.name}</li>}</For></ul>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	if !strings.Contains(out, "gxscene.ForGetterOfSignal[Row]") {
		t.Errorf("函数调用实参含索引仍应推断为 Row，got:\n%s", out)
	}
	if strings.Contains(out, "ForGetterOfSignal[any]") || strings.Contains(out, "[]rune") {
		t.Errorf("不应退化为 any/rune, got:\n%s", out)
	}
	// 生成代码必须通过 Go 语法校验（防止 any 版本残留非法调用形态）。
	if err := res.Validate(); err != nil {
		t.Errorf("生成 Go 应语法有效: %v\n---\n%s", err, out)
	}
}

// 第十二轮 P3-E：each 索引集合是函数参数时也能推断元素类型（Binding.Type 优先于文本回扫）。
func TestJSXSceneForInferFromIndexedParam(t *testing.T) {
	res, err := CompileScene("for_param.gx", `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type Row struct { id int; name string }
func render(rows map[string][]Row) any {
	return <ul><For each={rows["k"]} key={(r) => r.id}>{(r) => <li>{r.name}</li>}</For></ul>
}
func main(){}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	if !strings.Contains(out, "gxscene.ForGetterOfSignal[Row]") {
		t.Errorf("函数参数索引应推断为 Row，got:\n%s", out)
	}
	if strings.Contains(out, "ForGetterOfSignal[map[string]") || strings.Contains(out, "[any]") {
		t.Errorf("不应退化为 map/any, got:\n%s", out)
	}
}

// 第十二轮 P3-F：each 是 map（非列表）时产出 for-not-list 诊断，不再误导成 []map[...] 编译错误。
func TestJSXSceneForMapDiagnostic(t *testing.T) {
	res, err := CompileScene("for_map.gx", `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type Row struct { id int; name string }
func main() {
	m := map[string]Row{"k": {1, "a"}}
	_ = <ul><For each={m}>{(r) => <li>x</li>}</For></ul>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range res.Analysis.Diagnostics() {
		if d.Code == "for-not-list" && strings.Contains(d.Message, "请先转成切片") {
			found = true
		}
	}
	if !found {
		t.Errorf("each=map 应产出 for-not-list 诊断, got:\n%v", res.Analysis.Diagnostics())
	}
	// 生成代码不再含 []map[...] / []rune 的误导类型。
	if strings.Contains(res.GoSource, "[]map[") || strings.Contains(res.GoSource, "[]rune") {
		t.Errorf("不应生成误导的 []map/[]rune, got:\n%s", res.GoSource)
	}
}

// 跨行缩进的 JSX 元素不得让生成代码多出/残留换行，导致宿主参数列表语法错误。
// 复现自 examples/jsxapp-full.gx：根视图原来必须写紧凑单行，
// 写成 app.render(() => <div>\n...\n</div>\n) 会报
// "missing ',' before newline in argument list"。
func TestJSXMultilineNoStrayNewlineBeforeClose(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "箭头后换行+JSX跨行+闭括号独立行",
			src: `package main
import gxapp "github.com/robertpanvip/gox/runtime/app"
func main() {
	app, _ := gxapp.open(nil); _ = app
	app.render(() =>
		<div id="app">
			<span>hello</span>
		</div>
	)
}
`,
		},
		{
			name: "JSX跨行+闭括号独立行",
			src: `package main
func main() {
	f := () => <div id="app">
		<span>hello</span>
		<b>world</b>
	</div>
	_ = f
}
`,
		},
		{
			name: "JSX作为多参数之一跨行",
			src: `package main
func main() {
	foo(
		<div>
			<span>x</span>
		</div>,
		other,
	)
}
`,
		},
		{
			name: "JSX作最后参数带尾逗号",
			src: `package main
func main() {
	foo(a,
		<div>
			<span>x</span>
		</div>,
	)
}
`,
		},
		{
			name: "JSX直接作实参跨行(无lambda包装)",
			src: `package main
func main() {
	foo(
		<div>
			<span>x</span>
		</div>
	)
}
`,
		},
		{
			name: "JSX作slice元素跨行",
			src: `package main
func main() {
	x := []any{
		<div>
			<span>x</span>
		</div>,
	}
	_ = x
}
`,
		},
		{
			name: "JSX作map值跨行",
			src: `package main
func main() {
	x := map[string]any{"k": <div>
		<span>x</span>
	</div>}
	_ = x
}
`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := CompileScene("multiline.gx", c.src)
			if err != nil {
				t.Fatalf("编译失败: %v", err)
			}
			if verr := res.Validate(); verr != nil {
				t.Errorf("生成 Go 语法无效: %v\n---\n%s", verr, res.GoSource)
			}
		})
	}
}

// 真正的语句结束换行必须保留——JSX 后跟标识符（另一条语句）时不能把两行粘一起。
func TestJSXMultilineKeepsStatementBreak(t *testing.T) {
	res, err := CompileScene("stmtbreak.gx", `package main
func main() {
	x := <div>
		<span>x</span>
	</div>
	_ = x
}
`)
	if err != nil {
		t.Fatal(err)
	}
	if verr := res.Validate(); verr != nil {
		t.Fatalf("生成 Go 语法无效: %v\n---\n%s", verr, res.GoSource)
	}
	// 两条语句必须各占一行：保留 JSX 后的语句分隔换行。
	if !strings.Contains(res.GoSource, "\n\t_ = x") {
		t.Errorf("JSX 后的语句换行被误吞，两条语句粘在一起:\n%s", res.GoSource)
	}
}

// return JSX 后跟 `}`（函数体结束）的换行必须保留：不能把 return 语句与函数闭合
// 挤到同一行（曾因把 `}` 误列为"吞换行触发符"导致 golden 回归）。
func TestJSXMultilineKeepsReturnFuncClose(t *testing.T) {
	res, err := CompileScene("returnclose.gx", `package main
func Card() any {
	return <div>
		<span>x</span>
	</div>
}
func main() {}
`)
	if err != nil {
		t.Fatal(err)
	}
	if verr := res.Validate(); verr != nil {
		t.Fatalf("生成 Go 语法无效: %v", verr)
	}
	// 函数闭合 `}` 必须独占一行，不能出现 `...)}` 挤在一起。
	if !strings.Contains(res.GoSource, "\n}\n") {
		t.Errorf("return JSX 后的函数闭合换行被误吞:\n%s", res.GoSource)
	}
}

// Issue #2 回归：JSX 属性值内嵌 lambda 中的 signal 复合赋值/简单赋值，
// RHS 的裸 signal 读必须与顶层闭包一致递归补 ()，否则生成 Signal[T] 与
// 标量相加的类型错误（load(load + 1)）。
func TestJSXSceneLambdaSignalCompoundRHS(t *testing.T) {
	res, err := CompileScene("iss2.gx", `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
signal load = 42
signal other = 8
func main() {
	let inc = func() { load = load + 1 } // 顶层闭包对照
	_ = <div>
		<button onClick={gxscene.Handler(func(ctx gxscene.EventContext) {
			load = load + 1
			load = other
			load += other
		})}/>
	</div>
}
`)
	if err != nil {
		t.Fatal(err)
	}
	out := res.GoSource
	if verr := res.Validate(); verr != nil {
		t.Fatalf("生成 Go 语法无效: %v\n---\n%s", verr, out)
	}
	// 顶层闭包与 JSX lambda 的空白排版不同（前者-token 带空格），去空格后统一比对。
	compact := strings.ReplaceAll(strings.ReplaceAll(out, " ", ""), "\t", "")
	// JSX prop 内 lambda：三种赋值的 RHS 都必须递归补 ()，绝不能出现裸 signal 相加/赋值。
	for _, bad := range []string{"load(load+1)", "load(other)", "load(load+other)"} {
		if strings.Contains(compact, bad) {
			t.Errorf("JSX lambda RHS 漏了读调用 %q, got:\n%s", bad, out)
		}
	}
	// 两种正确形态都必须至少各出现一次（顶层闭包与 JSX lambda 各一处）。
	if n := strings.Count(compact, "load(load()+1)"); n < 2 {
		t.Errorf("复合赋值 RHS 读调用应至少出现在顶层闭包与 JSX lambda 两处, got %d:\n%s", n, out)
	}
	if !strings.Contains(compact, "load(other())") {
		t.Errorf("简单赋值 RHS 的 other 应补 (), got:\n%s", out)
	}
	if !strings.Contains(compact, "load(load()+other())") {
		t.Errorf("+= 复合赋值 RHS 的 other 应补 (), got:\n%s", out)
	}
}
