package gxapp_test

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	"github.com/robertpanvip/gox/runtime/canvas"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

// rafWindow 在每次 Draw 时记录 canvas 节点的 canvas-ops，供验证 RAF 回调里的
// 命令式绘制是否在本帧 Draw 之前生效。
type rafWindow struct {
	countingWindow
	ops []string
}

func (w *rafWindow) Draw(r *gxscene.Node) error {
	w.draws++
	w.lastRoot = r
	w.ops = append(w.ops, findCanvasOps(r))
	return nil
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
	if len(win.ops) == 0 {
		t.Fatal("窗口未记录到绘制")
	}
	last := win.ops[len(win.ops)-1]
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
	last := findCanvasOps(win.lastRoot)
	if got := len(canvas.Decode(last)); int32(got) != target {
		t.Fatalf("canvas 指令数应等于帧数 %d，got %d (%s)", target, got, last)
	}
}
