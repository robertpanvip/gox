// P4 验收测试：UIPromise 状态机、线性化投递与并发行为（提案 §7.3/§7.5/§12 P4）。

package loop

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func drainAll(l *Queue) {
	for l.RunOneMacro() {
	}
	for {
		if rep := l.RunMicrotasks(); rep.Ran == 0 {
			break
		}
	}
}

func TestPromiseThenRunsInUIContext(t *testing.T) {
	l := New(Config{})
	var ran bool
	q := Then(Resolved(l, 21), func(v int) (int, error) { ran = true; return v * 2, nil })
	if ran {
		t.Fatal("Then 回调在调用栈内同步执行（违反 §7.5 前提）")
	}
	drainAll(l)
	if !ran {
		t.Fatal("Then 回调未在 UI 上下文执行")
	}
	var got int
	_ = Then(q, func(v int) (int, error) { got = v; return v, nil })
	drainAll(l)
	if got != 42 {
		t.Fatalf("链式传递 got=%d want 42", got)
	}
}

func TestMicroCheckpointPlacement(t *testing.T) {
	l := New(Config{})
	var order []string
	l.Post(func() { order = append(order, "macro1") })
	_ = Then(Resolved(l, 1), func(v int) (int, error) { order = append(order, "then"); return v, nil })
	l.Post(func() { order = append(order, "macro2") })
	l.RunOneMacro()
	if strings.Contains(fmt.Sprint(order), "then") {
		t.Fatalf("continuation 是微任务，不应先于检查点执行: %v", order)
	}
	l.RunMicrotasks()
	l.RunOneMacro()
	want := []string{"macro1", "then", "macro2"}
	if fmt.Sprint(order) != fmt.Sprint(want) {
		t.Fatalf("顺序=%v want %v", order, want)
	}
}

func TestSettleOnceFirstWins(t *testing.T) {
	l := New(Config{})
	p := newPromise[int](l)
	p.fulfill(1)
	p.reject(errors.New("late"))
	st, v, e := p.terminal()
	if st != stateFulfilled || v != 1 || e != nil {
		t.Fatalf("重复 settle 应无效: st=%d v=%d e=%v", st, v, e)
	}
}

func TestThenBeforeAndAfterSettle(t *testing.T) {
	l := New(Config{})
	p := newPromise[int](l)
	var before, after int32
	_ = Then(p, func(v int) (int, error) { before = int32(v); return v, nil })
	if before != 0 {
		t.Fatal("pending 注册不应立即执行")
	}
	p.fulfill(7)
	drainAll(l)
	if before != 7 {
		t.Fatalf("settle 前注册未执行: %d", before)
	}
	_ = Then(p, func(v int) (int, error) { after = int32(v); return v, nil })
	if after != 0 {
		t.Fatal("已 settle 的注册也必须经投递，不得同步执行")
	}
	drainAll(l)
	if after != 7 {
		t.Fatalf("settle 后注册未执行: %d", after)
	}
}

func TestMultipleThenRegistrationOrder(t *testing.T) {
	l := New(Config{})
	p := newPromise[int](l)
	var seq []string
	for i := 0; i < 5; i++ {
		s := fmt.Sprint(i)
		_ = Then(p, func(v int) (int, error) { seq = append(seq, s); return v, nil })
	}
	p.fulfill(0)
	drainAll(l)
	if fmt.Sprint(seq) != fmt.Sprint([]string{"0", "1", "2", "3", "4"}) {
		t.Fatalf("注册顺序违例: %v", seq)
	}
}

func TestThenErrorPropagates(t *testing.T) {
	l := New(Config{})
	sentinel := errors.New("boom")
	q := Then(Resolved(l, 1), func(v int) (int, error) { return 0, sentinel })
	drainAll(l)
	st, _, e := q.terminal()
	if st != stateRejected || !errors.Is(e, sentinel) {
		t.Fatalf("Then 错误未传播: %v", e)
	}
}

func TestCatchRecoverAndRethrow(t *testing.T) {
	l := New(Config{})
	e1 := errors.New("e1")
	q := Catch(Rejected[int](l, e1), func(err error) (int, error) {
		if !errors.Is(err, e1) {
			t.Errorf("Catch 收到错误不符: %v", err)
		}
		return 99, nil
	})
	drainAll(l)
	if st, v, e := q.terminal(); st != stateFulfilled || v != 99 || e != nil {
		t.Fatalf("Catch 恢复失败: st=%d v=%d e=%v", st, v, e)
	}
	e2 := errors.New("e2")
	q2 := Catch(Rejected[int](l, e1), func(err error) (int, error) { return 0, e2 })
	drainAll(l)
	if st, _, e := q2.terminal(); st != stateRejected || !errors.Is(e, e2) {
		t.Fatalf("Catch 再失败未传播: %v", e)
	}
	q3 := Catch(Resolved(l, 5), func(err error) (int, error) { t.Error("成功不应进 Catch"); return 0, nil })
	drainAll(l)
	if st, v, _ := q3.terminal(); st != stateFulfilled || v != 5 {
		t.Fatalf("成功透传失败: st=%d", st)
	}
}

func TestFinallySemantics(t *testing.T) {
	l := New(Config{})
	var hit int32
	fe := errors.New("finally-boom")
	a := Finally(Resolved(l, 5), func() error { atomic.AddInt32(&hit, 1); return nil })
	drainAll(l)
	if st, v, _ := a.terminal(); st != stateFulfilled || v != 5 {
		t.Fatal("Finally 应保留成功值")
	}
	e1 := errors.New("orig")
	b := Finally(Rejected[int](l, e1), func() error { atomic.AddInt32(&hit, 1); return nil })
	drainAll(l)
	if st, _, e := b.terminal(); st != stateRejected || !errors.Is(e, e1) {
		t.Fatal("Finally 应保留原错误")
	}
	c := Finally(Resolved(l, 5), func() error { atomic.AddInt32(&hit, 1); return fe })
	drainAll(l)
	if st, _, e := c.terminal(); st != stateRejected || !errors.Is(e, fe) {
		t.Fatal("Finally 自身失败应成为下游失败原因")
	}
	if hit != 3 {
		t.Fatalf("finally 执行次数=%d want 3", hit)
	}
}

func TestCallbackPanicBecomesError(t *testing.T) {
	l := New(Config{})
	q := Then(Resolved(l, 1), func(v int) (int, error) { panic("cb-boom") })
	drainAll(l) // panic 不得逃出宿主 drain
	st, _, e := q.terminal()
	if st != stateRejected || !errors.Is(e, ErrCallbackPanic) {
		t.Fatalf("panic 未转下游错误: %v", e)
	}
	var after int32
	l.Post(func() { atomic.AddInt32(&after, 1) })
	l.RunOneMacro()
	if after != 1 {
		t.Fatal("panic 后 Loop 应继续")
	}
	if l.Stats().Panics != 0 {
		t.Fatalf("回调内 recover 不应记入 Loop Panic 计数: %d", l.Stats().Panics)
	}
}

func TestExecutorClosedPropagates(t *testing.T) {
	l := New(Config{})
	l.Stop()
	var ran int32
	q := Then(Resolved(l, 1), func(v int) (int, error) { atomic.AddInt32(&ran, 1); return v, nil })
	if atomic.LoadInt32(&ran) != 0 {
		t.Fatal("关闭后不得执行回调")
	}
	if st, _, e := q.terminal(); st != stateRejected || !errors.Is(e, ErrClosed) {
		t.Fatalf("关闭投递错误未传播: %v", e)
	}
	p := newPromise[int](l)
	q2 := Then(p, func(v int) (int, error) { atomic.AddInt32(&ran, 1); return v, nil })
	p.fulfill(1)
	if st, _, e := q2.terminal(); st != stateRejected || !errors.Is(e, ErrClosed) {
		t.Fatalf("settle 时投递失败未传播: %v", e)
	}
	if atomic.LoadInt32(&ran) != 0 {
		t.Fatal("被关闭的 continuation 不得执行")
	}
}

func TestThenSettleConcurrentLinearized(t *testing.T) {
	l := New(Config{})
	p := newPromise[int](l)
	var ran int64
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = Then(p, func(v int) (int, error) { atomic.AddInt64(&ran, 1); return v, nil })
		}()
	}
	var swg sync.WaitGroup
	swg.Add(1)
	go func() {
		defer swg.Done()
		p.fulfill(1)
		p.reject(errors.New("should-be-noop"))
	}()
	wg.Wait()
	swg.Wait()
	drainAll(l)
	if atomic.LoadInt64(&ran) != 8 {
		t.Fatalf("continuation 执行不全: %d/8", ran)
	}
	if st, v, e := p.terminal(); st != stateFulfilled || v != 1 || e != nil {
		t.Fatalf("settle 应恰好成功一次: st=%d v=%d e=%v", st, v, e)
	}
}

func TestGoWorkerToUIContext(t *testing.T) {
	l := New(Config{})
	done := make(chan struct{})
	got := 0
	_ = Then(Go(l, func() (int, error) { time.Sleep(5 * time.Millisecond); return 21, nil }),
		func(v int) (int, error) { got = v; close(done); return v, nil })
	timeout := time.After(3 * time.Second)
	for {
		select {
		case <-done:
			if got != 21 {
				t.Fatalf("got=%d want 21", got)
			}
			return
		case <-timeout:
			t.Fatal("Go 结果未回到 UI 上下文")
		default:
			drainAll(l)
			time.Sleep(time.Millisecond)
		}
	}
}

func TestGoRejectAndPanic(t *testing.T) {
	l := New(Config{})
	e1 := errors.New("worker-fail")
	q := Go(l, func() (int, error) { return 0, e1 })
	waitSettled(t, q)
	if st, _, e := q.terminal(); st != stateRejected || !errors.Is(e, e1) {
		t.Fatalf("worker 错误未 reject: %v", e)
	}
	var cb int32
	c := Catch(q, func(err error) (int, error) { atomic.AddInt32(&cb, 1); return 7, nil })
	drainAll(l)
	if st, v, _ := c.terminal(); st != stateFulfilled || v != 7 || cb != 1 {
		t.Fatal("worker 错误未进 Catch 恢复")
	}
	// worker panic → reject（ErrCallbackPanic）
	qp := Go(l, func() (int, error) { panic("worker-boom") })
	waitSettled(t, qp)
	if st, _, e := qp.terminal(); st != stateRejected || !errors.Is(e, ErrCallbackPanic) {
		t.Fatalf("worker panic 未转错误: %v", e)
	}
}

func waitSettled(t *testing.T, p *UIPromise[int]) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if st, _, _ := p.terminal(); st != statePending {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("worker 未 settle")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
