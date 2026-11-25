package main

import (
	"testing"

	skia "github.com/robertpanvip/gox-backend-skia"
)

// TestDrawLogToInputRects 固化"用 DrawLog 而非 hitbox 生成输入形状"的口径：
// 透明容器/隐藏节点（ARGB alpha=0）不算输入区，否则整窗不可穿透；
// 只保留真正画出的不透明内容，并按 dpr 换算到物理像素。
func TestDrawLogToInputRects(t *testing.T) {
	log := skia.DrawLog{
		{Kind: "fragment", Name: "#root", W: 500, H: 400, ARGB: 0x00000000}, // 透明根容器：跳过
		{Kind: "rect", Name: "div", X: 4, Y: 4, W: 200, H: 150, ARGB: 0xff3b82f6},
		{Kind: "text", Name: "Hi", X: 8, Y: 8, W: 20, H: 21, ARGB: 0xffffffff},
		{Kind: "rect", Name: "div", X: 0, Y: 0, W: 10, H: 10, ARGB: 0x003b82f6}, // 透明内容：跳过
	}
	rects := drawLogToInputRects(log, 2) // DPR=2
	if len(rects) != 2 {
		t.Fatalf("输入矩形数 = %d，期望 2（透明 fragment/内容应跳过）: %+v", len(rects), rects)
	}
	want := []inputRect{{8, 8, 400, 300}, {16, 16, 40, 42}}
	for i := range want {
		if rects[i] != want[i] {
			t.Errorf("rect[%d] = %+v, 期望 %+v", i, rects[i], want[i])
		}
	}
}

// TestDrawLogToInputRectsEmpty 全透明场景：无输入矩形 → 整窗鼠标穿透。
func TestDrawLogToInputRectsEmpty(t *testing.T) {
	log := skia.DrawLog{
		{Kind: "fragment", Name: "#root", W: 500, H: 400, ARGB: 0},
	}
	if rects := drawLogToInputRects(log, 1); len(rects) != 0 {
		t.Fatalf("全透明场景输入矩形应为空（整窗穿透），得到 %+v", rects)
	}
	if rects := drawLogToInputRects(nil, 1); len(rects) != 0 {
		t.Fatalf("空 DrawLog 输入矩形应为空，得到 %+v", rects)
	}
}
