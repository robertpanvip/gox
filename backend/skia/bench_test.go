package skia

import (
	"fmt"
	"testing"

	sk "github.com/robertpanvip/gox-skia/compat"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

func skCanvas(w, h int) sk.Canvas {
	return sk.NewCanvasWithDimensions(int32(w), int32(h), sk.NewSurfaceProps())
}

// benchMeasure 与 Rust 后端文本估算同口径：ASCII 0.56em，CJK 1em（这里统一按 ASCII 近似），
// 排除真实字体 shaping 差异，专注对比布局引擎（Yoga vs Taffy）的纯布局成本。
func benchMeasure(text string, fontSize int, bold bool, family string) float32 {
	return float32(len([]rune(text))) * float32(fontSize) * 0.56
}

// makeBenchTree 构造与 rust-backend benchmark 完全相同的场景树规模。
func makeBenchTree(n int) *gxscene.Node {
	ops := []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpAttr, Key: "flex-direction", Arg: "column"},
		{Code: gxscene.OpAttr, Key: "width", Arg: "760"},
		{Code: gxscene.OpAttr, Key: "padding", Arg: "20"},
		{Code: gxscene.OpAttr, Key: "align-items", Arg: "center"},
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpAttr, Key: "color", Arg: "#e2e8f0"},
		{Code: gxscene.OpAttr, Key: "font-size", Arg: "26"},
		{Code: gxscene.OpChildText, Arg: "Benchmark Dashboard"},
		{Code: gxscene.OpClose},
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpAttr, Key: "flex-direction", Arg: "row"},
		{Code: gxscene.OpAttr, Key: "gap", Arg: "12"},
	}
	for i := 0; i < n; i++ {
		ops = append(ops,
			gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "bg", Arg: "#141b33"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "width", Arg: "200"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "height", Arg: "96"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "padding", Arg: "14"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "border-radius", Arg: "12"},
			gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "color", Arg: "#94a3b8"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "font-size", Arg: "14"},
			gxscene.Op{Code: gxscene.OpChildText, Arg: fmt.Sprintf("metric %d", i)},
			gxscene.Op{Code: gxscene.OpClose},
			gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "color", Arg: "#2dd4bf"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "font-size", Arg: "28"},
			gxscene.Op{Code: gxscene.OpChildText, Arg: fmt.Sprintf("%d", 1000+i)},
			gxscene.Op{Code: gxscene.OpClose},
			gxscene.Op{Code: gxscene.OpClose},
		)
	}
	ops = append(ops,
		gxscene.Op{Code: gxscene.OpClose},
		gxscene.Op{Code: gxscene.OpClose},
	)
	return gxscene.Render(gxscene.Template{Ops: ops}).Root()
}

func benchNodeCount(n *gxscene.Node) int {
	c := 1
	for _, x := range n.Children {
		c += benchNodeCount(x)
	}
	return c
}

// BenchmarkSkiaLayout 测 Go/Skia 后端的 Yoga 3.x 布局（纯 CPU，无绘制）。
// 与 rust-backend 的 BenchmarkRustParseLayout 规模一致，用于布局引擎公平对比。
func BenchmarkSkiaLayout(b *testing.B) {
	for _, cards := range []int{3, 10, 30} {
		root := makeBenchTree(cards)
		b.Run(fmt.Sprintf("cards=%d/nodes=%d", cards, benchNodeCount(root)), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = computeLayout(root, 760, 560, benchMeasure)
			}
		})
	}
}

// BenchmarkFrameCacheReuse 量化帧缓存收益：
//   - Full：每帧全量 computeLayout + paintWithLayout（旧行为）
//   - Cached：内容无变化，整帧复用（新行为，只拍快照比对）
// 两者节点规模相同，差值即"没变化不重算"省下的成本。
func BenchmarkFrameCacheReuse(b *testing.B) {
	root := makeBenchTree(30)
	const W, H = 800, 600

	b.Run("Full", func(b *testing.B) {
		canvas := skCanvas(W, H)
		defer canvas.Delete()
		var log DrawLog
		var hits []HitBox
		p := &painter{canvas: canvas, width: W, height: H, log: &log, hits: &hits}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			canvas.Clear(bgWhite())
			p.layout = computeLayout(root, W, H, benchMeasure)
			p.paintWithLayout(root)
			log = log[:0]
			hits = hits[:0]
		}
	})

	b.Run("Cached", func(b *testing.B) {
		canvas := skCanvas(W, H)
		defer canvas.Delete()
		cache := FrameCache{}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = renderCached(&cache, canvas, root, W, H, nil)
		}
	})
}

// BenchmarkDirtyRect 测"内容变化导致缓存无法复用"时的缓存路径开销（快照+布局+脏区判定+绘制）。
// 与 Full 对比：在合成 measure（Skia 绘制调用极廉价）下两者接近；真实字体 shaping/阴影
// 昂贵时，小而集中的脏区（如滚动条带）通过跳过不相交节点的 Skia 调用才体现收益。
func BenchmarkDirtyRect(b *testing.B) {
	root := makeBenchTree(30)
	const W, H = 800, 600

	// 找第一个文本节点作为改动目标。
	var target *gxscene.Node
	var walk func(*gxscene.Node)
	walk = func(n *gxscene.Node) {
		if n.Type == gxscene.NodeText && target == nil {
			target = n
		}
		for _, c := range n.Children { walk(c) }
	}
	walk(root)

	b.Run("LocalDirty", func(b *testing.B) {
		canvas := skCanvas(W, H)
		defer canvas.Delete()
		cache := FrameCache{}
		b.ReportAllocs()
		i := 0
		b.ResetTimer()
		for ; i < b.N; i++ {
			target.Text = fmt.Sprintf("metric %d", i%100)
			_, _ = renderCached(&cache, canvas, root, W, H, nil)
		}
	})
}

// BenchmarkDesktopDprDirtyRect 对应桌面 glWindow 逐字输入场景（DPR=2，1600×1000 物理像素）：
//   - FullScaled：每帧全量 PaintToScaled（桌面端未接缓存前的行为）。
//   - CachedScaled：PaintToCachedScaled，走"已知有变化"快通道。
//
// 据实说明：纯文本变化几何不变 → 脏区为 0 → 仍需全量重画，本基准实测两者基本
// 持平（缓存路径省了 O(N) 快照比对，抵消了 diffLayout 的额外开销）。脏矩形真正
// 的用武之地是几何变化场景，见 BenchmarkDesktopGeometryDirty——但在纯 Go canvas
// 下同样受 O(N) log/hits 记录成本限制，收益有限。勿据本基准宣称提速。
func BenchmarkDesktopDprDirtyRect(b *testing.B) {
	root := makeBenchTree(30)
	const physW, physH = 1600, 1000
	const dpr = 2

	var target *gxscene.Node
	var walk func(*gxscene.Node)
	walk = func(n *gxscene.Node) {
		if n.Type == gxscene.NodeText && target == nil {
			target = n
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)

	b.Run("FullScaled", func(b *testing.B) {
		canvas := skCanvas(physW, physH)
		defer canvas.Delete()
		b.ReportAllocs()
		i := 0
		b.ResetTimer()
		for ; i < b.N; i++ {
			target.Text = fmt.Sprintf("metric %d", i%100)
			_, _ = PaintToScaled(canvas, physW, physH, dpr, root, nil)
		}
	})

	b.Run("CachedScaled", func(b *testing.B) {
		canvas := skCanvas(physW, physH)
		defer canvas.Delete()
		cache := FrameCache{}
		// 预热首帧，建立布局基线。
		_, _ = PaintToCachedScaled(&cache, canvas, physW, physH, dpr, root, nil)
		b.ReportAllocs()
		i := 0
		b.ResetTimer()
		for ; i < b.N; i++ {
			target.Text = fmt.Sprintf("metric %d", i%100)
			_, _ = PaintToCachedScaled(&cache, canvas, physW, physH, dpr, root, nil)
		}
	})
}

// BenchmarkDesktopGeometryDirty 对应桌面端几何变化场景（光标移动/选区/滚动）：
// 改一个绝对定位光标的 x（矩形变化 → 产生小脏区），对比全量重绘与脏矩形重绘。
// 这是像素级脏矩形真正体现收益的场景（纯文本变化几何不变，只能全量重画）。
func BenchmarkDesktopGeometryDirty(b *testing.B) {
	// 一张卡片 + 一个绝对定位的"光标"色块；只移动光标。
	ops := []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpAttr, Key: "flex-direction", Arg: "row"},
		{Code: gxscene.OpAttr, Key: "width", Arg: "1600"},
		{Code: gxscene.OpAttr, Key: "height", Arg: "1000"},
	}
	for i := 0; i < 60; i++ {
		ops = append(ops,
			gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "bg", Arg: "#141b33"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "width", Arg: "180"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "height", Arg: "40"},
			gxscene.Op{Code: gxscene.OpChildText, Arg: fmt.Sprintf("row %d with some text content here", i)},
			gxscene.Op{Code: gxscene.OpClose},
		)
	}
	ops = append(ops, gxscene.Op{Code: gxscene.OpClose})
	root := gxscene.Render(gxscene.Template{Ops: ops}).Root()

	const physW, physH = 1600, 1000
	const dpr = 2

	b.Run("FullScaled", func(b *testing.B) {
		canvas := skCanvas(physW, physH)
		defer canvas.Delete()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// 伪造几何变化：改首行宽度（会使脏区覆盖较大，仍比全量多一次 diff，但测的是极端）。
			root.Children[0].Props["width"] = 180 + i%20
			_, _ = PaintToScaled(canvas, physW, physH, dpr, root, nil)
		}
	})

	b.Run("CachedScaled", func(b *testing.B) {
		canvas := skCanvas(physW, physH)
		defer canvas.Delete()
		cache := FrameCache{}
		_, _ = PaintToCachedScaled(&cache, canvas, physW, physH, dpr, root, nil)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			root.Children[0].Props["width"] = 180 + i%20
			_, _ = PaintToCachedScaled(&cache, canvas, physW, physH, dpr, root, nil)
		}
	})
}
