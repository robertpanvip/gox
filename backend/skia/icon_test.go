package skia

// 图标矢量绘制端到端：Icon 组件产出的 canvas-ops 被后端解释为矢量 path 并记录绘制。
// 验证「矢量」特性：同一图标不同 size 时，canvas-ops 里的坐标随尺寸缩放，
// 后端按这些绝对坐标绘制（放大不失真、零位图）。

import (
	"fmt"
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	"github.com/robertpanvip/gox/runtime/canvas"
	gxicon "github.com/robertpanvip/gox/runtime/icon"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/widgets"
)

func TestIconVectorDraw(t *testing.T) {
	inst := widgets.Icon(gxscene.Props{"name": "house", "size": 24, "color": "#0a84ff"})
	n := findKindSkia(inst.Root(), "canvas")
	if n == nil {
		t.Fatal("Icon 应产出 canvas 节点")
	}
	raw := n.Props[canvas.PropKey].(string)
	ops := canvas.Decode(raw)
	if len(ops) == 0 {
		t.Fatal("应有矢量指令")
	}

	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 100, Height: 100}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Draw(inst.Root()); err != nil {
		t.Fatal(err)
	}
	if w.DrawLog().Find("canvas", "canvas") != 1 {
		t.Fatalf("图标矢量指令应产生 1 条 canvas 绘制记录, log=%v", w.DrawLog())
	}
}

func TestIconVectorScalesWithSize(t *testing.T) {
	// 同一矢量图标，size 翻倍 → path 坐标翻倍（放大不失真）。
	small, err := gxicon.IconOps("plus", "", gxicon.ParseOptions{Size: 12, Color: "#fff", StrokeWidth: 1.8})
	if err != nil {
		t.Fatal(err)
	}
	large, err := gxicon.IconOps("plus", "", gxicon.ParseOptions{Size: 48, Color: "#fff", StrokeWidth: 1.8})
	if err != nil {
		t.Fatal(err)
	}
	if len(small) == 0 || len(large) == 0 || small[0].D == "" || large[0].D == "" {
		t.Fatal("应有 path 指令")
	}
	// size 12 → viewBox 24 缩放 0.5；size 48 → 缩放 2。坐标应相差 4 倍。
	// 用首条 M 后的第一个数做近似比较。
	firstNum := func(d string) float64 {
		var v float64
		for i := 1; i < len(d); i++ {
			if d[i] >= '0' && d[i] <= '9' || d[i] == '.' {
				_, _ = fmt.Sscanf(d[i:], "%f", &v)
				break
			}
		}
		return v
	}
	a, b := firstNum(small[0].D), firstNum(large[0].D)
	if a <= 0 || b <= 0 {
		t.Fatalf("无法提取坐标: %q %q", small[0].D, large[0].D)
	}
	ratio := b / a
	if ratio < 3.9 || ratio > 4.1 {
		t.Fatalf("矢量缩放比例应为 4（48/12），got %.2f (a=%v b=%v)", ratio, a, b)
	}
}

// findKindSkia 在场景树里按 Kind 找节点（skia 测试包内 helper）。
func findKindSkia(n *gxscene.Node, kind string) *gxscene.Node {
	if n == nil {
		return nil
	}
	if n.Kind == kind {
		return n
	}
	for _, c := range n.Children {
		if hit := findKindSkia(c, kind); hit != nil {
			return hit
		}
	}
	return nil
}
