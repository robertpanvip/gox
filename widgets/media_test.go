package widgets

import (
	"testing"

	"github.com/robertpanvip/gox/runtime/canvas"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

func TestImgStructure(t *testing.T) {
	inst := Img(gxscene.Props{
		"src": "data:image/png;base64,xxx", "width": "120", "height": "80",
		"object-fit": "cover", "border-radius": "8",
	})
	root := inst.Root()
	n := findKind(root, "img")
	if n == nil {
		t.Fatal("应产出 Kind=img 节点")
	}
	if n.Props["src"] != "data:image/png;base64,xxx" {
		t.Errorf("src=%v", n.Props["src"])
	}
	if n.Props["object-fit"] != "cover" || n.Props["border-radius"] != "8" {
		t.Errorf("样式属性未透传: %v", n.Props)
	}
}

func TestCanvasStructure(t *testing.T) {
	ops := []canvas.Op{
		{Kind: canvas.OpRect, X: 0, Y: 0, W: 100, H: 50, Fill: "#ff0000"},
		{Kind: canvas.OpCircle, CX: 50, CY: 25, R: 20, Fill: "blue"},
	}
	inst := Canvas(gxscene.Props{
		"width": "200", "height": "100", "ops": ops,
	})
	root := inst.Root()
	n := findKind(root, "canvas")
	if n == nil {
		t.Fatal("应产出 Kind=canvas 节点")
	}
	raw, _ := n.Props[canvas.PropKey].(string)
	decoded := canvas.Decode(raw)
	if len(decoded) != 2 {
		t.Fatalf("canvas-ops 应有 2 条指令, got %d (%q)", len(decoded), raw)
	}
	if decoded[0].Kind != canvas.OpRect || decoded[1].Kind != canvas.OpCircle {
		t.Errorf("指令类型错误: %v", decoded)
	}
}

func TestCanvasFuncOps(t *testing.T) {
	inst := Canvas(gxscene.Props{
		"width": "100", "height": "100",
		"ops": func() []canvas.Op {
			return []canvas.Op{{Kind: canvas.OpLine, X1: 0, Y1: 0, X2: 10, Y2: 10, Stroke: "#000"}}
		},
	})
	n := findKind(inst.Root(), "canvas")
	if n == nil {
		t.Fatal("应产出 canvas 节点")
	}
	if got := canvas.Decode(n.Props[canvas.PropKey].(string)); len(got) != 1 || got[0].Kind != canvas.OpLine {
		t.Errorf("func() []Op 未生效: %v", got)
	}
}
