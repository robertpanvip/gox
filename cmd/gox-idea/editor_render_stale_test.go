package main

import (
	"testing"

	sk "github.com/robertpanvip/gox-skia/compat"
	skia "github.com/robertpanvip/gox-backend-skia"
	"github.com/robertpanvip/gox/runtime/signal"
	"github.com/robertpanvip/gox/widgets"
)

// TestEditorRenderStaleAfterInsert 像素级复现「打空格后显示错位/字符丢失」：
// 用真实编辑器场景 + 真实 Skia 光栅画布走桌面渲染路径（PaintToCachedScaled），
// 插入 3 个空格后对比「局部重绘」与「全量重绘」的像素。
// 若两者不一致，即证明桌面增量渲染在该场景下留下陈旧像素（显示 bug 实锤）。
func TestEditorRenderStaleAfterInsert(t *testing.T) {
	const (
		W = 900.0
		H = 400.0
	)
	// 第 1 行即用户报障的行。
	src := "    return <div style={{ flexDirection: \"column\",\n}\n"
	st := widgets.NewEditorState(src)
	view := widgets.Editor(st)
	view.BindLeafEffects(signal.Effect)
	view.BindStructural(signal.Effect, func() {})
	root := view.Root()

	mkCanvas := func() sk.Canvas {
		return sk.NewCanvasWithDimensions(int32(W), int32(H), sk.NewSurfaceProps())
	}

	// —— 帧序列（模拟桌面 Present 的 knownChange 快通道，每键一帧）——
	var cacheA skia.FrameCache
	c1 := mkCanvas()
	skia.PaintToCachedScaled(&cacheA, c1, W, H, 1, root, nil) // 首帧全量
	skia.PaintToCachedScaled(&cacheA, c1, W, H, 1, root, nil) // caret 静止帧
	st.SetSelect(1, 21)
	skia.PaintToCachedScaled(&cacheA, c1, W, H, 1, root, nil) // 点击后 caret 移动帧
	for i := 0; i < 3; i++ {
		st.Insert(" ")
		skia.PaintToCachedScaled(&cacheA, c1, W, H, 1, root, nil)
	}

	// —— 对照组：终态全新画布全量重绘 ——
	var cacheB skia.FrameCache
	c2 := mkCanvas()
	skia.PaintToCachedScaled(&cacheB, c2, W, H, 1, root, nil)

	// —— 比对第 1 行 band（y≈6..28）——
	pix1 := c1.Pixmap().RGBA8888Bytes()
	pix2 := c2.Pixmap().RGBA8888Bytes()
	if len(pix1) != len(pix2) {
		t.Fatalf("像素缓冲大小不一致: %d vs %d", len(pix1), len(pix2))
	}
	const rowH = 22.0
	firstTop := 6 // caretTop = 6 + line*rowHeight；行 band y ∈ [6, 28)
	diff := 0
	firstDiffX, lastDiffX := -1, -1
	for y := firstTop; y < firstTop+int(rowH); y++ {
		for x := 0; x < int(W); x++ {
			i := (y*int(W) + x) * 4
			r1, g1, b1 := pix1[i], pix1[i+1], pix1[i+2]
			r2, g2, b2 := pix2[i], pix2[i+1], pix2[i+2]
			if r1 != r2 || g1 != g2 || b1 != b2 {
				diff++
				if firstDiffX < 0 {
					firstDiffX = x
				}
				lastDiffX = x
			}
		}
	}
	if diff > 0 {
		t.Fatalf("局部重绘与全量重绘在第 1 行像素不一致: %d 像素不同, x∈[%d,%d] —— 显示层残留陈旧像素", diff, firstDiffX, lastDiffX)
	}
	t.Log("局部重绘与全量重绘像素一致，显示层无残留")
}
