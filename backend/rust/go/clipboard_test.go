package rustbackend

import (
	"encoding/json"
	"testing"
)

// ClipboardGet/Set 语义与旧 GLFW 路径对齐：无显示环境返回空串/false，不 panic。
func TestClipboardGetSet(t *testing.T) {
	got := ClipboardGet() // 无显示服务时为空串，合法
	if got == "\x00" {
		t.Fatal("ClipboardGet 不应含 NUL")
	}
	_ = ClipboardSet("gx-desktop-test") // 成功 true / 无显示 false，均合法
}

// 事件分级：键盘/指针按下抬起/滚轮/IME 是可靠事件（不能丢）；
// pointer_move 可合并只保留最新（拖拽只需最新位置）。
func TestEventReliabilityClasses(t *testing.T) {
	reliable := []string{"click", "focus", "ime_enabled", "ime_commit", "ime_preedit",
		"backspace", "key", "pointer_down", "pointer_up", "wheel"}
	for _, kind := range reliable {
		if !(inputEvent{Kind: kind}).reliable() {
			t.Errorf("%s 应为可靠事件", kind)
		}
	}
	if (inputEvent{Kind: "pointer_move"}).reliable() {
		t.Error("pointer_move 应走合并槽（高频状态事件）")
	}
}

// InputEvent.Mods 字段贯穿 JSON 解析：Rust 端 mods 串原样透传翻译层。
func TestInputEventModsParsed(t *testing.T) {
	ev := parseTestEvent(`{"kind":"key","node_id":"","event":"down","text":"Left","x":0,"y":0,"mods":"ctrl+shift"}`)
	if ev.Mods != "ctrl+shift" {
		t.Fatalf("mods 应解析为 ctrl+shift, got %q", ev.Mods)
	}
}

// parseTestEvent 复用 goEventBridge 的解析路径（json.Unmarshal 到 inputEvent）。
func parseTestEvent(j string) inputEvent {
	var ev inputEvent
	_ = json.Unmarshal([]byte(j), &ev)
	return ev
}
