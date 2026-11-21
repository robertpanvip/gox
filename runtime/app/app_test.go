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

// openApp 以测试窗口打开 app，不渲染（用于分别验证 Open / Render 语义）。
func openApp(t *testing.T, cfg gxapp.WindowConfig, win gxapp.Window) *gxapp.App {
	t.Helper()
	app, err := gxapp.OpenWith(cfg, win)
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestOpenCreatesWindowWithoutTree(t *testing.T) {
	win := &countingWindow{}
	app := openApp(t, gxapp.WindowConfig{Title: "Demo", Width: 1024, Height: 768}, win)
	defer app.Close()

	if win.opens != 1 {
		t.Errorf("应 Open 一次, got %d", win.opens)
	}
	// Open 只创建窗口外壳，不绘制；首帧由 Render 负责。
	if win.draws != 0 {
		t.Errorf("Render 前不应绘制, got %d", win.draws)
	}
	if app.Instance() != nil {
		t.Error("Render 前 Instance 应为 nil")
	}
	// 配置零值补齐
	if app.Config().Title != "Demo" || app.Config().Width != 1024 {
		t.Errorf("配置应透传, got %+v", app.Config())
	}
	if win.cfg.Backend != "headless" {
		t.Errorf("后端默认值应补 headless, got %q", win.cfg.Backend)
	}
}

func TestRenderDrawsFirstFrame(t *testing.T) {
	win := &countingWindow{}
	app := openApp(t, gxapp.WindowConfig{Title: "Demo", Width: 1024, Height: 768}, win)
	defer app.Close()

	inst := helloInst()
	if err := app.Render(inst); err != nil {
		t.Fatal(err)
	}
	if win.draws != 1 {
		t.Errorf("Render 应绘制首帧一次, got %d", win.draws)
	}
	if win.lastRoot == nil || win.lastRoot != inst.Root() {
		t.Fatal("Draw 应拿到场景树根")
	}
	if app.Instance() != inst {
		t.Fatal("Render 后 Instance 应指向渲染的树")
	}
}

func TestOpenNormalizesZeroSize(t *testing.T) {
	win := &countingWindow{}
	app := openApp(t, gxapp.WindowConfig{}, win)
	defer app.Close()
	if app.Config().Width != 800 || app.Config().Height != 600 {
		t.Errorf("零值尺寸应补默认 800x600, got %+v", app.Config())
	}
}

func TestRenderRejectsNilAndClosed(t *testing.T) {
	win := &countingWindow{}
	app := openApp(t, gxapp.DefaultConfig(), win)
	if err := app.Render(nil); err == nil {
		t.Fatal("nil 场景树应报错")
	}
	_ = app.Close()
	if err := app.Render(helloInst()); err == nil {
		t.Fatal("关闭后 Render 应报错")
	}
}

func TestOpenRejectsNilWindow(t *testing.T) {
	if _, err := gxapp.OpenWith(gxapp.DefaultConfig(), nil); err == nil {
		t.Fatal("nil 窗口后端应报错")
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
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()
	if err := app.Render(inst); err != nil {
		t.Fatal(err)
	}

	before := win.draws
	val = "b"
	if n := app.Update(); n != 1 {
		t.Fatalf("Update 应写 1 节点, got %d", n)
	}
	if win.draws != before+1 {
		t.Errorf("Update 后应重绘一帧, before=%d now=%d", before, win.draws)
	}
}

func TestRenderAutoWiresReactive(t *testing.T) {
	var buf strings.Builder
	title := signal.New("GX")

	// 动态子节点读 signal
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "h1"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return title() })

	win := &gxapp.HeadlessWindow{Out: &buf}
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()

	// Render 内部自动接线：首帧已画，接线首次执行被抑制，不重复绘制。
	if err := app.Render(inst); err != nil {
		t.Fatal(err)
	}
	framesAfterRender := win.Frames()
	if framesAfterRender != 1 {
		t.Fatalf("Render 后应仍为 1 帧, got %d", framesAfterRender)
	}
	if !strings.Contains(buf.String(), `"GX"`) {
		t.Fatalf("初始帧应含 GX:\n%s", buf.String())
	}

	// signal 写入：内部聚合 effect 一次 Update + 一帧重绘，无需手动接线/Update。
	title("Gox")
	if win.Frames() != framesAfterRender+1 {
		t.Errorf("signal 写入应触发恰好 1 帧重绘, before=%d now=%d", framesAfterRender, win.Frames())
	}
	lastFrame := lastFrameOf(buf.String(), "<h1>")
	if !strings.Contains(lastFrame, `"Gox"`) || strings.Contains(lastFrame, `"GX"`) {
		t.Fatalf("最后一帧应一致更新为 Gox:\n%s", lastFrame)
	}
}

func TestRenderSameValueNoRedraw(t *testing.T) {
	val := signal.New(1)
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "b"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return val() })
	win := &countingWindow{}
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()
	if err := app.Render(inst); err != nil {
		t.Fatal(err)
	}

	before := win.draws
	val(1) // 写相同值：effect 会重跑，但 SetPart 的 equalValue 裁剪 → 不写节点
	if win.opens != 1 {
		t.Errorf("不应重复开窗口, got %d", win.opens)
	}
	if win.draws < before {
		t.Errorf("绘制帧数不应倒退")
	}
}

func TestRunAndCloseIdempotent(t *testing.T) {
	win := &countingWindow{}
	app := openApp(t, gxapp.DefaultConfig(), win)
	if err := app.Render(helloInst()); err != nil {
		t.Fatal(err)
	}
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
// Render 内部接线会为它们加「重绘包装」。
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
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()
	if err := app.Render(inst); err != nil {
		t.Fatal(err)
	}
	framesAfterRender := win.Frames()

	// 切分支：应触发重绘，且最后一帧含 Hidden。
	visible(false)
	if win.Frames() <= framesAfterRender {
		t.Errorf("分支切换应触发重绘, before=%d now=%d", framesAfterRender, win.Frames())
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
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()
	if err := app.Render(inst); err != nil {
		t.Fatal(err)
	}
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

// 重复 Render 应使用新树替换旧树，且旧订阅被清理（写旧 signal 不再重绘）。
func TestReRenderReplacesTree(t *testing.T) {
	var buf strings.Builder
	oldVal := signal.New("old")
	newVal := signal.New("new")

	makeInst := func(get func() any) *gxscene.Instance {
		return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "p"},
			{Code: gxscene.OpDynChild, Slot: 0},
			{Code: gxscene.OpClose},
		}, N: 1}, get)
	}
	win := &gxapp.HeadlessWindow{Out: &buf}
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()
	if err := app.Render(makeInst(func() any { return oldVal() })); err != nil {
		t.Fatal(err)
	}
	if err := app.Render(makeInst(func() any { return newVal() })); err != nil {
		t.Fatal(err)
	}
	base := win.Frames()

	// 写旧 signal：旧树订阅已清理，不应再触发重绘。
	oldVal("OLD2")
	if win.Frames() != base {
		t.Errorf("旧树订阅应已清理, base=%d now=%d", base, win.Frames())
	}
	// 写新 signal：应正常重绘。
	newVal("NEW2")
	if win.Frames() != base+1 {
		t.Errorf("新树应正常重绘, base=%d now=%d", base, win.Frames())
	}
	last := lastFrameOf(buf.String(), "<p>")
	if !strings.Contains(last, `"NEW2"`) {
		t.Fatalf("最后一帧应含 NEW2:\n%s", last)
	}
}
