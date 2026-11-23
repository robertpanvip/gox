package gxapp_test

import (
	"runtime"
	"sync"
	"testing"
	"time"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// goidFromStack 解析当前 goroutine id（测试独立实现，与被测代码无关）。
func goidFromStack() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	const p = "goroutine "
	if n < len(p) || string(buf[:len(p)]) != p {
		return 0
	}
	var id uint64
	for i := len(p); i < n; i++ {
		c := buf[i]
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + uint64(c-'0')
	}
	return id
}

// threadRecordingWindow 记录每次 Draw 发生的 goroutine id 与调用次数。
type threadRecordingWindow struct {
	cfg      gxapp.WindowConfig
	mu       sync.Mutex
	drawGoid map[uint64]int
	draws    int
	failErr  error
}

func (w *threadRecordingWindow) Open(c gxapp.WindowConfig) error { w.cfg = c; return nil }
func (w *threadRecordingWindow) Draw(r *gxscene.Node) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	g := goidFromStack()
	if w.drawGoid == nil {
		w.drawGoid = map[uint64]int{}
	}
	w.drawGoid[g]++
	w.draws++
	return w.failErr
}
func (w *threadRecordingWindow) Run() error                 { return nil }
func (w *threadRecordingWindow) Close() error               { return nil }
func (w *threadRecordingWindow) Config() gxapp.WindowConfig { return w.cfg }

// T2.1 验收：后台 goroutine 写 signal，Draw 必须发生在 UI 线程，不在后台线程。
func TestQueueDispatcherDrawsOnUIThread(t *testing.T) {
	val := signal.New("A")
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"}, {Code: gxscene.OpDynChild, Slot: 0}, {Code: gxscene.OpClose},
	}, N: 1}, func() any { return val() })

	win := &threadRecordingWindow{}
	app, err := gxapp.OpenWith(gxapp.DefaultConfig(), win)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	// UI 线程：一个专属 goroutine + 任务 channel；post 把任务投给它。
	uiTasks := make(chan func(), 16)
	var uiGoid uint64
	uiReady := make(chan struct{})
	go func() {
		uiGoid = goidFromStack()
		close(uiReady)
		for task := range uiTasks {
			task()
		}
	}()
	<-uiReady
	app.SetDispatcher(gxapp.NewQueueDispatcher(uiGoid, func(f func()) { uiTasks <- f }))

	if err := app.Render(func() any { return inst }); err != nil {
		t.Fatal(err)
	}
	// 首帧是 Render 同步绘制（在测试 goroutine），单独记录；后续计数只看信号写入帧。
	win.mu.Lock()
	win.drawGoid = map[uint64]int{}
	win.draws = 0
	win.mu.Unlock()

	// 后台 goroutine 写 signal。
	bgDone := make(chan struct{})
	go func() {
		val("B")
		close(bgDone)
	}()
	<-bgDone

	// 等 UI 线程消费完投递的帧请求。
	deadline := time.After(2 * time.Second)
	for {
		win.mu.Lock()
		n := win.draws
		win.mu.Unlock()
		if n >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("超时：UI 线程未执行 Draw")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	close(uiTasks)

	win.mu.Lock()
	defer win.mu.Unlock()
	if win.drawGoid[uiGoid] != win.draws {
		t.Errorf("所有 Draw 应发生在 UI 线程, drawGoid=%v uiGoid=%d", win.drawGoid, uiGoid)
	}
	text := gxscene.TextBackend{}.Paint(inst.Root())
	if !contains(text, "B") {
		t.Errorf("UI 线程绘制应含最新值 B: %s", text)
	}
}

// T2.2 验收：Draw 失败被记录，且错误回调被调用。
func TestDrawErrorReported(t *testing.T) {
	fail := &drawFailWindow{}
	app, err := gxapp.OpenWith(gxapp.DefaultConfig(), fail)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	var got []error
	app.SetErrorHandler(func(e error) { got = append(got, e) })

	if err := app.Render(func() any { return helloInst() }); err != nil {
		t.Fatal(err) // Render 不返回 Draw 错误，但会记录
	}
	if app.LastDrawError() == nil {
		t.Error("Draw 失败应可通过 LastDrawError 查询")
	}
	if len(got) == 0 {
		t.Error("Draw 失败应触发错误回调")
	}
}

type drawFailWindow struct{ cfg gxapp.WindowConfig }

func (w *drawFailWindow) Open(c gxapp.WindowConfig) error { w.cfg = c; return nil }
func (w *drawFailWindow) Draw(r *gxscene.Node) error      { return errDrawBoom }
func (w *drawFailWindow) Run() error                      { return nil }
func (w *drawFailWindow) Close() error                    { return nil }
func (w *drawFailWindow) Config() gxapp.WindowConfig      { return w.cfg }

var errDrawBoom = &testErr{"draw boom"}

type testErr struct{ s string }

func (e *testErr) Error() string { return e.s }
