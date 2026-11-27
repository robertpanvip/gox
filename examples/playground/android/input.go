//go:build android

package main

// playgroundTouch 实现能力包的 rbandroid.TouchSink：能力包只做坐标换算与
// 手势阈值判定，编辑器专属语义（点击定位 / 拖动滚动 / onClick 冒泡）在此处理，
// 与桌面 inputBridge 同一角色。业务代码（ui.Editor*）保持与桌面/网页同源。

import (
	rbandroid "github.com/robertpanvip/gox-backend-android"
	"github.com/robertpanvip/gox-playground/ui"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

var _ rbandroid.TouchSink = (*playgroundTouch)(nil)

type playgroundTouch struct{}

// OnTap 等价桌面 onMouseButton(Press)：编辑器行定位 + 命中矩形冒泡 onClick。
func (p *playgroundTouch) OnTap(w *rbandroid.Window, px, py float32) {
	if line, relX, ok := w.EditorRowAt(px, py); ok {
		ui.EditorFocus()
		ui.EditorSetSelect(line, ui.PickColumn(line, relX))
	}
	root, hits := w.Root(), w.Hits()
	if root == nil {
		return
	}
	bestIdx, bestArea := -1, float32(0)
	for i := range hits {
		if !hits[i].Contains(px, py) {
			continue
		}
		area := hits[i].W * hits[i].H
		if bestIdx < 0 || area < bestArea {
			bestIdx, bestArea = i, area
		}
	}
	if bestIdx >= 0 {
		node := hits[bestIdx].Node
		ui.EditorFocus()
		node.DispatchBubble("onClick", gxscene.EventContext{
			Raw: "android-touch",
			Synthetic: &gxscene.SyntheticEvent{
				Type:          gxscene.EvClick,
				NativeName:    "onClick",
				Target:        node,
				CurrentTarget: node,
				X:             float64(px),
				Y:             float64(py),
			},
		})
	}
}

// OnDrag 编辑器内拖动 → 虚拟滚动；其余容器交给能力包通用滚动路由。
func (p *playgroundTouch) OnDrag(w *rbandroid.Window, px, py, dy float32) bool {
	if _, _, ok := w.EditorRowAt(px, py); ok {
		ui.EditorScrollLines(float64(-dy / 22))
		return true
	}
	return w.DragScrollAt(px, py, dy)
}
