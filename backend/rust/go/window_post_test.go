package rustbackend

import (
	"sync"
	"testing"
)

// Post 任务只入队、不在调用栈执行；drainPosts 按 FIFO 串行执行并清空队列。
func TestPostQueuesUntilDrain(t *testing.T) {
	w := NewWindow()
	var order []int
	var mu sync.Mutex
	for i := 0; i < 3; i++ {
		i := i
		if err := w.Post(func() {
			mu.Lock()
			order = append(order, i)
			mu.Unlock()
		}); err != nil {
			t.Fatalf("Post(%d) 返回错误: %v", i, err)
		}
	}
	// Post 绝不在调用栈执行：入队后立刻断言队列仍为空执行结果。
	mu.Lock()
	if len(order) != 0 {
		t.Fatalf("Post 不应在调用栈执行任务, 已执行 %v", order)
	}
	mu.Unlock()

	w.drainPosts()

	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 {
		t.Fatalf("drainPosts 应执行全部 3 个任务, got %v", order)
	}
	for i, v := range order {
		if v != i {
			t.Fatalf("任务应按 FIFO 执行, got %v", order)
		}
	}
}

// drain 期间新到的任务不丢失：回调内再入队的任务在同一轮 drain 内继续取走
// （drain-until-empty，与 loop.Queue 的清空语义一致，微任务不清空会饿死）。
func TestPostReentrantDuringDrain(t *testing.T) {
	w := NewWindow()
	done := make(chan struct{})
	if err := w.Post(func() {
		// 第一批任务里再入队一个（模拟绘制回调触发微任务）。
		_ = w.Post(func() { close(done) })
	}); err != nil {
		t.Fatalf("Post 返回错误: %v", err)
	}
	w.drainPosts() // 执行第一批；再入队任务在同一轮 drain 内继续消费
	select {
	case <-done:
	default:
		t.Fatal("再入队任务应在同一轮 drain 内执行（清空语义）")
	}
	if len(w.postQ) != 0 {
		t.Fatalf("drain 后队列应已清空, len=%d", len(w.postQ))
	}
}

// 零值/未初始化窗口与已关闭窗口：Post 必须拒绝而不是 panic 或静默排队。
func TestPostRejectsInvalidStates(t *testing.T) {
	var zero Window
	if err := zero.Post(func() {}); err == nil {
		t.Fatal("未初始化 Window（零值）的 Post 应返回错误")
	}
	if err := zero.Post(nil); err == nil {
		t.Fatal("nil 回调应返回错误")
	}

	w := NewWindow()
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	if err := w.Post(func() {}); err == nil {
		t.Fatal("已关闭窗口的 Post 应返回错误")
	}
}

// drainPosts 幂等：空队列重复调用不 panic、不执行任何任务。
func TestDrainPostsIdempotent(t *testing.T) {
	w := NewWindow()
	w.drainPosts()
	w.drainPosts()
	ran := false
	if err := w.Post(func() { ran = true }); err != nil {
		t.Fatalf("Post 返回错误: %v", err)
	}
	w.drainPosts()
	if !ran {
		t.Fatal("任务应被执行一次")
	}
	w.drainPosts()
	if w.postQ != nil && len(w.postQ) != 0 {
		t.Fatalf("drain 后队列应已清空, len=%d", len(w.postQ))
	}
}
