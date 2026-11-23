package skia

import (
	"fmt"
	"testing"

	sk "github.com/pekim/go-skia"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

func skCanvas(w, h int) sk.Canvas {
	return sk.NewCanvasWithDimensions(int32(w), int32(h), sk.NewSurfaceProps())
}

// benchMeasure 与 Rust 后端文本估算同口径：ASCII 0.56em，CJK 1em（这里统一按 ASCII 近似），
// 排除真实字体 shaping 差异，专注对比布局引擎（Yoga vs Taffy）的纯布局成本。
func benchMeasure(text string, fontSize int, bold bool) float32 {
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
