package widgets

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// TestPopoverNoScrimMounts noScrim 受控打开必须挂载浮层（不挂=缺陷复现）。
// 对照 TestPopoverControlledBoolGetter（有 scrim 已验证可挂载），隔离变量。
func TestPopoverNoScrimMounts(t *testing.T) {
	p := Popover(gxscene.Props{
		"open":     func() bool { return true },
		"noScrim":  true,
		"trigger":  func() any { return triggerButton() },
		"content":  func() any { return menuContent() },
	})
	p.BindReactive(signal.Effect)
	root := p.Root()
	scrim := findByGxRole(root, "scrim")
	pop := findByGxRole(root, "popover")
	t.Logf("DIAG: popover=%v scrim=%v", pop != nil, scrim != nil)
	if pop == nil {
		t.Fatal("noScrim 模式打开时浮层未挂载（缺陷复现）")
	}
	// noScrim 语义：不应有遮罩
	if findByGxRole(root, "scrim") != nil {
		t.Error("noScrim 模式不应有 scrim")
	}
}
