package skia

// Skia 端 CSS 渐变语法高亮回归：编辑器用 linear-gradient 编码逐 token 颜色，
// Skia 把同一条渐变解析回 RichText 分段绘制，与 Web 的 background-clip:text 同源。

import (
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

func TestParseGradientCh(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"0ch", 0, true},
		{"7ch", 7, true},
		{" 12ch ", 12, true},
		{"abc", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		got, ok := parseCh(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("parseCh(%q) = (%d,%v), want (%d,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestParseGradientStops(t *testing.T) {
	s := "linear-gradient(90deg, #c586c0 0ch 7ch, #d4d4d4 7ch 8ch, #ce9178 8ch 12ch)"
	stops := parseGradient(s)
	if len(stops) != 3 {
		t.Fatalf("want 3 stops, got %d: %v", len(stops), stops)
	}
	if stops[0].color != "#c586c0" || stops[0].startCh != 0 || stops[0].endCh != 7 {
		t.Errorf("stop0 = %+v", stops[0])
	}
	if stops[2].color != "#ce9178" || stops[2].startCh != 8 || stops[2].endCh != 12 {
		t.Errorf("stop2 = %+v", stops[2])
	}
	// 非渐变串 / 空串应返回 nil。
	if parseGradient("#d4d4d4") != nil || parseGradient("") != nil {
		t.Error("非渐变串应返回 nil")
	}
}

func TestGradientToRichASCII(t *testing.T) {
	// "func main" = func(4) + space(1) + main(4) → 三段色，ch 边界 0/4/5/9。
	grad := "linear-gradient(90deg, #c586c0 0ch 4ch, #d4d4d4 4ch 5ch, #dcdcaa 5ch 9ch)"
	rt := gradientToRich(grad, "func main")
	if len(rt) != 3 {
		t.Fatalf("want 3 segments, got %d: %v", len(rt), rt)
	}
	if rt[0].Text != "func" || rt[0].Color != "#c586c0" {
		t.Errorf("seg0 = %+v", rt[0])
	}
	if rt[1].Text != " " || rt[1].Color != "#d4d4d4" {
		t.Errorf("seg1 = %+v", rt[1])
	}
	if rt[2].Text != "main" || rt[2].Color != "#dcdcaa" {
		t.Errorf("seg2 = %+v", rt[2])
	}
	if rt.Plain() != "func main" {
		t.Errorf("Plain() = %q", rt.Plain())
	}
}

func TestGradientToRichCJK(t *testing.T) {
	// CJK 字符占 2 格（与 buildGradient 的 runeWidth 一致）：
	// "中a" = 中(2ch) + a(1ch)，色段 0-2 与 2-3。
	grad := "linear-gradient(90deg, #569cd6 0ch 2ch, #d4d4d4 2ch 3ch)"
	rt := gradientToRich(grad, "中a")
	if len(rt) != 2 {
		t.Fatalf("want 2 segments, got %d: %v", len(rt), rt)
	}
	if rt[0].Text != "中" || rt[0].Color != "#569cd6" {
		t.Errorf("CJK seg0 = %+v", rt[0])
	}
	if rt[1].Text != "a" || rt[1].Color != "#d4d4d4" {
		t.Errorf("CJK seg1 = %+v", rt[1])
	}
}

func TestGradientCacheConsistency(t *testing.T) {
	grad := "linear-gradient(90deg, #c586c0 0ch 4ch, #d4d4d4 4ch 5ch)"
	a := cachedGradientRich(grad, "func ")
	b := cachedGradientRich(grad, "func ")
	if a.Plain() != b.Plain() || len(a) != len(b) {
		t.Fatalf("缓存结果不一致: %v vs %v", a, b)
	}
}

func TestGradientTextSkipsPlainTextChild(t *testing.T) {
	// 集成：带 gradient-text 标记的节点 + 渐变 bg + 纯文本子节点
	// → 走 rich-text 彩色绘制，纯文本子节点被跳过（编辑器代码行）。
	grad := "linear-gradient(90deg, #c586c0 0ch 4ch, #dcdcaa 4ch 8ch)"
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpDynAttr, Key: "bg", Slot: 0},
		{Code: gxscene.OpAttr, Key: "gradient-text", Arg: "1"},
		{Code: gxscene.OpAttr, Key: "font-size", Arg: "13"},
		{Code: gxscene.OpAttr, Key: "color", Arg: "#d4d4d4"},
		{Code: gxscene.OpChildText, Arg: "func main"}, // 纯文本子节点：应被跳过
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return grad })
	defer inst.Dispose()

	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 200, Height: 40}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Draw(inst.Root()); err != nil {
		t.Fatalf("Draw: %v", err)
	}
	log := w.DrawLog()
	if got := log.Find("rich-text", "func main"); got != 1 {
		t.Fatalf("应有 1 条渐变着色的 rich-text 记录, log=%v", log)
	}
	if got := log.Find("text", "func main"); got != 0 {
		t.Fatalf("纯文本子节点应被跳过, got=%d log=%v", got, log)
	}
}

func TestGradientBackgroundOnPlainDiv(t *testing.T) {
	// 通用能力：普通 div 的 bg=linear-gradient 画成渐变背景，文本子节点正常绘制。
	grad := "linear-gradient(90deg, #ff0000 0%, #0000ff 100%)"
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynAttr, Key: "bg", Slot: 0},
		{Code: gxscene.OpAttr, Key: "width", Arg: "100"},
		{Code: gxscene.OpAttr, Key: "height", Arg: "30"},
		{Code: gxscene.OpChildText, Arg: "hello"}, // 普通文本：应正常绘制
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return grad })
	defer inst.Dispose()

	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 200, Height: 60}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Draw(inst.Root()); err != nil {
		t.Fatalf("Draw: %v", err)
	}
	log := w.DrawLog()
	if got := log.Find("gradient-bg", ""); got < 1 {
		t.Fatalf("应有渐变背景绘制记录, log=%v", log)
	}
	if got := log.Find("text", "hello"); got != 1 {
		t.Fatalf("普通 div 的文本子节点应正常绘制, got=%d log=%v", got, log)
	}
}

func TestParseLinearGradientGeneral(t *testing.T) {
	g := parseLinearGradient("linear-gradient(90deg, #ff0000 0%, #0000ff 100%)")
	if g == nil || g.angle != 90 || len(g.stops) != 2 {
		t.Fatalf("解析失败: %+v", g)
	}
	if g.stops[0].color != "#ff0000" || g.stops[1].color != "#0000ff" {
		t.Errorf("色标错误: %+v", g.stops)
	}
	// 平滑插值中点应为紫红色系（R≈B≈128）。
	mid := sampleGradient(g, 0.5, 100)
	r, b := mid>>16&0xff, mid&0xff
	if r < 110 || r > 145 || b < 110 || b > 145 {
		t.Errorf("中点插值应接近 #800080, got #%08x", mid)
	}
	// 方向关键字。
	if g2 := parseLinearGradient("linear-gradient(to right, red, blue)"); g2 == nil || g2.angle != 90 {
		t.Errorf("to right 应解析为 90deg, got %+v", g2)
	}
}

func TestGradientShaderColorsHardStops(t *testing.T) {
	// 双位置硬色块：red[0,6ch) + blue[6ch,12ch)，chTotal=12。
	// 期望展开成 [red@0, red@0.5, blue@0.5, blue@1]，边界 0.5 处硬跳变。
	g := parseLinearGradient("linear-gradient(90deg, #ff0000 0ch 6ch, #0000ff 6ch 12ch)")
	if g == nil {
		t.Fatal("parseLinearGradient 返回 nil")
	}
	colors, positions := gradientShaderColors(g, 0, 12)
	if len(colors) != 4 || len(positions) != 4 {
		t.Fatalf("期望 4 个色标, got colors=%d positions=%d", len(colors), len(positions))
	}
	wantPos := []float32{0, 0.5, 0.5, 1}
	for i := range wantPos {
		if positions[i] != wantPos[i] {
			t.Errorf("positions[%d]=%v want %v", i, positions[i], wantPos[i])
		}
	}
	// 前两个红色（R 高 B 低），后两个蓝色（B 高 R 低）。
	argbBytes := func(c uint32) (r, b uint32) { return c >> 16 & 0xff, c & 0xff }
	for i := 0; i < 2; i++ {
		r, b := argbBytes(uint32(colors[i]))
		if r < 200 || b > 50 {
			t.Errorf("colors[%d] 应为红色, got R=%d B=%d", i, r, b)
		}
	}
	for i := 2; i < 4; i++ {
		r, b := argbBytes(uint32(colors[i]))
		if b < 200 || r > 50 {
			t.Errorf("colors[%d] 应为蓝色, got R=%d B=%d", i, r, b)
		}
	}
}

func TestGradientShaderColorsSmoothStops(t *testing.T) {
	// 单位置色标：red@0% + blue@100%，平滑渐变，锚定首尾。
	g := parseLinearGradient("linear-gradient(90deg, #ff0000 0%, #0000ff 100%)")
	if g == nil {
		t.Fatal("parseLinearGradient 返回 nil")
	}
	colors, positions := gradientShaderColors(g, 100, 0)
	if len(colors) != 2 || len(positions) != 2 {
		t.Fatalf("期望 2 个色标, got %d/%d", len(colors), len(positions))
	}
	if positions[0] != 0 || positions[1] != 1 {
		t.Errorf("positions=%v want [0 1]", positions)
	}
}
