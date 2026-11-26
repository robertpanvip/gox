package widgets

import (
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// TestPopoverControlledBoolGetter 受控模式的文档协议是 open: func() bool，
// 组件必须兑现该类型（Prop 只认 func() any 是历史实现缺口）。
func TestPopoverControlledBoolGetter(t *testing.T) {
	p := Popover(gxscene.Props{
		"open":      func() bool { return true },
		"trigger":   func() any { return triggerButton() },
		"content":   func() any { return menuContent() },
	})
	p.BindReactive(signal.Effect)
	if findByGxRole(p.Root(), "popover") == nil {
		t.Fatal("受控 open=func() bool 返回 true 时应挂载浮层")
	}
	if findByGxRole(p.Root(), "scrim") == nil {
		t.Error("受控打开时应有 scrim 遮罩")
	}
}
