package skia

import (
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"testing"
)

// overflow 容器高 50，内含 4 个高度 30 的子节点（总 120）；scroll-top=30。
func overflowTree(scrollTop float32) *gxscene.Node {
	root := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"}, // viewport
		{Code: gxscene.OpAttr, Key: "width", Arg: "100"},
		{Code: gxscene.OpAttr, Key: "height", Arg: "50"},
		{Code: gxscene.OpAttr, Key: "overflow", Arg: "scroll"},
		{Code: gxscene.OpAttr, Key: "scroll-top", Arg: "30"},
	}})
	// 用 elemNode 方式构造更直观：直接手动建
	vp := elemNode("div", map[string]any{
		"width": 100, "height": 50, "overflow": "scroll", "scroll-top": scrollTop,
	})
	for i := 0; i < 4; i++ {
		vp.Children = append(vp.Children, elemNode("div", map[string]any{
			"width": 100, "height": 30, "bg": "#ffffff",
		}, textNode(itemLabel(i), nil)))
	}
	_ = root
	return vp
}

func itemLabel(i int) string { return []string{"A", "B", "C", "D"}[i] }

func TestScrollOffsetApplied(t *testing.T) {
	vp := overflowTree(30)
	cache := computeLayout(vp, 800, 600, fixedMeasure2)
	// 无滚动时第一个子节点 y=0（默认 padding 已归零）；滚动 30 后应上移到 ≈ -30
	first := cache[vp.Children[0]]
	if !near(first.Y, -30, 2) {
		t.Errorf("首个子节点 y=%.1f want ≈-30", first.Y)
	}
	// 裁剪矩形 = viewport 矩形 (0,0,100,50)
	if !first.HasClip || !near(first.ClipW, 100, 1) || !near(first.ClipH, 50, 1) {
		t.Errorf("裁剪矩形未传递: %+v", first)
	}
}

func TestScrollHitClip(t *testing.T) {
	vp := overflowTree(30)
	cache := computeLayout(vp, 800, 600, fixedMeasure2)
	first := cache[vp.Children[0]] // 滚出视口上方
	// 点落在第一子节点原位置 y=-30..0，但视口裁剪 0..50 → y=-10 应被裁掉
	if first.visibleContains(50, -10) {
		t.Error("视口上方的子节点不应被命中（已滚出裁剪区）")
	}
	// 第三个子节点（y≈34..64）在视口内的部分可命中
	third := cache[vp.Children[2]]
	if !third.visibleContains(50, 40) {
		t.Errorf("视口内的子节点应可命中, box=%+v", third)
	}
}

func TestNoOverflowNoClip(t *testing.T) {
	root := elemNode("div", map[string]any{"width": 100, "height": 50},
		elemNode("div", map[string]any{"width": 100, "height": 30}),
	)
	cache := computeLayout(root, 800, 600, fixedMeasure2)
	if cache[root.Children[0]].HasClip {
		t.Error("无 overflow 时不应有裁剪")
	}
}
