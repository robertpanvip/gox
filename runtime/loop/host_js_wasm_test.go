//go:build js && wasm

// P3 宿主契约验证（提案 §5.2 / §12 P3）：把 loop 的关键调度契约放进真实的
// Go/js-wasm 运行时（node 宿主事件循环，setTimeout/queueMicrotask 语义与浏览器
// 同族）验证：
//   - Post/PostMicro 只入队，回调一律在宿主轮次执行，不在调用栈内同步执行；
//   - 微任务检查点在宏任务之后、同一宿主轮次内收尾；
//   - UIPromise continuation 经宿主轮次调度，注册处不同步执行。
//
// 边界声明：浏览器 requestAnimationFrame 的精确时序、wasm 流式加载、页面关闭
// 行为仍属浏览器集成测试范围，沙箱不具备真实浏览器——按提案「只在实际验证通过
// 的平台能力上声明语义」，这些项记录在 docs/ui-loop-p3-web.md 的待验收清单，
// 本文件不做任何未验证声明。

package loop

import (
	"sync"
	"syscall/js"
	"testing"
	"time"
)

// scheduleTurn 用宿主 setTimeout(0) 把回调排到下一轮宏任务——与浏览器用
// postMessage/rAF 唤醒宿主循环的模式同构；回调执行完自动 Release。
func scheduleTurn(fn func()) {
	var cb js.Func
	cb = js.FuncOf(func(this js.Value, args []js.Value) any {
		cb.Release()
		fn()
		return nil
	})
	js.Global().Call("setTimeout", cb, 0)
}

// hostDrainTurn 按提案 §4.1 的宿主节奏排空一轮：一个宏任务 → 一次微任务
// 检查点，直到无可执行任务。只在宿主回调（新 goroutine）里进入 Go。
func hostDrainTurn(l *Queue) {
	for {
		ran := l.RunOneMacro()
		rep := l.RunMicrotasks()
		if !ran && rep.Ran == 0 && rep.Remaining == 0 {
			return
		}
	}
}

// newWasmHostLoop 创建一个以宿主事件循环为宿主的 Queue：Wake 安排一次
// setTimeout(0) 轮次来 drain。
func newWasmHostLoop() *Queue {
	l := New(Config{})
	l.cfg.Wake = func() { scheduleTurn(func() { hostDrainTurn(l) }) }
	return l
}

func TestWasmPostRunsOnlyInHostTurn(t *testing.T) {
	l := newWasmHostLoop()
	ack := make(chan int, 4)

	if err := l.Post(func() { ack <- 1 }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ack:
	case <-time.After(10 * time.Second):
		t.Fatal("Wake→setTimeout 宿主轮次未执行宏任务")
	}

	// 调用栈契约：Post 返回时回调尚未执行（select-default 在 Go 栈内，
	// 宿主回调此时不可能进入 Go）。
	if err := l.Post(func() { ack <- 2 }); err != nil {
		t.Fatal(err)
	}
	select {
	case v := <-ack:
		t.Fatalf("Post 在调用栈内同步执行了回调: %d", v)
	default:
	}
	select {
	case v := <-ack:
		if v != 2 {
			t.Fatalf("宿主轮次乱序: %d", v)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("第二个宏任务未在宿主轮次执行")
	}
}

func TestWasmMicroCheckpointOrder(t *testing.T) {
	l := newWasmHostLoop()
	var mu sync.Mutex
	var order []string
	done := make(chan struct{})

	push := func(s string) {
		mu.Lock()
		order = append(order, s)
		mu.Unlock()
	}
	l.Post(func() { push("macro") })
	l.PostMicro(func() { push("micro"); close(done) })

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("宿主轮次未收尾微任务")
	}
	mu.Lock()
	defer mu.Unlock()
	if order[0] != "macro" || order[1] != "micro" {
		t.Fatalf("微任务检查点违序（应宏→微）: %v", order)
	}
}

func TestWasmPromiseContinuationHostTurn(t *testing.T) {
	l := newWasmHostLoop()
	ack := make(chan int, 1)
	q := Then(Resolved(l, 21), func(v int) (int, error) { ack <- v * 2; return v * 2, nil })
	_ = q

	select {
	case <-ack:
		t.Fatal("continuation 在注册调用栈内同步执行")
	default:
	}
	select {
	case v := <-ack:
		if v != 42 {
			t.Fatalf("continuation 值错误: %d", v)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("continuation 未在宿主轮次执行")
	}
}

func TestWasmStopRejectsAndDrops(t *testing.T) {
	l := newWasmHostLoop()
	l.Stop()
	if err := l.Post(func() { t.Error("Stop 后任务不应执行") }); err != ErrClosed {
		t.Fatalf("Stop 后 Post=%v want ErrClosed", err)
	}
	if err := l.PostMicro(func() {}); err != ErrClosed {
		t.Fatalf("Stop 后 PostMicro=%v want ErrClosed", err)
	}
}
