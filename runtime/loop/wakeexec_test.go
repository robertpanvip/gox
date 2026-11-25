package loop

import (
	"sync/atomic"
	"testing"
	"time"
)

// TestWakeExecutorWakesAndDrains 验证：入队触发 wake；continuation 只在 drain 后执行，
// 不在 Post 的调用栈同步执行。
func TestWakeExecutorWakesAndDrains(t *testing.T) {
	var wakes int64
	exec := NewWakeExecutor(func() { atomic.AddInt64(&wakes, 1) })
	defer exec.Stop()

	var ran int64
	p := Go(exec, func() (int, error) { return 42, nil })
	Then(p, func(v int) (int, error) {
		atomic.AddInt64(&ran, int64(v))
		return v, nil
	})

	// 等 worker goroutine settle 并把 continuation 投递进微任务队列。
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_, micro := exec.HasPending()
		if micro > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// drain 前 continuation 尚未执行（Post 不同步跑）。
	if got := atomic.LoadInt64(&ran); got != 0 {
		t.Fatalf("drain 前回调不应执行，ran=%d", got)
	}
	if atomic.LoadInt64(&wakes) == 0 {
		t.Fatal("入队应触发 wake")
	}

	_ = exec.DrainTurn()
	if got := atomic.LoadInt64(&ran); got != 42 {
		t.Fatalf("drain 后回调应执行且 v=42，ran=%d", got)
	}
}

// TestWakeExecutorPostAndPostMicroOrder 验证宏/微同轮 FIFO：宏先、微后。
func TestWakeExecutorPostAndPostMicroOrder(t *testing.T) {
	exec := NewWakeExecutor(nil)
	defer exec.Stop()

	var order []int
	_ = exec.Post(func() { order = append(order, 1) })
	_ = exec.PostMicro(func() { order = append(order, 2) })
	_ = exec.Post(func() { order = append(order, 3) })

	// DrainTurn：执行一个宏(1) → 微检查点(2) → 再一个宏(3) → 微检查点。
	exec.DrainTurn()
	if len(order) < 3 || order[0] != 1 || order[1] != 2 || order[2] != 3 {
		t.Fatalf("宏微顺序错误: %v", order)
	}
}

// TestWakeExecutorStopRejects 验证 Stop 后投递被拒。
func TestWakeExecutorStopRejects(t *testing.T) {
	exec := NewWakeExecutor(nil)
	if err := exec.Post(func() {}); err != nil {
		t.Fatalf("关闭前 Post 应成功: %v", err)
	}
	_ = exec.Stop()
	if err := exec.Post(func() {}); err != ErrClosed {
		t.Fatalf("关闭后 Post 应返回 ErrClosed，实际 %v", err)
	}
	if err := exec.PostMicro(func() {}); err != ErrClosed {
		t.Fatalf("关闭后 PostMicro 应返回 ErrClosed，实际 %v", err)
	}
}
