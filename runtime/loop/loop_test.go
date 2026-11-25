// P1 验收测试：对应提案 §12「Loop 核心与队列」全部条目。
//   - 多 goroutine 并发 Post；Post 与 Stop 并发
//   - Stop 后 Post 返回 ErrClosed
//   - 宏任务与微任务的顺序；微任务中继续 PostMicro
//   - 微任务预算耗尽后剩余任务仍可继续处理
//   - Post 不在调用栈内同步执行回调
//   - go test -race 无数据竞争；队列无丢失、无重复执行

package loop

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFifoOrder(t *testing.T) {
	l := New(Config{})
	var got []string
	for i := 0; i < 5; i++ {
		s := fmt.Sprint("m", i)
		l.Post(func() { got = append(got, s) })
	}
	for l.RunOneMacro() {
	}
	if len(got) != 5 {
		t.Fatalf("宏任务执行数=%d want 5", len(got))
	}
	for i, s := range got {
		if want := fmt.Sprint("m", i); s != want {
			t.Fatalf("宏任务次序 %d: %s want %s", i, s, want)
		}
	}
	if l.Stats().RanMacro != 5 {
		t.Fatalf("RanMacro=%d want 5", l.Stats().RanMacro)
	}
}

func TestMacroMicroCheckpointOrder(t *testing.T) {
	l := New(Config{})
	var seq []string
	l.Post(func() { seq = append(seq, "A") })
	l.PostMicro(func() { seq = append(seq, "MICRO") })
	l.Post(func() { seq = append(seq, "B") })
	// 宿主规则：一个宏任务 → 微任务检查点 → 下一个宏任务
	l.RunOneMacro()
	l.RunMicrotasks()
	l.RunOneMacro()
	want := []string{"A", "MICRO", "B"}
	if fmt.Sprint(seq) != fmt.Sprint(want) {
		t.Fatalf("检查点顺序=%v want %v", seq, want)
	}
}

func TestPostMicroInsideDrainSameBatch(t *testing.T) {
	l := New(Config{})
	var seq []string
	l.PostMicro(func() {
		seq = append(seq, "1")
		l.PostMicro(func() { seq = append(seq, "2") }) // drain 期间新增 → 同批 FIFO
	})
	rep := l.RunMicrotasks()
	if rep.Ran != 2 || !rep.Complete() {
		t.Fatalf("同批收尾失败: %+v", rep)
	}
	if fmt.Sprint(seq) != fmt.Sprint([]string{"1", "2"}) {
		t.Fatalf("微任务顺序=%v", seq)
	}
}

func TestMicroBudgetExhaustion(t *testing.T) {
	l := New(Config{MaxMicrotasksPerDrain: 3})
	n := 0
	inc := func() { l.PostMicro(func() { n++ }) }
	inc()
	inc()
	inc()
	// 队列共 4（3 个常规 + 1 个会再追加），预算 3：首批 Ran=3、积压 1
	l.PostMicro(func() {
		n++
		l.PostMicro(func() { n++ })
	})
	rep := l.RunMicrotasks()
	if rep.Ran != 3 || !rep.HitBudget || rep.Remaining != 1 {
		t.Fatalf("预算中止异常: %+v", rep)
	}
	if rep.Complete() {
		t.Fatal("有积压时不得报告检查点完整结束")
	}
	// 续 drain：追加者在第二批执行，其新增的同批收尾 → Ran=2、全部完成
	rep2 := l.RunMicrotasks()
	if rep2.Ran != 2 || !rep2.Complete() {
		t.Fatalf("剩余任务续处理失败: %+v", rep2)
	}
	if n != 5 {
		t.Fatalf("执行总数=%d want 5", n)
	}
}

func TestStopSemantics(t *testing.T) {
	l := New(Config{})
	var ran int32
	l.Post(func() { atomic.AddInt32(&ran, 1) }) // 入队后不 drain
	l.PostMicro(func() { atomic.AddInt32(&ran, 1) })
	if err := l.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := l.Stop(); err != nil {
		t.Fatalf("Stop 非幂等: %v", err)
	}
	if err := l.Post(func() {}); err != ErrClosed {
		t.Fatalf("Stop 后 Post=%v want ErrClosed", err)
	}
	if err := l.PostMicro(func() {}); err != ErrClosed {
		t.Fatalf("Stop 后 PostMicro=%v want ErrClosed", err)
	}
	if atomic.LoadInt32(&ran) != 0 {
		t.Fatal("被丢弃任务不得执行")
	}
	if s := l.Stats(); s.Dropped != 2 {
		t.Fatalf("Dropped=%d want 2", s.Dropped)
	}
	if l.RunOneMacro() {
		t.Fatal("关闭后不应有宏任务可执行")
	}
	if rep := l.RunMicrotasks(); rep.Ran != 0 {
		t.Fatalf("关闭后 drain=%+v", rep)
	}
}

func TestPostNotSyncExecuted(t *testing.T) {
	l := New(Config{})
	var flag atomic.Bool
	if err := l.Post(func() { flag.Store(true) }); err != nil {
		t.Fatal(err)
	}
	if flag.Load() {
		t.Fatal("Post 在调用栈内同步执行了回调，违反契约（UIPromise 线性化依赖此条）")
	}
	if err := l.PostMicro(func() { flag.Store(true) }); err != nil {
		t.Fatal(err)
	}
	if flag.Load() {
		t.Fatal("PostMicro 同步执行了回调")
	}
}

func TestConcurrentPostNoLossNoDup(t *testing.T) {
	l := New(Config{})
	const G, N = 50, 200
	var executed int64
	var wg sync.WaitGroup
	for g := 0; g < G; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < N; i++ {
				l.Post(func() { atomic.AddInt64(&executed, 1) })
			}
		}()
	}
	wg.Wait()
	// 生产者已停止，宿主式 drain 清空
	for l.RunOneMacro() {
	}
	if executed != G*N {
		t.Fatalf("执行数=%d want %d（丢失或重复）", executed, G*N)
	}
	if m, mi := l.HasPending(); m != 0 || mi != 0 {
		t.Fatalf("应无积压: macro=%d micro=%d", m, mi)
	}
	s := l.Stats()
	if s.PostedMacro != int64(G*N) || s.RanMacro != int64(G*N) {
		t.Fatalf("计数不守恒: %+v", s)
	}
}

// Post 与 Stop 并发（提案 §14「Post 与 Stop 竞态」）：每次 Post 要么被接收
// （随后被执行、或被 Stop 丢弃并计数），要么返回 ErrClosed。
// 注意测试形态：持续多生产者 + 单消费者时「drain 到队列空」不可达（活锁），
// 宿主真实语义就是有界 drain——这里主线程做固定次数 drain 后 Stop，
// 生产者在收到 ErrClosed 时退出，断言任务守恒。
func TestPostStopRaceDeterministic(t *testing.T) {
	l := New(Config{})
	var executed int64
	var accepted int64
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				if err := l.Post(func() { atomic.AddInt64(&executed, 1) }); err != nil {
					if err != ErrClosed {
						t.Errorf("Stop 后应恰好得到 ErrClosed，实际: %v", err)
					}
					return // 被关闭：确定性的退出路径
				}
				atomic.AddInt64(&accepted, 1)
			}
		}()
	}
	// 与生产并发地进行有界宿主 drain，然后关闭
	for i := 0; i < 500; i++ {
		l.RunOneMacro()
	}
	if err := l.Stop(); err != nil {
		t.Fatal(err)
	}
	wg.Wait() // 所有生产者都会因 ErrClosed 退出
	s := l.Stats()
	if s.PostedMacro != atomic.LoadInt64(&accepted) {
		t.Fatalf("接收计数不一致: accepted=%d posted=%d", accepted, s.PostedMacro)
	}
	if s.RanMacro+s.Dropped != s.PostedMacro {
		t.Fatalf("任务不守恒: ran=%d dropped=%d posted=%d", s.RanMacro, s.Dropped, s.PostedMacro)
	}
	if atomic.LoadInt64(&executed) != s.RanMacro {
		t.Fatalf("执行与计数不符: ran=%d executed=%d", s.RanMacro, executed)
	}
}

func TestWakeOnlyWhenPendingEmptyToNonEmpty(t *testing.T) {
	var wakes int32
	l := New(Config{Wake: func() { atomic.AddInt32(&wakes, 1) }})
	l.Post(func() {})
	l.Post(func() {})
	l.PostMicro(func() {})
	if atomic.LoadInt32(&wakes) != 1 {
		t.Fatalf("空→非空应只唤醒一次: %d", wakes)
	}
	for l.RunOne() {
	} // 清到全空
	if atomic.LoadInt32(&wakes) != 1 {
		t.Fatal("drain 不应产生额外唤醒")
	}
	l.Post(func() {}) // 再次空→非空
	if atomic.LoadInt32(&wakes) != 2 {
		t.Fatalf("重新入队应再次唤醒: %d", wakes)
	}
	l.Stop()
	l.Post(func() {}) // 关闭后不唤醒
	if atomic.LoadInt32(&wakes) != 2 {
		t.Fatal("Stop 后不得再唤醒")
	}
}

func TestPanicRecoveredAndIsolated(t *testing.T) {
	var got []TaskInfo
	l := New(Config{OnTaskPanic: func(ti TaskInfo, r any) { got = append(got, ti) }})
	var after int32
	l.Post(func() { panic("boom") })
	l.Post(func() { atomic.AddInt32(&after, 1) })
	for l.RunOneMacro() {
	}
	if len(got) != 1 || got[0].Kind != KindMacro {
		t.Fatalf("panic 钩子未收到宏任务信息: %v", got)
	}
	if atomic.LoadInt32(&after) != 1 {
		t.Fatal("panic 后 Loop 应继续处理后续任务")
	}
	if l.Stats().Panics != 1 {
		t.Fatalf("Panics=%d want 1", l.Stats().Panics)
	}
	// 微任务 panic 同样隔离且不中断 drain
	var microAfter int32
	l.PostMicro(func() { panic("micro-boom") })
	l.PostMicro(func() { atomic.AddInt32(&microAfter, 1) })
	rep := l.RunMicrotasks()
	if rep.Ran != 2 || atomic.LoadInt32(&microAfter) != 1 {
		t.Fatalf("微任务 panic 未隔离: %+v", rep)
	}
	if l.Stats().Panics != 2 {
		t.Fatalf("Panics=%d want 2", l.Stats().Panics)
	}
}

func TestRunMicrotasksReentrantSafe(t *testing.T) {
	l := New(Config{})
	var seq []string
	l.PostMicro(func() {
		seq = append(seq, "outer")
		rep := l.RunMicrotasks() // 嵌套调用：不得开启第二层 drain
		if rep.Ran != 0 {
			t.Error("嵌套 drain 应被重入保护拒绝")
		}
		l.PostMicro(func() { seq = append(seq, "inner") })
	})
	l.RunMicrotasks()
	if fmt.Sprint(seq) != fmt.Sprint([]string{"outer", "inner"}) {
		t.Fatalf("顺序异常: %v", seq)
	}
}

func TestTimeBudget(t *testing.T) {
	l := New(Config{MicroDrainTimeBudget: 20 * time.Millisecond})
	l.PostMicro(func() { time.Sleep(30 * time.Millisecond) })
	l.PostMicro(func() {})
	rep := l.RunMicrotasks()
	if rep.Ran != 1 || !rep.HitBudget || rep.Remaining != 1 {
		t.Fatalf("时间预算未生效: %+v", rep)
	}
}
