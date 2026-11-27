package skia

// canvas 原语回归：验证 canvas-ops 指令被解释执行，DrawLog 出现 canvas 记录，
// 非法/空指令安全降级。

import (
	"testing"

	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	"github.com/robertpanvip/gox-core/runtime/canvas"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

func canvasTemplate(ops string) gxscene.Template {
	return gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "canvas"},
		{Code: gxscene.OpAttr, Key: "width", Arg: "200"},
		{Code: gxscene.OpAttr, Key: "height", Arg: "150"},
		{Code: gxscene.OpAttr, Key: canvas.PropKey, Arg: ops},
		{Code: gxscene.OpClose},
	}}
}

func TestCanvasDrawsOps(t *testing.T) {
	ops := canvas.Encode([]canvas.Op{
		{Kind: canvas.OpRect, X: 10, Y: 10, W: 80, H: 40, Fill: "#ff0000", Radius: 6},
		{Kind: canvas.OpCircle, CX: 100, CY: 80, R: 30, Fill: "blue"},
		{Kind: canvas.OpLine, X1: 0, Y1: 0, X2: 200, Y2: 150, Stroke: "#000", StrokeWidth: 2},
		{Kind: canvas.OpPath, D: "M0 0 L20 20 L40 0 Z", Fill: "green"},
	})
	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 300, Height: 250}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Draw(gxscene.Render(canvasTemplate(ops)).Root()); err != nil {
		t.Fatal(err)
	}
	log := w.DrawLog()
	if log.Find("canvas", "canvas") != 1 {
		t.Fatalf("应有 1 条 canvas 绘制记录, log=%v", log)
	}
}

func TestCanvasEmptyOpsDegrades(t *testing.T) {
	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 100, Height: 100}); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for _, ops := range []string{"", "not json", "[]"} {
		if err := w.Draw(gxscene.Render(canvasTemplate(ops)).Root()); err != nil {
			t.Fatalf("ops=%q: %v", ops, err)
		}
		if w.DrawLog().Find("canvas", "canvas") != 0 {
			t.Fatalf("ops=%q 空指令不应产生 canvas 记录", ops)
		}
	}
}
