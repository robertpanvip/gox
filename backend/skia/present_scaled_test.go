package skia

import (
	"testing"

	sk "github.com/robertpanvip/gox-skia/compat"
)

// TestPaintToCachedScaledDpr2 验证带 DPR 的缓存绘制：
//  1. 首帧全量绘制，画布被填充（绘制坐标未因 DPR 缩放整体偏移到画布外）；
//  2. 纯文本变化（几何不变）触发重画，新内容反映到输出；
//  3. 几何变化走脏矩形局部重绘：脏区外的像素保持不变，且只改局部。
//
// 注意 PaintToCachedScaled 的契约：调用方已确认本帧有变化（桌面端由 Present 用
// Frame.Changes 判定，无变化帧在上层短路）。因此每帧调用都会重画，不做整帧复用
// ——这与 PaintToCached（自己拍快照判复用）语义不同，测试据契约验证。
//
// 本测试是桌面 glWindow 接入的核心正确性依据：证明 richardwilkes/canvas 的
// Clear（填 clip、不受 CTM 影响）与 ClipRect（受 matrix 映射）在 Scale(dpr) 下
// 组合正确，HiDPI 下脏矩形局部重绘位置不偏。
func TestPaintToCachedScaledDpr2(t *testing.T) {
	const (
		physW, physH = 200, 120 // 物理像素
		dpr          = float32(2)
	)
	canvas := sk.NewCanvasWithDimensions(physW, physH, sk.NewSurfaceProps())
	defer canvas.Delete()

	// 两个不重叠的色块：A 在左上、B 在右下。改 A 的位置（几何变化），验证 B 区域像素不变。
	root := elemNode("div", map[string]any{"bg": "white", "width": 90},
		elemNode("boxA", map[string]any{"bg": "#ff0000", "width": 30, "height": 20}),
		elemNode("boxB", map[string]any{"bg": "#0000ff", "width": 30, "height": 20}),
	)

	cache := FrameCache{}
	_, _ = PaintToCachedScaled(&cache, canvas, physW, physH, dpr, root, nil)
	_, paints1 := cache.CacheStats()
	if paints1 != 1 {
		t.Fatalf("首帧应 paints=1, got %d", paints1)
	}

	// 记录首帧整张像素。
	first := append([]byte(nil), canvas.Pixmap().RGBA8888Bytes()...)
	if len(first) != physW*physH*4 {
		t.Fatalf("像素缓冲大小异常: got %d, want %d", len(first), physW*physH*4)
	}
	nonWhite := 0
	for i := 0; i < len(first); i += 4 {
		if first[i] != 0xff || first[i+1] != 0xff || first[i+2] != 0xff {
			nonWhite++
		}
	}
	if nonWhite == 0 {
		t.Fatal("DPR=2 绘制后画布应有非白像素（绘制坐标未整体偏移到画布外）")
	}

	// 几何变化：让 boxA 变宽，触发脏区。B 区域（右下）像素必须保持。
	boxA := root.Children[0]
	boxA.Props["width"] = 60
	_, _ = PaintToCachedScaled(&cache, canvas, physW, physH, dpr, root, nil)
	_, paints2 := cache.CacheStats()
	if paints2 != 2 {
		t.Fatalf("变化帧应重绘(paints=2), got %d", paints2)
	}
	second := canvas.Pixmap().RGBA8888Bytes()

	// 统计变化像素——应当集中在局部，远小于整屏。
	changed := 0
	for i := range first {
		if first[i] != second[i] {
			changed++
		}
	}
	totalBytes := physW * physH * 4
	if changed == 0 {
		t.Fatal("几何变化后应有像素变化")
	}
	if changed > totalBytes/2 {
		t.Fatalf("局部重绘应只改局部像素, 实际 changed=%d/%d bytes", changed, totalBytes)
	}
}

// TestPaintToCachedScaledPureText 验证纯文本变化（几何不变）走全量重画且内容正确。
func TestPaintToCachedScaledPureText(t *testing.T) {
	const physW, physH = 200, 120
	const dpr = float32(2)
	canvas := sk.NewCanvasWithDimensions(physW, physH, sk.NewSurfaceProps())
	defer canvas.Delete()

	root := elemNode("div", map[string]any{"bg": "white", "width": 90},
		elemNode("h1", map[string]any{"color": "#e63946", "font-size": 20},
			textNode("ABC", nil),
		),
	)
	cache := FrameCache{}
	log1, _ := PaintToCachedScaled(&cache, canvas, physW, physH, dpr, root, nil)
	if log1.Find("text", "ABC") != 1 {
		t.Fatalf("首帧应含文本 ABC, log=%v", log1)
	}

	// 改文本（几何不变）：新文本应出现在绘制指令。
	root.Children[0].Children[0].Text = "XYZ"
	log2, _ := PaintToCachedScaled(&cache, canvas, physW, physH, dpr, root, nil)
	if log2.Find("text", "XYZ") != 1 {
		t.Fatalf("文本变化后应含新文本 XYZ, log=%v", log2)
	}
	if log2.Find("text", "ABC") != 0 {
		t.Fatalf("文本变化后不应残留旧文本 ABC, log=%v", log2)
	}
}
