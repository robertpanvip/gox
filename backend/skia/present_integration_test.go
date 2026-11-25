package skia

import (
	"testing"

	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// TestSkiaWindowAsFramePresenter：skia.Window 经 App 装配后应走 FramePresenter
// 路径（App 自动 EnableChanges）。signal 更新触发的帧带 Change→重绘；随后一个
// 无变化的显式帧（模拟相等性短路）应快速复用。验证增量帧在真实装配链路中生效。
func TestSkiaWindowAsFramePresenter(t *testing.T) {
	w := NewWindow()
	app, err := gxapp.OpenWith(gxapp.WindowConfig{Width: 400, Height: 200}, w)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()

	val := signal.New("A")
	inst := gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpDynChild, Slot: 0},
		{Code: gxscene.OpClose},
	}, N: 1}, func() any { return val() })

	if err := app.Render(func() any { return inst }); err != nil {
		t.Fatal(err)
	}
	r0, p0 := w.cache.CacheStats()
	if p0 < 1 {
		t.Fatalf("首帧应至少绘制一次, paints=%d", p0)
	}

	// signal 变化 → 带 Change 的更新帧 → 必须重绘。
	val("B")
	r1, p1 := w.cache.CacheStats()
	if p1 <= p0 {
		t.Fatalf("signal 变化应触发重绘: paints %d→%d (reuses %d→%d)", p0, p1, r0, r1)
	}

	// 无变化的显式 Present 帧应快速复用：只增 reuses、不增 paints。
	r2, p2 := w.cache.CacheStats()
	f := &gxapp.Frame{Root: inst.Root(), Version: 999}
	if err := w.Present(f); err != nil {
		t.Fatal(err)
	}
	r3, p3 := w.cache.CacheStats()
	if p3 != p2 || r3 != r2+1 {
		t.Fatalf("无 Change 帧应快速复用: paints %d→%d reuses %d→%d", p2, p3, r2, r3)
	}
}
