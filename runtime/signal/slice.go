package signal

import "github.com/robertpanvip/gox/runtime/pvector"

// VecAdaptiveThreshold 是 Vec 自适应后端的切换阈值：长度 ≤ 阈值时用「每元素
// 一个 sig」的小后端，> 阈值时切到「持久化向量 + 稀疏索引 sig」的大后端。
//
// 阈值取舍：小列表下 N 个 sig 节点的订阅开销可接受，且细粒度最直接；超过
// 阈值后，为「从未被订阅的元素」也建 sig 成为浪费，大后端的稀疏惰性订阅与
// 结构共享（撤销 O(1)、整值 O(1) 指针相等）开始占优。
const VecAdaptiveThreshold = 64

// Vec[T] 是"sig 修饰切片"的响应式载体，按数据规模自适应选择后端。
//
// 普通 Signal[[]T] 把整个切片当作一个值：写任意元素都要替换整个切片，
// 所有依赖它的 effect 全部重跑（无法区分"改了第几个元素"）。
// Vec[T] 把每个元素提升为可按索引订阅的信号：At(i) 返回第 i 个元素的 Signal[T]，
// 读它只订阅该元素，写它只通知订阅该元素的 effect——改第 3 行不会让关注第 0 行
// 的 effect 重跑。
//
// 两套后端（对调用方完全透明，公开 API 与语义一致）：
//   - 小后端（长度 ≤ VecAdaptiveThreshold）：[]Signal[T]，每个元素一个独立 sig，
//     细粒度订阅零额外开销。
//   - 大后端（长度 > 阈值）：底层是持久化向量 pvector.PVector[T]（32 叉 trie，
//     结构共享），索引 sig 按需惰性创建——只有被 At(i) 实际读过的索引才分配
//     sig 节点，未订阅的索引零开销。整值快照共享底层节点，撤销/回滚 O(1)。
//
// 语义（两套后端一致）：
//   - 下标语义稳定：Len/At 读的是当前逻辑下标；
//   - Append/SetLen 改变长度时创建/销毁元素 sig，并对订阅 Len 的 effect 发通知，
//     使 <For each={vec.Values()}> 这类结构消费者能增删列表项；
//   - 每个元素 sig 用默认 DeepEqual 相等策略：写入相等值不触发下游；
//   - 跨阈值迁移时保留已物化的元素 sig，既有订阅不丢失。
//
// 线程模型：Vec 与元素 sig 都只属于 UI Runtime（单线程），后台任务须经
// Signal.Safe 投递后来操作；因此这里无需锁。
type Vec[T any] struct {
	lenSig Signal[int] // 长度信号：结构消费者（For）订阅它感知增删
	n      int         // 当前逻辑长度（与 lenSig 同步）

	// 小后端：每个元素一个独立 sig。非 nil 表示处于小后端模式。
	small []Signal[T]

	// 大后端：持久化向量 + 稀疏惰性索引 sig。big 为 true 时生效。
	big bool
	pv  pvector.PVector[T]
	idx map[int]Signal[T] // 只有被 At(i) 读过的索引才有条目
}

// NewVec 用初值创建一个响应式切片。长度超过阈值时直接采用大后端——不会先
// 创建 N 个元素 sig 再迁移，这是大数据量省内存的关键。
func NewVec[T any](vals []T) *Vec[T] {
	v := &Vec[T]{lenSig: New(len(vals)), n: len(vals)}
	if len(vals) <= VecAdaptiveThreshold {
		items := make([]Signal[T], len(vals))
		for i, x := range vals {
			items[i] = New(x)
		}
		v.small = items
	} else {
		v.big = true
		v.pv = pvector.Of(vals)
		v.idx = make(map[int]Signal[T])
	}
	return v
}

// NewVecLen 创建长度为 n、元素全为零值的 Vec（等价于 NewVec(make([]T, n))，
// 省去业务侧手写一长串零值字面量）。GX 语法糖 Signal.newVecLen(n)。
func NewVecLen[T any](n int) *Vec[T] {
	return NewVec(make([]T, n))
}

// At 返回第 i 个元素的信号。越界返回 nil（调用方应先确认下标合法）。
// 返回的 Signal 可无参读、带参写：v.At(2)(newVal) 只通知关注第 2 个元素的 effect。
//
// 大后端下索引 sig 惰性创建：首次读某索引时才分配，之后复用同一 sig，
// 因此同一索引的订阅稳定。
func (v *Vec[T]) At(i int) Signal[T] {
	if i < 0 || i >= v.n {
		return nil
	}
	if !v.big {
		return v.small[i]
	}
	if s, ok := v.idx[i]; ok {
		return s
	}
	s := v.newIdxSig(i)
	v.idx[i] = s
	return s
}

// newIdxSig 为大后端的索引创建一个「写入回写 pv」的信号。
// 调用方既可能用 Vec.Set(i,v)，也可能直接写 At(i)(v)（编辑器即此种写法），
// 两条路径都必须让 pvector 底层值与 sig 保持同步——否则 SetLen 缩容重建
// pv 时会丢失经 At(i)(v) 直接写入的值。
func (v *Vec[T]) newIdxSig(i int) Signal[T] {
	base := New(v.pv.Get(i))
	return func(args ...T) T {
		if len(args) > 0 {
			x := args[0]
			v.pv = v.pv.Set(i, x) // 回写持久化向量（幂等）
			base(x)               // 触发该索引的订阅者（相等写入由 base 短路）
		}
		return base()
	}
}

// set 写第 i 个元素的值（内部统一入口，调用方需已确认下标合法）。
func (v *Vec[T]) set(i int, val T) {
	if !v.big {
		v.small[i](val) // 相等写入由 sig 自身短路
		return
	}
	if s, ok := v.idx[i]; ok {
		s(val) // 包装 sig 同时回写 pv 并通知订阅者
	} else {
		// 未物化的索引无订阅者：只更新 pv，不分配 sig。
		v.pv = v.pv.Set(i, val)
	}
}

// LenSig 返回长度信号。读它订阅长度变化；<For> 等结构消费者据此增删项。
func (v *Vec[T]) LenSig() Signal[int] {
	return v.lenSig
}

// Len 返回当前长度（读长度信号，若在 effect 内调用会订阅长度变化）。
func (v *Vec[T]) Len() int {
	return v.lenSig()
}

// Values 返回当前所有元素值的快照切片。在 effect 内调用会订阅"长度"以及
// 逐个元素的读——适合 <For each={vec.Values()}>：任一元素值变化都会让 each
// 重算（由 For 的 item-signal 机制进一步只更新变化项的子树）。
//
// 两套后端都经 At(i) 逐项读取，保证订阅语义一致。
func (v *Vec[T]) Values() []T {
	out := make([]T, v.n)
	for i := range out {
		out[i] = v.At(i)()
	}
	return out
}

// Set 写第 i 个元素的值（等价于 Vec.At(i)(val)）。越界返回 false。
func (v *Vec[T]) Set(i int, val T) bool {
	if i < 0 || i >= v.n {
		return false
	}
	v.set(i, val)
	return true
}

// Append 追加一个元素，通知长度订阅者。跨阈值时自动迁移到大后端，
// 已物化的元素 sig 在迁移中保留，既有订阅不丢失。
func (v *Vec[T]) Append(val T) {
	if !v.big {
		v.small = append(v.small, New(val))
		v.n++
		if v.n > VecAdaptiveThreshold {
			v.promote()
		}
	} else {
		v.pv = v.pv.Append(val)
		v.n++
	}
	v.lenSig(v.n)
}

// promote 从「每元素一个 sig」迁移到「持久化向量 + 稀疏索引 sig」。
// 所有已存在的元素 sig 被保留到 idx 映射中——它们可能正被 effect 订阅。
func (v *Vec[T]) promote() {
	pv := pvector.PVector[T]{}
	idx := make(map[int]Signal[T], len(v.small))
	for i, s := range v.small {
		pv = pv.Append(s())
		idx[i] = s
	}
	v.pv = pv
	v.idx = idx
	v.small = nil
	v.big = true
}

// SetLen 调整长度：扩容用零值填充新元素，缩容截断。通知长度订阅者。
func (v *Vec[T]) SetLen(n int) {
	if n < 0 {
		n = 0
	}
	old := v.n
	if n == old {
		return
	}
	if n < old {
		if !v.big {
			v.small = v.small[:n]
		} else {
			// 缩容：丢弃越界的惰性 sig，并重建前缀向量。
			for i := range v.idx {
				if i >= n {
					delete(v.idx, i)
				}
			}
			v.pv = pvector.Of(v.pv.Values()[:n])
		}
		v.n = n
	} else {
		var zero T
		if !v.big {
			for i := old; i < n; i++ {
				v.small = append(v.small, New(zero))
			}
			v.n = n
			if v.n > VecAdaptiveThreshold {
				v.promote()
			}
		} else {
			for i := old; i < n; i++ {
				v.pv = v.pv.Append(zero)
			}
			v.n = n
		}
	}
	v.lenSig(v.n)
}

// Replace 用新值整体替换：长度对齐 + 逐元素写。只有内容/长度真正变化的
// 元素与长度才通知下游——未变的行不 bump，变了的行的 At(i) 订阅者精确重跑。
func (v *Vec[T]) Replace(vals []T) {
	oldLen := v.n
	if len(vals) != oldLen {
		v.SetLen(len(vals)) // 长度变化只通知一次；SetLen 可能触发迁移
	}
	if !v.big {
		for i, x := range vals {
			v.small[i](x)
		}
	} else {
		// 大后端整体重建（结构共享，O(N)），再逐已物化索引写以驱动订阅者；
		// 相等值被 sig 的相等策略短路，不会无谓通知。索引 sig 的写入回写
		// pv 是幂等的（值已由整体重建设为 vals[i]）。
		v.pv = pvector.Of(vals)
		for i, s := range v.idx {
			if i < len(vals) {
				s(vals[i])
			}
		}
	}
}
