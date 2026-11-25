package signal

import (
	"math/rand"
	"reflect"
	"sync/atomic"
	"testing"
)

// 构造长度为 n 的 Vec，n 跨越阈值时用于对比两套后端行为一致。
func seqVals(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i
	}
	return out
}

// 同一套语义必须在小后端与大后端上都成立。
func TestVecBothBackendsValues(t *testing.T) {
	for _, n := range []int{0, 1, 8, VecAdaptiveThreshold, VecAdaptiveThreshold + 1, 500} {
		v := NewVec(seqVals(n))
		if v.Len() != n {
			t.Fatalf("n=%d Len=%d", n, v.Len())
		}
		if got := v.Values(); !reflect.DeepEqual(got, seqVals(n)) {
			t.Fatalf("n=%d Values 不一致: %v", n, got)
		}
		for i := 0; i < n; i++ {
			if got := v.At(i)(); got != i {
				t.Fatalf("n=%d At(%d)=%d", n, i, got)
			}
		}
		if v.At(-1) != nil || (n > 0 && v.At(n) != nil) {
			t.Fatalf("n=%d 越界 At 应返回 nil", n)
		}
	}
}

// 大后端：改第 i 个元素只通知订阅 i 的 effect，不连坐其它元素。
func TestVecBigElementIsolation(t *testing.T) {
	n := VecAdaptiveThreshold + 20
	v := NewVec(seqVals(n))
	var runs0, runs1, runs2 atomic.Int64
	CreateRoot(func(o *Owner) {
		Effect(func() { _ = v.At(0)(); runs0.Add(1) })
		Effect(func() { _ = v.At(1)(); runs1.Add(1) })
		Effect(func() { _ = v.At(2)(); runs2.Add(1) })
	})
	if runs0.Load() != 1 || runs1.Load() != 1 || runs2.Load() != 1 {
		t.Fatalf("首帧: %d %d %d", runs0.Load(), runs1.Load(), runs2.Load())
	}
	v.Set(1, 1001)
	if runs0.Load() != 1 || runs1.Load() != 2 || runs2.Load() != 1 {
		t.Fatalf("改元素1应只让 runs1 增加: %d %d %d", runs0.Load(), runs1.Load(), runs2.Load())
	}
	v.Set(1, 1001) // 相等写入
	if runs1.Load() != 2 {
		t.Fatalf("相等写入不应触发, got %d", runs1.Load())
	}
	if v.At(1)() != 1001 {
		t.Fatalf("值未更新: %d", v.At(1)())
	}
}

// 大后端稀疏惰性：只有被 At 读过的索引才物化 sig。
func TestVecBigSparseMaterialization(t *testing.T) {
	v := NewVec(seqVals(1000))
	if len(v.idx) != 0 {
		t.Fatalf("初始不应物化任何索引 sig, got %d", len(v.idx))
	}
	s := v.At(500)
	if len(v.idx) != 1 {
		t.Fatalf("At(500) 后应只物化 1 个, got %d", len(v.idx))
	}
	// 同一索引重复 At 复用同一 sig：通过旧句柄写入，新句柄应读到新值。
	s(7777)
	if got := v.At(500)(); got != 7777 {
		t.Fatalf("sig 不稳定复用: got %d", got)
	}
	if len(v.idx) != 1 {
		t.Fatalf("重复 At 不应新增物化, got %d", len(v.idx))
	}
	// 未物化的索引写入不产生 sig，但值仍更新。
	v.Set(999, 9999)
	if len(v.idx) != 1 {
		t.Fatalf("未物化索引写入不应物化 sig, got %d", len(v.idx))
	}
	if v.At(999)() != 9999 {
		t.Fatal("未物化索引写入后读取应反映新值")
	}
	if len(v.idx) != 2 {
		t.Fatalf("读取后应物化该索引, got %d", len(v.idx))
	}
	// 直接 At(i)(v) 写入必须回写 pv：缩容重建 pv 后值不能丢。
	v.At(500)(8888)
	v.SetLen(600) // 触发 pv 重建（缩容）
	if got := v.At(500)(); got != 8888 {
		t.Fatalf("At 直接写入未回写 pv，缩容后丢失: got %d", got)
	}
}

// 跨阈值迁移（Append 触发）：迁移前后元素可访问，且迁移前建立的订阅不丢失。
func TestVecPromoteOnAppendKeepsSubscriptions(t *testing.T) {
	v := NewVec(seqVals(VecAdaptiveThreshold))
	if v.big {
		t.Fatal("阈值长度应处于小后端")
	}
	var runs atomic.Int64
	var last int
	CreateRoot(func(o *Owner) {
		Effect(func() { last = v.At(3)(); runs.Add(1) })
	})
	if runs.Load() != 1 || last != 3 {
		t.Fatalf("迁移前订阅失败: runs=%d last=%d", runs.Load(), last)
	}
	// 跨阈值
	for i := 0; i < 10; i++ {
		v.Append(VecAdaptiveThreshold + i)
	}
	if !v.big {
		t.Fatal("超过阈值应迁移到大后端")
	}
	// 迁移前订阅的 At(3) 仍应收到通知
	v.Set(3, 3333)
	if runs.Load() != 2 || last != 3333 {
		t.Fatalf("迁移后订阅丢失: runs=%d last=%d", runs.Load(), last)
	}
	if v.Len() != VecAdaptiveThreshold+10 {
		t.Fatalf("迁移后长度错误: %d", v.Len())
	}
	for i := 0; i < v.Len(); i++ {
		want := i
		if i == 3 {
			want = 3333
		}
		if v.At(i)() != want {
			t.Fatalf("迁移后 At(%d)=%d want=%d", i, v.At(i)(), want)
		}
	}
}

// 迁移后新元素可被订阅（大后端惰性 sig）。
func TestVecPromoteNewElementSubscriptions(t *testing.T) {
	v := NewVec(seqVals(VecAdaptiveThreshold))
	for i := 0; i < 5; i++ {
		v.Append(1000 + i)
	}
	var runs atomic.Int64
	var last int
	CreateRoot(func(o *Owner) {
		Effect(func() { last = v.At(VecAdaptiveThreshold + 2)(); runs.Add(1) })
	})
	if runs.Load() != 1 || last != 1002 {
		t.Fatalf("新元素订阅失败: runs=%d last=%d", runs.Load(), last)
	}
	v.Set(VecAdaptiveThreshold+2, 9999)
	if runs.Load() != 2 || last != 9999 {
		t.Fatalf("新元素写入未通知: runs=%d last=%d", runs.Load(), last)
	}
}

// SetLen 缩容后越界索引返回 nil，扩容新元素为零值（两套后端一致）。
func TestVecSetLenBothBackends(t *testing.T) {
	for _, n := range []int{VecAdaptiveThreshold, VecAdaptiveThreshold + 100} {
		v := NewVec(seqVals(n))
		v.SetLen(5)
		if v.Len() != 5 {
			t.Fatalf("n=%d 缩容后 Len=%d", n, v.Len())
		}
		if v.At(5) != nil {
			t.Fatalf("n=%d 缩容后越界应 nil", n)
		}
		v.SetLen(8)
		if got := v.At(7)(); got != 0 {
			t.Fatalf("n=%d 扩容新元素应为零值, got %d", n, got)
		}
		v.Set(6, 66)
		if got := v.At(6)(); got != 66 {
			t.Fatalf("n=%d Set 失败, got %d", n, got)
		}
	}
}

// Replace 两套后端：只有变化元素通知，未变元素不 bump。
func TestVecReplaceBothBackends(t *testing.T) {
	for _, n := range []int{3, VecAdaptiveThreshold + 30} {
		v := NewVec(seqVals(n))
		var r0, r1, r2 atomic.Int64
		CreateRoot(func(o *Owner) {
			Effect(func() { _ = v.At(0)(); r0.Add(1) })
			Effect(func() { _ = v.At(1)(); r1.Add(1) })
			Effect(func() { _ = v.At(2)(); r2.Add(1) })
		})
		vals := seqVals(n)
		vals[1] = 1111
		v.Replace(vals)
		if r0.Load() != 1 || r1.Load() != 2 || r2.Load() != 1 {
			t.Fatalf("n=%d Replace 只应 bump 变化元素: %d %d %d", n, r0.Load(), r1.Load(), r2.Load())
		}
		if got := v.Values(); !reflect.DeepEqual(got, vals) {
			t.Fatalf("n=%d Replace 值错误", n)
		}
	}
}

// 长度订阅：Append 跨阈值迁移只通知一次，大后端 Append 也通知。
func TestVecLenSignalBothBackends(t *testing.T) {
	for _, start := range []int{VecAdaptiveThreshold - 2, VecAdaptiveThreshold + 2} {
		v := NewVec(seqVals(start))
		var lens atomic.Int64
		var lastLen int
		CreateRoot(func(o *Owner) {
			Effect(func() { lastLen = v.Len(); lens.Add(1) })
		})
		before := lens.Load()
		v.Append(9999)
		if lens.Load() != before+1 || lastLen != start+1 {
			t.Fatalf("start=%d Append 应通知长度一次: +%d len=%d", start, lens.Load()-before, lastLen)
		}
	}
}

// 大后端随机 Append/Set/Replace/SetLen 差分对照 []int 模型。
func TestVecBigRandomAgainstSlice(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	start := VecAdaptiveThreshold + 10
	model := seqVals(start)
	v := NewVec(append([]int(nil), model...))
	for step := 0; step < 2000; step++ {
		switch rng.Intn(4) {
		case 0:
			x := rng.Intn(1 << 16)
			v.Append(x)
			model = append(model, x)
		case 1:
			if len(model) > 0 {
				i := rng.Intn(len(model))
				x := rng.Intn(1 << 16)
				v.Set(i, x)
				model[i] = x
			}
		case 2:
			if len(model) > 0 {
				i := rng.Intn(len(model))
				x := rng.Intn(1 << 16)
				vals := append([]int(nil), model...)
				vals[i] = x
				v.Replace(vals)
				model = vals
			}
		case 3:
			nlen := rng.Intn(200)
			v.SetLen(nlen)
			if nlen < len(model) {
				model = model[:nlen]
			} else {
				for i := len(model); i < nlen; i++ {
					model = append(model, 0)
				}
			}
		}
		if v.Len() != len(model) {
			t.Fatalf("step %d Len=%d want=%d", step, v.Len(), len(model))
		}
		if got := v.Values(); !reflect.DeepEqual(got, model) {
			t.Fatalf("step %d Values 与模型不一致 (len=%d)", step, len(model))
		}
	}
}

// 小后端 Append 到跨阈值全程差分对照。
func TestVecPromotePathAgainstSlice(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	model := []int{}
	v := NewVec[int](nil)
	for step := 0; step < VecAdaptiveThreshold*3; step++ {
		if rng.Intn(100) < 70 {
			x := rng.Intn(1000)
			v.Append(x)
			model = append(model, x)
		} else if len(model) > 0 {
			i := rng.Intn(len(model))
			x := rng.Intn(1000)
			v.Set(i, x)
			model[i] = x
		}
	}
	if !v.big {
		t.Fatal("循环结束应处于大后端")
	}
	if got := v.Values(); !reflect.DeepEqual(got, model) {
		t.Fatal("跨阈值路径与模型不一致")
	}
}
