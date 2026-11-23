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

// 不定高列表：每项高度 = 20 + (i%5)*20，即 20/40/60/80/100 循环。
// 前缀和：item0 在 [0,20)，item1 在 [20,60)，item2 在 [60,120) ...
func variableHeight(i int) float64 { return float64(20 + (i%5)*20) }

func TestVirtualListVariableHeightWindow(t *testing.T) {
	const total = 200
	scroll := signal.New(0.0)
	vl := VirtualList(gxscene.Props{
		"count":           total,
		"height":          variableHeight,
		"estimate-height": 20,
		"viewport-height": 100,
		"overscan":        0,
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

	// scrollTop=0 → 首项应为 item-0
	var texts []string
	collectText(vl.Root(), &texts)
	if len(texts) == 0 || texts[0] != "item-0" {
		t.Fatalf("scrollTop=0 首项应为 item-0: %v", texts[:min(3, len(texts))])
	}

	// scrollTop=150 → 落在 item3 区间 [120,200)，首项应为 item-3（overscan=0）
	scroll(150)
	texts = nil
	collectText(vl.Root(), &texts)
	if len(texts) == 0 || texts[0] != "item-3" {
		t.Fatalf("scrollTop=150 首项应为 item-3: %v", texts[:min(4, len(texts))])
	}
	for _, s := range texts {
		if s == "item-0" {
			t.Error("scrollTop=150 窗口不应再含 item-0")
		}
	}
}

func TestVirtualListVariableHeightSpacer(t *testing.T) {
	// spacer（撑高 div）的高度 = 可见首项之前的累计高度。
	// scrollTop=150 → first=item3 → spacer = h0+h1+h2 = 20+40+60 = 120。
	scroll := signal.New(150.0)
	vl := VirtualList(gxscene.Props{
		"count":           50,
		"height":          variableHeight,
		"viewport-height": 100,
		"overscan":        0,
		"scroll-top":      func() any { return scroll() },
		"render-item": func(i int) *gxscene.Instance {
			return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				{Code: gxscene.OpElem, Arg: "div"},
				{Code: gxscene.OpChildText, Arg: fmt.Sprintf("v-%d", i)},
				{Code: gxscene.OpClose},
			}})
		},
	})
	vl.BindReactive(signal.Effect)

	root := vl.Root()
	// 找到属性 height=120 的 spacer div（第一个子节点，在 For 之前）。
	found := false
	var walk func(*gxscene.Node)
	walk = func(n *gxscene.Node) {
		if n == nil { return }
		if n.Type == gxscene.NodeDiv {
			if h, ok := n.Props["height"]; ok && fmt.Sprintf("%v", h) == "120" {
				found = true
			}
		}
		for _, c := range n.Children { walk(c) }
	}
	walk(root)
	if !found {
		t.Error("spacer 高度应为 120（item0..2 累计），未找到")
	}
}

func TestVirtualListHeightChangeRecomputes(t *testing.T) {
	// height(i) 依赖的数据变化时，滚动定位应随之改变（每次求值重算 offsets）。
	scale := signal.New(1.0)
	scroll := signal.New(100.0)
	vl := VirtualList(gxscene.Props{
		"count":           50,
		"height":          func(i int) float64 { return 40 * scale() },
		"viewport-height": 80,
		"overscan":        0,
		"scroll-top":      func() any { return scroll() },
		"render-item": func(i int) *gxscene.Instance {
			return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				{Code: gxscene.OpElem, Arg: "div"},
				{Code: gxscene.OpChildText, Arg: fmt.Sprintf("h-%d", i)},
				{Code: gxscene.OpClose},
			}})
		},
	})
	vl.BindReactive(signal.Effect)

	// scale=1 → 每项 40，scrollTop=100 落在 item2 → 首项 h-2
	var texts []string
	collectText(vl.Root(), &texts)
	if texts[0] != "h-2" {
		t.Fatalf("scale=1 首项应为 h-2: %v", texts[:min(3, len(texts))])
	}
	// scale=2 → 每项 80，scrollTop=100 落在 item1 → 首项 h-1
	scale(2.0)
	texts = nil
	collectText(vl.Root(), &texts)
	if texts[0] != "h-1" {
		t.Fatalf("scale=2 首项应为 h-1: %v", texts[:min(3, len(texts))])
	}
}
