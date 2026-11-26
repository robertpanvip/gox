package interp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robertpanvip/gox/runtime/signal"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// 本文件是「GUI 缺陷自动化检测」的解释器层探针。
// 桌面 IDE 右侧预览走的是 interp.Interpret(EditorText())，因此很多肉眼可见的
// GUI 乱码（如 0x... 指针地址）可以在纯逻辑层被稳定、快速地断言，无需起窗口。

// collectTexts 收集场景树里所有文本节点的文本。
func collectTexts(n *gxscene.Node, out *[]string) {
	if n == nil {
		return
	}
	if n.Type == gxscene.NodeText && n.Text != "" {
		*out = append(*out, n.Text)
	}
	for _, c := range n.Children {
		collectTexts(c, out)
	}
}

// TestPropGetterNotLeakedAsPointer 检测「预览区指针乱码」缺陷：
// <Card title={text}/> 的动态 prop 在读取 p.title 时，必须解引用 getter 拿到
// 真实字符串（"Hello"），而不是把 getter 函数本身 fmt.Sprint 成 0x... 地址。
//
// 该探针固化的是用户可见的正确行为：预览里应出现业务文本，且绝不能出现指针地址。
func TestPropGetterNotLeakedAsPointer(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "samples", "12_component.gx"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Interpret(string(src))
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	collectTexts(res.Instance.Root(), &texts)
	joined := strings.Join(texts, "|")

	if strings.Contains(joined, "0x") {
		t.Fatalf("抓到缺陷：组件动态 prop 泄露为指针地址（预览乱码）：%q", joined)
	}
	if !strings.Contains(joined, "Hello") {
		t.Fatalf("抓到缺陷：组件 prop 未显示业务文本 Hello：%q", joined)
	}
}

// TestPropGetterReactiveAfterClick 检测响应式：点击「改标题」后，组件 prop 应
// 读到 sig 的实时值并更新为 Changed——这是「保鲜」语义的用户可见行为。
func TestPropGetterReactiveAfterClick(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "samples", "12_component.gx"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Interpret(string(src))
	if err != nil {
		t.Fatal(err)
	}
	// 桌面装配层在渲染时会绑定 signal effect；纯逻辑测试需显式绑定，
	// 否则文本节点不会随 sig 更新（与 playground 的响应式测试同口径）。
	res.Instance.BindLeafEffects(signal.Effect)
	res.Instance.BindStructural(signal.Effect)

	var findButton func(*gxscene.Node) *gxscene.Node
	findButton = func(n *gxscene.Node) *gxscene.Node {
		if n == nil {
			return nil
		}
		if n.Kind == "button" {
			return n
		}
		for _, c := range n.Children {
			if b := findButton(c); b != nil {
				return b
			}
		}
		return nil
	}
	btn := findButton(res.Instance.Root())
	if btn == nil {
		t.Fatal("找不到「改标题」button 节点")
	}
	if !btn.Dispatch("onClick", gxscene.EventContext{}) {
		t.Fatal("onClick 事件派发失败")
	}

	var texts []string
	collectTexts(res.Instance.Root(), &texts)
	joined := strings.Join(texts, "|")
	if !strings.Contains(joined, "Changed") {
		t.Fatalf("抓到缺陷：点击后组件 prop 未更新为 Changed：%q", joined)
	}
}
