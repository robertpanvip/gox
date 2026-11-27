package gxapp_test

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	"github.com/robertpanvip/gox/runtime/canvas"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// rafWindow 在每次 Draw 时记录 canvas 节点的 canvas-ops，供验证 RAF 回调里的
// 命令式绘制是否在本帧 Draw 之前生效。Draw 在调度 goroutine 里异步执行，
// 记录的读写必须加锁（-race 下测试主 goroutine 会在收尾后读取）。
type rafWindow struct {
	countingWindow
	mu  sync.Mutex
	ops []string
}

func (w *rafWindow) Draw(r *gxscene.Node) error {
	w.mu.Lock()
	w.draws++
	w.lastRoot = r
	w.ops = append(w.ops, findCanvasOps(r))
	w.mu.Unlock()
	return nil
}

// lastCanvasOps 返回最近一帧记录的 canvas-ops（加锁快照）。
func (w *rafWindow) lastCanvasOps() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.ops) == 0 {
		return ""
	}
	return w.ops[len(w.ops)-1]
}

func findCanvasOps(n *gxscene.Node) string {
	if n == nil {
		return ""
	}
	if strings.EqualFold(n.Kind, "canvas") {
		if s, _ := n.Props[canvas.PropKey].(string); s != "" {
			return s
		}
	}
	for _, c := range n.Children {
		if s := findCanvasOps(c); s != "" {
			return s
		}
	}
	return ""
}

// canvasRAFInst 构造一个 canvas + ref 的场景，ref 回调接住 Element。
func canvasRAFInst(capture *atomic.Value) *gxscene.Instance {
	return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "canvas"},
		{Code: gxscene.OpRef, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any {
		return func(e *gxscene.Element) { capture.Store(e) }
	})
}

// TestRAFDrawBeforePaint 验证 el.requestAnimationFrame 的回调在下一帧 Draw 之前
// 执行：回调里 ctx.fillRect 的指令必须出现在该帧读取到的 canvas-ops 里。
func TestRAFDrawBeforePaint(t *testing.T) {
	win := &rafWindow{}
	app, err := gxapp.OpenWith(gxapp.DefaultConfig(), win)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	var cap atomic.Value
	if err := app.Render(func() any { return canvasRAFInst(&cap) }); err != nil {
		t.Fatal(err)
	}
	el := cap.Load().(*gxscene.Element)

	painted := make(chan struct{})
	el.RequestAnimationFrame(func() {
		ctx := el.GetContext()
		ctx.SetFillStyle("#abcdef")
		ctx.FillRect(1, 2, 3, 4)
		close(painted)
	})

	// 首帧 Render 已画；这里 DrawNow 触发的帧应先 drain RAF 再 Draw。
	if err := app.DrawNow(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-painted:
	default:
		t.Fatal("RAF 回调应在本帧 Draw 前执行")
	}
	last := win.lastCanvasOps()
	if last == "" {
		t.Fatal("窗口未记录到绘制")
	}
	decoded := canvas.Decode(last)
	if len(decoded) != 1 || decoded[0].Kind != canvas.OpRect || decoded[0].Fill != "#abcdef" {
		t.Fatalf("本帧 canvas-ops 应含 RAF 回调画的矩形，got %s", last)
	}
}

// TestRAFAnimationLoop 验证回调里再次登记 RAF 能形成动画循环，多帧自动推进，
// 且 drain 期间的写回不会同步递归自锁。
func TestRAFAnimationLoop(t *testing.T) {
	win := &rafWindow{}
	app, err := gxapp.OpenWith(gxapp.DefaultConfig(), win)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	var cap atomic.Value
	if err := app.Render(func() any { return canvasRAFInst(&cap) }); err != nil {
		t.Fatal(err)
	}
	el := cap.Load().(*gxscene.Element)

	const target = 4
	var n atomic.Int32
	done := make(chan struct{})
	var tick func()
	tick = func() {
		c := n.Add(1)
		el.GetContext().FillRect(float32(c), 0, 1, 1)
		if c < target {
			el.RequestAnimationFrame(tick) // drain 内再登记 → 下一帧
		} else {
			close(done)
		}
	}
	el.RequestAnimationFrame(tick)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("RAF 动画循环未在时限内推进到 %d 帧（当前 %d），可能自锁", target, n.Load())
	}
	if got := n.Load(); got != target {
		t.Fatalf("动画应恰好推进 %d 帧，got %d", target, got)
	}
	// 最后一帧的 canvas 指令数应等于帧数（每条 RAF 追加一个矩形）。
	last := win.lastCanvasOps()
	if got := len(canvas.Decode(last)); int32(got) != target {
		t.Fatalf("canvas 指令数应等于帧数 %d，got %d (%s)", target, got, last)
	}
}

// TestRAFCancelStopsLoop 验证在动画循环里取消句柄后，循环停止、不再预约帧。
func TestRAFCancelStopsLoop(t *testing.T) {
	win := &rafWindow{}
	app, err := gxapp.OpenWith(gxapp.DefaultConfig(), win)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	var cap atomic.Value
	if err := app.Render(func() any { return canvasRAFInst(&cap) }); err != nil {
		t.Fatal(err)
	}
	el := cap.Load().(*gxscene.Element)

	var n atomic.Int32
	stopped := make(chan struct{})
	// RAF 回调在调度 goroutine 里执行（无 FrameClock 的 InlineDispatcher 路径
	// 经 scheduleRAFNext 的后台 goroutine），与测试主 goroutine 并发——句柄
	// 读写必须同步，否则 -race 报测试自身的数据竞争。
	var handleMu sync.Mutex
	var handle gxscene.RAFHandle
	setHandle := func(h gxscene.RAFHandle) { handleMu.Lock(); handle = h; handleMu.Unlock() }
	getHandle := func() gxscene.RAFHandle {
		handleMu.Lock()
		defer handleMu.Unlock()
		return handle
	}
	var tick func()
	tick = func() {
		c := n.Add(1)
		el.GetContext().FillRect(float32(c), 0, 1, 1)
		if c == 3 {
			el.CancelAnimationFrame(getHandle()) // 取消自己登记的下一帧
			close(stopped)
			return
		}
		setHandle(el.RequestAnimationFrame(tick))
	}
	setHandle(el.RequestAnimationFrame(tick))

	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatalf("动画未在时限内跑到第 3 帧（当前 %d）", n.Load())
	}
	// 再给调度器一点时间，确认没有多余的帧被画出。
	time.Sleep(100 * time.Millisecond)
	if got := n.Load(); got != 3 {
		t.Fatalf("取消后不应再推进帧，期望 3，got %d", got)
	}
}

// TestAppCancelAnimationFrame 验证 App 级取消入口与 Element 级共享同一 sink。
func TestAppCancelAnimationFrame(t *testing.T) {
	win := &rafWindow{}
	app, err := gxapp.OpenWith(gxapp.DefaultConfig(), win)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	if err := app.Render(func() any { return helloInst() }); err != nil {
		t.Fatal(err)
	}

	called := false
	h := app.RequestAnimationFrame(func() { called = true })
	if h == 0 {
		t.Fatal("App.RequestAnimationFrame 应返回有效句柄")
	}
	app.CancelAnimationFrame(h)
	if err := app.DrawNow(); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("App.CancelAnimationFrame 应取消回调")
	}
}
