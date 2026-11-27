package skia

import (
	"testing"

	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// 构造一个用于 Present 的 Frame；Changes/Version/FullSync 由各用例指定。
func presentFrame(root *gxscene.Node, ver uint64, changes ...gxscene.Change) *gxapp.Frame {
	return &gxapp.Frame{Root: root, Version: ver, Changes: changes}
}

// TestPresentNoChangeFastReuse：首帧 Present 全量绘制，第二帧无 Change 再 Present
// 应走 O(1) 快速复用——paints 不增、reuses 增，且 log/hits 完整返回。
func TestPresentNoChangeFastReuse(t *testing.T) {
	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 400, Height: 200}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	root := sampleTree()

	if err := w.Present(presentFrame(root, 1)); err != nil {
		t.Fatal(err)
	}
	firstLog := append(DrawLog(nil), w.DrawLog()...)
	r1, p1 := w.cache.CacheStats()
	if p1 != 1 || r1 != 0 {
		t.Fatalf("首帧应 paints=1 reuses=0, 实际 paints=%d reuses=%d", p1, r1)
	}

	if err := w.Present(presentFrame(root, 2)); err != nil {
		t.Fatal(err)
	}
	r2, p2 := w.cache.CacheStats()
	if p2 != 1 || r2 != 1 {
		t.Fatalf("无 Change 应快速复用: paints 保持 1、reuses 增 1, 实际 paints=%d reuses=%d", p2, r2)
	}
	if len(w.DrawLog()) != len(firstLog) {
		t.Fatalf("复用帧 log 应完整: got %d want %d", len(w.DrawLog()), len(firstLog))
	}
	if len(w.Hits()) == 0 {
		t.Fatal("复用帧命中盒不应为空")
	}
}

// TestPresentChangeForcesRedraw：带 ChangeSetText 的帧必须重绘，且反映新文本。
func TestPresentChangeForcesRedraw(t *testing.T) {
	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 800, Height: 600}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	root := sampleTree()
	if err := w.Present(presentFrame(root, 1)); err != nil {
		t.Fatal(err)
	}

	var target *gxscene.Node
	var walk func(*gxscene.Node)
	walk = func(n *gxscene.Node) {
		if n.Type == gxscene.NodeText && n.Text == "12345" {
			target = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	if target == nil {
		t.Fatal("未找到目标文本节点")
	}
	target.Text = "1234567890"
	ch := gxscene.Change{Kind: gxscene.ChangeSetText, NodeID: "x", Value: "1234567890"}

	if err := w.Present(presentFrame(root, 2, ch)); err != nil {
		t.Fatal(err)
	}
	r, p := w.cache.CacheStats()
	if p != 2 || r != 0 {
		t.Fatalf("有 Change 应重绘: paints=2 reuses=0, 实际 paints=%d reuses=%d", p, r)
	}
	if w.DrawLog().Find("text", "1234567890") != 1 {
		t.Fatalf("重绘后 log 应含新文本, log=%v", w.DrawLog())
	}
}

// TestPresentFullSyncForcesRedraw：FullSync 即使无 Change 也必须全量重绘。
func TestPresentFullSyncForcesRedraw(t *testing.T) {
	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 400, Height: 200}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	root := sampleTree()
	if err := w.Present(presentFrame(root, 1)); err != nil {
		t.Fatal(err)
	}

	f := presentFrame(root, 2)
	f.FullSync = true
	if err := w.Present(f); err != nil {
		t.Fatal(err)
	}
	r, p := w.cache.CacheStats()
	if p != 2 || r != 0 {
		t.Fatalf("FullSync 应全量重绘: paints=2 reuses=0, 实际 paints=%d reuses=%d", p, r)
	}
}

// TestPresentAnimationBlocksFastReuse：动画期间无 Change 也不能快速复用。
func TestPresentAnimationBlocksFastReuse(t *testing.T) {
	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 400, Height: 200}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	// 用 Render 构造带稳定 NodeID 的节点：Animator 只对 ID != NoNode 的节点登记动画。
	n := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpAttr, Key: "transition", Arg: "width 1s linear"},
		{Code: gxscene.OpAttr, Key: "width", Arg: "100"},
		{Code: gxscene.OpClose},
	}})
	root := n.Root().Children[0]

	if err := w.Present(presentFrame(root, 1)); err != nil {
		t.Fatal(err)
	}
	// 制造一个进行中的动画：改目标属性值并 Present（带 Change）触发动画登记。
	root.Props["width"] = "200"
	ch := gxscene.Change{Kind: gxscene.ChangeSetAttr, NodeID: "x", Key: "width", Value: "200"}
	if err := w.Present(presentFrame(root, 2, ch)); err != nil {
		t.Fatal(err)
	}
	if !w.anim.Active() {
		t.Fatal("改动画目标后 Animator 应处于活跃状态")
	}
	rBefore, pBefore := w.cache.CacheStats()
	// 动画活跃时，无 Change 的下一帧不得走"快速复用"——它必须经过 renderCached。
	if err := w.Present(presentFrame(root, 3)); err != nil {
		t.Fatal(err)
	}
	rAfter, pAfter := w.cache.CacheStats()
	// 快速复用只增 reuses 不增 paints；动画期这帧必须增 paints、不增 reuses。
	if pAfter <= pBefore || rAfter != rBefore {
		t.Fatalf("动画期间应重绘而非快速复用: paints %d→%d reuses %d→%d",
			pBefore, pAfter, rBefore, rAfter)
	}
}

// TestDrawPathUnchanged：老 Draw 路径不受 Present 改造影响，连续两帧仍复用。
func TestDrawPathUnchanged(t *testing.T) {
	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 400, Height: 200}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	root := sampleTree()
	if err := w.Draw(root); err != nil {
		t.Fatal(err)
	}
	if err := w.Draw(root); err != nil {
		t.Fatal(err)
	}
	r, p := w.cache.CacheStats()
	if p != 1 || r != 1 {
		t.Fatalf("Draw 老路径应 paints=1 reuses=1, 实际 paints=%d reuses=%d", p, r)
	}
}
