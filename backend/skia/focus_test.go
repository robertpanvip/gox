package skia

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// TestWindowFocus 验证 Skia 后端的 Focuser：记录焦点目标，供 GUI 集成层查询。
// Skia 是离屏光栅后端，不持有组件焦点状态，本测试只断言焦点意图被正确记录。
func TestWindowFocus(t *testing.T) {
	w := NewWindow()
	if _, ok := w.FocusedID(); ok {
		t.Fatal("初始状态不应有焦点目标")
	}
	id := gxscene.NodeID(1234)
	w.Focus(id)
	got, ok := w.FocusedID()
	if !ok {
		t.Fatal("Focus 后应有焦点目标")
	}
	if got != id {
		t.Fatalf("焦点目标期望 %d，got %d", id, got)
	}
}

// TestWindowFocusHandler 验证焦点意图回调在注册后被调用，且回调在锁外执行
// （回调里读取 FocusedID 不应死锁）。
func TestWindowFocusHandler(t *testing.T) {
	w := NewWindow()
	var called gxscene.NodeID
	w.SetFocusHandler(func(id gxscene.NodeID) {
		called = id
		// 回调在锁外：重入查询不应死锁。
		if _, ok := w.FocusedID(); !ok {
			t.Error("回调内应能读到刚记录的焦点目标")
		}
	})
	id := gxscene.NodeID(99)
	w.Focus(id)
	if called != id {
		t.Fatalf("回调收到的焦点目标期望 %d，got %d", id, called)
	}

	// 清空后 Focus 不再回调，但仍记录目标。
	w.SetFocusHandler(nil)
	called = 0
	w.Focus(gxscene.NodeID(100))
	if called != 0 {
		t.Fatal("清空后不应再收到回调")
	}
	if got, _ := w.FocusedID(); got != 100 {
		t.Fatalf("清空回调后仍应记录焦点目标，got %d", got)
	}
}

// TestWindowFocusZeroValue 验证零值 Window（未 Open）的 Focus 安全：
// Skia 的焦点记录不依赖画布，未 Open 也能用。
func TestWindowFocusZeroValue(t *testing.T) {
	var w Window
	w.Focus(gxscene.NodeID(5))
	if got, ok := w.FocusedID(); !ok || got != 5 {
		t.Fatalf("零值 Window 应记录焦点目标，got (%d,%v)", got, ok)
	}
}
