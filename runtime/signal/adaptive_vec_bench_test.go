package signal

import (
	"fmt"
	"testing"
)

var sinkI int

// 旧方案对照：为大列表的每个元素都建一个独立 sig（即自适应前 Vec 的做法）。
// 这是大后端「稀疏惰性」要省掉的成本基线。
func BenchmarkVecLegacyAllSignalsBig(b *testing.B) {
	vals := benchVals(4096)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		items := make([]Signal[int], len(vals))
		for j, x := range vals {
			items[j] = New(x)
		}
		sinkI = len(items)
	}
}

// 构造对比：小后端每元素一个 sig vs 大后端持久化向量 + 稀疏 sig。
// 关键看「构造大列表」与「为所有元素建 sig」的成本差异。
func benchVals(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

func BenchmarkVecConstructSmall(b *testing.B) {
	vals := benchVals(VecAdaptiveThreshold)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v := NewVec(vals)
		sinkI = v.Len()
	}
}

func BenchmarkVecConstructBig(b *testing.B) {
	vals := benchVals(4096)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v := NewVec(vals)
		sinkI = v.Len()
	}
}

// 只订阅少数索引：大后端稀疏惰性 sig 的核心收益——未订阅元素零 sig 节点。
func BenchmarkVecSparseReadBig(b *testing.B) {
	v := NewVec(benchVals(4096))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkI = v.At(i % 7)() // 只触及 7 个索引
	}
}

func BenchmarkVecAllReadBig(b *testing.B) {
	v := NewVec(benchVals(4096))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < v.Len(); j++ {
			sinkI = v.At(j)()
		}
	}
}

// 大后端单点写：不物化 sig 的索引走 pv.Set（O(log32 N)），无通知开销。
func BenchmarkVecSetBig(b *testing.B) {
	v := NewVec(benchVals(4096))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.Set(i%4096, i)
	}
}

// 大后端追加：均摊 O(1) 尾追加。
func BenchmarkVecAppendBig(b *testing.B) {
	b.ReportAllocs()
	v := NewVec(benchVals(4096))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.Append(i)
	}
}

func BenchmarkVecValuesBig(b *testing.B) {
	v := NewVec(benchVals(4096))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = v.Values()
	}
}

// 演示阈值行为，供 -run 手动查看（非基准）。
func ExampleVec_adaptiveBackends() {
	small := NewVec(seqVals(VecAdaptiveThreshold))
	big := NewVec(seqVals(VecAdaptiveThreshold + 1))
	fmt.Println(small.big, big.big)
	// Output: false true
}
