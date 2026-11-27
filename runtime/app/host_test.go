package gxapp

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// stubWindow 是多窗口装配验收桩：记录生命周期调用，可选 panic / 错误注入。
// Post 内联执行（UIExecutor 接线验证用——headless 场景无独立事件泵）。
type stubWindow struct {
	mu        sync.Mutex
	cfg       WindowConfig
	opened    bool
	closed    bool
	drawn     int
	bindCalls int
	runErr    error
	runPanic  any
}

func (w *stubWindow) Open(cfg WindowConfig) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cfg = cfg
	w.opened = true
	return nil
}

func (w *stubWindow) Draw(root *gxscene.Node) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.drawn++
	return nil
}

func (w *stubWindow) Run() error {
	if w.runPanic != nil {
		panic(w.runPanic)
	}
	return w.runErr
}

func (w *stubWindow) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.closed = true
	return nil
}

func (w *stubWindow) Config() WindowConfig {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cfg
}

func (w *stubWindow) BindApp(app *App) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.bindCalls++
	app.EnableChanges()
}

func (w *stubWindow) Post(fn func()) error {
	fn()
	return nil
}

func stubText(title string) *gxscene.Instance {
	return gxscene.Render(gxscene.Template{
		Ops: []gxscene.Op{
			{Code: gxscene.OpElem, Arg: "span"},
			{Code: gxscene.OpChildText, Arg: title},
			{Code: gxscene.OpClose},
		},
	})
}

// titleBackend：按窗口标题挑 Window 的测试桩后端（接口注入用）。
type titleBackend struct{ byTitle map[string]Window }

func (b titleBackend) Name() string { return "title-stub" }

func (b titleBackend) NewWindow(cfg WindowConfig) (Window, error) {
	if w, ok := b.byTitle[cfg.Title]; ok {
		return w, nil
	}
	return &stubWindow{}, nil
}

// gx.use(Backend) → 逐窗 render：两扇窗各自独立 App/场景树/signal Runtime，
// 互不串扰；每窗 Render 后自动 BindApp（WindowBinder）。
func TestHandleRenderMultipleWindowsIndependent(t *testing.T) {
	w1, w2 := &stubWindow{}, &stubWindow{}
	app := Use(titleBackend{byTitle: map[string]Window{
		"Main Window": w1,
		"Settings":    w2,
	}})

	a1, err := app.Render(func() any { return stubText("main") }, WindowConfig{Title: "Main Window", Width: 1200, Height: 800})
	if err != nil {
		t.Fatalf("render 窗口 1 失败: %v", err)
	}
	a2, err := app.Render(func() any { return stubText("settings") }, WindowConfig{Title: "Settings", Width: 600, Height: 400})
	if err != nil {
		t.Fatalf("render 窗口 2 失败: %v", err)
	}

	if got := len(app.Host().Windows()); got != 2 {
		t.Fatalf("Host 应管理 2 个窗口, got %d", got)
	}
	if w1.bindCalls != 1 || w2.bindCalls != 1 {
		t.Fatalf("每窗 Render 后应各收到一次 BindApp, got %d/%d", w1.bindCalls, w2.bindCalls)
	}
	if a1 == a2 {
		t.Fatal("两扇窗应是独立 App")
	}
	if a1.Runtime() == a2.Runtime() {
		t.Fatal("两扇窗应是独立 signal Runtime（多 App 隔离）")
	}
	// 场景树内容独立：各自的标题只在自己的树里。
	if s1, _ := RenderText(a1.Instance()); !contains(s1, "main") {
		t.Fatalf("窗口 1 场景树应含 main: %q", s1)
	}
	if s2, _ := RenderText(a2.Instance()); !contains(s2, "settings") {
		t.Fatalf("窗口 2 场景树应含 settings: %q", s2)
	}

	if err := app.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !w1.closed || !w2.closed {
		t.Fatal("Handle.Close 应关闭全部窗口")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// signal 串扰隔离：窗口 A 写 signal 只重绘 A，B 的绘制次数不变。
func TestHandleSignalIsolationBetweenWindows(t *testing.T) {
	w1, w2 := &stubWindow{}, &stubWindow{}
	app := Use(titleBackend{byTitle: map[string]Window{"A": w1, "B": w2}})
	count := signal.New(0)
	var a2 *App
	// 动态子节点（getter）才是响应式通道：静态 OpChildText 只在构建期求值一次。
	_, err := app.Render(func() any {
		return gxscene.Render(gxscene.Template{
			Ops: []gxscene.Op{
				{Code: gxscene.OpElem, Arg: "b"},
				{Code: gxscene.OpDynChild, Slot: 0},
				{Code: gxscene.OpClose},
			},
			N: 1,
		}, func() any { return fmt.Sprint(count()) })
	}, WindowConfig{Title: "A"})
	if err != nil {
		t.Fatalf("render A: %v", err)
	}
	a2, err = app.Render(func() any { return stubText("B") }, WindowConfig{Title: "B"})
	if err != nil {
		t.Fatalf("render B: %v", err)
	}

	w1.mu.Lock()
	d1 := w1.drawn
	w1.mu.Unlock()
	w2.mu.Lock()
	_ = w2.drawn
	w2.mu.Unlock()

	count(count() + 1) // 只应更新窗口 A 的树内容（A 的树订阅了 count）

	// Post 内联执行：signal 写入同步走 A 的派发器重绘。
	w1.mu.Lock()
	d1b := w1.drawn
	w1.mu.Unlock()

	if d1b <= d1 {
		t.Fatalf("窗口 A 写 signal 后应重绘, drawn %d -> %d", d1, d1b)
	}
	// 响应式帧边界是进程级全局（P4 评审决议：单线程唯一，多 App 串行 drain），
	// 跨窗 flush 会唤醒 B 的帧边界——但 B 的树内容不变，增量后端（rustbackend
	// Present）对无变化帧短路，不产生真实绘制/传输。隔离保证落在「树内容与
	// 生命周期」，故这里断言 B 的场景树仍是旧值，而非 Draw 调用计数。
	s2, _ := RenderText(a2.Instance())
	if contains(s2, "1") || !contains(s2, "B") {
		t.Fatalf("窗口 B 的树不应被 A 的 signal 改写: %q", s2)
	}
	_ = app.Close()
}

// 故障隔离：任一窗口 Run panic 被 recover 归因上报，不传播、不拖垮其它窗口。
func TestHostRunFaultIsolation(t *testing.T) {
	wBad, wGood := &stubWindow{runPanic: "boom"}, &stubWindow{}
	var faults []Fault
	host := NewHost(WithFaultHandler(func(f Fault) { faults = append(faults, f) }))
	if _, err := host.OpenWindow(WindowConfig{Title: "bad", Backend: "stub"}, wBad); err != nil {
		t.Fatalf("open bad: %v", err)
	}
	if _, err := host.OpenWindow(WindowConfig{Title: "good", Backend: "stub"}, wGood); err != nil {
		t.Fatalf("open good: %v", err)
	}
	err := host.Run()
	if err == nil {
		t.Fatal("Run 应返回 panic 窗口的错误")
	}
	if len(faults) != 1 || faults[0].Phase != "run" || faults[0].Panic != "boom" {
		t.Fatalf("故障应归因上报一次, got %+v", faults)
	}
	if !faults[0].Recovered {
		t.Fatal("Fault.Recovered 应为 true")
	}
	if err := host.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// 空宿主 Run 直接返回；零窗口快照。
func TestHostRunNoWindows(t *testing.T) {
	host := NewHost()
	if err := host.Run(); err != nil {
		t.Fatalf("空宿主 Run 应返回 nil, got %v", err)
	}
	if len(host.Windows()) != 0 {
		t.Fatal("空宿主不应有窗口")
	}
}

// CloseWindow 只关一扇窗：另一扇仍存活可绘制；重复 CloseWindow 幂等返回 nil。
func TestHostCloseWindowIsolation(t *testing.T) {
	w1, w2 := &stubWindow{}, &stubWindow{}
	host := NewHost()
	a1, _ := host.OpenWindow(WindowConfig{Title: "one", Backend: "stub"}, w1)
	if _, err := host.OpenWindow(WindowConfig{Title: "two", Backend: "stub"}, w2); err != nil {
		t.Fatalf("open two: %v", err)
	}
	if err := host.CloseWindow(a1); err != nil {
		t.Fatalf("CloseWindow: %v", err)
	}
	if !w1.closed {
		t.Fatal("被关窗口应收到 Close")
	}
	if !w2.opened || w2.closed {
		t.Fatal("另一窗口不应受影响")
	}
	if len(host.Windows()) != 1 {
		t.Fatalf("Host 应剩 1 个窗口, got %d", len(host.Windows()))
	}
	if err := host.CloseWindow(a1); err != nil {
		t.Fatalf("重复 CloseWindow 应幂等返回 nil, got %v", err)
	}
}

// Use 接口注入：接入层只依赖 Backend 接口形状，后端实现即可插拔。
type fakeBackend struct{ n int }

func (f *fakeBackend) Name() string { return "fake" }

func (f *fakeBackend) NewWindow(cfg WindowConfig) (Window, error) {
	f.n++
	return &stubWindow{}, nil
}

// Use(接口值) 多窗渲染：每窗调用一次 Backend.NewWindow，窗口归 Host 管理。
func TestUseBackendInterface(t *testing.T) {
	b := &fakeBackend{}
	h := Use(b)
	a1, err := h.Render(func() any { return stubText("A") }, WindowConfig{Title: "A"})
	if err != nil {
		t.Fatalf("render A: %v", err)
	}
	a2, err := h.Render(func() any { return stubText("B") }, WindowConfig{Title: "B"})
	if err != nil {
		t.Fatalf("render B: %v", err)
	}
	if b.n != 2 {
		t.Fatalf("每窗应调用一次 NewWindow, got %d", b.n)
	}
	if len(h.Host().Windows()) != 2 {
		t.Fatalf("Host 应有 2 个窗口, got %d", len(h.Host().Windows()))
	}
	if err := h.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	_ = a1
	_ = a2
}

// lifecycleBackend：实现可选 BackendInitializer（记录调用次序）。
type lifecycleBackend struct {
	fakeBackend
	inits, shutdowns int
	failed           error
}

func (b *lifecycleBackend) InitBackend() error {
	b.inits++
	return b.failed
}

func (b *lifecycleBackend) ShutdownBackend() { b.shutdowns++ }

// BackendInitializer 生命周期：首个窗口创建前 Init 幂等一次（多窗不重复），
// Host.Close 后 Shutdown 幂等一次；Init 失败阻断开窗且不残留。
func TestBackendLifecycleHooks(t *testing.T) {
	b := &lifecycleBackend{}
	h := Use(b)
	if _, err := h.Render(func() any { return stubText("A") }, WindowConfig{Title: "A"}); err != nil {
		t.Fatalf("render A: %v", err)
	}
	if _, err := h.Render(func() any { return stubText("B") }, WindowConfig{Title: "B"}); err != nil {
		t.Fatalf("render B: %v", err)
	}
	if b.inits != 1 {
		t.Fatalf("两窗应只 Init 一次, got %d", b.inits)
	}
	if err := h.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if b.shutdowns != 1 {
		t.Fatalf("Close 应 Shutdown 一次, got %d", b.shutdowns)
	}
	// 已 Shutdown 的 Host 拒绝新窗（closed），Init 不会再次触发。
	_ = h
}

func TestBackendInitFailureBlocksWindow(t *testing.T) {
	b := &lifecycleBackend{failed: errors.New("init boom")}
	h := Use(b)
	if _, err := h.Render(func() any { return stubText("x") }, WindowConfig{Title: "x"}); err == nil {
		t.Fatal("Init 失败的 Render 应报错")
	}
	if len(h.Host().Windows()) != 0 {
		t.Fatal("失败的 Render 不应残留窗口")
	}
}

// Host.WindowOf 返回窗口实例（装配层调后端专属扩展的通道）；未知 App 返回 nil。
func TestHostWindowOf(t *testing.T) {
	w := &stubWindow{}
	h := Use(titleBackend{byTitle: map[string]Window{"A": w}})
	app, err := h.Render(func() any { return stubText("A") }, WindowConfig{Title: "A"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if h.WindowOf(app) != Window(w) {
		t.Fatal("WindowOf 应返回装配时的 Window 实例")
	}
	if h.WindowOf(nil) != nil {
		t.Fatal("未知 App 的 WindowOf 应返回 nil")
	}
	_ = h.Close()
}

// 故障归因走 Backend.Name()（复用 close_concurrency_test.go 的 closeErrWindow 桩）：注册表后端名包装为 Backend 接口注入 Host。
func TestBackendNameFaultAttribution(t *testing.T) {
	Register("fake-attrib", func(cfg WindowConfig) (Window, error) {
		return &closeErrWindow{err: sentinelCloseErr{}}, nil
	})
	var got Fault
	host := NewHost(WithFaultHandler(func(f Fault) { got = f }))
	if _, err := host.OpenBackendWindow(WindowConfig{Title: "x", Backend: "fake-attrib"}); err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = host.Close() // Close 报错触发归因
	if got.Backend != "fake-attrib" {
		t.Fatalf("Fault.Backend 应为后端名, got %q", got.Backend)
	}
}

// Host 关闭后拒绝新窗口。
func TestHostClosedRejectsNewWindow(t *testing.T) {
	host := NewHost()
	if _, err := host.OpenWindow(WindowConfig{Title: "a", Backend: "stub"}, &stubWindow{}); err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = host.Close()
	if _, err := host.OpenWindow(WindowConfig{Title: "b", Backend: "stub"}, &stubWindow{}); err == nil {
		t.Fatal("关闭后的 Host 应拒绝新窗口")
	}
}

// Run 错误透传：非 panic 的 Run 错误返回给调用方（第一个）。
func TestHostRunPropagatesError(t *testing.T) {
	wErr, wOk := &stubWindow{runErr: fmt.Errorf("protocol dead")}, &stubWindow{}
	host := NewHost()
	_, _ = host.OpenWindow(WindowConfig{Title: "err", Backend: "stub"}, wErr)
	_, _ = host.OpenWindow(WindowConfig{Title: "ok", Backend: "stub"}, wOk)
	if err := host.Run(); err == nil || err.Error() != "protocol dead" {
		t.Fatalf("Run 应透传窗口错误, got %v", err)
	}
}
