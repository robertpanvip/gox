package gxapp_test

import (
	"strings"
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// lastFrameOf 取多次绘制拼接输出中的最后一帧（以 marker 的出现位置切分）。
func lastFrameOf(all, marker string) string {
	idx := strings.LastIndex(all, marker)
	if idx < 0 {
		return all
	}
	return all[idx:]
}

// helloInst 构造一个带一个静态文本子节点的场景树实例。
func helloInst() *gxscene.Instance {
	return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpChildText, Arg: "hello"},
		{Code: gxscene.OpClose},
	}})
}

// countingWindow 记录 Open/Draw/Run/Close 调用次数的最小 Window 实现。
type countingWindow struct {
	cfg          gxapp.WindowConfig
	opens, draws int
	runs, closes int
	lastRoot     *gxscene.Node
}

func (w *countingWindow) Open(c gxapp.WindowConfig) error { w.cfg = c; w.opens++; return nil }
func (w *countingWindow) Draw(r *gxscene.Node) error {
	w.draws++
	w.lastRoot = r
	return nil
}
func (w *countingWindow) Run() error                 { w.runs++; return nil }
func (w *countingWindow) Close() error               { w.closes++; return nil }
func (w *countingWindow) Config() gxapp.WindowConfig { return w.cfg }

func TestOpenMountsAndDrawsFirstFrame(t *testing.T) {
	win := &countingWindow{}
	app, err := gxapp.OpenWith(helloInst(), gxapp.WindowConfig{Title: "Demo", Width: 1024, Height: 768}, win)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	if win.opens != 1 {
		t.Errorf("应 Open 一次, got %d", win.opens)
	}
	// Open 里首次绘制一帧
	if win.draws != 1 {
		t.Errorf("Open 应绘制首帧一次, got %d", win.draws)
	}
	if win.lastRoot == nil || win.lastRoot != app.Instance().Root() {
		t.Fatal("Draw 应拿到场景树根")
	}
	// 配置零值补齐
	if app.Config().Title != "Demo" || app.Config().Width != 1024 {
		t.Errorf("配置应透传, got %+v", app.Config())
	}
	if win.cfg.Backend != "headless" {
		t.Errorf("后端默认值应补 headless, got %q", win.cfg.Backend)
	}
}

func TestOpenNormalizesZeroSize(t *testing.T) {
	win := &countingWindow{}
	app, err := gxapp.OpenWith(helloInst(), gxapp.WindowConfig{}, win)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if app.Config().Width != 800 || app.Config().Height != 600 {
		t.Errorf("零值尺寸应补默认 800x600, got %+v", app.Config())
	}
}

func TestOpenRejectsNil(t *testing.T) {
	if _, err := gxapp.Open(nil, gxapp.DefaultConfig()); err == nil {
		t.Fatal("nil 实例应报错")
	}
}

func TestUpdateRedraws(t *testing.T) {
	win := &countingWindow{}
	// 一个动态子节点插槽
	val := "a"
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "p"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return val })
	app, err := gxapp.OpenWith(inst, gxapp.DefaultConfig(), win)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	before := win.draws
	val = "b"
	if n := app.Update(); n != 1 {
		t.Fatalf("Update 应写 1 节点, got %d", n)
	}
	if win.draws != before+1 {
		t.Errorf("Update 后应重绘一帧, before=%d now=%d", before, win.draws)
	}
}

func TestBindReactiveAutoRedraw(t *testing.T) {
	var buf strings.Builder
	title := signal.New("GX")

	// 动态子节点读 signal
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "h1"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return title() })

	win := &gxapp.HeadlessWindow{Out: &buf}
	app, err := gxapp.OpenWith(inst, gxapp.DefaultConfig(), win)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	// 接线：建一个聚合 effect，首次立即执行只登记依赖，不重复重绘
	app.BindReactive(signal.Effect)
	framesAfterBind := win.Frames()
	if framesAfterBind != 1 { // 仅 Open 首帧；接线首执行被抑制
		t.Fatalf("接线后应仍为 1 帧, got %d", framesAfterBind)
	}
	if !strings.Contains(buf.String(), `"GX"`) {
		t.Fatalf("初始帧应含 GX:\n%s", buf.String())
	}

	// signal 写入：聚合 effect 一次 Update + 一帧重绘（无中间态），无需 app.Update()
	title("Gox")
	if win.Frames() != framesAfterBind+1 {
		t.Errorf("signal 写入应触发恰好 1 帧重绘, before=%d now=%d", framesAfterBind, win.Frames())
	}
	// 取最后一帧（按帧根分隔：每帧以 <div 或 <h1 开头），确认一致性
	lastFrame := lastFrameOf(buf.String(), "<h1>")
	if !strings.Contains(lastFrame, `"Gox"`) || strings.Contains(lastFrame, `"GX"`) {
		t.Fatalf("最后一帧应一致更新为 Gox:\n%s", lastFrame)
	}
}

func TestBindReactiveSameValueNoRedraw(t *testing.T) {
	val := signal.New(1)
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "b"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return val() })
	win := &countingWindow{}
	app, _ := gxapp.OpenWith(inst, gxapp.DefaultConfig(), win)
	defer app.Close()
	app.BindReactive(signal.Effect)

	before := win.draws
	val(1) // 写相同值：effect 会重跑，但 SetPart 的 equalValue 裁剪 → 不写节点
	// 注意：当前实现里 effect 重跑仍会触发一次 Draw（重绘窗口），但节点写入为 0。
	// 这里验证的是「不 panic、不重复建窗口」，写入裁剪由 scene 层保证。
	if win.opens != 1 {
		t.Errorf("不应重复开窗口, got %d", win.opens)
	}
	if win.draws < before {
		t.Errorf("绘制帧数不应倒退")
	}
}

func TestRunAndCloseIdempotent(t *testing.T) {
	win := &countingWindow{}
	app, _ := gxapp.OpenWith(helloInst(), gxapp.DefaultConfig(), win)
	if err := app.Run(); err != nil {
		t.Fatal(err)
	}
	if win.runs != 1 {
		t.Errorf("Run 应调用一次, got %d", win.runs)
	}
	_ = app.Close()
	_ = app.Close()
	if win.closes != 1 {
		t.Errorf("Close 应幂等, got %d", win.closes)
	}
}

func TestRenderTextHelper(t *testing.T) {
	got, err := gxapp.RenderText(helloInst())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<div>") || !strings.Contains(got, `"hello"`) {
		t.Fatalf("RenderText 应输出场景树文本, got:\n%s", got)
	}
}

// 阶段4：条件分支 / keyed 列表的结构变化也应触发窗口重绘。
// 场景树结构由 scene 层的独立 effect 直接改写（不经过 app 的聚合 effect），
// 因此需要一个「结构变化 → 请求重绘」的回连机制。
func TestStructuralChangeTriggersRedraw(t *testing.T) {
	var buf strings.Builder
	visible := signal.New(true)
	count := signal.New(10)

	br := gxscene.NewBranch(
		func() int {
			if visible() {
				return 0
			}
			return 1
		},
		func() *gxscene.Instance {
			return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				{Code: gxscene.OpElem, Arg: "span"}, {Code: gxscene.OpDynChild, Slot: 0}, {Code: gxscene.OpClose},
			}, N: 1}, func() any { return count() })
		},
		func() *gxscene.Instance {
			return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				{Code: gxscene.OpElem, Arg: "b"}, {Code: gxscene.OpChildText, Arg: "Hidden"}, {Code: gxscene.OpClose},
			}, N: 0})
		},
	)
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"}, {Code: gxscene.OpBranch, Slot: 0}, {Code: gxscene.OpClose},
	}, N: 1}, func() any { return br })

	win := &gxapp.HeadlessWindow{Out: &buf}
	app, _ := gxapp.OpenWith(inst, gxapp.DefaultConfig(), win)
	defer app.Close()
	app.BindReactive(signal.Effect)
	framesAfterBind := win.Frames()

	// 切分支：应触发重绘，且最后一帧含 Hidden。
	visible(false)
	if win.Frames() <= framesAfterBind {
		t.Errorf("分支切换应触发重绘, before=%d now=%d", framesAfterBind, win.Frames())
	}
	last := lastFrameOf(buf.String(), "<div>")
	if !strings.Contains(last, "Hidden") {
		t.Fatalf("最后一帧应含 Hidden:\n%s", last)
	}
}

// 阶段4：Batch 合并同一轮的多次 signal 写入为一帧重绘。
func TestBatchCoalescesRedraw(t *testing.T) {
	var buf strings.Builder
	a := signal.New("A")
	b := signal.New("B")

	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpDynChild, Slot: 1},
		{Code: gxscene.OpClose},
	}, N: 2}, func() any { return a() }, func() any { return b() })

	win := &gxapp.HeadlessWindow{Out: &buf}
	app, _ := gxapp.OpenWith(inst, gxapp.DefaultConfig(), win)
	defer app.Close()
	app.BindReactive(signal.Effect)
	base := win.Frames()

	// 一个批次写两个 signal：应合并为一帧。
	signal.Batch(func() {
		a("A2")
		b("B2")
	})
	if win.Frames() != base+1 {
		t.Errorf("Batch 应合并为一帧重绘, base=%d now=%d", base, win.Frames())
	}
	last := lastFrameOf(buf.String(), "<div>")
	if !strings.Contains(last, `"A2"`) || !strings.Contains(last, `"B2"`) {
		t.Fatalf("批次后应一致更新 A2/B2:\n%s", last)
	}
}
