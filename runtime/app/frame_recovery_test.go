package gxapp

import (
	"strings"
	"sync"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// TestFrameFailurePreservesChanges 验收评审第 8 项：Frame 2 的 Present 失败后，
// Host 侧的 Changes 缓冲不得被清空——否则下一帧 FullSync 之前会永久丢增量。
func TestFrameFailurePreservesChanges(t *testing.T) {
	val := signal.New("A")
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return val() })

	w := &flakyFrameWindow{}
	app, err := OpenWith(DefaultConfig(), w)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err := app.Render(func() any { return inst }); err != nil {
		t.Fatal(err)
	}

	// Frame 1 成功（首帧）。
	if frames := w.snapshot(); len(frames) == 0 || frames[0].FullSync {
		t.Fatalf("首帧应成功且非 FullSync: %+v", w.snapshot())
	}

	// Frame 2：Present 失败。
	w.SetFail(true)
	val("B")
	if len(app.Changes()) == 0 {
		t.Fatal("Present 失败后 Changes 缓冲不应被清空")
	}

	// Frame 3：FullSync 全量重建，Root 应含失败期间的最新值。
	w.SetFail(false)
	val("C")
	frames := w.snapshot()
	recovered := frames[len(frames)-1]
	if !recovered.FullSync {
		t.Fatal("失败后的恢复帧必须 FullSync")
	}
	if text := (gxscene.TextBackend{}).Paint(recovered.Root); !strings.Contains(text, "C") {
		t.Fatalf("FullSync 帧 Root 应含最新值 C: %s", text)
	}

	// Frame 4：正常增量。
	val("D")
	frames = w.snapshot()
	last := frames[len(frames)-1]
	if last.FullSync {
		t.Fatal("成功恢复后应回到增量模式")
	}
}

// TestFrameRecoveryConcurrentClose 验收评审第 8 项：失败恢复与 Close 并发时，
// 提交序号严格递增、不重复提交，且 Close 后不再产生新帧。
func TestFrameRecoveryConcurrentClose(t *testing.T) {
	val := signal.New("A")
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return val() })

	w := &flakyFrameWindow{}
	app, err := OpenWith(DefaultConfig(), w)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.Render(func() any { return inst }); err != nil {
		t.Fatal(err)
	}

	// 制造一次失败，进入待 FullSync 状态。
	w.SetFail(true)
	val("B")
	w.SetFail(false)

	var wg sync.WaitGroup
	closeDone := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = app.DrawNow()
			}
		}()
	}
	go func() {
		defer close(closeDone)
		_ = app.Close()
	}()
	wg.Wait()
	<-closeDone

	frames := w.snapshot()
	for i := 1; i < len(frames); i++ {
		if frames[i].Version <= frames[i-1].Version {
			t.Fatalf("提交序号必须严格递增（无重复提交）: #%d=%d #%d=%d",
				i-1, frames[i-1].Version, i, frames[i].Version)
		}
	}
	after := len(w.snapshot())
	_ = app.DrawNow()
	if got := len(w.snapshot()); got != after {
		t.Fatalf("Close 后不应再提交新帧: before=%d after=%d", after, got)
	}
}
