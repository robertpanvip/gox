package signal

import (
	"runtime"
	"testing"
)

// 防止编译器把「普通变量读」优化成常量/消除。
//
//go:noinline
func readInt(p *int) int { return *p }

// BenchmarkPlainVariableRead 是对照基线：一次普通的 int 变量读。
func BenchmarkPlainVariableRead(b *testing.B) {
	v := 0
	var sink int
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink = readInt(&v)
	}
	b.StopTimer()
	runtime.KeepAlive(sink)
}

// BenchmarkSignalRead 是最常见的热路径：读一个源 signal（无观察者上下文）。
func BenchmarkSignalRead(b *testing.B) {
	s := New(0)
	var sink int
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink = s()
	}
	b.StopTimer()
	runtime.KeepAlive(s)
	runtime.KeepAlive(sink)
}

// BenchmarkComputedRead 读一个依赖未变化的 computed（每次都会 recompute 检查）。
func BenchmarkComputedRead(b *testing.B) {
	s := New(1)
	c := Computed(func() int { return s() * 2 })
	var sink int
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink = c()
	}
	b.StopTimer()
	runtime.KeepAlive(s)
	runtime.KeepAlive(c)
	runtime.KeepAlive(sink)
}

// BenchmarkSignalWrite 写信号（触发依赖传播，但无订阅者）。
func BenchmarkSignalWrite(b *testing.B) {
	s := New(0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s(i)
	}
	b.StopTimer()
	runtime.KeepAlive(s)
}
