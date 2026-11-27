package rustbackend

import (
	"fmt"
	"testing"
)

// BenchmarkRustParseLayoutCached 缓存命中路径（同一棵树 → 布局复用只跑 emit）。
// 与 BenchmarkRustParseLayout 同树规模对比 = 纯绘制属性变化帧的加速比。
func BenchmarkRustParseLayoutCached(b *testing.B) {
	const iters = 200
	for _, cards := range []int{3, 10, 30} {
		root := makeBenchTree(cards)
		buf, _ := serializeFrameBin(root, 760, 560)
		b.Run(fmt.Sprintf("cards=%d/nodes=%d", cards, countBenchNodes(root)), func(b *testing.B) {
			b.ReportMetric(0, "allocs/op")
			for i := 0; i < b.N; i++ {
				ns := cBenchFrameCached(buf, 760, 560, iters)
				b.ReportMetric(float64(ns)/float64(iters), "ns/op-rust")
			}
		})
	}
}
