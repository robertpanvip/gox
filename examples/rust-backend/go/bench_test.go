package rustbackend

import (
	"fmt"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// makeBenchTree 构造 n 张卡片 + n 个文本的场景树，规模可控。
// 结构：root(column) → [title text, row → n×(card div → 2 text), input div → text]
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
		{Code: gxscene.OpElem, Arg: "div"}, // row
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
		gxscene.Op{Code: gxscene.OpClose}, // row
		gxscene.Op{Code: gxscene.OpClose}, // root
	)
	return gxscene.Render(gxscene.Template{Ops: ops}).Root()
}

func countBenchNodes(n *gxscene.Node) int {
	c := 1
	for _, x := range n.Children {
		c += countBenchNodes(x)
	}
	return c
}

// BenchmarkGXEncode 测 Go 侧 GRX1 场景树编码（纯 CPU）。
func BenchmarkGXEncode(b *testing.B) {
	for _, cards := range []int{3, 10, 30} {
		root := makeBenchTree(cards)
		buf, _ := serializeFrameBin(root, 760, 560)
		b.Run(fmt.Sprintf("cards=%d/nodes=%d/bytes=%d", cards, countBenchNodes(root), len(buf)), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = serializeFrameBin(root, 760, 560)
			}
		})
	}
}

// BenchmarkRustParseLayout 测 Rust 侧纯 CPU 管线：解析 GRX1 → 建镜像树 → Taffy 布局 → 指令生成。
// iters 固定在 Rust 内部循环以摊销 cgo 开销，结果为 ns/op（单次管线）。
func BenchmarkRustParseLayout(b *testing.B) {
	const iters = 200
	for _, cards := range []int{3, 10, 30} {
		root := makeBenchTree(cards)
		buf, _ := serializeFrameBin(root, 760, 560)
		b.Run(fmt.Sprintf("cards=%d/nodes=%d", cards, countBenchNodes(root)), func(b *testing.B) {
			b.ReportMetric(0, "allocs/op") // cgo 边界不统计 Go 分配
			for i := 0; i < b.N; i++ {
				ns := cBenchFrame(buf, 760, 560, iters)
				b.ReportMetric(float64(ns)/float64(iters), "ns/op-rust")
			}
		})
	}
}
