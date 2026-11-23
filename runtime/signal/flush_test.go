package signal_test

import (
	"testing"

	"github.com/robertpanvip/gox/runtime/signal"
)

// AfterEffects 在每批 effect 跑完后触发一次；钩子在锁外执行，可安全读 signal。
func TestAfterEffectsFiresOncePerWrite(t *testing.T) {
	c := signal.New(0)
	var snapshots []int
	signal.Effect(func() { _ = c() }) // 订阅者
	un := signal.AfterEffects(func() { snapshots = append(snapshots, c()) })
	defer un()

	c(1)
	if len(snapshots) != 1 || snapshots[0] != 1 {
		t.Fatalf("单次写应触发 1 次 hook 且读到最新值 1, got %v", snapshots)
	}
}

// Batch 内多次写只触发一次 hook，且读到批次结束后的最终值（一帧一致性，T1.2）。
func TestAfterEffectsBatchCoalescesToFinalValue(t *testing.T) {
	a := signal.New("A")
	b := signal.New("B")
	var snapshots [][2]string
	signal.Effect(func() { _, _ = a(), b() })
	un := signal.AfterEffects(func() { snapshots = append(snapshots, [2]string{a(), b()}) })
	defer un()

	signal.Batch(func() {
		a("A2")
		b("B2")
	})
	if len(snapshots) != 1 {
		t.Fatalf("Batch 应只触发 1 次 hook, got %d", len(snapshots))
	}
	if snapshots[0] != [2]string{"A2", "B2"} {
		t.Fatalf("hook 应读到最后一致状态 A2/B2, got %v", snapshots[0])
	}
}

// effect 回调内写另一个 signal 形成嵌套边界：hook 只在最外层触发一次，
// 避免绘制出「半成品」帧。
func TestAfterEffectsNestedWriteFiresOnce(t *testing.T) {
	src := signal.New(0)
	derived := signal.New(0)
	signal.Effect(func() {
		v := src()
		derived(v * 2) // effect 内写信号 → 递归边界
	})
	var fires int
	var last int
	un := signal.AfterEffects(func() { fires++; last = derived() })
	defer un()

	src(3)
	if fires != 1 {
		t.Fatalf("嵌套写应只触发 1 次 hook, got %d", fires)
	}
	if last != 6 {
		t.Fatalf("hook 应读到嵌套写完成后的最终值 6, got %d", last)
	}
}

// 注销后不再触发。
func TestAfterEffectsUnregister(t *testing.T) {
	c := signal.New(0)
	signal.Effect(func() { _ = c() })
	fires := 0
	un := signal.AfterEffects(func() { fires++ })
	un()
	c(1)
	if fires != 0 {
		t.Fatalf("注销后不应触发, got %d", fires)
	}
}
