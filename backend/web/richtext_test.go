package gxweb

import (
	"strings"
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// 富文本节点（NodeText + rich-text 属性）在 web 后端应建成真实 span 元素，
// 并用一次 innerHTML 写入着色片段，而不是拆成 N 个子 span 节点。
func TestRichTextBuildUsesInnerHTML(t *testing.T) {
	d := newMemDriver()
	w := NewWithDriver(d)
	_ = w.Open(gxapp.DefaultConfig())

	rt := gxscene.RichText{
		{Text: "func", Color: "#c586c0"},
		{Text: " main() ", Color: ""},
		{Text: "<x>", Color: "#ce9178"}, // 验证 HTML 转义
	}
	root := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpDynAttr, Key: gxscene.PropRichText, Slot: 0},
		{Code: gxscene.OpChildText, Arg: rt.Plain()},
		{Code: gxscene.OpClose},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return rt })
	if err := w.Draw(root.Root()); err != nil {
		t.Fatal(err)
	}

	// 找到 span 元素，断言 richHTML 一次写入且 < 被转义。
	var found *memElem
	for _, el := range d.elements {
		if el.kind == "span" && el.richHTML != "" {
			found = el
			break
		}
	}
	if found == nil {
		t.Fatalf("未找到写入 richHTML 的 span 元素: %+v", d.elements)
	}
	if !strings.Contains(found.richHTML, `color:#c586c0`) {
		t.Errorf("富文本应含内联颜色: %q", found.richHTML)
	}
	if !strings.Contains(found.richHTML, "&lt;x&gt;") {
		t.Errorf("尖括号应被 HTML 转义: %q", found.richHTML)
	}
	if strings.Contains(found.richHTML, "<x>") {
		t.Errorf("不应出现未转义的 <x>: %q", found.richHTML)
	}
}

// 增量路径：rich-text 属性变化走 ChangeSetAttr，应更新 innerHTML 且不误用 SetText 抹掉。
func TestRichTextIncrementalUpdate(t *testing.T) {
	d := newMemDriver()
	w := NewWithDriver(d)
	_ = w.Open(gxapp.DefaultConfig())

	rt1 := gxscene.RichText{{Text: "a", Color: "#ff0000"}}
	root := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpDynAttr, Key: gxscene.PropRichText, Slot: 0},
		{Code: gxscene.OpClose},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return rt1 })
	if err := w.Draw(root.Root()); err != nil {
		t.Fatal(err)
	}

	spanNode := root.Root().Children[0].Children[0]
	rt2 := gxscene.RichText{{Text: "b", Color: "#00ff00"}}
	changes := []gxscene.Change{{
		Kind:  gxscene.ChangeSetAttr,
		NodeID: spanNode.ID.String(),
		Key:   gxscene.PropRichText,
		Value: rt2,
	}}
	frame := &gxapp.Frame{Root: root.Root(), Changes: changes, Version: 2}
	if err := w.Present(frame); err != nil {
		t.Fatal(err)
	}

	var found *memElem
	for _, el := range d.elements {
		if el.kind == "span" {
			found = el
			break
		}
	}
	if found == nil || !strings.Contains(found.richHTML, "color:#00ff00") {
		t.Fatalf("增量更新后 richHTML 未刷新: %+v", found)
	}
}
