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

// drain 用测试辅助：执行本帧所有 RAF。
func drain(inst *Instance) {
	inst.DrainRAF(func(fn func()) { fn() })
}

// TestRAFQueue 验证 RAF 队列语义：登记顺序、drain 清空、hasRAF、句柄唯一。
func TestRAFQueue(t *testing.T) {
	inst, _ := refCanvas(t)
	defer inst.Dispose()

	var order []int
	h1 := inst.RequestAnimationFrame(func() { order = append(order, 1) })
	h2 := inst.RequestAnimationFrame(func() { order = append(order, 2) })
	if h1 == 0 || h2 == 0 || h1 == h2 {
		t.Fatalf("RAF 句柄应非零且互异，got %v %v", h1, h2)
	}
	if !inst.HasRAF() {
		t.Fatal("登记后 HasRAF 应为 true")
	}
	drain(inst)
	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("RAF 应按登记顺序执行，got %v", order)
	}
	if inst.HasRAF() {
		t.Fatal("drain 后 HasRAF 应为 false")
	}
}

// TestElementRAF 验证 el.requestAnimationFrame 入队并写入同一 sink。
func TestElementRAF(t *testing.T) {
	inst, el := refCanvas(t)
	defer inst.Dispose()

	called := false
	if h := el.RequestAnimationFrame(func() { called = true }); h == 0 {
		t.Fatal("el.RequestAnimationFrame 应返回有效句柄")
	}
	if !inst.HasRAF() {
		t.Fatal("el.RequestAnimationFrame 应入队")
	}
	drain(inst)
	if !called {
		t.Fatal("RAF 回调未执行")
	}
}

// TestRAFCancel 验证取消：pending 中取消后不执行，其它回调照常。
func TestRAFCancel(t *testing.T) {
	inst, _ := refCanvas(t)
	defer inst.Dispose()

	var ran []int
	inst.RequestAnimationFrame(func() { ran = append(ran, 1) })
	h2 := inst.RequestAnimationFrame(func() { ran = append(ran, 2) })
	inst.RequestAnimationFrame(func() { ran = append(ran, 3) })
	inst.CancelAnimationFrame(h2)

	drain(inst)
	if len(ran) != 2 || ran[0] != 1 || ran[1] != 3 {
		t.Fatalf("取消的回调不应执行，got %v", ran)
	}
}

// TestRAFCancelSemantics 验证取消幂等、未知句柄、已执行句柄 no-op、
// 以及同批里取消尚未执行的回调。
func TestRAFCancelSemantics(t *testing.T) {
	inst, _ := refCanvas(t)
	defer inst.Dispose()

	// 未知句柄 / 0 句柄取消是 no-op。
	inst.CancelAnimationFrame(0)
	inst.CancelAnimationFrame(RAFHandle(999999))

	// 场景 A：同批里前一个回调取消后一个尚未执行的回调。
	{
		inst, _ := refCanvas(t)
		var ran []int
		var h2SameBatch RAFHandle
		inst.RequestAnimationFrame(func() {
			ran = append(ran, 1)
			inst.CancelAnimationFrame(h2SameBatch) // 取消同批未执行的回调
		})
		h2SameBatch = inst.RequestAnimationFrame(func() { ran = append(ran, 2) })
		drain(inst)
		if len(ran) != 1 || ran[0] != 1 {
			t.Fatalf("同批取消应跳过第二个回调，got %v", ran)
		}
		inst.Dispose()
	}
	// 场景 B：drain 前取消 + 重复取消幂等。
	{
		inst, _ := refCanvas(t)
		called := 0
		h := inst.RequestAnimationFrame(func() { called++ })
		inst.CancelAnimationFrame(h)
		inst.CancelAnimationFrame(h) // 幂等
		drain(inst)
		if called != 0 {
			t.Fatalf("取消后不应执行，got %d", called)
		}
		inst.Dispose()
	}
}

// TestRAFCancelRunningNoop 验证回调开始执行后再取消为 no-op（浏览器语义）。
func TestRAFCancelRunningNoop(t *testing.T) {
	inst, _ := refCanvas(t)
	defer inst.Dispose()

	var hSelf RAFHandle
	calls := 0
	hSelf = inst.RequestAnimationFrame(func() {
		calls++
		inst.CancelAnimationFrame(hSelf) // 正在执行，取消应无效于当前次
	})
	drain(inst)
	if calls != 1 {
		t.Fatalf("当前次回调应完整执行，got %d", calls)
	}
	// 该句柄已被消费，再次 drain 无回调。
	drain(inst)
	if calls != 1 {
		t.Fatalf("已消费的回调不应再次执行，got %d", calls)
	}
}

// TestRAFCancelOnDispose 验证 Dispose 丢弃所有待执行回调。
func TestRAFCancelOnDispose(t *testing.T) {
	inst, el := refCanvas(t)
	called := false
	el.RequestAnimationFrame(func() { called = true })
	inst.Dispose()
	drain(inst)
	if called {
		t.Fatal("Dispose 后 RAF 回调不应执行")
	}
}

// TestElementCancel 验证 el.cancelAnimationFrame 经同一 sink 生效。
func TestElementCancel(t *testing.T) {
	inst, el := refCanvas(t)
	defer inst.Dispose()

	called := false
	h := el.RequestAnimationFrame(func() { called = true })
	el.CancelAnimationFrame(h)
	drain(inst)
	if called {
		t.Fatal("el.cancelAnimationFrame 应取消回调")
	}
}
