package skia

import (
	"strings"
	"testing"

	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// gxTestConfig 构造测试窗口配置。
func gxTestConfig(w, h int) gxapp.WindowConfig {
	return gxapp.WindowConfig{Width: w, Height: h}
}

// firstTextOp 返回 DrawLog 里第一条名为 name 的 text 指令（Find 返回计数，这里要坐标）。
func firstTextOp(log DrawLog, name string) (DrawOp, bool) {
	for _, op := range log {
		if op.Kind == "text" && op.Name == name {
			return op, true
		}
	}
	return DrawOp{}, false
}

// 本文件验证桌面端补齐 Web 消费面的属性：font-weight 数值、text-align、
// line-height、white-space 折叠、z-index 绘制层级、pointer-events 命中豁免、
// cursor 透出。与 docs/STYLE.md 的三端消费矩阵保持一致。

func TestCssWeightBold(t *testing.T) {
	cases := map[string]bool{
		"bold": true, "BOLD": true, "bolder": true,
		"600": true, "700": true, "900": true, "600px": true,
		"normal": false, "400": false, "100": false, "lighter": false,
		"": false, "junk": false,
	}
	for in, want := range cases {
		if got := cssWeightBold(in); got != want {
			t.Errorf("cssWeightBold(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestWeightPropPrecedence(t *testing.T) {
	// font-weight 在场时优先于 weight：500（medium）非粗体。
	if weightIsBoldFromProps(map[string]any{"font-weight": "500", "weight": "bold"}) {
		t.Error("font-weight 在场时应优先于 weight（500 非粗体）")
	}
	if !weightIsBoldFromProps(map[string]any{"weight": "700"}) {
		t.Error("weight=700 应为粗体（数值语义）")
	}
	if weightIsBoldFromProps(map[string]any{"font-weight": "400"}) {
		t.Error("font-weight=400 应为常规")
	}
	if weightIsBoldFromProps(nil) {
		t.Error("无 props 不为粗体")
	}
}

func TestInheritedStyleTextParity(t *testing.T) {
	n := boxNode("div", map[string]any{
		"text-align": "center", "line-height": "1.5", "white-space": "normal",
		"font-weight": "600",
	}, txtNode("hi", nil))
	eff := inheritedStyle(n, defaultTextStyle())
	if eff.align != 1 {
		t.Errorf("text-align:center → align=1, got %d", eff.align)
	}
	if eff.lineHeightFactor != 1.5 {
		t.Errorf("line-height:1.5 → factor=1.5, got %.2f", eff.lineHeightFactor)
	}
	if !whiteSpaceCollapses(eff.whiteSpace) {
		t.Errorf("white-space:normal 应折叠空白, got %q", eff.whiteSpace)
	}
	if !eff.bold {
		t.Error("font-weight:600 应为粗体")
	}
	if h := eff.lineH(); !near(h, 24, 0.01) {
		t.Errorf("16px × 1.5 = 24, got %.2f", h)
	}
	// px 值：line-height:30px
	eff2 := inheritedStyle(boxNode("div", map[string]any{"line-height": "30px"}, txtNode("x", nil)), defaultTextStyle())
	if eff2.lineHeightPx != 30 {
		t.Errorf("line-height:30px → px=30, got %.2f", eff2.lineHeightPx)
	}
}

func TestCollapseWhitespace(t *testing.T) {
	if got := collapseWhitespace("  a \t b\nc  "); got != "a b c" {
		t.Errorf("collapseWhitespace = %q, want %q", got, "a b c")
	}
	if !whiteSpaceCollapses("") || whiteSpaceCollapses("pre") || whiteSpaceCollapses("pre-wrap") || !whiteSpaceCollapses("nowrap") {
		t.Error("CSS 语义：默认/normal/nowrap 折叠，pre/pre-wrap 保留（需原样空白的组件须显式声明 pre）")
	}
}

// TestTextAlignCenterPaint：盒宽 200、内容 40（fixedMeasure 4 字 × 10px），
// text-align:center 时落笔 x = 80。
func TestTextAlignCenterPaint(t *testing.T) {
	root := boxNode("div", map[string]any{"width": 200, "height": 40, "text-align": "center"},
		txtNode("abcd", nil))
	w := NewWindow()
	if err := w.Open(gxTestConfig(200, 40)); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Draw(root); err != nil {
		t.Fatal(err)
	}
	op, ok := firstTextOp(w.log, "abcd")
	if !ok {
		t.Fatal("未找到 text 绘制指令")
	}
	// 内容宽 40，居中偏移 (200-8-40)/2 = 76（根 padding 4 → box.X=4，落笔 x=80）。
	if !near(op.X, 80, 1.0) {
		t.Errorf("center 落笔 x = %.1f, want ≈80", op.X)
	}
}

// TestLineHeightMeasureHeight：line-height:2 时 Yoga 文本行盒高度 = 32。
func TestLineHeightMeasureHeight(t *testing.T) {
	root := boxNode("div", map[string]any{"line-height": "2"}, txtNode("abcd", nil))
	cache := computeLayout(root, 200, 100, fixedMeasure)
	tb := cache[root.Children[0]]
	if !near(tb.H, 32, 0.5) {
		t.Errorf("line-height:2 行盒高 = %.1f, want 32", tb.H)
	}
}

// TestWhiteSpaceCollapsePaint：white-space:normal 时连续空格折叠进绘制与测量。
func TestWhiteSpaceCollapsePaint(t *testing.T) {
	root := boxNode("div", map[string]any{"white-space": "normal"}, txtNode("a   b", nil))
	w := NewWindow()
	if err := w.Open(gxTestConfig(120, 40)); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Draw(root); err != nil {
		t.Fatal(err)
	}
	if _, ok := firstTextOp(w.log, "a b"); !ok {
		t.Fatal("white-space:normal 未折叠空格")
	}
	// 默认（未设置 white-space）与浏览器 CSS 语义一致：折叠空白。需要原样空白的
	// 组件（编辑器代码行、终端）必须显式声明 white-space="pre"。
	root2 := boxNode("div", nil, txtNode("a   b", nil))
	w2 := NewWindow()
	if err := w2.Open(gxTestConfig(120, 40)); err != nil {
		t.Fatal(err)
	}
	defer w2.Close()
	if err := w2.Draw(root2); err != nil {
		t.Fatal(err)
	}
	if _, ok := firstTextOp(w2.log, "a b"); !ok {
		t.Error("默认应折叠空格（CSS normal 语义）")
	}
	if _, ok := firstTextOp(w2.log, "a   b"); ok {
		t.Error("默认不应保留连续空格")
	}
	// pre 显式声明时逐字节保留（含换行；绘制端按 \n 拆行落笔，测量端按行计高）。
	root3 := boxNode("div", map[string]any{"white-space": "pre"}, txtNode("a\tb\ncd", nil))
	w3 := NewWindow()
	if err := w3.Open(gxTestConfig(120, 80)); err != nil {
		t.Fatal(err)
	}
	defer w3.Close()
	if err := w3.Draw(root3); err != nil {
		t.Fatal(err)
	}
	if _, ok := firstTextOp(w3.log, "a\tb\ncd"); !ok {
		t.Error("pre 应逐字节保留文本（含换行符）")
	}
	// 文本盒高度应为两行（测量端按行数 × 行高计高）。
	cache3 := computeLayout(root3, 120, 80, fixedMeasure)
	tb3 := cache3[root3.Children[0]]
	if !near(tb3.H, 2*defaultTextStyle().lineH(), 1.0) {
		t.Errorf("pre 两行文本盒高度 = %.1f, want ≈%.1f", tb3.H, 2*defaultTextStyle().lineH())
	}
}

// TestZIndexPaintOrder：z-index 重排绘制顺序（布局不动），负值垫底、正值置顶。
func TestZIndexPaintOrder(t *testing.T) {
	mk := func(name string, z any) *gxscene.Node {
		return boxNode(name, map[string]any{"width": 50, "height": 20, "bg": "#112233", "z-index": z})
	}
	root := boxNode("div", nil, mk("a", 5), mk("b", nil), mk("c", -1))
	w := NewWindow()
	if err := w.Open(gxTestConfig(120, 120)); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Draw(root); err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, op := range w.log {
		if op.Kind == "rect" {
			order = append(order, op.Name)
		}
	}
	// c(-1) 最先画，a(5) 最后画；b(0) 居中（与 a 同值组内保持文档序 b<a? 不——b=0 > a? a=5 > b=0）。
	want := []string{"c", "b", "a"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Errorf("z-index 绘制顺序 = %v, want %v", order, want)
	}
}

// TestPointerEventsNoneSkipsHitbox：pointer-events:none 的节点不登记命中框，
// 仍绘制；后代节点照常命中（CSS 命中语义）。
func TestPointerEventsNoneSkipsHitbox(t *testing.T) {
	inner := boxNode("inner", map[string]any{"width": 30, "height": 10})
	pe := boxNode("pe-none", map[string]any{"width": 60, "height": 20, "pointer-events": "none"}, inner)
	plain := boxNode("plain", map[string]any{"width": 30, "height": 10})
	root := boxNode("div", map[string]any{"width": 120, "height": 60}, pe, plain)
	w := NewWindow()
	if err := w.Open(gxTestConfig(120, 60)); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Draw(root); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, h := range w.Hits() {
		names = append(names, h.Node.Kind)
	}
	for _, n := range names {
		if n == "pe-none" {
			t.Error("pointer-events:none 节点不应出现在命中框")
		}
	}
	foundInner, foundPlain := false, false
	for _, n := range names {
		if n == "inner" {
			foundInner = true
		}
		if n == "plain" {
			foundPlain = true
		}
	}
	if !foundInner || !foundPlain {
		t.Errorf("后代与普通节点应照常命中: %v", names)
	}
}

// TestHitBoxCursorExposed：cursor 属性透出到 HitBox.Cursor。
func TestHitBoxCursorExposed(t *testing.T) {
	btn := boxNode("btn", map[string]any{"width": 40, "height": 20, "cursor": "pointer"})
	root := boxNode("div", map[string]any{"width": 80, "height": 40}, btn)
	w := NewWindow()
	if err := w.Open(gxTestConfig(80, 40)); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Draw(root); err != nil {
		t.Fatal(err)
	}
	for _, h := range w.Hits() {
		if h.Node.Kind == "btn" && h.Cursor != "pointer" {
			t.Errorf("btn 命中框 cursor = %q, want pointer", h.Cursor)
		}
	}
}
