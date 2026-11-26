package gxweb

// 三端一致性矩阵测试（Web 端）：读取 testdata/threeway/cases.json 的共享用例，
// 用 memDriver（native 无头 DOM 镜像）消费每个场景，断言两件事：
//
//  1. style 期望：每个样式 prop 以**原样字符串**到达元素属性面（gx-name 定位），
//     即 host → Web 前端的透传链路无丢失、无改名、无改值。真实浏览器的
//     cssName/cssValue 转换由 css 期望抽查（同一共享函数，js 驱动运行时调用）。
//  2. css 期望：cssValue/cssName 对关键属性的转换输出（带 px、简写展开等）
//     与三端布局语义一致。
//
// rect/text/hit 等布局观测由 Skia（Yoga）与 Rust（Taffy）两端评估；Web 的
// 真实布局发生在浏览器里，headless native 不可测——浏览器侧行为由 cssName /
// cssValue / flexDefaultColumn 的单测 + 真实浏览器预览覆盖。
//
// 运行：go test ./backend/web -run TestThreeWayMatrixWeb
// GX_THREEWAY_OUT 指定观测结果 JSON 输出路径（scripts/threeway-parity.sh 用）。

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

func TestThreeWayMatrixWeb(t *testing.T) {
	suite := twLoadCasesWeb(t)

	type caseResult struct {
		Name  string   `json:"name"`
		Pass  bool     `json:"pass"`
		Fails []string `json:"fails,omitempty"`
	}
	report := struct {
		Backend string       `json:"backend"`
		Cases   []caseResult `json:"cases"`
	}{Backend: "web"}

	for _, c := range suite.Cases {
		var fails []string
		// 用例若只含布局类期望（rect/text/order/hit），Web 端 native 不评估，
		// 报告里记为跳过（pass=true, fails=nil，由脚本标注 NA）。
		hasWeb := false
		for _, e := range c.Expect {
			if e.Kind == "style" || e.Kind == "css" {
				hasWeb = true
			}
		}
		if hasWeb {
			root := twBuildNodeWeb(c.Scene, new(twIDGen))
			d := newMemDriver()
			w := NewWithDriver(d)
			if err := w.Open(gxapp.DefaultConfig()); err != nil {
				t.Fatalf("case %s: 打开窗口: %v", c.Name, err)
			}
			if err := w.Draw(root); err != nil {
				t.Fatalf("case %s: Draw: %v", c.Name, err)
			}
			// 收集元素属性面：gx-name → props 快照。
			byName := map[string]map[string]string{}
			for _, m := range w.elems {
				name := m.attrs["gx-name"]
				if name == "" {
					continue
				}
				snap := map[string]string{}
				for k, v := range m.attrs {
					snap[k] = v
				}
				byName[name] = snap
			}
			for _, e := range c.Expect {
				switch e.Kind {
				case "style":
					el, ok := byName[e.Name]
					if !ok {
						fails = append(fails, fmt.Sprintf("style: 找不到 gx-name=%q 的元素（元素未挂载或 name 未透传）", e.Name))
						continue
					}
					prop, want := e.Prop, e.Value
					got, ok := el[prop]
					if !ok {
						fails = append(fails, fmt.Sprintf("style: %s 的 %s 未透传到 Web（属性丢失）", e.Name, prop))
						continue
					}
					if got != want {
						fails = append(fails, fmt.Sprintf("style: %s 的 %s = %q, want %q（传值被改写）", e.Name, prop, got, want))
					}
				case "css":
					if got := cssValue(e.Prop, e.Value); got != e.Css {
						fails = append(fails, fmt.Sprintf("css: cssValue(%s,%q) = %q, want %q", e.Prop, e.Value, got, e.Css))
					}
				}
			}
		}
		report.Cases = append(report.Cases, caseResult{Name: c.Name, Pass: len(fails) == 0, Fails: fails})
		for _, f := range fails {
			t.Errorf("[web] %s: %s", c.Name, f)
		}
	}

	if out := os.Getenv("GX_THREEWAY_OUT"); out != "" {
		raw, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(out, raw, 0o644); err != nil {
			t.Logf("写出 %s 失败: %v（不影响断言）", out, err)
		}
	}
}

func twLoadCasesWeb(t *testing.T) twSuiteWeb {
	t.Helper()
	path := os.Getenv("GX_THREEWAY_CASES")
	if path == "" {
		path = "../../testdata/threeway/cases.json"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取用例 %s: %v", path, err)
	}
	var suite twSuiteWeb
	if err := json.Unmarshal(raw, &suite); err != nil {
		t.Fatalf("解析用例 %s: %v", path, err)
	}
	return suite
}

type twSuiteWeb struct {
	Viewport []int       `json:"viewport"`
	Cases    []twCaseWeb `json:"cases"`
}

type twCaseWeb struct {
	Name   string        `json:"name"`
	Scene  twNodeWeb     `json:"scene"`
	Expect []twExpectWeb `json:"expect"`
}

type twExpectWeb struct {
	Kind  string `json:"kind"`
	Name  string `json:"name"`  // style: gx-name
	Prop  string `json:"prop"`  // style/css: 属性名
	Value string `json:"value"` // style: 期望原样值；css: 输入值
	Css   string `json:"css"`   // css: 期望转换值
}

type twNodeWeb struct {
	Kind     string            `json:"kind"`
	Text     string            `json:"text"`
	Props    map[string]string `json:"props"`
	Name     string            `json:"name"`
	Children []twNodeWeb       `json:"children"`
}

// twIDGen 给手工构造的场景树分配唯一 NodeID。真实场景树由 host 分配稳定 ID；
// web 镜像按 NodeID 登记（重名节点先销毁），全零 ID 会让文本子节点把容器挤掉。
type twIDGen struct{ n gxscene.NodeID }

func (g *twIDGen) next() gxscene.NodeID {
	g.n++
	return g.n
}

func twBuildNodeWeb(n twNodeWeb, gen *twIDGen) *gxscene.Node {
	node := &gxscene.Node{}
	if n.Kind == "text" {
		node.Type = gxscene.NodeText
		node.Text = n.Text
	} else {
		node.Type = gxscene.NodeDiv
		node.Kind = "div"
	}
	node.ID = gen.next()
	if len(n.Props) > 0 || n.Name != "" {
		node.Props = map[string]any{}
		for k, v := range n.Props {
			node.Props[k] = v
		}
		if n.Name != "" {
			node.Props["gx-name"] = n.Name
		}
	}
	for _, c := range n.Children {
		node.Children = append(node.Children, twBuildNodeWeb(c, gen))
	}
	return node
}

// ---- Web 端默认 flex 方向约定的单测（js 驱动在运行时调用 flexDefaultColumn）----

func TestFlexDefaultColumn(t *testing.T) {
	cases := []struct {
		key, existing string
		want          bool
	}{
		{"gap", "", true},             // 任意 flex 意图属性 + 未声明方向 → 补 column
		{"justify-content", "", true}, //
		{"flex-direction", "", false}, // 显式声明本身不补（调用方 setProperty 已处理）
		{"gap", "row", false},         // 已显式声明 row → 不覆盖
		{"gap", "column", false},      // 已显式声明 column → 不覆盖
		{"row-gap", " row ", false},   // 带空白也算已声明
		{"flex-shrink", "", true},     //
	}
	for _, c := range cases {
		if got := flexDefaultColumn(c.key, c.existing); got != c.want {
			t.Errorf("flexDefaultColumn(%q, %q) = %v, want %v", c.key, c.existing, got, c.want)
		}
	}
}
