package gxscene

import "testing"

// Instance.Dispose / For.Dispose 销毁期异常隔离回归（评审 P1-01/P1-02 的 scene
// 层配套）：多 slot / 多 entry 遍历中某个用户 ref 回调 / owner cleanup panic
// 不得阻断其余资源的释放——被跳过的清单已从容器上摘除，中断即永久泄漏。全部
// 释放完毕后统一上报（单异常保真 re-panic）。

// panicOwner 是满足 Owner 接口的桩：Dispose 时按登记顺序执行回调，单点隔离。
type panicOwner struct {
	cleanups []func()
	disposed bool
}

func (o *panicOwner) OnCleanup(fn func()) { o.cleanups = append(o.cleanups, fn) }
func (o *panicOwner) Dispose() {
	if o.disposed {
		return
	}
	o.disposed = true
	cs := o.cleanups
	o.cleanups = nil
	var caught []any
	for _, c := range cs {
		caught = append(caught, runFuncSafely(c)...)
	}
	repanicDisposeErrors(caught)
}

// TestInstanceDisposeIsolatesRefCallbackPanic：两个 ref slot，第一个回调 panic、
// 第二个正常——第二个必须仍执行，panic 统一上报。
func TestInstanceDisposeIsolatesRefCallbackPanic(t *testing.T) {
	var secondRan bool
	tpl := Template{Ops: []Op{
		{Code: OpElem, Arg: "div"},
		{Code: OpRef, Slot: 0}, // panic 回调
		{Code: OpClose},
		{Code: OpElem, Arg: "span"},
		{Code: OpRef, Slot: 1}, // 正常回调
		{Code: OpClose},
	}, N: 2}
	// ref 回调按 Slot 区分：getter 返回的回调在建树时各调用一次。
	inst := Render(tpl, func() any {
		return func(e *Element) {}
	})
	// 直接改写两个 ref slot 的回调：第一个 panic，第二个正常。
	refs := 0
	for i := range inst.slots {
		if inst.slots[i].kind == slotRef {
			refs++
			if refs == 1 {
				inst.slots[i].refCb = func(*Element) { panic("ref-boom") }
			} else {
				inst.slots[i].refCb = func(*Element) { secondRan = true }
			}
		}
	}
	if refs != 2 {
		t.Fatalf("应有两个 ref slot, got %d", refs)
	}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Dispose 应统一上报收集到的 panic")
		}
		if s, ok := r.(string); !ok || s != "ref-boom" {
			t.Fatalf("单 panic 应原样 re-panic, got %T: %v", r, r)
		}
		if !secondRan {
			t.Errorf("panic 之后的 ref 回调必须仍执行")
		}
		if inst.comps != nil {
			t.Errorf("comps 清单应清空")
		}
	}()
	inst.Dispose()
	t.Fatal("Dispose 应 re-panic（不可达）")
}

// TestForDisposeIsolatesOwnerCleanupPanic：For 项的 owner cleanup panic 不阻断
// 其余项销毁（For 层隔离），panic 统一上报，entries 清单清空。
func TestForDisposeIsolatesOwnerCleanupPanic(t *testing.T) {
	f := NewFor(
		func() []any { return []any{} },
		func(v any) any { return v },
		func(v any) *Instance { return nil },
	)
	var afterPanicRan bool
	o1 := &panicOwner{}
	o1.OnCleanup(func() { panic("for-boom") })
	o2 := &panicOwner{}
	o2.OnCleanup(func() { afterPanicRan = true })
	f.mu.Lock()
	f.entries = map[any]*forEntry{
		"k1": {key: "k1", owner: o1},
		"k2": {key: "k2", owner: o2},
	}
	f.mu.Unlock()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("For.Dispose 应统一上报收集到的 panic")
		}
		if s, ok := r.(string); !ok || s != "for-boom" {
			t.Fatalf("单 panic 应原样 re-panic, got %T: %v", r, r)
		}
		if !afterPanicRan {
			t.Errorf("panic entry 之后的 entry cleanup 必须仍执行")
		}
		if f.entries != nil {
			t.Errorf("entries 清单应清空")
		}
	}()
	f.Dispose()
	t.Fatal("For.Dispose 应 re-panic（不可达）")
}
