package gxscene

import (
	"strings"
	"testing"

	"github.com/robertpanvip/gox/runtime/canvas"
)

// refCanvas 构造一个 Kind="canvas" 的节点，ref 回调里接住 *Element。
func refCanvas(t *testing.T) (*Instance, *Element) {
	t.Helper()
	var el *Element
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "canvas"},
		{Code: OpRef, Slot: 0},
		{Code: OpClose},
	}, N: 1}
	inst := Render(tpl, func() any { return func(e *Element) { el = e } })
	if el == nil {
		t.Fatal("ref 未拿到 Element")
	}
	return inst, el
}

func TestCanvasContextFillRect(t *testing.T) {
	inst, el := refCanvas(t)
	defer inst.Dispose()

	ctx := el.GetContext()
	if ctx == nil {
		t.Fatal("canvas 节点应拿到上下文")
	}
	if el.GetContext() != ctx {
		t.Fatal("同一节点 GetContext 应返回缓存的同一上下文")
	}

	ctx.SetFillStyle("#ff0000")
	ctx.FillRect(10, 20, 30, 40)

	raw, _ := el.node.Props[canvas.PropKey].(string)
	ops := canvas.Decode(raw)
	if len(ops) != 1 {
		t.Fatalf("应有 1 条指令，got %d (%s)", len(ops), raw)
	}
	if ops[0].Kind != canvas.OpRect || ops[0].X != 10 || ops[0].Y != 20 || ops[0].W != 30 || ops[0].H != 40 || ops[0].Fill != "#ff0000" {
		t.Fatalf("fillRect 指令不符: %+v", ops[0])
	}
}

func TestCanvasContextPathAndClear(t *testing.T) {
	inst, el := refCanvas(t)
	defer inst.Dispose()
	ctx := el.GetContext()

	ctx.SetStrokeStyle("blue")
	ctx.SetLineWidth(2)
	ctx.BeginPath()
	ctx.MoveTo(0, 0)
	ctx.LineTo(10, 10)
	ctx.ClosePath()
	ctx.Stroke()

	ops := canvas.Decode(el.node.Props[canvas.PropKey].(string))
	if len(ops) != 1 || ops[0].Kind != canvas.OpPath {
		t.Fatalf("应有 1 条 path 指令，got %+v", ops)
	}
	if !strings.Contains(ops[0].D, "M0 0") || !strings.Contains(ops[0].D, "L10 10") || !strings.Contains(ops[0].D, "Z") {
		t.Fatalf("path D 不符: %q", ops[0].D)
	}
	if ops[0].Stroke != "blue" || ops[0].StrokeWidth != 2 {
		t.Fatalf("path 描边样式不符: %+v", ops[0])
	}

	ctx.ClearRect(0, 0, 100, 100)
	if got := canvas.Decode(el.node.Props[canvas.PropKey].(string)); len(got) != 0 {
		t.Fatalf("clearRect 应清空指令，got %d", len(got))
	}
}

func TestCanvasContextArc(t *testing.T) {
	inst, el := refCanvas(t)
	defer inst.Dispose()
	ctx := el.GetContext()

	ctx.BeginPath()
	ctx.Arc(50, 50, 20, 0, 3.14159) // 半圆 → 2 段贝塞尔
	ctx.Stroke()

	ops := canvas.Decode(el.node.Props[canvas.PropKey].(string))
	if len(ops) != 1 || ops[0].Kind != canvas.OpPath {
		t.Fatalf("arc 应产出 path 指令，got %+v", ops)
	}
	if !strings.Contains(ops[0].D, "M") || !strings.Contains(ops[0].D, "C") {
		t.Fatalf("arc 应以贝塞尔近似，got %q", ops[0].D)
	}
}

func TestCanvasContextNonCanvasAndDead(t *testing.T) {
	// 非 canvas 节点返回 nil。
	var el *Element
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpRef, Slot: 0},
		{Code: OpClose},
	}, N: 1}
	inst := Render(tpl, func() any { return func(e *Element) { el = e } })
	if el.GetContext() != nil {
		t.Fatal("非 canvas 节点 GetContext 应为 nil")
	}
	inst.Dispose()
	if el.GetContext() != nil {
		t.Fatal("销毁后 GetContext 应为 nil")
	}
}

// TestRAFQueue 验证 RAF 队列语义：登记顺序、take 清空、hasRAF。
func TestRAFQueue(t *testing.T) {
	inst, _ := refCanvas(t)
	defer inst.Dispose()

	var order []int
	inst.RequestAnimationFrame(func() { order = append(order, 1) })
	inst.RequestAnimationFrame(func() { order = append(order, 2) })
	if !inst.HasRAF() {
		t.Fatal("登记后 HasRAF 应为 true")
	}
	fns := inst.TakeRAF()
	if len(fns) != 2 {
		t.Fatalf("应有 2 个 RAF 回调，got %d", len(fns))
	}
	for _, fn := range fns {
		fn()
	}
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("RAF 应按登记顺序执行，got %v", order)
	}
	if inst.HasRAF() {
		t.Fatal("TakeRAF 后 HasRAF 应为 false")
	}
}

// TestElementRAF 验证 el.requestAnimationFrame 入队并写入同一 sink。
func TestElementRAF(t *testing.T) {
	inst, el := refCanvas(t)
	defer inst.Dispose()

	called := false
	el.RequestAnimationFrame(func() { called = true })
	if !inst.HasRAF() {
		t.Fatal("el.RequestAnimationFrame 应入队")
	}
	for _, fn := range inst.TakeRAF() {
		fn()
	}
	if !called {
		t.Fatal("RAF 回调未执行")
	}
}
