package skia

import (
	"testing"

	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// textNode / elemNode 构造测试用场景树节点（不依赖编译器，直接构造协议数据）。
func textNode(text string, props map[string]any) *gxscene.Node {
	return &gxscene.Node{Type: gxscene.NodeText, Kind: "#text", Text: text, Props: props}
}

func elemNode(kind string, props map[string]any, children ...*gxscene.Node) *gxscene.Node {
	return &gxscene.Node{Type: gxscene.NodeDiv, Kind: kind, Props: props, Children: children}
}

func sampleTree() *gxscene.Node {
	return elemNode("div", map[string]any{"bg": "white", "width": 400},
		elemNode("h1", map[string]any{"color": "#e63946", "font-size": 28},
			textNode("Hello Skia", map[string]any{"color": "red", "font-size": 28}),
		),
		elemNode("span", nil,
			textNode("12345", map[string]any{"font-size": 16}),
		),
	)
}

// TestWindowImplementsContract：编译期断言已在 window.go；这里运行期验证接口可用。
func TestWindowImplementsContract(t *testing.T) {
	var w gxapp.Window = NewWindow()
	if w == nil {
		t.Fatal("NewWindow 应返回非 nil")
	}
}

// TestSkiaRendersSceneTree：真 Skia 端到端——Open→Draw 一棵场景树不崩，
// 且 DrawLog 证明每个节点都被分派绘制。这是"真 Skia 绑定可用"的回归测试。
func TestSkiaRendersSceneTree(t *testing.T) {
	w := NewWindow()
	cfg := gxapp.WindowConfig{Title: "test", Width: 400, Height: 200, Backend: "skia"}
	if err := w.Open(cfg); err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer w.Close()

	if err := w.Draw(sampleTree()); err != nil {
		t.Fatalf("Draw: %v", err)
	}
	log := w.DrawLog()
	if log.Find("rect", "div") < 1 {
		t.Errorf("应记录 div 背景矩形, log=%v", log)
	}
	if log.Find("text", "Hello Skia") != 1 {
		t.Errorf("应记录一次 'Hello Skia' 文本, log=%v", log)
	}
	if log.Find("text", "12345") != 1 {
		t.Errorf("应记录一次 '12345' 文本, log=%v", log)
	}
	if w.Frames() != 1 {
		t.Errorf("Frames=%d, want 1", w.Frames())
	}
}

// TestRepeatedDrawIncrementsFrames：验证重绘路径（响应式更新的底层驱动）。
func TestRepeatedDrawIncrementsFrames(t *testing.T) {
	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 100, Height: 100}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for i := 0; i < 3; i++ {
		if err := w.Draw(sampleTree()); err != nil {
			t.Fatal(err)
		}
	}
	if w.Frames() != 3 {
		t.Errorf("Frames=%d, want 3", w.Frames())
	}
}

// TestStyleParsing：样式解析（本后端 additive 约定）正确性。
func TestStyleParsing(t *testing.T) {
	cases := []struct {
		in     string
		want   uint32
		wantOK bool
	}{
		{"#ff0000", argb(255, 255, 0, 0), true},
		{"#f00", argb(255, 255, 0, 0), true},
		{"rgb(1, 2, 3)", argb(255, 1, 2, 3), true},
		{"BLUE", argb(255, 40, 90, 220), true},
		{"not-a-color", 0, false},
	}
	for _, c := range cases {
		got, ok := parseColor(c.in)
		if ok != c.wantOK || (ok && got != c.want) {
			t.Errorf("parseColor(%q) = %08x,%v; want %08x,%v", c.in, got, ok, c.want, c.wantOK)
		}
	}
	s := parseStyle(map[string]any{"font-size": "24px", "padding": 8, "width": "300px"})
	if s.fontSize != 24 || s.padding != (edges{8, 8, 8, 8}) || s.width != 300 {
		t.Errorf("parseStyle 数值解析失败: %+v", s)
	}
}

// TestDrawBeforeOpenFails：未 Open 就 Draw 应返回错误而非崩溃。
func TestDrawBeforeOpenFails(t *testing.T) {
	w := NewWindow()
	if err := w.Draw(sampleTree()); err == nil {
		t.Error("未 Open 就 Draw 应报错")
	}
}

// TestParseStylePaddingShorthand：padding 支持 1~4 值 CSS 简写，四边按
// [top,right,bottom,left] 落位；这也是 web 端按钮 "7 14" 挤压问题的桌面侧回归测试。
func TestParseStylePaddingShorthand(t *testing.T) {
	cases := []struct {
		in   any
		want edges
	}{
		{"7 14", edges{7, 14, 7, 14}},
		{"0 16 12 16", edges{0, 16, 12, 16}},
		{"1 2 3", edges{1, 2, 3, 2}},
		{10, edges{10, 10, 10, 10}},
		{"8px", edges{8, 8, 8, 8}},
	}
	for _, c := range cases {
		s := parseStyle(map[string]any{"padding": c.in})
		if s.padding != c.want {
			t.Errorf("padding %v = %v, want %v", c.in, s.padding, c.want)
		}
	}
}
