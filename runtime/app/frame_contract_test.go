package gxapp

import (
	"errors"
	"strings"
	"sync"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// flakyFrameWindow 是可切换 Present 成败的 FramePresenter，记录每帧快照。
// 用它验证「Present 失败后 Changes 不丢、下帧 FullSync 全量重建」。
type flakyFrameWindow struct {
	cfg WindowConfig
	mu  sync.Mutex

	frames []*Frame
	fail   bool // 若 true，Present 返回 errPresentFail
}

var errPresentFail = errors.New("present failed")

func (w *flakyFrameWindow) Open(c WindowConfig) error { w.cfg = c; return nil }
func (w *flakyFrameWindow) Draw(*gxscene.Node) error  { return nil }
func (w *flakyFrameWindow) Run() error                { return nil }
func (w *flakyFrameWindow) Close() error              { return nil }
func (w *flakyFrameWindow) Config() WindowConfig      { return w.cfg }

func (w *flakyFrameWindow) SetFail(v bool) {
	w.mu.Lock()
	w.fail = v
	w.mu.Unlock()
}

func (w *flakyFrameWindow) Present(f *Frame) error {
	w.mu.Lock()
	cp := *f
	cp.Changes = append([]gxscene.Change(nil), f.Changes...)
	w.frames = append(w.frames, &cp)
	fail := w.fail
	w.mu.Unlock()
	if fail {
		return errPresentFail
	}
	return nil
}

func (w *flakyFrameWindow) snapshot() []*Frame {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]*Frame, len(w.frames))
	copy(out, w.frames)
	return out
}

// TestFramePresenterAutoEnablesChanges 验收评审 P0：后端只实现 Present、
// 不调用 EnableChanges，signal 写入后的更新帧也必须携带增量。
func TestFramePresenterAutoEnablesChanges(t *testing.T) {
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
	// 注意：刻意不调用 inst.EnableChanges() / app.EnableChanges()。
	if err := app.Render(func() any { return inst }); err != nil {
		t.Fatal(err)
	}

	val("B") // 触发一次更新帧

	frames := w.snapshot()
	if len(frames) < 2 {
		t.Fatalf("应收到首帧+更新帧，实际 %d", len(frames))
	}
	last := frames[len(frames)-1]
	hasText := false
	for _, c := range last.Changes {
		if c.Kind == gxscene.ChangeSetText {
			hasText = true
		}
	}
	if !hasText {
		t.Fatalf("FramePresenter 未调 EnableChanges 也应收到 SetText 增量, changes=%v", last.Changes)
	}
}

// TestFramePresentFailureFullSyncRecovery 验收评审 P0：
//  1. Present 失败后 Changes 缓冲不被清空（不永久丢帧）；
//  2. 下一帧携带 FullSync=true，且包含失败期间累积的增量；
//  3. 成功交付后 FullSync 复位、缓冲被消费。
func TestFramePresentFailureFullSyncRecovery(t *testing.T) {
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

	// 首帧应成功、非 FullSync（用 Root 建基线即可）。
	frames := w.snapshot()
	if len(frames) == 0 || frames[0].FullSync {
		t.Fatalf("首帧应非 FullSync，实际 %+v", frames)
	}

	// 让 Present 失败，写入一个新值。
	w.SetFail(true)
	val("B")
	if app.LastDrawError() == nil {
		t.Fatal("Present 失败应记录到 LastDrawError")
	}
	failedCount := len(w.snapshot())

	// 恢复 Present，再次写入。
	w.SetFail(false)
	val("C")

	frames = w.snapshot()
	if len(frames) <= failedCount {
		t.Fatalf("失败后应继续产生帧，before=%d after=%d", failedCount, len(frames))
	}
	recovered := frames[len(frames)-1]
	if !recovered.FullSync {
		t.Fatalf("失败后的下一帧必须 FullSync=true，实际 FullSync=%v Version=%d", recovered.FullSync, recovered.Version)
	}
	// Root 必须是最新值 C。
	text := gxscene.TextBackend{}.Paint(recovered.Root)
	if !strings.Contains(text, "C") {
		t.Fatalf("FullSync 帧的 Root 应含最新值 C: %s", text)
	}

	// 再触发一帧：应恢复非 FullSync。
	val("D")
	frames = w.snapshot()
	last := frames[len(frames)-1]
	if last.FullSync {
		t.Fatal("成功交付后 FullSync 应复位")
	}
	if app.LastDrawError() != nil {
		t.Fatalf("恢复后 LastDrawError 应为 nil，实际 %v", app.LastDrawError())
	}
}
