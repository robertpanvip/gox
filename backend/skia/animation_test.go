package skia

import (
	"testing"
	"time"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// TestApplyAnimationsBlink 验证声明式 blink 动画按帧时钟把 opacity 采样写进
// 临时 props，restore 后不污染场景树。
func TestApplyAnimationsBlink(t *testing.T) {
	n := &gxscene.Node{Type: gxscene.NodeDiv, Kind: "div", ID: 42,
		Props: map[string]any{"animation": "blink 1000ms infinite"}}

	root := &gxscene.Node{Type: gxscene.NodeDiv, Kind: "div"}
	root.Children = []*gxscene.Node{n}

	// 固定相位：UnixMilli 落在周期前半 → opacity=1；后半 → 0。
	// 用 t=1000ms（周期起点）与 t=1500ms（半周期）验证。
	t0 := time.UnixMilli(1000)
	restore := applyAnimations(root, t0)
	if got := n.Props[propOpacity]; got != float32(1) {
		t.Errorf("相位起点 opacity=%v, want 1", got)
	}
	restore()
	if _, ok := n.Props[propOpacity]; ok {
		t.Error("restore 后不应残留临时 opacity prop")
	}

	t1 := time.UnixMilli(1500)
	restore = applyAnimations(root, t1)
	if got := n.Props[propOpacity]; got != float32(0) {
		t.Errorf("半周期 opacity=%v, want 0", got)
	}
	restore()
}

// TestTreeHasAnimation 验证活跃动画检测（供桌面主循环持续重绘）。
func TestTreeHasAnimation(t *testing.T) {
	root := &gxscene.Node{Type: gxscene.NodeDiv, Kind: "div"}
	if treeHasAnimation(root) {
		t.Error("空树不应有动画")
	}
	child := &gxscene.Node{Type: gxscene.NodeDiv, Kind: "div",
		Props: map[string]any{"animation": "blink 1.06s infinite"}}
	root.Children = []*gxscene.Node{child}
	if !treeHasAnimation(root) {
		t.Error("应检测到子树的 blink 动画")
	}
}
