package rope

import (
	"strings"
	"testing"
)

// 防止基准结果被编译器优化掉。
var (
	sinkInt     int
	sinkLine    int
	sinkCol     int
	sinkRune    rune
	sinkStr     string
	sinkStrings []string
	sinkRope    *Rope
)

// benchLineText 含 CJK，使 rune→byte 映射非平凡，能真实体现叶内索引优化。
const benchLineText = "// line of code 中文 with mixed text\n"

func benchText() *Rope {
	return Of(strings.Repeat(benchLineText, 1000))
}

// 光标定位：优化目标是 OffsetToLineCol 从两次树下降合并为一次。
func BenchmarkOffsetToLineCol(b *testing.B) {
	r := benchText()
	n := r.RuneLen()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkLine, sinkCol = r.OffsetToLineCol((i * 7919) % n)
	}
}

// 全量取行：优化目标是 Lines 从 O(N·log N) 改为一次中序遍历。
func BenchmarkLines(b *testing.B) {
	r := benchText()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkStrings = r.Lines()
	}
}

// 随机 rune 访问：优化目标是叶内 rune→byte 从线性扫描改为稀疏索引。
func BenchmarkRuneAt(b *testing.B) {
	r := benchText()
	n := r.RuneLen()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkRune, _ = r.RuneAt((i * 7919) % n)
	}
}

// 片段提取：同时受益于叶内索引与精确 Grow。
func BenchmarkSlice(b *testing.B) {
	r := benchText()
	n := r.RuneLen()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		off := (i * 7919) % (n - 32)
		sinkStr = r.Slice(off, off+32)
	}
}

func BenchmarkLineColToOffset(b *testing.B) {
	r := benchText()
	lc := r.LineCount()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkInt = r.LineColToOffset((i*31)%lc, i%40)
	}
}

func BenchmarkOf(b *testing.B) {
	s := strings.Repeat(benchLineText, 1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkRope = Of(s)
	}
}

func BenchmarkRopeString(b *testing.B) {
	r := benchText()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkStr = r.String()
	}
}

// string 基线：光标定位需从头扫描全文统计换行（编辑器当前 []string 方案的同量级成本），
// 与 BenchmarkOffsetToLineCol 对照，体现 SumTree 单次下降的收益。
func BenchmarkStringLineCol(b *testing.B) {
	s := strings.Repeat(benchLineText, 1000)
	runes := []rune(s)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		off := (i * 7919) % len(runes)
		line, col := 0, 0
		for _, ch := range runes[:off] {
			if ch == '\n' {
				line++
				col = 0
			} else {
				col++
			}
		}
		sinkLine, sinkCol = line, col
	}
}
