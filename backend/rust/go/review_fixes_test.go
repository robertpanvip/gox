package rustbackend

import (
	"testing"
)

// ---- Wire 协议版本协商（评审 P2）----

func TestParseProtocolVersion(t *testing.T) {
	cases := []struct {
		in           string
		major, minor int
		ok           bool
	}{
		{"gxbackend-rust 0.2.0 (binary protocol)", 0, 2, true},
		{"gxbackend-rust 1.10.3", 1, 10, true},
		{"version 2.0", 2, 0, true},
		{"no version", 0, 0, false},
		{"gx v2.0 ok", 2, 0, true}, // 容错：跳过非数字前缀，取第一段 x.y
		{"", 0, 0, false},
	}
	for _, c := range cases {
		maj, min, ok := parseProtocolVersion(c.in)
		if ok != c.ok || maj != c.major || min != c.minor {
			t.Errorf("parseProtocolVersion(%q) = (%d,%d,%v), want (%d,%d,%v)",
				c.in, maj, min, ok, c.major, c.minor, c.ok)
		}
	}
}

// ---- 事件分级可靠性（评审 P1）----

func TestEventReliabilityClassification(t *testing.T) {
	reliable := []string{"click", "focus", "ime_enabled", "ime_commit", "ime_preedit", "backspace", "keydown", "keyup"}
	for _, k := range reliable {
		ev := inputEvent{Kind: k}
		if !ev.reliable() {
			t.Errorf("事件 %q 应归类为可靠事件", k)
		}
	}
	mergeable := []string{"cursor", "hover", "measured", "videoframe"}
	for _, k := range mergeable {
		ev := inputEvent{Kind: k}
		if ev.reliable() {
			t.Errorf("事件 %q 应归类为可合并事件", k)
		}
	}
}

// 可靠事件在通道满时不得丢弃：填满 events 后继续投递，overflow 必须承接。
func TestReliableEventNoDropWhenFull(t *testing.T) {
	w := &Window{
		events: make(chan inputEvent, 2),
		pump:   make(chan struct{}, 1),
	}
	w.events <- inputEvent{Kind: "click"} // 占满
	w.events <- inputEvent{Kind: "focus"}
	w.postEvent(inputEvent{Kind: "ime_commit", Text: "你好"}) // 通道满 → overflow
	w.mu.Lock()
	n := len(w.overflow)
	w.mu.Unlock()
	if n != 1 {
		t.Fatalf("可靠事件应在 overflow 保留，got overflow=%d", n)
	}
}

// 可合并事件只保留最新：连续两次投递 cursor，mergeSlot 应为第二次的值。
func TestMergeableEventKeepsLatest(t *testing.T) {
	w := &Window{
		events: make(chan inputEvent, 4),
		pump:   make(chan struct{}, 1),
	}
	w.postEvent(inputEvent{Kind: "cursor", X: 10})
	w.postEvent(inputEvent{Kind: "cursor", X: 99})
	w.mu.Lock()
	slot := w.mergeSlot
	w.mu.Unlock()
	if slot == nil || slot.X != 99 {
		t.Fatalf("可合并事件应只保留最新 (X=99)，got %+v", slot)
	}
}

// ---- 注册表注销（评审 P1 泄漏）——纯逻辑验证，不触发 cgo ----

func TestRegisterKeyAssigned(t *testing.T) {
	w := NewWindow()
	if w.pump == nil || w.postCh == nil {
		t.Fatal("NewWindow 应初始化 pump/postCh 通道")
	}
}

// App.Close 错误传递（评审 P2）的测试在 runtime/app 包内（需访问未导出的 App 字段），
// 见 close_concurrency_test.go 同目录的 TestClosePropagatesWindowError。
