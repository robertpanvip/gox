package gxapp_test

import (
	"fmt"
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
	if err := app.Render(func() any { return inst }); err != nil {
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
		t.Fatal("nil 建树函数应报错")
	}
	_ = app.Close()
	if err := app.Render(func() any { return helloInst() }); err == nil {
		t.Fatal("关闭后 Render 应报错")
	}
}

func TestOpenRejectsNilWindow(t *testing.T) {
	if _, err := gxapp.OpenWith(gxapp.DefaultConfig(), nil); err == nil {
		t.Fatal("nil 窗口后端应报错")
	}
}

func TestRenderRejectsNonInstanceBuilder(t *testing.T) {
	win := &countingWindow{}
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()
	if err := app.Render(func() any { return 42 }); err == nil {
		t.Fatal("建树函数未返回 *gxscene.Instance 应报错")
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
	if err := app.Render(func() any { return inst }); err != nil {
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
	if err := app.Render(func() any { return inst }); err != nil {
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
	if err := app.Render(func() any { return inst }); err != nil {
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
	if err := app.Render(func() any { return helloInst() }); err != nil {
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
	if err := app.Render(func() any { return inst }); err != nil {
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
	if err := app.Render(func() any { return inst }); err != nil {
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
	if err := app.Render(func() any { return makeInst(func() any { return oldVal() }) }); err != nil {
		t.Fatal(err)
	}
	if err := app.Render(func() any { return makeInst(func() any { return newVal() }) }); err != nil {
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

// 组件生命周期（SolidJS 风格）：组件在建树时创建的 onCleanup 归属本次 Render 的
// 根 Owner，App.Close / 重复 Render 时级联执行；组件内 signal 写入照常驱动重绘。
func TestComponentLocalStateAndCleanupOwnedByApp(t *testing.T) {
	var buf strings.Builder
	win := &gxapp.HeadlessWindow{Out: &buf}
	app := openApp(t, gxapp.DefaultConfig(), win)

	cleaned := 0
	// build 模拟一个组件函数：局部 signal + computed + effect + onCleanup。
	build := func() any {
		title := signal.New("v0")
		// 组件内 effect：登记 cleanup，读 title 登记依赖。
		signal.Effect(func() {
			_ = title()
			signal.OnCleanup(func() { cleaned++ })
		})
		return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "h2"}, {Code: gxscene.OpDynChild, Slot: 0}, {Code: gxscene.OpClose},
		}, N: 1}, func() any { return title() })
	}
	if err := app.Render(build); err != nil {
		t.Fatal(err)
	}
	base := win.Frames()

	// cleanup 尚未执行（首次挂载）。
	if cleaned != 0 {
		t.Fatalf("挂载后不应执行 cleanup, got %d", cleaned)
	}
	// 关闭：级联执行组件 cleanup。
	_ = app.Close()
	if cleaned != 1 {
		t.Fatalf("Close 应执行一次组件 cleanup, got %d", cleaned)
	}
	// 二次关闭幂等，不重复执行。
	_ = app.Close()
	if cleaned != 1 {
		t.Fatalf("Close 应幂等, got %d", cleaned)
	}
	_ = base
}

func TestReRenderDisposesPreviousComponentCleanup(t *testing.T) {
	win := &countingWindow{}
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()

	cleaned := 0
	build := func() any {
		signal.Effect(func() {
			signal.OnCleanup(func() { cleaned++ })
		})
		return helloInst()
	}
	if err := app.Render(build); err != nil {
		t.Fatal(err)
	}
	if err := app.Render(build); err != nil {
		t.Fatal(err)
	}
	// 第二次 Render 销毁了第一棵树的 Owner，旧组件 cleanup 执行一次。
	if cleaned != 1 {
		t.Fatalf("重复 Render 应销毁旧组件 cleanup, got %d", cleaned)
	}
}

// TestEventDispatchToSignalRedraw 是事件协议的端到端 PoC：
// 后端命中节点 → Dispatch 事件 → handler 写 signal → App 自动 Update + 重绘一帧。
// 前端只负责把 handler 原样放进 Handlers，事件语义/命中归后端，响应式归 signal。
func TestEventDispatchToSignalRedraw(t *testing.T) {
	var buf strings.Builder
	clicks := signal.New(0)

	// 按钮：onClick handler 内写 signal；一个动态子节点显示点击数（读 signal）。
	// getter 返回 gxscene.Handler 签名（协议推荐），后端 Dispatch 即可调用。
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "button"},
		{Code: gxscene.OpEvent, Key: "onClick", Slot: 0},
		{Code: gxscene.OpElem, Arg: "span"},
		{Code: gxscene.OpDynChild, Slot: 1},
		{Code: gxscene.OpClose},
		{Code: gxscene.OpClose},
	}, N: 2},
		func() any {
			return gxscene.Handler(func(gxscene.EventContext) { clicks(clicks() + 1) })
		},
		func() any { return clicks() },
	)

	win := &gxapp.HeadlessWindow{Out: &buf}
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()
	if err := app.Render(func() any { return inst }); err != nil {
		t.Fatal(err)
	}
	frames0 := win.Frames()
	if !strings.Contains(buf.String(), `"0"`) {
		t.Fatalf("初始帧应含 0:\n%s", buf.String())
	}

	// ——模拟后端命中测试后派发事件——
	btn := app.Instance().Root().Children[0]
	if btn.Type != gxscene.NodeDiv {
		t.Fatalf("button 应为 NodeDiv, got %v", btn.Type)
	}
	if !btn.Dispatch("onClick", gxscene.EventContext{Raw: "native-click"}) {
		t.Fatal("onClick 应可派发")
	}
	if clicks() != 1 {
		t.Fatalf("handler 应把 signal 加到 1, got %d", clicks())
	}
	if win.Frames() != frames0+1 {
		t.Errorf("事件改 signal 后应重绘恰好 1 帧, before=%d now=%d", frames0, win.Frames())
	}
	last := lastFrameOf(buf.String(), "<button")
	if !strings.Contains(last, `"1"`) {
		t.Fatalf("最后一帧应显示 1:\n%s", last)
	}
}

// P2-1 回归守护：3 个结构插槽，接线期应整体静音，Render 后只画首帧 1 帧。
// 旧的 structPriming 布尔只挡第一个插槽的首次回调，其余每个各画一帧（共 3 帧）。
func TestWiringDrawsOnlyFirstFrame(t *testing.T) {
	v := signal.New(true)
	win := &countingWindow{}
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()

	branchComp := func() func() any {
		return func() any {
			br := gxscene.NewBranch(
				func() int {
					if v() {
						return 0
					}
					return 1
				},
				func() *gxscene.Instance {
					return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
						{Code: gxscene.OpElem, Arg: "span"}, {Code: gxscene.OpChildText, Arg: "T"}, {Code: gxscene.OpClose},
					}})
				},
				func() *gxscene.Instance {
					return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
						{Code: gxscene.OpElem, Arg: "b"}, {Code: gxscene.OpChildText, Arg: "E"}, {Code: gxscene.OpClose},
					}})
				},
			)
			return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				{Code: gxscene.OpElem, Arg: "div"}, {Code: gxscene.OpBranch, Slot: 0}, {Code: gxscene.OpClose},
			}, N: 1}, func() any { return br })
		}
	}

	if err := app.Render(func() any {
		return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "div"},
			{Code: gxscene.OpComp, Arg: "A", Slot: 0},
			{Code: gxscene.OpComp, Arg: "B", Slot: 1},
			{Code: gxscene.OpComp, Arg: "C", Slot: 2},
			{Code: gxscene.OpClose},
		}, N: 3}, branchComp(), branchComp(), branchComp())
	}); err != nil {
		t.Fatal(err)
	}
	if got := win.draws; got != 1 {
		t.Fatalf("Render 后应只画首帧 1 帧（接线期静音）, got %d", got)
	}
}

// P2-2 回归守护：app 场景下新建列表项内条件分支的 selector，一次切换只调用一次。
// 旧的 buildEntry 同时用 BindReactive + bindRec 接同一 slotBranch，selector 被调用两次。
func TestForKeyedItemSelectorSubscribedOnce(t *testing.T) {
	items := signal.New([]int{1})
	inner := signal.New(true)
	calls := 0

	f := gxscene.NewFor(
		func() []any { return []any{items()[0]} },
		func(v any) any { return v },
		func(v any) *gxscene.Instance {
			br := gxscene.NewBranch(
				func() int {
					calls++
					if inner() {
						return 0
					}
					return 1
				},
				func() *gxscene.Instance {
					return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
						{Code: gxscene.OpElem, Arg: "span"}, {Code: gxscene.OpChildText, Arg: "T"}, {Code: gxscene.OpClose},
					}})
				},
				func() *gxscene.Instance {
					return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
						{Code: gxscene.OpElem, Arg: "b"}, {Code: gxscene.OpChildText, Arg: "E"}, {Code: gxscene.OpClose},
					}})
				},
			)
			return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				{Code: gxscene.OpElem, Arg: "li"}, {Code: gxscene.OpBranch, Slot: 0}, {Code: gxscene.OpClose},
			}, N: 1}, func() any { return br })
		},
	)

	win := &countingWindow{}
	app := openApp(t, gxapp.DefaultConfig(), win)
	defer app.Close()
	if err := app.Render(func() any {
		return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "ul"}, {Code: gxscene.OpFor, Slot: 0}, {Code: gxscene.OpClose},
		}, N: 1}, func() any { return f })
	}); err != nil {
		t.Fatal(err)
	}

	calls = 0    // 接线期不计
	inner(false) // 一次项内分支切换
	if calls != 1 {
		t.Fatalf("一次分支切换 selector 应调用 1 次（无双订阅）, got %d", calls)
	}
}

// P2 回归守护：批量新增含 if 的列表项，无论多少项都只画 1 帧。
// 静音下沉到 bindCtx 前，每项内分支 effect 首次回调各画一帧（N 项 = N+1 帧）。
func TestBatchAddWithIfDrawsOneFrame(t *testing.T) {
	for _, n := range []int{3, 10, 50} {
		items := signal.New([]string{"init"})
		f := gxscene.NewFor(
			func() []any {
				out := []any{}
				for _, s := range items() {
					out = append(out, s)
				}
				return out
			},
			func(v any) any { return v },
			func(v any) *gxscene.Instance {
				s := v.(string)
				flag := signal.New(true)
				br := gxscene.NewBranch(
					func() int {
						if flag() {
							return 0
						}
						return 1
					},
					func() *gxscene.Instance {
						return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
							{Code: gxscene.OpElem, Arg: "span"}, {Code: gxscene.OpDynChild, Slot: 0}, {Code: gxscene.OpClose},
						}, N: 1}, func() any { return s })
					},
					func() *gxscene.Instance {
						return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
							{Code: gxscene.OpElem, Arg: "b"}, {Code: gxscene.OpChildText, Arg: "x"}, {Code: gxscene.OpClose},
						}})
					},
				)
				return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
					{Code: gxscene.OpElem, Arg: "li"}, {Code: gxscene.OpBranch, Slot: 0}, {Code: gxscene.OpClose},
				}, N: 1}, func() any { return br })
			},
		)
		win := &countingWindow{}
		app := openApp(t, gxapp.DefaultConfig(), win)
		app.Render(func() any {
			return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
				{Code: gxscene.OpElem, Arg: "ul"}, {Code: gxscene.OpFor, Slot: 0}, {Code: gxscene.OpClose},
			}, N: 1}, func() any { return f })
		})
		defer app.Close()
		base := win.draws
		// 用唯一 key 避免 For 把相同值的项去重成一项。
		next := []string{"init"}
		for i := 0; i < n; i++ {
			next = append(next, fmt.Sprintf("x%d", i))
		}
		items(next)
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			if got := win.draws - base; got != 1 {
				t.Errorf("批量新增 %d 个含 if 的项应只画 1 帧, got %d", n, got)
			}
		})
	}
}
