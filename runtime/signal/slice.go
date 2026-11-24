package signal

// Vec[T] 是"sig 修饰切片、切片每个元素都是独立 sig"的运行时载体。
//
// 普通 Signal[[]T] 把整个切片当作一个值：写任意元素都要替换整个切片，
// 所有依赖它的 effect 全部重跑（无法区分"改了第几个元素"）。
// Vec[T] 把每个元素提升为一等信号：
//   - At(i) 返回第 i 个元素的 Signal[T]：读它只订阅该元素，写它只通知订阅该元素的 effect；
//   - 改第 3 行不会让关注第 0 行的 effect 重跑——这正是编辑器"行级响应式"想要、
//     但此前只能手写 rowVer0..23 具名 sig 去模拟的能力。
//
// 语义：
//   - 下标语义稳定：Len/At 读的是当前逻辑下标；
//   - Append/SetLen 改变长度时会创建/销毁元素 sig，并对订阅 Len 的 effect 发通知，
//     使 <For each={vec.Values()}> 这类结构消费者能增删列表项；
//   - 每个元素 sig 用默认 DeepEqual 相等策略：写入相等值不触发下游。
//
// 线程模型：Vec 与元素 sig 都只属于 UI Runtime（单线程），后台任务须经
// Signal.Safe 投递后来操作；因此这里无需锁。
type Vec[T any] struct {
	items []Signal[T]
	len   Signal[int] // 长度信号：结构消费者（For）订阅它感知增删
}

// NewVec 用初值创建一个响应式切片，每个元素各自成为一个 Signal[T]。
func NewVec[T any](vals []T) *Vec[T] {
	v := &Vec[T]{}
	items := make([]Signal[T], len(vals))
	for i, x := range vals {
		items[i] = New(x)
	}
	v.items = items
	v.len = New(len(vals))
	return v
}

// NewVecLen 创建长度为 n、元素全为零值的 Vec（等价于 NewVec(make([]T, n))，
// 省去业务侧手写一长串零值字面量）。GX 语法糖 Signal.newVecLen(n)。
func NewVecLen[T any](n int) *Vec[T] {
	vals := make([]T, n)
	return NewVec(vals)
}

// At 返回第 i 个元素的信号。越界返回 nil（调用方应先确认 i < Len()）。
// 返回的 Signal 可无参读、带参写：v.At(2)(newVal) 只通知关注第 2 个元素的 effect。
func (v *Vec[T]) At(i int) Signal[T] {
	if i < 0 || i >= len(v.items) {
		return nil
	}
	return v.items[i]
}

// LenSig 返回长度信号。读它订阅长度变化；<For> 等结构消费者据此增删项。
func (v *Vec[T]) LenSig() Signal[int] {
	return v.len
}

// Len 返回当前长度（读长度信号，若在 effect 内调用会订阅长度变化）。
func (v *Vec[T]) Len() int {
	return v.len()
}

// Values 返回当前所有元素值的快照切片。在 effect 内调用会订阅"长度"以及
// 逐个元素的读——适合 <For each={vec.Values()}>：任一元素值变化都会让 each
// 重算（由 For 的 item-signal 机制进一步只更新变化项的子树）。
func (v *Vec[T]) Values() []T {
	out := make([]T, len(v.items))
	for i, s := range v.items {
		out[i] = s()
	}
	return out
}

// Set 写第 i 个元素的值（等价于 Vec.At(i)(val)）。越界为 no-op，返回是否成功。
func (v *Vec[T]) Set(i int, val T) bool {
	s := v.At(i)
	if s == nil {
		return false
	}
	s(val)
	return true
}

// Append 追加一个元素，通知长度订阅者。
func (v *Vec[T]) Append(val T) {
	v.items = append(v.items, New(val))
	v.len(len(v.items))
}

// SetLen 调整长度：扩容用零值填充新元素，缩容截断。通知长度订阅者。
func (v *Vec[T]) SetLen(n int) {
	if n < 0 {
		n = 0
	}
	old := len(v.items)
	if n < old {
		v.items = v.items[:n]
	} else if n > old {
		for i := old; i < n; i++ {
			var zero T
			v.items = append(v.items, New(zero))
		}
	}
	if n != old {
		v.len(n)
	}
}

// Replace 用新值整体替换：逐元素写 + 长度对齐。只有内容/长度真正变化的
// 元素与长度才通知下游——这是"切换示例"这类全量替换场景走细粒度通道的入口：
// 未变的行不 bump，变了的行的 At(i) 订阅者精确重跑。
func (v *Vec[T]) Replace(vals []T) {
	oldLen := len(v.items)
	// 长度变化先对齐（一次长度通知）。
	if len(vals) < oldLen {
		v.items = v.items[:len(vals)]
	} else if len(vals) > oldLen {
		for i := oldLen; i < len(vals); i++ {
			var zero T
			v.items = append(v.items, New(zero))
		}
	}
	// 逐元素写：相等的元素被相等性策略短路，不通知。
	// 注意：不能在锁内取快照后写——单线程模型下 v.items 不会被并发改动。
	for i, x := range vals {
		v.items[i](x)
	}
	if len(vals) != oldLen {
		v.len(len(vals))
	}
}
