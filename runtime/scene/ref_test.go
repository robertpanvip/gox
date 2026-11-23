package gxscene

import (
	"testing"

	"github.com/robertpanvip/gox/runtime/signal"
)

// TestRefOnBuild 验证 ref 在建树时回调一次，且句柄内部已分配到节点 ID。
func TestRefOnBuild(t *testing.T) {
	var got *Element
	calls := 0
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "input"},
		{Code: OpRef, Slot: 0},
		{Code: OpClose},
	}, N: 1}
	inst := Render(tpl, func() any {
		return func(e *Element) {
			calls++
			got = e
		}
	})
	defer inst.Dispose()

	if calls != 1 {
		t.Fatalf("建树时 ref 应回调一次，实际 %d 次", calls)
	}
	if got == nil {
		t.Fatal("ref 回调收到 nil，期望 *Element 句柄")
	}
	if got.id == NoNode {
		t.Fatal("ref 回调时节点 ID 应已分配")
	}
	// 句柄不因 Update 重跑：多次更新后回调次数不变。
	inst.Update()
	inst.Update()
	if calls != 1 {
		t.Fatalf("Update 不应重跑 ref，实际 %d 次", calls)
	}
}

// TestRefOnDispose 验证 Dispose 时句柄失效且回调 nil。
func TestRefOnDispose(t *testing.T) {
	var gotNil any
	builtCalls, nilCalls := 0, 0
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpRef, Slot: 0},
		{Code: OpClose},
	}, N: 1}
	inst := Render(tpl, func() any {
		return func(e *Element) {
			if e == nil {
				nilCalls++
			} else {
				builtCalls++
				gotNil = e // 存建树句柄副本
			}
		}
	})
	if gotNil == nil {
		t.Fatal("建树时 ref 应收到非 nil 句柄")
	}
	el := gotNil.(*Element)
	id := el.id

	gotNil = nil
	inst.Dispose()
	if nilCalls != 1 {
		t.Fatalf("Dispose 应回调一次 nil，nilCalls=%d", nilCalls)
	}
	if builtCalls != 1 {
		t.Fatalf("建树回调应恰好一次，got %d", builtCalls)
	}
	if !el.dead {
		t.Fatal("Dispose 后句柄应标记 dead")
	}
	// 失效句柄的方法为 no-op。
	if el.Focus() {
		t.Fatal("失效句柄 Focus 应返回 false")
	}
	if r := el.BoundingClientRect(); r.Width != 0 || r.Height != 0 {
		t.Fatalf("失效句柄 BoundingClientRect 应为零矩形，got %+v", r)
	}
	_ = id
}

// TestRefDisposeAnySignature 验证 func(any) 形态的 ref 在 Dispose 也收到 nil。
func TestRefDisposeAnySignature(t *testing.T) {
	torn := false
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpRef, Slot: 0},
		{Code: OpClose},
	}, N: 1}
	inst := Render(tpl, func() any {
		return func(e any) {
			if e == nil {
				torn = true
			}
		}
	})
	inst.Dispose()
	if !torn {
		t.Fatal("func(any) 形态的 ref 应在 Dispose 收到 nil")
	}
}

// TestRefElementCaps 验证注入的宿主能力经 Element 生效，且未注入时安全降级。
func TestRefElementCaps(t *testing.T) {
	var el *Element
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "button"},
		{Code: OpRef, Slot: 0},
		{Code: OpClose},
	}, N: 1}
	inst := Render(tpl, func() any { return func(e *Element) { el = e } })
	defer inst.Dispose()

	// 未注入能力：Focus false，矩形为零，Measure 以零值回调不悬挂。
	if el.Focus() {
		t.Fatal("未注入能力时 Focus 应为 false")
	}
	called := false
	el.Measure(func(w, h float32) {
		called = true
		if w != 0 || h != 0 {
			t.Fatalf("未注入能力时 Measure 应回填 (0,0)，got (%v,%v)", w, h)
		}
	})
	if !called {
		t.Fatal("未注入能力时 Measure 仍应回调（不悬挂）")
	}

	// 注入能力后：Focus/Measure 走注入函数。
	focusID := NoNode
	inst.SetElementCaps(func(id NodeID) bool { focusID = id; return true },
		func(id NodeID) (float32, float32, bool) { return 120, 40, true })
	if !el.Focus() {
		t.Fatal("注入能力后 Focus 应返回 true")
	}
	if focusID != el.id {
		t.Fatalf("Focus 收到的 ID 应等于句柄内部 ID: %v vs %v", focusID, el.id)
	}
	r := el.BoundingClientRect()
	if r.Width != 120 || r.Height != 40 {
		t.Fatalf("BoundingClientRect 应为 120x40，got %+v", r)
	}
	el.Measure(func(w, h float32) {
		if w != 120 || h != 40 {
			t.Fatalf("Measure 同步命中应回填 120x40，got (%v,%v)", w, h)
		}
	})
}

// TestRefReadsMeasuredRegistry 验证 Element 的尺寸读取复用后端回流注册表
// （MeasuredSize / PublishMeasured）——与虚拟列表自动测量那套机制共享数据源，
// 无需后端额外实现 Measurer 接口。
func TestRefReadsMeasuredRegistry(t *testing.T) {
	var el *Element
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "measuretarget"},
		{Code: OpRef, Slot: 0},
		{Code: OpClose},
	}, N: 1}
	inst := Render(tpl, func() any { return func(e *Element) { el = e } })
	defer inst.Dispose()

	// 未回流：零矩形。
	if r := el.BoundingClientRect(); r.Width != 0 || r.Height != 0 {
		t.Fatalf("未回流时应零矩形，got %+v", r)
	}
	PublishMeasured([]Measured{{ID: el.id, W: 210, H: 55}})
	defer ForgetMeasured(el.id)

	if r := el.BoundingClientRect(); r.Width != 210 || r.Height != 55 {
		t.Fatalf("应读到回流尺寸 210x55，got %+v", r)
	}
	el.Measure(func(w, h float32) {
		if w != 210 || h != 55 {
			t.Fatalf("Measure 应同步命中回流数据 210x55，got (%v,%v)", w, h)
		}
	})
}

// TestRefNotReactive 验证 ref 插槽不进响应式接线：含 ref 与动态文本的模板，
// BindReactive 后只有动态文本插槽被接线（WiredSlots 不含 ref），signal 变化不重跑 ref。
func TestRefNotReactive(t *testing.T) {
	calls := 0
	flag := signal.New(false)
	// 用独一无二的标签名，避免与别的测试同结构模板共享 templateKey 缓存。
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "refscope"},
		{Code: OpDynChild, Slot: 0},
		{Code: OpElem, Arg: "target"},
		{Code: OpRef, Slot: 1},
		{Code: OpClose},
		{Code: OpClose},
	}, N: 2}
	inst := Render(tpl,
		func() any {
			_ = flag() // 读一次建立响应式依赖
			return nil
		},
		func() any {
			return func(e *Element) { calls++ }
		},
	)
	defer inst.Dispose()
	inst.BindReactive(signal.Effect)

	perf := inst.Perf()
	if perf.Slots != 2 {
		t.Fatalf("期望 2 个插槽，got %d", perf.Slots)
	}
	// slot0 是动态子节点（接线），slot1 是 ref（不接线）：WiredSlots 应为 1。
	if perf.WiredSlots != 1 {
		t.Fatalf("ref 不应被响应式接线，期望 WiredSlots=1，got %d", perf.WiredSlots)
	}
	if calls != 1 {
		t.Fatalf("建树时 ref 应回调一次，got %d", calls)
	}
	flag(true)
	if calls != 1 {
		t.Fatalf("signal 变化不应重跑 ref，got %d", calls)
	}
}

// TestRefMeasureAsyncHook 验证同步读不到布局时走 afterDraw 钩子，帧后由宿主回填。
func TestRefMeasureAsyncHook(t *testing.T) {
	var el *Element
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "img"},
		{Code: OpRef, Slot: 0},
		{Code: OpClose},
	}, N: 1}
	inst := Render(tpl, func() any { return func(e *Element) { el = e } })
	defer inst.Dispose()

	type req struct {
		id NodeID
		cb func(float32, float32)
	}
	var pending []req
	inst.SetMeasureHook(func(id NodeID, cb func(w, h float32)) {
		pending = append(pending, req{id, cb})
	})
	// 只注入 focus，不注入 measure → Element.Measure 同步读不到，走钩子。
	inst.SetElementCaps(func(NodeID) bool { return true }, nil)

	got := false
	el.Measure(func(w, h float32) {
		got = true
		if w != 88 || h != 66 {
			t.Fatalf("回填应为 88x66，got (%v,%v)", w, h)
		}
	})
	if got {
		t.Fatal("走钩子时不应立即回调")
	}
	if len(pending) != 1 || pending[0].id != el.id {
		t.Fatalf("应登记 1 条 pending 读取，got %d", len(pending))
	}
	// 宿主在帧后回填。
	pending[0].cb(88, 66)
	if !got {
		t.Fatal("回填后 callback 应被调用")
	}
}
