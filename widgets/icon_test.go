package widgets

import (
	"strings"
	"testing"

	"github.com/robertpanvip/gox/runtime/canvas"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

func TestIconStructure_Builtin(t *testing.T) {
	inst := Icon(gxscene.Props{
		"name": "house", "size": 28, "tone": "blue",
	})
	n := findKind(inst.Root(), "canvas")
	if n == nil {
		t.Fatal("应产出 Kind=canvas 节点")
	}
	if n.Props["width"] != "28" || n.Props["height"] != "28" {
		t.Fatalf("尺寸未透传: %v", n.Props)
	}
	raw, ok := n.Props[canvas.PropKey].(string)
	if !ok || raw == "" || raw == "[]" {
		t.Fatalf("canvas-ops 应含矢量指令: %v", n.Props[canvas.PropKey])
	}
	ops := canvas.Decode(raw)
	if len(ops) == 0 {
		t.Fatal("解码后应有绘制指令")
	}
	// blue tone → 苹果蓝 #0a84ff，应注入到 stroke。
	found := false
	for _, op := range ops {
		if op.Stroke == AppleBlue || op.Fill == AppleBlue {
			found = true
		}
	}
	if !found {
		t.Fatalf("tone 色未注入: %+v", ops)
	}
}

func TestIconStructure_CustomColorAndStroke(t *testing.T) {
	inst := Icon(gxscene.Props{
		"name": "plus", "size": 20, "color": "#ffffff", "stroke-width": 3,
	})
	n := findKind(inst.Root(), "canvas")
	ops := canvas.Decode(n.Props[canvas.PropKey].(string))
	if len(ops) == 0 {
		t.Fatal("应有指令")
	}
	if ops[0].Stroke != "#ffffff" {
		t.Fatalf("color 未生效: %+v", ops[0])
	}
	// stroke-width=3 随 size=20 缩放：scale=20/24 → 2.5
	if ops[0].StrokeWidth < 2.4 || ops[0].StrokeWidth > 2.6 {
		t.Fatalf("stroke-width 缩放错误: %f", ops[0].StrokeWidth)
	}
}

func TestIconStructure_RawSVG(t *testing.T) {
	svg := `<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="8" fill="currentColor"/></svg>`
	inst := Icon(gxscene.Props{"src": svg, "size": 24, "color": "#ff0000"})
	n := findKind(inst.Root(), "canvas")
	ops := canvas.Decode(n.Props[canvas.PropKey].(string))
	if len(ops) != 1 || ops[0].Kind != canvas.OpCircle {
		t.Fatalf("应解析出 circle: %+v", ops)
	}
	if ops[0].Fill != "#ff0000" {
		t.Fatalf("currentColor 未替换: %+v", ops[0])
	}
}

func TestIconStructure_Alias(t *testing.T) {
	// "home" 是 "house" 的别名，应能解析出矢量指令。
	inst := Icon(gxscene.Props{"name": "home", "size": 20})
	n := findKind(inst.Root(), "canvas")
	raw := n.Props[canvas.PropKey].(string)
	if !strings.Contains(raw, "M") {
		t.Fatalf("别名图标应有 path 指令: %q", raw)
	}
}

func TestIconStructure_InvalidGraceful(t *testing.T) {
	// 未知 name 且无 src → 空图标，不崩溃。
	inst := Icon(gxscene.Props{"name": "does-not-exist", "size": 20})
	n := findKind(inst.Root(), "canvas")
	if n == nil {
		t.Fatal("仍应产出 canvas 节点")
	}
	if raw := n.Props[canvas.PropKey].(string); raw != "[]" {
		t.Fatalf("无效图标应降级为空: %q", raw)
	}
}
