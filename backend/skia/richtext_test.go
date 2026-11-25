package skia

// Skia 富文本（语法高亮）回归：div 上挂 rich-text 时应一次分段绘制，
// 且纯文本子节点被跳过，避免单色文字覆盖正确着色。

import (
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

func TestRichTextOnDivSkipsPlainTextChild(t *testing.T) {
	rt := gxscene.RichText{
		{Text: "func", Color: "#c586c0"},
		{Text: " ", Color: "#d4d4d4"},
		{Text: "main", Color: "#dcdcaa"},
	}
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpDynAttr, Key: gxscene.PropRichText, Slot: 0},
		{Code: gxscene.OpAttr, Key: "font-size", Arg: "13"},
		{Code: gxscene.OpChildText, Arg: rt.Plain()}, // 纯文本子节点：应被跳过
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return rt })
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
		t.Fatalf("应有 1 条 rich-text 绘制记录, log=%v", log)
	}
	if got := log.Find("text", "func main"); got != 0 {
		t.Fatalf("纯文本子节点应被跳过（否则单色覆盖着色）, got=%d log=%v", got, log)
	}
}

func TestRichTextEmptyDoesNotSkipChild(t *testing.T) {
	// 富文本为空时不应跳过文本子节点，否则普通文本行会消失。
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpDynAttr, Key: gxscene.PropRichText, Slot: 0},
		{Code: gxscene.OpAttr, Key: "font-size", Arg: "13"},
		{Code: gxscene.OpChildText, Arg: "plain"},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return gxscene.RichText{} })
	defer inst.Dispose()

	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 200, Height: 40}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Draw(inst.Root()); err != nil {
		t.Fatalf("Draw: %v", err)
	}
	if got := w.DrawLog().Find("text", "plain"); got != 1 {
		t.Fatalf("空富文本时纯文本子节点应正常绘制, got=%d log=%v", got, w.DrawLog())
	}
}
