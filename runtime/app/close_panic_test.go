package gxapp_test

import (
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// App.Close 销毁期异常隔离回归（评审 P1-02）：
//
// 旧行为：closeOnce.Do 内 teardownTree（→ Owner.Dispose 执行用户 onCleanup）
// 一旦 panic，dispatcher.Close() 与 win.Close() 被跳过，closeErr 不记录；而
// sync.Once 已标记完成——后续 Close 调用全部 no-op，窗口永远关不掉。
//
// 新行为：四阶段（Runtime/租约 → 响应式树 → 派发器 → 窗口）各自单点捕获，
// 窗口关闭是最终兜底动作；全部释放完毕后统一上报（单异常保真 re-panic）。

// TestAppCloseSurvivesCleanupPanic：组件 cleanup panic 不阻断窗口关闭兜底。
func TestAppCloseSurvivesCleanupPanic(t *testing.T) {
	win := &countingWindow{}
	app := openApp(t, gxapp.DefaultConfig(), win)

	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return "close-panic" })

	if err := app.Render(func() any { return inst }); err != nil {
		t.Fatal(err)
	}

	var normalCleanupRan bool
	// 渲染闭包在 app.owner 下执行：注册一个 panic cleanup 和一个正常 cleanup。
	err := app.Render(func() any {
		signal.OnCleanup(func() { panic("cleanup-boom") }) // 唯一 panic 源
		signal.OnCleanup(func() { normalCleanupRan = true })
		return inst
	})
	if err != nil {
		t.Fatal(err)
	}

	// Close 必须 panic 上报（异常不静默丢失）——但窗口关闭已完成。
	var closedOnce bool
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("Close 应把收集到的 cleanup panic 统一上报")
			}
			if s, ok := r.(string); !ok || s != "cleanup-boom" {
				t.Fatalf("单 panic 应原样 re-panic, got %T: %v", r, r)
			}
			closedOnce = true
		}()
		_ = app.Close()
	}()
	if !closedOnce {
		t.Fatal("Close 应到达 re-panic（不可达）")
	}
	if !normalCleanupRan {
		t.Errorf("panic 之后的正常 cleanup 必须仍执行")
	}
	if win.closes != 1 {
		t.Errorf("窗口必须被关闭兜底, closes=%d", win.closes)
	}

	// 幂等：panic 后再次 Close 不重复执行（Once 已完成），资源已释放、不再 panic。
	if err := app.Close(); err != nil {
		t.Errorf("二次 Close 应幂等返回 nil, got %v", err)
	}
	if win.closes != 1 {
		t.Errorf("二次 Close 不得重复关窗, closes=%d", win.closes)
	}
}

// TestAppCloseIdempotentWithoutPanic：无 panic 时 Close 语义不变——错误记录、
// 幂等返回一致结果（守护新隔离逻辑不改变正常路径）。
func TestAppCloseIdempotentWithoutPanic(t *testing.T) {
	win := &countingWindow{}
	app := openApp(t, gxapp.DefaultConfig(), win)

	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return "ok" })
	if err := app.Render(func() any { return inst }); err != nil {
		t.Fatal(err)
	}

	if err := app.Close(); err != nil {
		t.Fatalf("正常 Close 应返回 nil, got %v", err)
	}
	if win.closes != 1 {
		t.Errorf("窗口应关闭一次, closes=%d", win.closes)
	}
	if err := app.Close(); err != nil {
		t.Errorf("二次 Close 应幂等返回 nil, got %v", err)
	}
	if win.closes != 1 {
		t.Errorf("二次 Close 不得重复关窗, closes=%d", win.closes)
	}
}
