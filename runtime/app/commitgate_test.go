package gxapp

import (
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

func TestCommitGateMutualExclusion(t *testing.T) {
	g := newCommitGate()
	var inside atomic.Int32
	var maxInside atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				g.lock()
				n := inside.Add(1)
				for {
					m := maxInside.Load()
					if n <= m || maxInside.CompareAndSwap(m, n) {
						break
					}
				}
				inside.Add(-1)
				g.unlock()
			}
		}()
	}
	wg.Wait()
	if got := maxInside.Load(); got != 1 {
		t.Fatalf("提交门未串行化：并发进入数=%d，期望 1", got)
	}
}

func TestCommitGateReentrantSameGoroutine(t *testing.T) {
	g := newCommitGate()
	done := make(chan struct{})
	go func() {
		g.lock()
		g.lock()
		g.lock()
		g.unlock()
		g.unlock()
		g.unlock()
		close(done)
	}()
	// 若不可重入，上面会在第二次 lock 自锁；用超时保护测试。
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("同一 goroutine 重入提交门发生死锁")
	}
}

// scanningWindow 的 Draw 真正遍历整棵树（TextBackend.Paint），从而与后台 effect
// 的节点写入形成读/写竞争——这是评审 P0 描述的核心场景。
type scanningWindow struct {
	cfg WindowConfig
}

func (w *scanningWindow) Open(c WindowConfig) error { w.cfg = c; return nil }
func (w *scanningWindow) Draw(r *gxscene.Node) error {
	_, err := io.WriteString(io.Discard, (gxscene.TextBackend{}).Paint(r))
	return err
}
func (w *scanningWindow) Run() error                 { return nil }
func (w *scanningWindow) Close() error               { return nil }
func (w *scanningWindow) Config() WindowConfig { return w.cfg }

// TestConcurrentMutationAndDraw 复现评审 P0：一个 goroutine 持续写 signal 触发
// effect 改树，另一个 goroutine 持续 Draw 遍历树。在 -race 下，若 commit gate 缺失
// 会报 data race；有 gate 则串行化、零竞争。
func TestConcurrentMutationAndDraw(t *testing.T) {
	val := signal.New(0)
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return val() })

	app, err := OpenWith(DefaultConfig(), &scanningWindow{})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if err := app.Render(func() any { return inst }); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	stop := make(chan struct{})
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			val(i)
		}
		close(stop)
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				app.Draw()
			}
		}
	}()
	wg.Wait()
}

// frameWindow 实现 FramePresenter，记录收到的帧。
type frameWindow struct {
	cfg   WindowConfig
	mu    sync.Mutex
	frames []*Frame
}
func (w *frameWindow) Open(c WindowConfig) error { w.cfg = c; return nil }
func (w *frameWindow) Draw(*gxscene.Node) error  { return nil } // 不应被调用
func (w *frameWindow) Run() error                 { return nil }
func (w *frameWindow) Close() error               { return nil }
func (w *frameWindow) Config() WindowConfig       { return w.cfg }
func (w *frameWindow) Present(f *Frame) error {
	w.mu.Lock()
	cp := *f
	cp.Changes = append([]gxscene.Change(nil), f.Changes...)
	w.frames = append(w.frames, &cp)
	w.mu.Unlock()
	return nil
}

// Frame 契约：实现 FramePresenter 的后端每帧收到 Root + 递增 Version + Changes。
func TestFramePresenterReceivesFrame(t *testing.T) {
	val := signal.New("A")
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"}, {Code: gxscene.OpDynChild, Slot: 0}, {Code: gxscene.OpClose},
	}, N: 1}, func() any { return val() })

	inst.EnableChanges()
	w := &frameWindow{}
	app, err := OpenWith(DefaultConfig(), w)
	if err != nil { t.Fatal(err) }
	defer app.Close()
	if err := app.Render(func() any { return inst }); err != nil { t.Fatal(err) }

	// 首帧（Render 同步绘制）+ 一次 signal 写入触发的帧。
	val("B")
	w.mu.Lock()
	n := len(w.frames)
	w.mu.Unlock()
	if n < 2 {
		t.Fatalf("应至少收到首帧+更新帧，实际 %d 帧", n)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for i, f := range w.frames {
		if f.Root == nil {
			t.Fatalf("帧 %d 的 Root 为空", i)
		}
		if f.Version == 0 {
			t.Fatalf("帧 %d 的 Version 应从 1 开始", i)
		}
		if i > 0 && f.Version <= w.frames[i-1].Version {
			t.Fatalf("帧 Version 未递增: %d -> %d", w.frames[i-1].Version, f.Version)
		}
	}
	// 更新帧应携带 SetText 增量。
	last := w.frames[len(w.frames)-1]
	hasText := false
	for _, c := range last.Changes {
		if c.Kind == gxscene.ChangeSetText { hasText = true }
	}
	if !hasText {
		t.Fatalf("更新帧应携带 SetText 增量, changes=%v", last.Changes)
	}
}
