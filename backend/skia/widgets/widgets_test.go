package widgets

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// findKind 深度优先找第一个匹配 Kind 的节点。
func findKind(n *gxscene.Node, kind string) *gxscene.Node {
	if n == nil {
		return nil
	}
	if n.Kind == kind {
		return n
	}
	for _, c := range n.Children {
		if hit := findKind(c, kind); hit != nil {
			return hit
		}
	}
	return nil
}

func countKind(n *gxscene.Node, kind string) int {
	if n == nil {
		return 0
	}
	c := 0
	if n.Kind == kind {
		c++
	}
	for _, ch := range n.Children {
		c += countKind(ch, kind)
	}
	return c
}

func TestButtonStructure(t *testing.T) {
	btn := Button(gxscene.Props{
		"label":   "GO",
		"variant": "primary",
		"onClick": gxscene.Handler(func(gxscene.EventContext) {}),
	})
	root := btn.Root()
	n := findKind(root, "button")
	if n == nil {
		t.Fatal("未生成 button 节点")
	}
	if got := n.Props["bg"]; got != ThemeAccent {
		t.Errorf("primary 按钮底色 = %v, want %v", got, ThemeAccent)
	}
	if got := n.Props["border-radius"]; got != "8" {
		t.Errorf("圆角 = %v, want 8", got)
	}
	if n.Handler("onClick") == nil {
		t.Error("onClick handler 未绑定")
	}
	text := gxscene.TextBackend{}.Paint(root)
	if !containsStr(text, "GO") {
		t.Errorf("文本渲染未含 GO: %q", text)
	}
}

func TestCardWithChildren(t *testing.T) {
	inner := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpChildText, Arg: "BODY"},
		{Code: gxscene.OpClose},
	}, N: 0})
	card := Card(gxscene.Props{"title": "HEAD", "children": func() any { return inner }})
	text := gxscene.TextBackend{}.Paint(card.Root())
	if !containsStr(text, "HEAD") || !containsStr(text, "BODY") {
		t.Errorf("卡片应含标题与子内容: %q", text)
	}
}

func TestProgressClampAndWidth(t *testing.T) {
	for _, tc := range []struct {
		value int
		want  string
	}{{value: -5, want: "0"}, {value: 50, want: "150"}, {value: 200, want: "300"}} {
		p := Progress(gxscene.Props{"value": func() any { return tc.value }, "width": "300"})
		root := p.Root()
		// 进度条内层填充节点的 width 是动态属性，值为 trackW*value/100。
		var fillWidth any
		var walk func(*gxscene.Node)
		walk = func(n *gxscene.Node) {
			if n.Props != nil {
				if w, ok := n.Props["width"]; ok && n.Props["bg"] == toneColor("primary") {
					fillWidth = w
				}
			}
			for _, c := range n.Children {
				walk(c)
			}
		}
		walk(root)
		if fillWidth != tc.want {
			t.Errorf("value=%d 填充宽度 = %v, want %v", tc.value, fillWidth, tc.want)
		}
	}
}

// TestInputControlledEdit 验证受控输入闭环：InputChar/Backspace 计算新文本并经
// onInput handler 回写 signal，Props["value"] 与显示文本随之更新。
func TestInputControlledEdit(t *testing.T) {
	text := signal.New("Hi")
	inp := Input(gxscene.Props{
		"value":   func() any { return text() },
		"onInput": func() any { return BindInput(func(s string) { text(s) }) },
		"width":   "240",
	})
	root := inp.Root()
	box := findKind(root, "input")
	if box == nil {
		t.Fatal("未生成 input 节点")
	}
	if !IsInput(box) {
		t.Fatal("input 节点缺少焦点 token 标记")
	}
	if got := box.Props["value"]; got != "Hi" {
		t.Fatalf("初始 value = %v, want Hi", got)
	}

	if !InputChar(box, '!') {
		t.Fatal("InputChar 未处理")
	}
	if got := text(); got != "Hi!" {
		t.Errorf("输入后 signal = %q, want Hi!", got)
	}
	inp.Update()
	if got := box.Props["value"]; got != "Hi!" {
		t.Errorf("输入后 Props[value] = %v, want Hi!", got)
	}

	if !InputBackspace(box) {
		t.Fatal("InputBackspace 未处理")
	}
	if got := text(); got != "Hi" {
		t.Errorf("退格后 signal = %q, want Hi", got)
	}
}

// TestInputCharCJK 验证输入法上屏的中文 rune 经 InputChar 追加后按字节正确拼接、
// signal 回写正确，再逐 rune 退格能干净删掉整个汉字（不退成半个 UTF-8 字节）。
func TestInputCharCJK(t *testing.T) {
	text := signal.New("")
	inp := Input(gxscene.Props{
		"value":   func() any { return text() },
		"onInput": func() any { return BindInput(func(s string) { text(s) }) },
	})
	box := findKind(inp.Root(), "input")
	for _, r := range "中文" {
		if !InputChar(box, r) {
			t.Fatalf("InputChar(%q) 未处理", r)
		}
		// 真实 app 里 signal 写入即触发 effect 重绘、Props[value] 立即刷新；
		// 裸测试需手动 Update，否则下一次 InputChar 读到旧值。
		inp.Update()
	}
	if got := text(); got != "中文" {
		t.Fatalf("输入中文后 signal = %q, want 中文", got)
	}
	if got := box.Props["value"]; got != "中文" {
		t.Errorf("Props[value] = %v, want 中文", got)
	}
	// 逐 rune 退格：先删文、再删中，两次清空，不出现半个汉字。
	if !InputBackspace(box) || text() != "中" {
		t.Errorf("第一次退格后 = %q, want 中", text())
	}
	inp.Update()
	if !InputBackspace(box) || text() != "" {
		t.Errorf("第二次退格后 = %q, want 空", text())
	}
}

func TestInputBackspaceUnicode(t *testing.T) {
	text := signal.New("你")
	inp := Input(gxscene.Props{
		"value":   func() any { return text() },
		"onInput": func() any { return BindInput(func(s string) { text(s) }) },
	})
	box := findKind(inp.Root(), "input")
	if !InputBackspace(box) {
		t.Fatal("退格未处理")
	}
	if got := text(); got != "" {
		t.Errorf("中文退格后 = %q, want 空", got)
	}
	inp.Update() // 真实 app 里由响应式 effect 自动驱动；裸测试手动刷新 Props
	if InputBackspace(box) {
		t.Error("空文本退格应返回 false")
	}
}

func TestFocus(t *testing.T) {
	a := Input(gxscene.Props{"value": func() any { return "" }})
	b := Input(gxscene.Props{"value": func() any { return "" }})
	ba, bb := findKind(a.Root(), "input"), findKind(b.Root(), "input")
	sync := func() { a.Update(); b.Update() } // 模拟 app 聚合 effect 的 Update

	Focus(ba)
	sync()
	if !Focused() {
		t.Error("Focus 后应处于聚焦态")
	}
	if got := ba.Props["border"]; got != ThemeAccent {
		t.Errorf("聚焦边框 = %v, want %v", got, ThemeAccent)
	}
	Focus(bb)
	sync()
	if got := bb.Props["border"]; got != ThemeAccent {
		t.Errorf("切换后 b 边框 = %v, want %v", got, ThemeAccent)
	}
	if got := ba.Props["border"]; got != ThemeBorder {
		t.Errorf("切换后 a 边框 = %v, want %v（失焦）", got, ThemeBorder)
	}
	Blur()
	sync()
	if Focused() {
		t.Error("Blur 后不应处于聚焦态")
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
