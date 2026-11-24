package skia

import (
	"testing"

	sk "github.com/pekim/go-skia"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// newTestCanvas 创建一块离屏 raster canvas，供 PaintTo / PaintToScaled 测试使用。
func newTestCanvas(w, h int) sk.Canvas {
	c := sk.NewCanvasWithDimensions(int32(w), int32(h), sk.NewSurfaceProps())
	if c.IsNil() {
		panic("创建测试 canvas 失败")
	}
	return c
}

// TestPaintToScaledLogicalCoordinates 验证 DPR 缩放的坐标模型：
// 物理 800x400 + dpr=2 的绘制/命中结果，应与物理 400x200 + dpr=1 完全一致——
// 因为两者逻辑坐标都是 400x200。这证明布局与命中都在逻辑坐标系进行，
// dpr 只作用于绘制时的物理光栅化，不改变组件尺寸语义。
func TestPaintToScaledLogicalCoordinates(t *testing.T) {
	tree := sampleTree()

	hi := newTestCanvas(800, 400)
	logHi, hitsHi := PaintToScaled(hi, 800, 400, 2, tree, nil)
	lo := newTestCanvas(400, 200)
	logLo, hitsLo := PaintTo(lo, 400, 200, tree, nil)

	if len(logHi) != len(logLo) {
		t.Fatalf("绘制指令数不同：dpr2=%d dpr1=%d", len(logHi), len(logLo))
	}
	for i := range logHi {
		a, b := logHi[i], logLo[i]
		if a.Kind != b.Kind || a.Name != b.Name || a.X != b.X || a.Y != b.Y || a.W != b.W || a.H != b.H {
			t.Errorf("绘制指令 %d 坐标不一致：dpr2=%v dpr1=%v", i, a, b)
		}
	}
	if len(hitsHi) != len(hitsLo) {
		t.Fatalf("命中框数不同：dpr2=%d dpr1=%d", len(hitsHi), len(hitsLo))
	}
	for i := range hitsHi {
		a, b := hitsHi[i], hitsLo[i]
		if a.X != b.X || a.Y != b.Y || a.W != b.W || a.H != b.H {
			t.Errorf("命中框 %d 坐标不一致（应为逻辑坐标）：dpr2=%v dpr1=%v", i, a, b)
		}
	}
}

// TestPaintToScaledDPRFallback 验证 dpr<=0 退化为 1：物理尺寸直接当逻辑尺寸。
func TestPaintToScaledDPRFallback(t *testing.T) {
	tree := sampleTree()
	for _, dpr := range []float32{0, -1} {
		c := newTestCanvas(400, 200)
		log1, hits1 := PaintToScaled(c, 400, 200, dpr, tree, nil)
		c2 := newTestCanvas(400, 200)
		log2, hits2 := PaintTo(c2, 400, 200, tree, nil)
		if len(log1) != len(log2) || len(hits1) != len(hits2) {
			t.Errorf("dpr=%v 应退化为 PaintTo：log %d/%d hits %d/%d", dpr, len(log1), len(log2), len(hits1), len(hits2))
		}
	}
}

// TestPaintToScaledNilRoot 验证 nil 输入安全返回。
func TestPaintToScaledNilRoot(t *testing.T) {
	c := newTestCanvas(100, 100)
	log, hits := PaintToScaled(c, 100, 100, 2, nil, nil)
	if len(log) != 0 || len(hits) != 0 {
		t.Errorf("nil root 应返回空结果：log=%d hits=%d", len(log), len(hits))
	}
}

// TestHitBoxContainsLogicalPoint 验证命中框用逻辑坐标时点击判定正确
// （DPR 只影响绘制，命中仍用窗口逻辑坐标）。
func TestHitBoxContainsLogicalPoint(t *testing.T) {
	n := &gxscene.Node{Type: gxscene.NodeDiv, Kind: "div"}
	b := HitBox{Node: n, X: 10, Y: 20, W: 100, H: 40}
	if !b.Contains(15, 25) {
		t.Error("逻辑点 (15,25) 应命中")
	}
	if b.Contains(5, 5) {
		t.Error("逻辑点 (5,5) 不应命中")
	}
}
