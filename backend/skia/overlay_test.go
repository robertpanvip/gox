package skia

import (
	"strings"
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

func fixedMeasure2(text string, fontSize int, bold bool) float32 {
	return float32(len([]rune(text))) * float32(fontSize) * 0.56
}

// 构造：根容器里一个 trigger（gx-anchor-id=btn），一个 popover（gx-anchor=btn）。
func overlayTree(placement string) *gxscene.Node {
	return gxscene.Render(gxscene.Template{
		Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "div"}, // root column
			{Code: gxscene.OpAttr, Key: "width", Arg: "400"},
			// trigger 锚点
			{Code: gxscene.OpElem, Arg: "button"},
			{Code: gxscene.OpAttr, Key: "gx-anchor-id", Arg: "btn"},
			{Code: gxscene.OpAttr, Key: "width", Arg: "100"},
			{Code: gxscene.OpAttr, Key: "height", Arg: "30"},
			{Code: gxscene.OpChildText, Arg: "Open"},
			{Code: gxscene.OpClose},
			// popover 浮层
			{Code: gxscene.OpElem, Arg: "div"},
			{Code: gxscene.OpAttr, Key: "gx-role", Arg: "popover"},
			{Code: gxscene.OpAttr, Key: "gx-anchor", Arg: "btn"},
			{Code: gxscene.OpAttr, Key: "gx-placement", Arg: placement},
			{Code: gxscene.OpAttr, Key: "gx-offset", Arg: "8"},
			{Code: gxscene.OpAttr, Key: "width", Arg: "120"},
			{Code: gxscene.OpAttr, Key: "height", Arg: "60"},
			{Code: gxscene.OpChildText, Arg: "Menu"},
			{Code: gxscene.OpClose},
			{Code: gxscene.OpClose},
		},
	}).Root()
}

func findRole(n *gxscene.Node, role string) *gxscene.Node {
	if n == nil {
		return nil
	}
	if gxRole(n) == role {
		return n
	}
	for _, c := range n.Children {
		if h := findRole(c, role); h != nil {
			return h
		}
	}
	return nil
}

func findAnchorID(n *gxscene.Node, id string) *gxscene.Node {
	if n == nil {
		return nil
	}
	if v, ok := n.Props[propGxAnchorID].(string); ok && v == id {
		return n
	}
	for _, c := range n.Children {
		if h := findAnchorID(c, id); h != nil {
			return h
		}
	}
	return nil
}

func TestOverlayBottomStart(t *testing.T) {
	root := overlayTree("bottom-start")
	cache := computeLayout(root, 800, 600, fixedMeasure2)
	anchor := findAnchorID(root, "btn")
	pop := findRole(root, rolePopover)
	if anchor == nil || pop == nil {
		t.Fatal("未找到锚点或浮层")
	}
	ab, pb := cache[anchor], cache[pop]
	// bottom-start：浮层 x 对齐锚点左边，y = 锚点底部 + offset(8)
	if !near(pb.X, ab.X, 1) {
		t.Errorf("bottom-start x = %.1f, want anchor.x %.1f", pb.X, ab.X)
	}
	if !near(pb.Y, ab.Y+ab.H+8, 1) {
		t.Errorf("bottom-start y = %.1f, want %.1f", pb.Y, ab.Y+ab.H+8)
	}
}

func TestOverlayBottomCenter(t *testing.T) {
	root := overlayTree("bottom")
	cache := computeLayout(root, 800, 600, fixedMeasure2)
	anchor := findAnchorID(root, "btn")
	pop := findRole(root, rolePopover)
	ab, pb := cache[anchor], cache[pop]
	// 浮层(120)比锚点(100)宽，居中会溢出左边；应被约束在画布内（x>=0 且不右溢出）。
	if pb.X < 0 || pb.X+pb.W > 800 {
		t.Errorf("bottom x = %.1f 使浮层溢出画布 [0,800]", pb.X)
	}
	_ = ab
}

func TestOverlayFlipToTopWhenNoRoomBelow(t *testing.T) {
	// 自定义场景：锚点靠近底部，下方放不下浮层，但上方有充足空间 → 应翻转到 top。
	root := gxscene.Render(gxscene.Template{
		Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "div"},
			{Code: gxscene.OpAttr, Key: "width", Arg: "400"},
			// 用 padding 把锚点推到接近底部（浮层高 60，需锚点 y 在 60~140 之间）
			{Code: gxscene.OpElem, Arg: "div"},
			{Code: gxscene.OpAttr, Key: "height", Arg: "100"},
			{Code: gxscene.OpClose},
			{Code: gxscene.OpElem, Arg: "button"},
			{Code: gxscene.OpAttr, Key: "gx-anchor-id", Arg: "btn"},
			{Code: gxscene.OpAttr, Key: "width", Arg: "100"},
			{Code: gxscene.OpAttr, Key: "height", Arg: "30"},
			{Code: gxscene.OpClose},
			{Code: gxscene.OpElem, Arg: "div"},
			{Code: gxscene.OpAttr, Key: "gx-role", Arg: "popover"},
			{Code: gxscene.OpAttr, Key: "gx-anchor", Arg: "btn"},
			{Code: gxscene.OpAttr, Key: "gx-placement", Arg: "bottom-start"},
			{Code: gxscene.OpAttr, Key: "gx-offset", Arg: "8"},
			{Code: gxscene.OpAttr, Key: "width", Arg: "120"},
			{Code: gxscene.OpAttr, Key: "height", Arg: "60"},
			{Code: gxscene.OpClose},
			{Code: gxscene.OpClose},
		},
	}).Root()
	// 画布高 150：锚点 y≈104,底≈134；下方 134+8+60=202>150 放不下；上方 104-8-60=36>=0 可行。
	cache := computeLayout(root, 800, 150, fixedMeasure2)
	anchor := findAnchorID(root, "btn")
	pop := findRole(root, rolePopover)
	ab, pb := cache[anchor], cache[pop]
	// 翻转后浮层底边应在锚点顶边之上。
	if pb.Y+pb.H > ab.Y+1 {
		t.Errorf("应翻转到 top：浮层底 %.1f 应 <= 锚点顶 %.1f (锚点 y=%.1f, h=%.1f)", pb.Y+pb.H, ab.Y, ab.Y, ab.H)
	}
}

// 浮层不占流：比对同一棵树结构下有无浮层时锚点坐标。
func TestOverlayOutOfFlow(t *testing.T) {
	build := func(withPop bool) *gxscene.Node {
		ops := []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "div"},
			{Code: gxscene.OpAttr, Key: "width", Arg: "400"},
			{Code: gxscene.OpElem, Arg: "button"},
			{Code: gxscene.OpAttr, Key: "gx-anchor-id", Arg: "btn"},
			{Code: gxscene.OpAttr, Key: "width", Arg: "100"},
			{Code: gxscene.OpAttr, Key: "height", Arg: "30"},
			{Code: gxscene.OpChildText, Arg: "Open"},
			{Code: gxscene.OpClose},
		}
		if withPop {
			ops = append(ops,
				gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "gx-role", Arg: "popover"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "gx-anchor", Arg: "btn"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "width", Arg: "120"},
				gxscene.Op{Code: gxscene.OpAttr, Key: "height", Arg: "60"},
				gxscene.Op{Code: gxscene.OpClose},
			)
		}
		ops = append(ops, gxscene.Op{Code: gxscene.OpClose})
		return gxscene.Render(gxscene.Template{Ops: ops}).Root()
	}
	r1, r2 := build(true), build(false)
	c1 := computeLayout(r1, 800, 600, fixedMeasure2)
	c2 := computeLayout(r2, 800, 600, fixedMeasure2)
	a1 := c1[findAnchorID(r1, "btn")]
	a2 := c2[findAnchorID(r2, "btn")]
	if !near(a1.X, a2.X, 0.5) || !near(a1.Y, a2.Y, 0.5) {
		t.Errorf("浮层影响了文档流：有浮层锚点 (%.1f,%.1f) vs 无浮层 (%.1f,%.1f)", a1.X, a1.Y, a2.X, a2.Y)
	}
}

func TestParsePlacement(t *testing.T) {
	cases := map[string]placement{
		"bottom":       {side: "bottom", align: "start"},
		"bottom-start": {side: "bottom", align: "start"},
		"bottom-end":   {side: "bottom", align: "end"},
		"top-center":   {side: "top", align: "center"},
		"garbage":      {side: "bottom", align: "start"},
	}
	for in, want := range cases {
		got := parsePlacement(in)
		if got != want {
			t.Errorf("parsePlacement(%q) = %+v, want %+v", in, got, want)
		}
	}
}

// TestOverlayDrawOrder 验证绘制层级：普通内容 → 遮罩 → 浮层。
// 用 Window 的 DrawLog 观察实际绘制顺序。
func TestOverlayDrawOrder(t *testing.T) {
	root := elemNode("div", map[string]any{"width": 400},
		elemNode("button", map[string]any{"gx-anchor-id": "btn", "width": 100, "height": 30},
			textNode("Trigger", nil),
		),
		elemNode("div", map[string]any{"gx-role": "scrim", "bg": "#00000080"},
			textNode("Scrim", nil),
		),
		elemNode("div", map[string]any{
			"gx-role": "popover", "gx-anchor": "btn", "gx-placement": "bottom-start",
			"width": 120, "height": 60, "bg": "#ffffff",
		},
			textNode("Menu", nil),
		),
		// 普通内容里放一个标记，确认它在浮层之前绘制。
		elemNode("div", map[string]any{}, textNode("NormalContent", nil)),
	)

	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Title: "ov", Width: 800, Height: 600, Backend: "skia"}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Draw(root); err != nil {
		t.Fatal(err)
	}
	log := w.DrawLog()
	idx := func(substr string) int {
		for i, op := range log {
			if op.Kind == "text" && strings.Contains(op.Name, substr) {
				return i
			}
		}
		return -1
	}
	iNormal := idx("NormalContent")
	iMenu := idx("Menu")
	iScrim := idx("Scrim")
	if iNormal < 0 || iMenu < 0 || iScrim < 0 {
		t.Fatalf("绘制缺失: normal=%d scrim=%d menu=%d, log=%v", iNormal, iScrim, iMenu, log)
	}
	if !(iScrim > iNormal && iMenu > iScrim) {
		t.Errorf("绘制顺序应为 普通<%d> < 遮罩<%d> < 浮层<%d>", iNormal, iScrim, iMenu)
	}
}

// TestAbsolutePosition 验证 position:absolute + inset：脱离 flex 流、相对直接父节点定位。
func TestAbsolutePosition(t *testing.T) {
	root := elemNode("div", map[string]any{"width": 300, "height": 200},
		// 普通流内节点 A
		elemNode("div", map[string]any{"width": 100, "height": 40, "bg": "#ff0000"}),
		// absolute 节点：top=20 left=50
		elemNode("div", map[string]any{
			"position": "absolute", "top": 20, "left": 50, "width": 60, "height": 30,
			"bg": "#00ff00",
		}),
		// 普通流内节点 B：位置应紧接 A（不受 absolute 影响）
		elemNode("div", map[string]any{"width": 100, "height": 40, "bg": "#0000ff"}),
	)
	cache := computeLayout(root, 800, 600, fixedMeasure2)
	children := root.Children
	a, abs, b := cache[children[0]], cache[children[1]], cache[children[2]]

	// absolute 按 top/left 定位（相对父 padding box 边缘，CSS 语义）
	if !near(abs.X, 50, 1.5) || !near(abs.Y, 20, 1.5) {
		t.Errorf("absolute pos = (%.1f,%.1f), want ≈(50,20)", abs.X, abs.Y)
	}
	// B 应紧跟 A：A 高 40，A 从 padding 4 起 → B.y ≈ 4+40+gap(0)
	if !near(b.Y, a.Y+a.H, 1.5) {
		t.Errorf("B.y=%.1f 应紧跟 A 底部 %.1f（absolute 不应占流）", b.Y, a.Y+a.H)
	}
}

// TestInsetRightBottom 验证 right/bottom inset 定位。
func TestInsetRightBottom(t *testing.T) {
	root := elemNode("div", map[string]any{"width": 300, "height": 200},
		elemNode("div", map[string]any{
			"position": "absolute", "right": 10, "bottom": 10, "width": 50, "height": 20,
		}),
	)
	cache := computeLayout(root, 800, 600, fixedMeasure2)
	b := cache[root.Children[0]]
	// 父 300x200；right=10,width=50 → x = 300-10-50 = 240；bottom=10,height=20 → y = 200-10-20 = 170
	if !near(b.X, 240, 2) || !near(b.Y, 170, 2) {
		t.Errorf("right/bottom inset pos = (%.1f,%.1f), want ≈(240,170)", b.X, b.Y)
	}
}
