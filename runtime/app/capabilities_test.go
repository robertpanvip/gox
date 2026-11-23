package gxapp_test

import (
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// capableWindow 是一个实现了可选 Focuser/Measurer 的测试窗口。
type capableWindow struct {
	countingWindow
	lastFocus gxscene.NodeID
	measure   map[gxscene.NodeID]struct{ w, h float32 }
}

func (w *capableWindow) Focus(id gxscene.NodeID) { w.lastFocus = id }
func (w *capableWindow) Measure(id gxscene.NodeID) (float32, float32, bool) {
	r, ok := w.measure[id]
	if !ok {
		return 0, 0, false
	}
	return r.w, r.h, true
}

func newCapableWindow() *capableWindow {
	return &capableWindow{measure: map[gxscene.NodeID]struct{ w, h float32 }{}}
}

// TestMeasureBackfill 验证 App.Measure 登记的异步读取在下一帧 Draw 后回填真实布局。
func TestMeasureBackfill(t *testing.T) {
	win := newCapableWindow()
	app, err := gxapp.OpenWith(gxapp.DefaultConfig(), win)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err := app.Render(func() any { return helloInst() }); err != nil {
		t.Fatal(err)
	}

	id := gxscene.NodeID(12345)
	win.measure[id] = struct{ w, h float32 }{200, 80}
	got := false
	app.Measure(id, func(w, h float32) {
		got = true
		if w != 200 || h != 80 {
			t.Fatalf("应回填 200x80，got (%v,%v)", w, h)
		}
	})
	if got {
		t.Fatal("登记后不应立即回调（需等帧后回填）")
	}
	if err := app.DrawNow(); err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("帧后应回填 callback")
	}
}

// TestMeasureNoMeasurer 验证非 Measurer 后端（含 headless）零改动且以 (0,0) 回填，
// 不悬挂调用方。
func TestMeasureNoMeasurer(t *testing.T) {
	win := &countingWindow{}
	app, err := gxapp.OpenWith(gxapp.DefaultConfig(), win)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err := app.Render(func() any { return helloInst() }); err != nil {
		t.Fatal(err)
	}

	called := false
	app.Measure(gxscene.NodeID(7), func(w, h float32) {
		called = true
		if w != 0 || h != 0 {
			t.Fatalf("非 Measurer 后端应回填 (0,0)，got (%v,%v)", w, h)
		}
	})
	if err := app.DrawNow(); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("非 Measurer 后端也必须回填，不能悬挂 callback")
	}
	// 普通 Window 仍正常绘制。
	if win.draws < 1 {
		t.Fatal("非 Measurer 后端应正常 Draw")
	}
}

// TestRefElementFocusAndMeasure 端到端验证：ref 句柄拿到 Element，Focus 经 Focuser
// 下发到后端；Measure 经 Measurer 在帧后回填。组件子树/普通后端零改动。
func TestRefElementFocusAndMeasure(t *testing.T) {
	win := newCapableWindow()
	app, err := gxapp.OpenWith(gxapp.DefaultConfig(), win)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	var el *gxscene.Element
	tpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "input"},
		{Code: gxscene.OpRef, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
	if err := app.Render(func() any {
		return gxscene.Render(tpl, func() any {
			return func(e *gxscene.Element) { el = e }
		})
	}); err != nil {
		t.Fatal(err)
	}
	if el == nil {
		t.Fatal("ref 未收到 Element 句柄")
	}

	// Focus 能力：验证后端收到的 ID 与句柄内部节点一致。
	if !el.Focus() {
		t.Fatal("实现 Focuser 后 Focus 应返回 true")
	}
	if win.lastFocus == gxscene.NoNode {
		t.Fatal("Focus 未下发到后端")
	}

	// Measure 能力：后端有该节点布局 → BoundingClientRect 同步读到。
	// 用句柄内部 ID 注册布局（通过 Focus 下发的 ID 即节点 ID）。
	win.measure[win.lastFocus] = struct{ w, h float32 }{150, 30}
	r := el.BoundingClientRect()
	if r.Width != 150 || r.Height != 30 {
		t.Fatalf("BoundingClientRect 应为 150x30，got %+v", r)
	}
}

// TestMeasureHookAfterRender 验证 ref 句柄的 el.Measure 走 App 的 pending 回填：
// 同步拿不到布局时登记，DrawNow 后才回填。
func TestMeasureHookAfterRender(t *testing.T) {
	win := newCapableWindow()
	app, err := gxapp.OpenWith(gxapp.DefaultConfig(), win)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	var el *gxscene.Element
	tpl := gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "img"},
		{Code: gxscene.OpRef, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}
	if err := app.Render(func() any {
		return gxscene.Render(tpl, func() any {
			return func(e *gxscene.Element) { el = e }
		})
	}); err != nil {
		t.Fatal(err)
	}

	// 无该节点布局：Measure 走 afterDraw 钩子登记。
	got := false
	el.Measure(func(w, h float32) {
		got = true
		if w != 0 || h != 0 {
			t.Fatalf("无布局时应回填 (0,0)，got (%v,%v)", w, h)
		}
	})
	if got {
		t.Fatal("登记后不应立即回填")
	}
	if err := app.DrawNow(); err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("帧后应回填 el.Measure 的 callback")
	}
}
