package signal

import (
	"sync/atomic"
	"testing"
)

// 核心语义：改第 i 个元素只通知订阅该元素的 effect，不连坐其它元素。
func TestVecElementIsolation(t *testing.T) {
	v := NewVec([]string{"a", "b", "c"})
	var runs0, runs1, runs2 atomic.Int64
	done := make(chan struct{}, 3)
	CreateRoot(func(o *Owner) {
		Effect(func() { _ = v.At(0)(); runs0.Add(1) })
		Effect(func() { _ = v.At(1)(); runs1.Add(1) })
		Effect(func() { _ = v.At(2)(); runs2.Add(1) })
	})
	_ = done
	// 首帧各跑 1 次
	if runs0.Load() != 1 || runs1.Load() != 1 || runs2.Load() != 1 {
		t.Fatalf("首帧应各跑 1 次: %d %d %d", runs0.Load(), runs1.Load(), runs2.Load())
	}
	// 只改第 1 个元素
	v.Set(1, "B")
	if runs0.Load() != 1 || runs1.Load() != 2 || runs2.Load() != 1 {
		t.Fatalf("改元素1应只让 runs1 增加: got %d %d %d", runs0.Load(), runs1.Load(), runs2.Load())
	}
	// 写相等值不触发
	v.Set(1, "B")
	if runs1.Load() != 2 {
		t.Fatalf("相等写入不应触发, got %d", runs1.Load())
	}
}

// Replace 全量替换：只有内容变化的元素重跑，未变元素不 bump。
func TestVecReplaceBumpsOnlyChangedElements(t *testing.T) {
	v := NewVec([]string{"a", "b", "c"})
	var r0, r1, r2 atomic.Int64
	CreateRoot(func(o *Owner) {
		Effect(func() { _ = v.At(0)(); r0.Add(1) })
		Effect(func() { _ = v.At(1)(); r1.Add(1) })
		Effect(func() { _ = v.At(2)(); r2.Add(1) })
	})
	// 替换成 ["a", "B", "c"]：仅第 1 行变。
	v.Replace([]string{"a", "B", "c"})
	if r0.Load() != 1 || r1.Load() != 2 || r2.Load() != 1 {
		t.Fatalf("Replace 只应 bump 变化元素: %d %d %d", r0.Load(), r1.Load(), r2.Load())
	}
	if got := v.Values(); len(got) != 3 || got[1] != "B" {
		t.Fatalf("Values 未反映替换: %v", got)
	}
}

// 长度变化通知订阅 LenSig 的 effect，驱动 <For> 结构消费者。
func TestVecLengthSignal(t *testing.T) {
	v := NewVec([]string{"a"})
	var lens atomic.Int64
	var lastLen int
	CreateRoot(func(o *Owner) {
		Effect(func() { lastLen = v.Len(); lens.Add(1) })
	})
	if lens.Load() != 1 || lastLen != 1 {
		t.Fatalf("首帧 len=1: runs=%d len=%d", lens.Load(), lastLen)
	}
	v.Append("b")
	if lens.Load() != 2 || lastLen != 2 {
		t.Fatalf("Append 应通知长度: runs=%d len=%d", lens.Load(), lastLen)
	}
	v.SetLen(4)
	if lens.Load() != 3 || lastLen != 4 {
		t.Fatalf("SetLen 应通知长度: runs=%d len=%d", lens.Load(), lastLen)
	}
	v.SetLen(2)
	if lens.Load() != 4 || lastLen != 2 {
		t.Fatalf("SetLen 缩容应通知长度: runs=%d len=%d", lens.Load(), lastLen)
	}
}

// Replace 同时改变长度：长度只通知一次，元素按变化通知。
func TestVecReplaceWithLengthChange(t *testing.T) {
	v := NewVec([]string{"a"})
	var lens atomic.Int64
	CreateRoot(func(o *Owner) {
		Effect(func() { _ = v.Len(); lens.Add(1) })
	})
	v.Replace([]string{"a", "b", "c"})
	if lens.Load() != 2 || v.Len() != 3 {
		t.Fatalf("Replace 长度变化应通知一次: runs=%d len=%d", lens.Load(), v.Len())
	}
}
