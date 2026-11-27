package skia

import (
	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	"testing"
)

func TestBoxShadowDrawOp(t *testing.T) {
	root := elemNode("div", map[string]any{"width": 200, "height": 100},
		elemNode("div", map[string]any{
			"width": 100, "height": 40, "bg": "#ffffff", "border-radius": 8,
			"box-shadow": "0 4px 12px 2px rgba(0,0,0,0.35)",
		}),
	)
	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 400, Height: 300}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Draw(root); err != nil {
		t.Fatal(err)
	}
	shadows := 0
	for _, op := range w.DrawLog() {
		if op.Kind == "shadow" {
			shadows++
		}
	}
	if shadows == 0 {
		t.Error("带 box-shadow 的元素应产生 shadow 绘制指令")
	}
	// 无 shadow 的元素不产生
	root2 := elemNode("div", map[string]any{"width": 100, "height": 40, "bg": "#ffffff"})
	w.Draw(root2)
	for _, op := range w.DrawLog() {
		if op.Kind == "shadow" {
			t.Error("无 box-shadow 不应产生 shadow 指令")
		}
	}
}
