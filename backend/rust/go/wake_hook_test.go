package rustbackend

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robertpanvip/gox-core/runtime/scheduler"
)

// ---- P4 唤醒注入验收：帧时钟 wake 绝不阻塞（fpsbench 第二帧死锁回归） ----
//
// 历史 bug：FrameClock 的 wake 曾用阻塞发送写 w.pump。P4 目标态移除了自驱
// Run（pump 无读者），第一帧填满 1 容量缓冲后，第二帧 RAF 续帧的
// RequestFrame → wake 永久阻塞 UI 线程。本组测试固化「wake 只提示、不等待」。

// 高频 RequestFrame 无读者不阻塞：无 wakeHook、pump 缓冲填满后继续请求。
func TestFrameClockWakeNeverBlocks(t *testing.T) {
	w := NewWindow()
	clock := w.FrameClock().(*scheduler.LoopClock)
	clock.OnFrame(func(ft scheduler.FrameTime) {}) // 消费 pending，模拟 RAF 续帧

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10000; i++ {
			clock.RequestFrame() // 每 2 次 Poll 才清 pending；高频请求必命中已满通道
			clock.Poll()
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RequestFrame/Poll 循环阻塞——wake 仍在等待无读者的 pump")
	}
}

// pump 缓冲已满时 wakeHost 也不阻塞（非阻塞退化路径）。
func TestWakeHostFullPumpDoesNotBlock(t *testing.T) {
	w := NewWindow()
	// 预填满容量 1 的 pump 缓冲（无读者）；wakeHost 内部 select/default
	// 应丢弃本次唤醒信号而非阻塞。
	w.pump <- struct{}{}

	done := make(chan struct{})
	go func() {
		w.wakeHost()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("wakeHost 在 pump 已满时阻塞")
	}
}

// SetWakeHook 注入后：RequestFrame 触发 hook（帧调度即时唤醒主循环），
// Poll 消费 pending 后再次 RequestFrame 再次触发。
func TestWakeHookInvokedByRequestFrame(t *testing.T) {
	w := NewWindow()
	var calls atomic.Int64
	w.SetWakeHook(func() { calls.Add(1) })

	clock := w.FrameClock().(*scheduler.LoopClock)
	clock.OnFrame(func(ft scheduler.FrameTime) {})
	clock.RequestFrame() // pending 无→有：必须唤醒
	if clock.Poll() != true {
		t.Fatal("pending 帧应派发一次 OnFrame")
	}
	clock.RequestFrame() // 新周期再次唤醒
	clock.Poll()

	if n := calls.Load(); n != 2 {
		t.Fatalf("wakeHook 应被调用 2 次，实际 %d", n)
	}
}

// SetWakeHook(nil) 解除注入：退化为非阻塞 pump 信号。
func TestWakeHookClearFallsBackNonBlocking(t *testing.T) {
	w := NewWindow()
	w.SetWakeHook(func() {})
	w.SetWakeHook(nil)

	done := make(chan struct{})
	var mu sync.Mutex
	started := false
	go func() {
		mu.Lock()
		started = true
		mu.Unlock()
		w.wakeHost()
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	s := started
	mu.Unlock()
	if !s {
		t.Fatal("goroutine 未启动")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("wakeHost(nil hook) 阻塞")
	}
}
