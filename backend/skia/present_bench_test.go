package skia

import (
	"testing"

	gxapp "github.com/robertpanvip/gox-core/runtime/app"
	gxscene "github.com/robertpanvip/gox-core/runtime/scene"
)

// benchBigTree 构造一棵较大的场景树，放大 snapshotTree 的 O(N) 成本。
func benchBigTree(n int) *gxscene.Node {
	children := make([]*gxscene.Node, 0, n)
	for i := 0; i < n; i++ {
		children = append(children, elemNode("div",
			map[string]any{"color": "#334455", "font-size": 14, "padding": 4},
			textNode("line of code text", nil)))
	}
	return elemNode("div", map[string]any{"width": 760, "flex-direction": "column"}, children...)
}

// BenchmarkNoChangeFrame_Draw：老 Draw 路径每帧全量 snapshotTree（O(N)）。
func BenchmarkNoChangeFrame_Draw(b *testing.B) {
	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 800, Height: 600}); err != nil {
		b.Fatal(err)
	}
	defer w.Close()
	root := benchBigTree(1000)
	w.Draw(root) // 预热，建立缓存
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Draw(root)
	}
}

// BenchmarkNoChangeFrame_Present：FramePresenter 路径，无 Change 时 O(1) 快速复用，
// 跳过 snapshotTree。对比 Draw 体现接入增量帧的核心收益。
func BenchmarkNoChangeFrame_Present(b *testing.B) {
	w := NewWindow()
	if err := w.Open(gxapp.WindowConfig{Width: 800, Height: 600}); err != nil {
		b.Fatal(err)
	}
	defer w.Close()
	root := benchBigTree(1000)
	f := &gxapp.Frame{Root: root, Version: 1}
	w.Present(f) // 预热
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Version++
		w.Present(f)
	}
}
