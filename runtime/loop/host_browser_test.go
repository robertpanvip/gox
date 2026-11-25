//go:build js && wasm

// P3 真实浏览器宿主验证（提案 §5.2 / §12 P3）：仅在具备 requestAnimationFrame
// 的宿主执行——node 里自动 Skip，真实浏览器（scripts/runwasm_browser.js 用
// Chrome headless + CDP 驱动）里实测：
//  1. rAF 与 setTimeout(0) 都会到达；两者先后只记录、不做跨引擎断言（§5.2
//     禁止凭 API 名称推断顺序）；
//  2. Loop 以 rAF 帧回调为宿主轮次时：Post/PostMicro 不在调用栈执行，
//     轮次内宏→微（§4.1 检查点语义在浏览器成立）；
//  3. 原生微任务（Promise.then / queueMicrotask）先于宿主宏任务轮次，
//     Loop 微任务只在自己的检查点收尾——固化「Loop 微任务 ≠ 浏览器原生
//     微任务」的实测语义；未来浏览器行为漂移会直接打破本断言。
package loop

import (
	"fmt"
	"testing"
	"syscall/js"
	"time"
)

// requireBrowser：非浏览器宿主（无 rAF）Skip。
func requireBrowser(t *testing.T) js.Value {
	raf := js.Global().Get("requestAnimationFrame")
	if !raf.Truthy() {
		t.Skip("宿主无 requestAnimationFrame（非浏览器），跳过 P3 浏览器实测项")
	}
	return raf
}

// drainTurn 排空一轮：宏→微检查点直到无可执行任务。
func drainTurn(l *Queue) {
	for {
		ran := l.RunOneMacro()
		rep := l.RunMicrotasks()
		if !ran && rep.Ran == 0 && rep.Remaining == 0 {
			return
		}
	}
}

func TestBrowserRAFAndTimeoutBothArrive(t *testing.T) {
	raf := requireBrowser(t)
	ch := make(chan string, 4)
	var cbT, cbR js.Func
	cbT = js.FuncOf(func(js.Value, []js.Value) any { ch <- "timeout"; return nil })
	cbR = js.FuncOf(func(js.Value, []js.Value) any { ch <- "raf"; return nil })
	defer cbT.Release()
	defer cbR.Release()
	js.Global().Call("setTimeout", cbT, 0)
	raf.Invoke(cbR)

	var order []string
	deadline := time.After(10 * time.Second)
	for len(order) < 2 {
		select {
		case s := <-ch:
			order = append(order, s)
		case <-deadline:
			t.Fatalf("rAF/setTimeout(0) 未全部到达: %v", order)
		}
	}
	t.Logf("浏览器事实记录：两种宿主回调均到达，本次顺序=%v（先后不做跨引擎断言）", order)
}

func TestBrowserLoopMacroMicroViaRAF(t *testing.T) {
	raf := requireBrowser(t)
	l := New(Config{})
	var seq []string
	done := make(chan struct{}, 1)
	var drain js.Func
	drain = js.FuncOf(func(js.Value, []js.Value) any {
		drainTurn(l)
		select {
		case done <- struct{}{}:
		default:
		}
		return nil
	})
	l.cfg.Wake = func() { raf.Invoke(drain) }

	l.Post(func() { seq = append(seq, "macro") })
	l.PostMicro(func() { seq = append(seq, "micro") })
	if len(seq) != 0 {
		t.Fatal("Post/PostMicro 在调用栈内执行")
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("rAF 宿主轮次未执行 Loop 任务: %v", seq)
	}
	if fmt.Sprint(seq) != fmt.Sprint([]string{"macro", "micro"}) {
		t.Fatalf("rAF 轮内顺序违例（应宏→微）: %v", seq)
	}
}

func TestBrowserNativeMicrotaskBeforeLoopCheckpoint(t *testing.T) {
	requireBrowser(t)
	qmFn := js.Global().Get("queueMicrotask")
	if !qmFn.Truthy() {
		t.Skip("宿主无 queueMicrotask")
	}
	l := New(Config{})
	var seq []string
	done := make(chan struct{}, 1)
	var turn js.Func
	turn = js.FuncOf(func(js.Value, []js.Value) any {
		drainTurn(l)
		select {
		case done <- struct{}{}:
		default:
		}
		return nil
	})
	l.cfg.Wake = func() { js.Global().Call("setTimeout", turn, 0) }

	// 原生微任务两路：Promise.then 与 queueMicrotask。
	// 注意用 Call 走对象接收者：Get("resolve").Invoke() 的 this 是 undefined，
	// V8 会抛 "PromiseResolve called on non-object"。
	promiseObj := js.Global().Get("Promise")
	if !promiseObj.Truthy() {
		t.Skip("宿主无 Promise")
	}
	resolved := promiseObj.Call("resolve", js.Undefined())
	var pm js.Func
	pm = js.FuncOf(func(js.Value, []js.Value) any { seq = append(seq, "js-promise-micro"); return nil })
	resolved.Call("then", pm)
	var qm js.Func
	qm = js.FuncOf(func(js.Value, []js.Value) any { seq = append(seq, "js-micro"); return nil })
	qmFn.Invoke(qm)

	l.Post(func() { seq = append(seq, "loop-macro") })
	l.PostMicro(func() { seq = append(seq, "loop-micro") })

	deadline := time.After(10 * time.Second)
	for len(seq) < 4 {
		select {
		case <-done:
		case <-deadline:
			t.Fatalf("宿主轮次未收尾: %v", seq)
		}
	}
	want := []string{"js-promise-micro", "js-micro", "loop-macro", "loop-micro"}
	if fmt.Sprint(seq) != fmt.Sprint(want) {
		t.Fatalf("浏览器微任务交错漂移（§5.2 实测语义变化，需重估声明）:\n got=%v\nwant=%v", seq, want)
	}
}
