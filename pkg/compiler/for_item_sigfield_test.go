package compiler

import (
	"strings"
	"testing"
)

// Bug 01 回归：<For> 渲染体读取 item 的 sig 字段必须补 ()，否则 getter 函数值
// 被当普通值打印成函数地址（静默错值）。三处同源，一次修：
//  1. render body 直接插值 {u.name}
//  2. key lambda: key={(u)=>u.name}
//  3. item sig 字段传入组件的 sig props: <Card title={u.name}/>
//
// 根因：item 参数 u 的静态类型只登记在 ctx.forItemTypes，不在 Bindings 作用域表，
// sig 字段链解析 recvTypeAt 推不出 receiver 类型 → 不补 ()。修法：For 转换期间
// 临时注入 item 类型覆盖（Bindings.WithTypeOverride）。
func TestForItemSigFieldReadGetsCall(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		mustHave []string // 必须出现的补 () 形态
		mustNot  []string // 不得出现的裸函数值形态
	}{
		{
			name: "render body 直接插值",
			src: `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type User struct { id int; sig name string }
func main() {
	sig users = []User{{1,"Tom"}}
	_ = <ul><For each={users} key={(u) => u.id}>{(u User) => <li>{u.name} {u.id}</li>}</For></ul>
}
`,
			mustHave: []string{"u().name()"},
			mustNot:  []string{"return u().name }", "u().id()"}, // 普通字段 id 不得补 ()
		},
		{
			name: "key lambda 读 sig 字段",
			src: `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type User struct { sig name string }
func main() {
	sig users = []User{{"Tom"}}
	_ = <ul><For each={users} key={(u) => u.name}>{(u User) => <li>{u.name}</li>}</For></ul>
}
`,
			mustHave: []string{"return u.name()"},
			mustNot:  []string{"return u.name }"},
		},
		{
			name: "item sig 字段传入组件 sig props",
			src: `package main
import gxscene "github.com/robertpanvip/gox/runtime/scene"
type User struct { sig name string }
type CardProps struct { sig title string }
func Card(p CardProps) *gxscene.Instance { return nil }
func main() {
	sig users = []User{{"Tom"}}
	_ = <ul><For each={users}>{(u User) => <li><Card title={u.name}/></li>}</For></ul>
}
`,
			mustHave: []string{"return u().name()"},
			mustNot:  []string{"return u().name }"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := CompileScene("for-sigfield.gx", c.src)
			if err != nil {
				t.Fatal(err)
			}
			out := res.GoSource
			if err := res.Validate(); err != nil {
				t.Fatalf("产物应为合法 Go: %v\n%s", err, out)
			}
			containsAll(t, out, c.mustHave...)
			for _, bad := range c.mustNot {
				if strings.Contains(out, bad) {
					t.Errorf("不应出现 %q：\n%s", bad, out)
				}
			}
		})
	}
}
