package skia

import (
	"testing"

	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// TestEndToEndHostUnmodified：最关键的解耦验证——用 host 自己的 gxscene.Render
// 构建真实场景树，经 gxapp.OpenWith 注入 skia backend，app.Render 自动触发后端 Draw。
// 全程不修改任何 host 代码，证明"协议是唯一耦合面"。
func TestEndToEndHostUnmodified(t *testing.T) {
	// 用 host 的 opcode 构造一棵真实场景树：<div bg="white"><h1 color="red">Hi</h1></div>
	tpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpAttr, Key: "bg", Arg: "white"},
		{Code: gxscene.OpElem, Arg: "h1"},
		{Code: gxscene.OpAttr, Key: "color", Arg: "red"},
		{Code: gxscene.OpChildText, Arg: "Hi Skia"},
		{Code: gxscene.OpClose},
		{Code: gxscene.OpClose},
	}}

	w := NewWindow()
	app, err := gxapp.OpenWith(gxapp.WindowConfig{Title: "e2e", Width: 300, Height: 120}, w)
	if err != nil {
		t.Fatalf("OpenWith: %v", err)
	}
	defer app.Close()

	// build 返回 host 编译出的实例；app.Render 内部会自动 a.Draw() → w.Draw(root)。
	if err := app.Render(func() any { return gxscene.Render(tpl) }); err != nil {
		t.Fatalf("Render: %v", err)
	}

	// 首帧应已被后端绘制（Frames>=1），且 DrawLog 含真实节点。
	if w.Frames() < 1 {
		t.Fatalf("app.Render 应触发首帧 Draw，Frames=%d", w.Frames())
	}
	log := w.DrawLog()
	if log.Find("text", "Hi Skia") != 1 {
		t.Errorf("DrawLog 应含 'Hi Skia' 文本节点, log=%v", log)
	}
	// 后端配置里 Backend 被规范化为 skia。
	if w.Config().Backend != "skia" {
		t.Errorf("Backend=%q, want skia", w.Config().Backend)
	}
}

// TestManualDrawHostTree：直接把 host 实例的根交给后端绘制（不经 gxapp），
// 验证后端只依赖 gxscene.Node 协议数据这一最小耦合面。
func TestManualDrawHostTree(t *testing.T) {
	tpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpChildText, Arg: "manual"},
		{Code: gxscene.OpClose},
	}}
	inst := gxscene.Render(tpl)
	defer inst.Dispose()

	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 100, Height: 40}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Draw(inst.Root()); err != nil {
		t.Fatalf("Draw host tree: %v", err)
	}
	if w.DrawLog().Find("text", "manual") != 1 {
		t.Errorf("应绘制 host 树的文本节点, log=%v", w.DrawLog())
	}
}
