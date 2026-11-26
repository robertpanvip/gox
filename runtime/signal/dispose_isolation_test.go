package signal

import (
	"errors"
	"testing"
)

// 销毁期异常隔离回归（评审 P1-01）：
//
// 旧行为：Dispose 三阶段（computation / 子 Owner / owner cleanup）任何一个用户
// cleanup panic 都会直接中断整个销毁，而 comps/children/cleanups 清单已从 Owner
// 上摘除——被跳过的 effect 不会被再次销毁、子 Owner 清理永久丢失，且 sync.Once
// / disposed 标记保证「再次 Dispose 也无法补做」。
//
// 新行为：三阶段全部单点捕获收集，所有资源尽力释放完毕后统一上报（单异常保真
// re-panic，多个包装成 disposeErrors）。

// TestOwnerDisposeIsolatesCleanupPanic：多源 panic 汇总上报，其余资源全部释放。
// 场景对照评审给出的树：Effect A（cleanup panic）、Effect B、Child Owner（cleanup
// panic）、Root cleanup（panic）——四类资源都要被释放，任何一类不得因其他类的
// panic 而被跳过。
func TestOwnerDisposeIsolatesCleanupPanic(t *testing.T) {
	a, b := New(0), New(0)
	var runsA, runsB int
	var childCleanup, rootCleanup2, rootCleanup3 bool

	var root, child *Owner
	CreateRoot(func(o *Owner) {
		root = o
		Effect(func() { runsA++; _ = a() })
		OnCleanup(func() { panic("cleanup-A") }) // Effect A 的 cleanup：panic
		Effect(func() { runsB++; _ = b() })
		child = newChildOwner(o)
		RunWithOwner(child, func() {
			OnCleanup(func() { childCleanup = true; panic("cleanup-child") })
		})
		OnCleanup(func() { panic("cleanup-root-1") }) // Root cleanup：panic
		OnCleanup(func() { rootCleanup2 = true })                  // Root cleanup：正常
		OnCleanup(func() { rootCleanup3 = true })                  // Root cleanup：正常
	})

	if runsA != 1 || runsB != 1 {
		t.Fatalf("两个 Effect 应各首跑一次, A=%d B=%d", runsA, runsB)
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Dispose 应把收集到的 panic 统一上报，而不是静默吞掉")
		}
		de, ok := r.(disposeErrors)
		if !ok {
			t.Fatalf("多个 panic 应汇总为 disposeErrors, got %T: %v", r, r)
		}
		if len(de) != 3 {
			t.Fatalf("应收集到 3 个 panic（cleanup-A/child/root-1）, got %d: %v", len(de), de)
		}

		// —— 资源释放断言：panic 不得阻断任何一类清理 ——
		// 1. panic 的 Effect A 已销毁：写 a 不再触发重跑。
		a(100)
		if runsA != 1 {
			t.Errorf("panic 的 Effect A 销毁后不得再重跑, runsA=%d", runsA)
		}
		// 2. 正常的 Effect B 也已销毁：写 b 不再触发重跑（旧实现会被 cleanup-A
		//    panic 中断而漏销毁，导致这里 runsB 增长）。
		b(100)
		if runsB != 1 {
			t.Errorf("Effect B 必须在 cleanup-A panic 后仍被销毁, runsB=%d", runsB)
		}
		// 3. panic cleanup 前后的正常 cleanup 都执行了（panic 的 cleanup 自己没跑完）。
		if !rootCleanup2 || !rootCleanup3 {
			t.Errorf("Root cleanup 中正常项必须全部执行, c2=%v c3=%v", rootCleanup2, rootCleanup3)
		}
		// 4. 子 Owner 清理执行、且子 Owner 已 disposed（旧实现会跳过整个子 Owner）。
		if !childCleanup {
			t.Errorf("子 Owner 的 cleanup 必须执行（panic 不阻断）")
		}
		if !child.disposed {
			t.Errorf("子 Owner 必须已销毁")
		}
		if !root.disposed {
			t.Errorf("根 Owner 必须已销毁")
		}
		// 5. 幂等：上报后再次 Dispose 不重复执行清理、不再 panic。
		root.Dispose() // disposed 已置位，no-op
	}()

	root.Dispose() // 隔离执行后在阶段 4 re-panic
	t.Fatal("Dispose 应 re-panic（不可达）")
}

// TestDisposeSingleCleanupPanicPreserved：单个 panic 原样 re-panic（类型保真），
// 调用方的类型断言 / errors.As 可用；且 panic cleanup 之后的其余资源仍释放。
func TestDisposeSingleCleanupPanicPreserved(t *testing.T) {
	var afterPanic bool
	var h Computation
	var o *Owner
	CreateRoot(func(owner *Owner) {
		o = owner
		OnCleanup(func() { panic(mySentinel) }) // 唯一 panic 源
		OnCleanup(func() { afterPanic = true }) // 不得被上面的 panic 跳过
		h = Effect(func() {})
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Dispose 应上报唯一 panic")
		}
		if r != error(mySentinel) && !errors.Is(toErr(r), mySentinel) {
			t.Fatalf("单 panic 应原样 re-panic（类型保真）, got %T: %v", r, r)
		}
		if _, multi := r.(disposeErrors); multi {
			t.Fatalf("单 panic 不应包装成 disposeErrors, got %v", r)
		}
		if !afterPanic {
			t.Errorf("panic 之后的 cleanup 必须仍执行")
		}
		if hh, ok := h.(*handle); !ok || hh.n == nil || !hh.n.disposed {
			t.Errorf("effect 必须已销毁")
		}
	}()
	o.Dispose()
	t.Fatal("不可达")
}

// TestHandleDisposeIsolatesCleanupPanic：句柄 Dispose 的 cleanup panic 不阻断
// 依赖边摘除——节点已 disposed，后续写入不再调度它；panic 统一上报。
func TestHandleDisposeIsolatesCleanupPanic(t *testing.T) {
	count := New(0)
	var runs int
	var edgeCut bool

	finished := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				edgeCut = true // panic 已上报，且此时依赖边必须已摘除
			}
		}()
		h := Effect(func() {
			runs++
			_ = count()
			OnCleanup(func() { panic("handle-cleanup") }) // effect 重跑期注册：归属 runCleanups
		})
		h.Dispose() // cleanup panic → 上报
		finished = true
	}()
	if finished {
		t.Fatal("句柄 Dispose 应 re-panic（不可达）")
	}
	if !edgeCut {
		t.Fatal("应捕获到 cleanup panic")
	}
	count(42)
	if runs != 1 {
		t.Errorf("节点销毁后不得再重跑（依赖边应在 panic 前摘除）, runs=%d", runs)
	}
}

// TestRepanicDisposeErrorsForms：上报形态单元测试——空 no-op、单个保真、多个汇总。
func TestRepanicDisposeErrorsForms(t *testing.T) {
	if caught := runFuncSafely(func() {}); len(caught) != 0 {
		t.Fatalf("正常函数不应收集到 panic, got %v", caught)
	}
	if caught := runFuncSafely(func() { panic("x") }); len(caught) != 1 || caught[0] != "x" {
		t.Fatalf("应收集到单个 panic, got %v", caught)
	}

	func() {
		defer func() {
			if r := recover(); r != "boom" {
				t.Fatalf("单 panic 应原样 re-panic, got %v", r)
			}
		}()
		repanicDisposeErrors([]any{"boom"})
	}()
	func() {
		defer func() {
			r := recover()
			de, ok := r.(disposeErrors)
			if !ok || len(de) != 2 {
				t.Fatalf("多 panic 应汇总, got %T: %v", r, r)
			}
			if de.Error() == "" {
				t.Fatal("disposeErrors.Error() 不应为空")
			}
		}()
		repanicDisposeErrors([]any{"a", "b"})
	}()
}

// toErr 把 panic 值转为 error 以便 errors.Is 判断；非 error 返回 nil。
func toErr(v any) error {
	if e, ok := v.(error); ok {
		return e
	}
	return nil
}

// mySentinel 是可比较的哨兵错误：验证单 panic 保真传递。
var mySentinel = errors.New("sentinel-cleanup-panic")
