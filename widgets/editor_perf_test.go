package widgets

// 性能对比：string 全量重建（原 editor 方案）vs Rope 持久结构（现方案）。
// 场景是最常见的编辑器热点——在文档中间某行插入一个字符。
// string 版每步 splitLines+joinLines 全量 O(N)；rope 版 split+join 路径复制 O(log N)。

import (
	"fmt"
	"strings"
	"testing"

	Rope "github.com/robertpanvip/gox/runtime/rope"
)

func benchDoc(lines int) string {
	var b strings.Builder
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&b, "line %d: const value_%d = %d;\n", i, i, i)
	}
	return b.String()
}

func BenchmarkEditMiddleLine_StringVsRope(b *testing.B) {
	for _, n := range []int{10, 1000, 5000} {
		doc := benchDoc(n)
		mid := n / 2

		b.Run(fmt.Sprintf("string_lines=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = insertText(doc, mid, 1, "x") // O(N) split + join
			}
		})

		b.Run(fmt.Sprintf("rope_lines=%d", n), func(b *testing.B) {
			r := Rope.Of(doc)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				nr, _ := ropeInsertText(r, mid, 1, "x") // O(log N) 持久结构共享
				r = nr // 累积编辑：验证结构上"新根复用旧子树"的真实路径
			}
		})
	}
}

// BenchmarkLineColLookup 对比行列换算：光标定位是另一个 O(N) 热点。
// string 版需 splitLines 后逐 rune 计数；rope 版 LineColToOffset 单次树下降。
func BenchmarkLineColLookup_StringVsRope(b *testing.B) {
	const n = 5000
	doc := benchDoc(n)
	r := Rope.Of(doc)
	targetLine := n - 2

	b.Run("string_split", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			lines := splitLines(doc)
			_ = len([]rune(lines[targetLine]))
		}
	})
	b.Run("rope_offset", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = r.LineColToOffset(targetLine, 5)
		}
	})
}
