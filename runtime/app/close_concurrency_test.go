package gxapp

import (
	"sync"
	"sync/atomic"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// closeRaceWindow 统计 Draw 次数，用于验证关闭后不再有新帧提交。
type closeRaceWindow struct {
	cfg   WindowConfig
	draws atomic.Int64
	mu    sync.Mutex
}

func (w *closeRaceWindow) Open(c WindowConfig) error { w.cfg = c; return nil }
func (w *closeRaceWindow) Draw(r *gxscene.Node) error {
	w.draws.Add(1)
	return nil
}
func (w *closeRaceWindow) Run() error           { return nil }
func (w *closeRaceWindow) Close() error         { return nil }
func (w *closeRaceWindow) Config() WindowConfig { return w.cfg }

// TestCloseConcurrentWithMutationAndDraw 验收评审 P0：Close 与持续 signal 写入
// （Scene Mutation）+ Draw 并发，在 -race 下不得报数据竞争、不得 panic。
// 关键修复：teardownTree 在 commit gate 内、gate 内二次检查 closed/inst、
// drawNow/Update 取局部 inst 引用。
func TestCloseConcurrentWithMutationAndDraw(t *testing.T) {
	for round := 0; round < 20; round++ {
		val := signal.New(0)
		inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "div"},
			{Code: gxscene.OpDynChild, Slot: 0},
			{Code: gxscene.OpClose},
		}, N: 1}, func() any { return val() })

		w := &closeRaceWindow{}
		app, err := OpenWith(DefaultConfig(), w)
		if err != nil {
			t.Fatal(err)
		}
		if err := app.Render(func() any { return inst }); err != nil {
			t.Fatal(err)
		}

		var wg sync.WaitGroup
		// 3 个写者持续写 signal（触发 effect mutation + draw 请求）。
		for k := 0; k < 3; k++ {
			wg.Add(1)
			go func(k int) {
				defer wg.Done()
				for i := 0; i < 500; i++ {
					val(i)
				}
			}(k)
		}
		// 1 个绘制者持续 Draw。
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				app.Draw()
				app.Update()
			}
		}()
		// 关闭者：稍后关闭，制造与上述操作的交错。
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				val(i) // 关闭前也写，确保关闭瞬间有 in-flight mutation
			}
			_ = app.Close()
		}()

		wg.Wait()
		_ = app.Close() // 幂等

		// 关闭后 Draw/Update 不得再触发任何帧。
		before := w.draws.Load()
		app.Draw()
		app.Update()
		if after := w.draws.Load(); after != before {
			t.Fatalf("关闭后不应再提交新帧: before=%d after=%d", before, after)
		}
	}
}
