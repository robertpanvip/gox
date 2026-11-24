package signal

import (
	"fmt"
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

// ---------- 单线程热路径性能基准集 ----------
//
// 这组基准对应线程模型重构的验收清单：记录 ns/op、B/op、allocs/op，
// 用来量化「去掉 runtime.Stack + 全局锁 + goroutine 分桶」后的收益。
// 运行：go test ./runtime/signal -run '^$' -bench 'Benchmark(SignalReadInEffect|SignalWriteSubs|EffectCreate|ComputedChain)' -benchmem

// BenchmarkSignalReadInEffect 在观察者栈内读 signal（需登记依赖边）。
// 直接压入一个观察者节点模拟 effect/computed 的重算上下文，与 BenchmarkSignalRead
// 对比可量化依赖追踪的增量成本。
func BenchmarkSignalReadInEffect(b *testing.B) {
	s := New(1)
	ob := newNode(true) // 观察者节点（effect）
	observers = append(observers, ob)
	defer func() { observers = observers[:len(observers)-1] }()
	var sink int
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink = s()
	}
	b.StopTimer()
	runtime.KeepAlive(s)
	runtime.KeepAlive(sink)
}

// BenchmarkSignalWriteSubscribers 写信号并按订阅者数量（1/10/100/1000）测量
// 依赖传播与 effect 调度成本。
func BenchmarkSignalWriteSubscribers(b *testing.B) {
	for _, subs := range []int{1, 10, 100, 1000} {
		b.Run(fmt.Sprintf("subs=%d", subs), func(b *testing.B) {
			s := New(0)
			for i := 0; i < subs; i++ {
				Effect(func() { _ = s() })
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s(i)
			}
			b.StopTimer()
			runtime.KeepAlive(s)
		})
	}
}

// BenchmarkEffectCreateDispose 测量 Effect 创建（首次执行）与销毁的成本。
func BenchmarkEffectCreateDispose(b *testing.B) {
	s := New(0)
	b.Run("create", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			Effect(func() { _ = s() })
		}
	})
	b.Run("create-dispose", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			h := Effect(func() { _ = s() })
			h.Dispose()
		}
	})
}

// BenchmarkComputedChain 测量链式 computed（10/100/1000 层）的首次求值成本。
func BenchmarkComputedChain(b *testing.B) {
	for _, depth := range []int{10, 100, 1000} {
		b.Run(fmt.Sprintf("depth=%d", depth), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				cur := New(1)
				var c Signal[int] = cur
				for d := 0; d < depth; d++ {
					up := c
					c = Computed(func() int { return up() + 1 })
				}
				_ = c()
			}
		})
	}
}
