package rustbackend

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// TestWindowFocus 验证 ref 句柄的 Focus 走与原生点击输入框完全相同的
// focus 事件通道：事件 kind/node_id 正确，且能被已注册的 Dispatcher 消费。
func TestWindowFocus(t *testing.T) {
	w := NewWindow()
	id := gxscene.NodeID(4242)

	// Dispatcher 就是应用在 SetDispatcher 里注册的处理链；demo 对 focus
	// 事件调用 widgets.Focus(findNode(app, ev.NodeID))。这里验证事件能
	// 原样送到这条链，node_id 是 ref 句柄持有的那个 NodeID。
	var got InputEvent
	w.SetDispatcher(func(ev InputEvent) { got = ev })

	w.Focus(id)

	select {
	case ev := <-w.events:
		if ev.Kind != "focus" {
			t.Fatalf("事件 kind 期望 focus，got %q", ev.Kind)
		}
		if ev.NodeID != id.String() {
			t.Fatalf("事件 node_id 期望 %q，got %q", id.String(), ev.NodeID)
		}
		// 模拟 Run() 的消费分派：同一条 events → Dispatcher 链。
		w.mu.Lock()
		fn := w.dispatcher
		w.mu.Unlock()
		if fn != nil {
			fn(InputEvent{Kind: ev.Kind, NodeID: ev.NodeID, Event: ev.Event, Text: ev.Text, X: ev.X, Y: ev.Y})
		}
		if got.Kind != "focus" || got.NodeID != id.String() {
			t.Fatalf("Dispatcher 未收到正确的 focus 事件：%+v", got)
		}
	default:
		t.Fatal("Focus 未向事件通道投递 focus 事件")
	}
}

// TestWindowFocusNoDispatcher 验证未注册 Dispatcher 时 Focus 仍安全：
// 事件留在通道里（真实应用 Run 起来后会由后续注册的处理器消费），不 panic、不阻塞。
func TestWindowFocusNoDispatcher(t *testing.T) {
	w := NewWindow()
	w.Focus(gxscene.NodeID(7))
	if len(w.events) != 1 {
		t.Fatalf("期望通道里有一条 focus 事件，got %d", len(w.events))
	}
}

// TestWindowFocusZeroValue 验证零值 Window（未 NewWindow/Open）的 Focus 不阻塞
// ——nil 通道的发送分支不可达，select 走 default 丢弃，与 goEventBridge 策略一致。
func TestWindowFocusZeroValue(t *testing.T) {
	var w Window
	w.Focus(gxscene.NodeID(9)) // 不 panic、不阻塞即通过
}
