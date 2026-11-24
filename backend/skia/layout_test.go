package skia

import (
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/widgets"
)

// fixedMeasure 是测试用的确定性文本测宽：每个字符 10px，行高固定 20px。
func fixedMeasure(text string, fontSize int, bold bool) float32 {
	return float32(len([]rune(text))) * 10
}

func txtNode(text string, props map[string]any) *gxscene.Node {
	p := map[string]any{"font-size": 16}
	for k, v := range props {
		p[k] = v
	}
	return &gxscene.Node{Type: gxscene.NodeText, Kind: "#text", Text: text, Props: p}
}

func boxNode(kind string, props map[string]any, children ...*gxscene.Node) *gxscene.Node {
	return &gxscene.Node{Type: gxscene.NodeDiv, Kind: kind, Props: props, Children: children}
}

func near(got, want, tol float32) bool {
	d := got - want
	if d < 0 {
		d = -d
	}
	return d <= tol
}

// TestFlexRowWithGap：row + gap=12 让三个子节点横排且间距精确为 12。
func TestFlexRowWithGap(t *testing.T) {
	root := boxNode("div", map[string]any{
		"flex-direction": "row", "gap": 12,
	},
		boxNode("a", map[string]any{"width": 100, "height": 40}),
		boxNode("b", map[string]any{"width": 100, "height": 40}),
		boxNode("c", map[string]any{"width": 100, "height": 40}),
	)
	cache := computeLayout(root, 640, 200, fixedMeasure)
	a, b, c := cache[root.Children[0]], cache[root.Children[1]], cache[root.Children[2]]
	if !near(b.X, a.X+a.W+12, 0.5) {
		t.Errorf("a→b 间距 = %.1f, want 12 (a.X=%.1f W=%.1f b.X=%.1f)", b.X-a.X-a.W, a.X, a.W, b.X)
	}
	if !near(c.X, b.X+b.W+12, 0.5) {
		t.Errorf("b→c 间距 = %.1f, want 12", c.X-b.X-b.W)
	}
	// 同一行：Y 相同
	if !near(a.Y, b.Y, 0.5) || !near(b.Y, c.Y, 0.5) {
		t.Errorf("row 子节点应同一 Y: a=%.1f b=%.1f c=%.1f", a.Y, b.Y, c.Y)
	}
}

// TestFlexColumnWithGap：column + gap=8 让子节点纵向间距精确为 8。
func TestFlexColumnWithGap(t *testing.T) {
	root := boxNode("div", map[string]any{
		"flex-direction": "column", "gap": 8,
	},
		boxNode("a", map[string]any{"width": 100, "height": 30}),
		boxNode("b", map[string]any{"width": 100, "height": 30}),
	)
	cache := computeLayout(root, 640, 200, fixedMeasure)
	a, b := cache[root.Children[0]], cache[root.Children[1]]
	if !near(b.Y, a.Y+a.H+8, 0.5) {
		t.Errorf("a→b 纵向间距 = %.1f, want 8 (a.Y=%.1f H=%.1f b.Y=%.1f)", b.Y-a.Y-a.H, a.Y, a.H, b.Y)
	}
}

// TestFlexRowJustifySpaceBetween：两端对齐，首项贴左、末项贴右。
func TestFlexRowJustifySpaceBetween(t *testing.T) {
	root := boxNode("div", map[string]any{
		"flex-direction": "row", "justify-content": "space-between", "width": 600, "padding": 0,
	},
		boxNode("a", map[string]any{"width": 100, "height": 40}),
		boxNode("b", map[string]any{"width": 100, "height": 40}),
	)
	cache := computeLayout(root, 600, 200, fixedMeasure)
	a, b := cache[root.Children[0]], cache[root.Children[1]]
	if !near(a.X, 0, 0.5) {
		t.Errorf("首项应贴左 x=0, got %.1f", a.X)
	}
	if !near(b.X+b.W, 600, 0.5) {
		t.Errorf("末项应贴右 right=600, got %.1f", b.X+b.W)
	}
}

// TestFlexAlignCenter：row + align-items=center 让子节点在交叉轴（Y）居中。
func TestFlexAlignCenter(t *testing.T) {
	root := boxNode("div", map[string]any{
		"flex-direction": "row", "align-items": "center", "height": 200,
	},
		boxNode("a", map[string]any{"width": 100, "height": 40}),
	)
	cache := computeLayout(root, 640, 200, fixedMeasure)
	a := cache[root.Children[0]]
	// 容器高 200，子元素高 40 → 居中 y=(200-40)/2=80
	if !near(a.Y, 80, 0.5) {
		t.Errorf("align=center 应使 y=80, got %.1f", a.Y)
	}
}

// TestFlexGapWithJustify：gap 与 justify=center 混合，间距仍为 gap，整体居中。
func TestFlexGapWithJustify(t *testing.T) {
	root := boxNode("div", map[string]any{
		"flex-direction": "row", "gap": 10, "justify-content": "center", "width": 400,
	},
		boxNode("a", map[string]any{"width": 50, "height": 30}),
		boxNode("b", map[string]any{"width": 50, "height": 30}),
	)
	cache := computeLayout(root, 400, 200, fixedMeasure)
	a, b := cache[root.Children[0]], cache[root.Children[1]]
	if !near(b.X-a.X-a.W, 10, 0.5) {
		t.Errorf("gap 应精确为 10, got %.1f", b.X-a.X-a.W)
	}
	// 总宽 = 50+10+50 = 110，居中起点 = (400-110)/2 = 145
	if !near(a.X, 145, 0.5) {
		t.Errorf("整体应居中 x=145, got %.1f", a.X)
	}
}

// TestFlexNestedAbsoluteCoords：嵌套容器（如组件 Fragment）中，子节点坐标必须是
// 画布绝对坐标（累加父偏移），而不是 Yoga 返回的相对父节点坐标。
// 这个回归用例对应真实 bug：组件包装导致子节点全部堆在窗口顶部。
func TestFlexNestedAbsoluteCoords(t *testing.T) {
	root := boxNode("div", map[string]any{
		"flex-direction": "column", "padding": 10, "width": 300,
	},
		boxNode("header", map[string]any{"width": 100, "height": 40}),
		// mid 是一个带 padding 的容器（模拟组件 Fragment），内部有子节点。
		boxNode("mid", map[string]any{"width": 100, "height": 60, "padding": 8},
			boxNode("inner", map[string]any{"width": 40, "height": 20}),
		),
	)
	cache := computeLayout(root, 300, 400, fixedMeasure)
	header := cache[root.Children[0]]
	mid := cache[root.Children[1]]
	inner := cache[root.Children[1].Children[0]]

	// header 在 root padding 之后：y=10
	if !near(header.Y, 10, 0.5) {
		t.Errorf("header.Y = %.1f, want 10", header.Y)
	}
	// mid 紧接 header：y=10+40=50
	if !near(mid.Y, 50, 0.5) {
		t.Errorf("mid.Y = %.1f, want 50", mid.Y)
	}
	// inner 是 mid 的子节点，绝对坐标应累加：mid.Y + mid.padding(8) = 58，
	// 而不是 Yoga 相对坐标 8。若实现错误会得到 8（堆到顶部）。
	if !near(inner.Y, 58, 0.5) {
		t.Errorf("inner.Y = %.1f, want 58 (绝对坐标，含父偏移 50+8)", inner.Y)
	}
}

// TestFlexDefaultColumn：无 flex 属性时默认纵向排布（兼容历史行为）。
func TestFlexDefaultColumn(t *testing.T) {
	root := boxNode("div", map[string]any{"width": 300, "padding": 0},
		boxNode("a", map[string]any{"width": 100, "height": 30}),
		boxNode("b", map[string]any{"width": 100, "height": 30}),
	)
	cache := computeLayout(root, 300, 200, fixedMeasure)
	a, b := cache[root.Children[0]], cache[root.Children[1]]
	if !near(b.Y, a.Y+a.H, 0.5) {
		t.Errorf("默认 column 应纵向紧邻: a.Y=%.1f H=%.1f b.Y=%.1f", a.Y, a.H, b.Y)
	}
	if !near(a.X, b.X, 0.5) {
		t.Errorf("默认 column 子节点 X 应一致: a=%.1f b=%.1f", a.X, b.X)
	}
}

// === 以下用例验证 Yoga 3.x 新能力（旧 kjk/flex 不支持） ===

// TestFlexRowColumnGap：row-gap / column-gap 独立设置（wrap 场景下的行列间距）。
func TestFlexRowColumnGap(t *testing.T) {
	root := boxNode("div", map[string]any{
		"flex-direction": "row", "flex-wrap": "wrap",
		"column-gap": 20, "width": 1000, "padding": 0,
	},
		boxNode("a", map[string]any{"width": 100, "height": 40}),
		boxNode("b", map[string]any{"width": 100, "height": 40}),
	)
	cache := computeLayout(root, 1000, 200, fixedMeasure)
	a, b := cache[root.Children[0]], cache[root.Children[1]]
	if !near(b.X-a.X-a.W, 20, 0.5) {
		t.Errorf("column-gap 应精确为 20, got %.1f", b.X-a.X-a.W)
	}
}

// TestFlexSpaceEvenly：space-evenly 让间距与两端留白相等（旧版只到 space-around）。
func TestFlexSpaceEvenly(t *testing.T) {
	root := boxNode("div", map[string]any{
		"flex-direction": "row", "justify-content": "space-evenly", "width": 400, "padding": 0,
	},
		boxNode("a", map[string]any{"width": 100, "height": 40}),
		boxNode("b", map[string]any{"width": 100, "height": 40}),
	)
	cache := computeLayout(root, 400, 200, fixedMeasure)
	a, b := cache[root.Children[0]], cache[root.Children[1]]
	// 剩余 = 400-200 = 200，三等分：左留白 ≈ 66.7，中间距 ≈ 66.7，右留白 ≈ 66.7
	if !near(a.X, 200.0/3, 1.0) {
		t.Errorf("space-evenly 首项 x ≈ 66.7, got %.1f", a.X)
	}
	if !near(b.X-a.X-a.W, 200.0/3, 1.0) {
		t.Errorf("space-evenly 中间距 ≈ 66.7, got %.1f", b.X-a.X-a.W)
	}
}

// TestFlexWrap：flex-wrap=wrap 时超宽子元素换行（旧版完全不支持换行）。
func TestFlexWrap(t *testing.T) {
	root := boxNode("div", map[string]any{
		"flex-direction": "row", "flex-wrap": "wrap", "align-content": "flex-start", "width": 200, "padding": 0,
	},
		boxNode("a", map[string]any{"width": 120, "height": 40}),
		boxNode("b", map[string]any{"width": 120, "height": 40}),
	)
	cache := computeLayout(root, 200, 400, fixedMeasure)
	a, b := cache[root.Children[0]], cache[root.Children[1]]
	if !near(b.Y, a.Y+a.H, 0.5) {
		t.Errorf("换行后 b 应在 a 下方：a.Y=%.1f H=%.1f b.Y=%.1f", a.Y, a.H, b.Y)
	}
}

// TestTextStyleInheritance：文本节点的颜色/字号/字重从祖先元素继承。
// 回归用例：场景树里文本节点 Props 为空，若不继承样式，文字会用默认黑色小字
// （曾在深色看板截图里表现为"文字看不见"）。
func TestTextStyleInheritance(t *testing.T) {
	tpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpAttr, Arg: "#ff0000", Key: "color"},
		{Code: gxscene.OpAttr, Arg: "30", Key: "font-size"},
		{Code: gxscene.OpAttr, Arg: "bold", Key: "weight"},
		{Code: gxscene.OpChildText, Arg: "HELLO"},
		{Code: gxscene.OpClose},
	}, N: 0}
	root := gxscene.Render(tpl, nil).Root()

	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 200, Height: 100}); err != nil {
		t.Fatal(err)
	}
	if err := w.Draw(root); err != nil {
		t.Fatal(err)
	}
	log := w.DrawLog()
	want := argb(255, 255, 0, 0) // 继承父元素的红色
	found := false
	for _, op := range log {
		if op.Kind == "text" && op.Name == "HELLO" {
			found = true
			if op.ARGB != want {
				t.Errorf("文本应继承父元素颜色 #ffff0000, got #%08x", op.ARGB)
			}
		}
	}
	if !found {
		t.Error("未找到 HELLO 文本绘制指令")
	}
}

// fragNode 构造透明包装节点（模拟组件槽 OpComp / Branch / For 的 #frag）。
func fragNode(children ...*gxscene.Node) *gxscene.Node {
	return &gxscene.Node{Type: gxscene.NodeFragment, Kind: "#frag", Props: map[string]any{}, Children: children}
}

// TestFragmentTransparentInRow：组件包装节点（NodeFragment）用 display:contents
// 语义，不得破坏父 row 容器的横排。回归用例：预制控件库里 Row>Card 会形成
// row div > #frag > card#frag > div 的多层包装，早期版本每层 fragment 各自成
// column 盒子，导致三个卡片竖排。
func TestFragmentTransparentInRow(t *testing.T) {
	a := boxNode("a", map[string]any{"width": 100, "height": 40})
	b := boxNode("b", map[string]any{"width": 100, "height": 40})
	// row > #frag > (a, #frag > b)：两层透明包装
	root := boxNode("div", map[string]any{
		"flex-direction": "row", "gap": 12,
	},
		fragNode(a, fragNode(b)),
	)
	cache := computeLayout(root, 640, 200, fixedMeasure)
	ba, bb := cache[a], cache[b]
	if !near(bb.X, ba.X+ba.W+12, 0.5) {
		t.Errorf("透明包装后 a→b 横排间距 = %.1f, want 12 (a.X=%.1f W=%.1f b.X=%.1f)",
			bb.X-ba.X-ba.W, ba.X, ba.W, bb.X)
	}
	if !near(ba.Y, bb.Y, 0.5) {
		t.Errorf("透明包装后应同一行：a.Y=%.1f b.Y=%.1f", ba.Y, bb.Y)
	}
}

// TestInputNaturalHeight：Input 不得写死会塌陷的高度（border-box 下被 padding
// 吃光，内容区为 0）。Apple macOS regular control 高度为 28pt，因此断言外框
// 落在 26~32 的 HIG 区间，且文字子节点仍有可见高度。
func TestInputNaturalHeight(t *testing.T) {
	inp := widgets.Input(gxscene.Props{
		"value": func() any { return "Hello" },
		"width": "240",
	})
	root := inp.Root()
	// 找到 input 容器节点
	var inputNode *gxscene.Node
	var walk func(*gxscene.Node)
	walk = func(n *gxscene.Node) {
		if n == nil {
			return
		}
		if _, ok := n.Props["gx-input-id"]; ok {
			inputNode = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	if inputNode == nil {
		t.Fatal("未生成 input 节点")
	}
	cache := computeLayout(root, 800, 600, fixedMeasure)
	box := cache[inputNode]
	// HIG regular control = 28pt；允许 26~32 的实现浮动，但不能回到塌陷值。
	if box.H < 26 || box.H > 32 {
		t.Errorf("Input 高度 = %.1f，应在 26~32（macOS HIG 28pt）", box.H)
	}
}
