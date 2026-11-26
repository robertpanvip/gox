package main

import (
	"testing"

	skia "github.com/robertpanvip/gox-backend-skia"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

func hbox(x, y, w, h float32, cursor string, late bool) skia.HitBox {
	n := &gxscene.Node{Kind: "div"}
	return skia.HitBox{Node: n, X: x, Y: y, W: w, H: h, Cursor: cursor}
}

// TestHoverCursorTopmostFirst hover 光标必须与点击派发同一规则：
// 登记序倒序（顶绘制层 + 同层最内层）取第一个带 cursor 属性的包含框。
// 旧"面积最小"规则会让 6px 分隔条输给任意更小的文本框——分隔条悬停
// 不给 resize 光标，用户 perceived 为"拖不动"。
func TestHoverCursorTopmostFirst(t *testing.T) {
	// 后登记的 resize 分隔条（顶层），与大面积容器重叠点在 (300,300)
	hits := []skia.HitBox{
		hbox(0, 0, 1400, 860, "default", false),     // 根容器（先登记）
		hbox(250, 50, 100, 700, "col-resize", true), // 分隔条（后登记=更顶层）
	}
	got := hoverCursorFor(hits, 300, 300)
	if got != "col-resize" {
		t.Errorf("hover 应取顶登记层的 col-resize（面积最小规则会选容器=default）, got %q", got)
	}
}
