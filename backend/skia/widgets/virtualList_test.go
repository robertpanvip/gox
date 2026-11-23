package widgets

import (
	"fmt"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

func countNodes(n *gxscene.Node) int {
	if n == nil {
		return 0
	}
	c := 1
	for _, ch := range n.Children {
		c += countNodes(ch)
	}
	return c
}

func collectText(n *gxscene.Node, out *[]string) {
	if n == nil {
		return
	}
	if n.Type == gxscene.NodeText && n.Text != "" {
		*out = append(*out, n.Text)
	}
	for _, c := range n.Children {
		collectText(c, out)
	}
}

func TestVirtualListWindow(t *testing.T) {
	const total = 1000
	scroll := signal.New(0.0)
	var vl *gxscene.Instance
	vl = VirtualList(gxscene.Props{
		"count":           total,
		"item-height":     40,
		"viewport-height": 200, // 可见 5 项
		"overscan":        2,
		"scroll-top":      func() any { return scroll() },
		"render-item": func() any {
			return func(i int) *gxscene.Instance {
				return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
					{Code: gxscene.OpElem, Arg: "div"},
					{Code: gxscene.OpChildText, Arg: fmt.Sprintf("item-%d", i)},
					{Code: gxscene.OpClose},
				}})
			}
		},
	})
	vl.BindReactive(signal.Effect)
	root := vl.Root()

	var texts []string
	collectText(root, &texts)
	if len(texts) == 0 {
		t.Fatal("应渲染出可见项")
	}
	if len(texts) >= total/4 {
		t.Errorf("窗口节点过多: %d（应远小于总数 %d）", len(texts), total)
	}
	// 首窗口应含 0..~8（5 可见 + 2*2 overscan）
	first := texts[0]
	if first != "item-0" {
		t.Errorf("首项 = %s, want item-0", first)
	}
}

func TestVirtualListScrollMovesWindow(t *testing.T) {
	const total = 1000
	scroll := signal.New(0.0)
	vl := VirtualList(gxscene.Props{
		"count":           total,
		"item-height":     40,
		"viewport-height": 200,
		"overscan":        1,
		"scroll-top":      func() any { return scroll() },
		"render-item": func(i int) *gxscene.Instance {
			return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				{Code: gxscene.OpElem, Arg: "div"},
				{Code: gxscene.OpChildText, Arg: fmt.Sprintf("item-%d", i)},
				{Code: gxscene.OpClose},
			}})
		},
	})
	vl.BindReactive(signal.Effect)
	var texts []string
	collectText(vl.Root(), &texts)
	if len(texts) == 0 || texts[0] != "item-0" {
		t.Fatalf("初始窗口应从 item-0 开始: %v", texts[:min(3, len(texts))])
	}
	// 滚动到 400px（第 10 项）→ 窗口应含 item-9 左右，不含 item-0
	scroll(400)
	texts = nil
	collectText(vl.Root(), &texts)
	for _, s := range texts {
		if s == "item-0" {
			t.Error("滚动后窗口不应再含 item-0")
		}
	}
	found9 := false
	for _, s := range texts {
		if s == "item-9" || s == "item-10" {
			found9 = true
		}
	}
	if !found9 {
		t.Errorf("滚动后窗口应含 item-9/10: %v", texts[:min(6, len(texts))])
	}
}

func min(a, b int) int { if a < b { return a }; return b }
