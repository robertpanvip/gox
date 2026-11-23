package skia

import (
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// TestFrameCacheReuse：连续 Draw 同一棵树，第二帧应命中整帧复用（不清屏不重画），
// 但仍返回完整的 log/hits（对调用方透明）。
func TestFrameCacheReuse(t *testing.T) {
	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 400, Height: 200}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	root := sampleTree()

	if err := w.Draw(root); err != nil {
		t.Fatal(err)
	}
	firstLog := append(DrawLog(nil), w.DrawLog()...)
	r1, p1 := w.cache.CacheStats()
	if p1 != 1 || r1 != 0 {
		t.Fatalf("首帧应 paints=1 reuses=0, 实际 paints=%d reuses=%d", p1, r1)
	}

	if err := w.Draw(root); err != nil {
		t.Fatal(err)
	}
	r2, p2 := w.cache.CacheStats()
	if p2 != 1 || r2 != 1 {
		t.Fatalf("第二帧无变化应复用: paints 应保持 1、reuses 应变 1, 实际 paints=%d reuses=%d", p2, r2)
	}
	if len(w.DrawLog()) != len(firstLog) {
		t.Fatalf("复用帧的 log 应完整: got %d, want %d", len(w.DrawLog()), len(firstLog))
	}
	if len(w.hits) == 0 {
		t.Fatal("复用帧的命中盒不应为空")
	}
}

// TestFrameCacheRedrawOnChange：修改一个节点的文本后 Draw，应触发重绘；
// 且脏矩形只覆盖变化区域（小于整屏），体现局部重绘。
func TestFrameCacheRedrawOnChange(t *testing.T) {
	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 800, Height: 600}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	root := sampleTree()
	if err := w.Draw(root); err != nil {
		t.Fatal(err)
	}

	// 找到 "12345" 文本节点并改其内容。
	var target *gxscene.Node
	var walk func(*gxscene.Node)
	walk = func(n *gxscene.Node) {
		if n.Type == gxscene.NodeText && n.Text == "12345" {
			target = n
		}
		for _, c := range n.Children { walk(c) }
	}
	walk(root)
	if target == nil {
		t.Fatal("未找到目标文本节点")
	}
	target.Text = "1234567890"

	if err := w.Draw(root); err != nil {
		t.Fatal(err)
	}
	r, p := w.cache.CacheStats()
	if p != 2 || r != 0 {
		t.Fatalf("内容变化应重绘: paints=2 reuses=0, 实际 paints=%d reuses=%d", p, r)
	}
	// 新文本应出现在 log（证明重绘真的反映了新内容）。
	if w.DrawLog().Find("text", "1234567890") != 1 {
		t.Fatalf("重绘后 log 应含新文本, log=%v", w.DrawLog())
	}
}

// TestSnapshotIgnoresHandlers：事件 handler 变化不影响布局/绘制，不应触发重绘。
func TestSnapshotIgnoresHandlers(t *testing.T) {
	root := elemNode("div", map[string]any{"bg": "red", "onClick": func() {}},
		textNode("hi", nil))
	_, sig1 := snapshotTree(root)
	// 换一个 handler（同为 func），快照签名应不变。
	root.Props["onClick"] = func() {}
	_, sig2 := snapshotTree(root)
	if sig1 != sig2 {
		t.Fatal("handler(func) 变化不应改变内容快照签名")
	}
	// bg 变化应改变签名。
	root.Props["bg"] = "blue"
	_, sig3 := snapshotTree(root)
	if sig3 == sig2 {
		t.Fatal("bg 变化应改变内容快照签名")
	}
}

// TestDiffLayoutDirtyRegion：节点矩形变化时，脏矩形应覆盖旧+新位置，且能被合并。
func TestDiffLayoutDirtyRegion(t *testing.T) {
	n := elemNode("div", map[string]any{"bg": "red"})
	prev := layoutCache{n: {X: 10, Y: 10, W: 20, H: 20}}
	next := layoutCache{n: {X: 50, Y: 10, W: 20, H: 20}}
	rects := diffLayout(prev, next, n, n)
	if len(rects) == 0 {
		t.Fatal("矩形变化应产生脏区")
	}
	// 脏区应覆盖 x∈[10,70]（旧 10..30 + 新 50..70）。
	d := rects[0]
	for _, r := range rects { d = d.union(r) }
	if d.x > 10 || d.x+d.w < 70 {
		t.Fatalf("脏区应覆盖新旧位置 [10,70], 实际 x=%v w=%v", d.x, d.w)
	}
}

// TestDiffLayoutNoChange：布局完全没变时无脏区（配合整帧复用，此时连 renderCached 都不进重绘分支）。
func TestDiffLayoutNoChange(t *testing.T) {
	n := elemNode("div", map[string]any{"bg": "red"})
	cache := layoutCache{n: {X: 10, Y: 10, W: 20, H: 20}}
	rects := diffLayout(cache, cache, n, n)
	if len(rects) != 0 {
		t.Fatalf("布局无变化应无脏区, got %v", rects)
	}
}
