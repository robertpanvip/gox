// GX FPS Bench —— 高刷专项（P4.5）实测工具。
//
// 持续 RAF 动画连帧驱动整条渲染管线（signal 更新 → Host 唯一 UI Loop →
// GRX3 增量 → wgpu present），帧节奏被 swapchain 背压对齐到屏幕刷新率：
// Fifo 下 144Hz 屏应实测 ~144fps。帧时间直方图（avg/p50/p95/p99/max）实时
// 显示在窗口内；按 V 键循环切换 vsync 模式对比真实上屏节奏；关窗后向 stdout
// 打印本次会话汇总，供真机 60/120/144Hz 验收归档。
//
// 运行：cd examples/fpsbench && go run .
// 可选环境变量：
//
//	GX_VSYNC=fifo|mailbox|immediate   初始 vsync 模式（默认 fifo）
//	GX_SAMPLE_WINDOW=N                直方图窗口帧数（默认 600）
//	GX_AUTOCLOSE_MS=N                 N 毫秒后自动关窗（脚本化验收）
//
// 已知环境限制：Xvfb / 无 vblank 的虚拟显示下 Fifo 的 get_current_texture
// 永久阻塞（swapchain 永不被显示方释放），本 demo 需在真实显示器上运行——
// 这正是 Fifo 背压节奏控制的物理机制，也暴露了「present 阻塞占住 UI 线程」
// 的多窗口风险（P4.5 遗留：present 线程化，见 BACKEND_ARCHITECTURE_V2）。
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	rb "github.com/robertpanvip/gox-backend-rust"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
	"github.com/robertpanvip/gox/runtime/signal"
)

// ---- 会话级帧统计（关窗后打印汇总；读写都在 UI 线程，仅汇总打印跨线程读） ----

type bench struct {
	mu       sync.Mutex
	samples  []float64 // 最近 N 帧的帧间隔（ms）
	window   int       // 直方图窗口（GX_SAMPLE_WINDOW）
	total    int       // 会话总帧数
	started  time.Time
	lastMode string
}

func (b *bench) record(dtMS float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.total++
	b.samples = append(b.samples, dtMS)
	if len(b.samples) > b.window {
		b.samples = b.samples[len(b.samples)-b.window:]
	}
}

func (b *bench) reset() {
	b.mu.Lock()
	b.samples = b.samples[:0]
	b.mu.Unlock()
}

func (b *bench) setMode(name string) {
	b.mu.Lock()
	b.lastMode = name
	b.mu.Unlock()
}

// snapshot 返回窗口内的统计（帧数不足时 ok=false）。
func (b *bench) snapshot() (avg, p50, p95, p99, max float64, n int, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.samples) < 10 {
		return 0, 0, 0, 0, 0, len(b.samples), false
	}
	s := append([]float64(nil), b.samples...)
	sort.Float64s(s)
	sum := 0.0
	for _, v := range s {
		sum += v
	}
	pick := func(q float64) float64 { return s[int(q*float64(len(s)-1))] }
	return sum / float64(len(s)), pick(0.50), pick(0.95), pick(0.99), s[len(s)-1], len(s), true
}

func (b *bench) totals() (int, time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.total, time.Since(b.started)
}

func percentileLine(avg, p50, p95, p99, max float64, n int) string {
	return fmt.Sprintf("avg %.2fms | p50 %.2f | p95 %.2f | p99 %.2f | max %.2f  (窗口 %d 帧)",
		avg, p50, p95, p99, max, n)
}

// ---- UI ----

func main() {
	sampleWindow := 600
	if v := os.Getenv("GX_SAMPLE_WINDOW"); v != "" {
		var n int
		if cnt, perr := fmt.Sscanf(v, "%d", &n); perr == nil && cnt > 0 && n > 0 {
			sampleWindow = n
		}
	}
	b := &bench{window: sampleWindow, started: time.Now()}

	fpsSig := signal.New("…")
	statsSig := signal.New("采集中，稍候")
	vsyncSig := signal.New("Fifo（vsync 对齐屏幕刷新率）")

	// V2 接入层装配：gx.use(Backend) → Use(接口值) + 逐窗 Render。
	handle := gxapp.Use(rb.NewBackend())

	// 紧凑单行规避 GX 编译器跨行缩进边界；signal 读用 () 显式求值。
	build := func() any {
		return gxscene.Render(gxscene.Template{Ops: []gxscene.Op{
			gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
			gxscene.Op{Code: gxscene.OpAttr, Arg: "#0b1020", Key: "bg"},
			gxscene.Op{Code: gxscene.OpAttr, Arg: "28", Key: "padding"},
			gxscene.Op{Code: gxscene.OpAttr, Arg: "column", Key: "flex-direction"},
			gxscene.Op{Code: gxscene.OpAttr, Arg: "14", Key: "gap"},
			gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
			gxscene.Op{Code: gxscene.OpAttr, Arg: "#eef3fb", Key: "color"},
			gxscene.Op{Code: gxscene.OpAttr, Arg: "24", Key: "font-size"},
			gxscene.Op{Code: gxscene.OpAttr, Arg: "bold", Key: "weight"},
			gxscene.Op{Code: gxscene.OpChildText, Arg: "GX FPS Bench"},
			gxscene.Op{Code: gxscene.OpClose},
			gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
			gxscene.Op{Code: gxscene.OpAttr, Arg: "#2dd4bf", Key: "color"},
			gxscene.Op{Code: gxscene.OpAttr, Arg: "56", Key: "font-size"},
			gxscene.Op{Code: gxscene.OpAttr, Arg: "bold", Key: "weight"},
			gxscene.Op{Code: gxscene.OpDynChild, Slot: 0},
			gxscene.Op{Code: gxscene.OpClose},
			gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
			gxscene.Op{Code: gxscene.OpAttr, Arg: "#8b9bb4", Key: "color"},
			gxscene.Op{Code: gxscene.OpAttr, Arg: "13", Key: "font-size"},
			gxscene.Op{Code: gxscene.OpDynChild, Slot: 0},
			gxscene.Op{Code: gxscene.OpClose},
			gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
			gxscene.Op{Code: gxscene.OpAttr, Arg: "#a78bfa", Key: "color"},
			gxscene.Op{Code: gxscene.OpAttr, Arg: "16", Key: "font-size"},
			gxscene.Op{Code: gxscene.OpDynChild, Slot: 0},
			gxscene.Op{Code: gxscene.OpClose},
			gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
			gxscene.Op{Code: gxscene.OpAttr, Arg: "#546078", Key: "color"},
			gxscene.Op{Code: gxscene.OpAttr, Arg: "12", Key: "font-size"},
			gxscene.Op{Code: gxscene.OpChildText, Arg: "按 V 切换 vsync 模式（Fifo → Mailbox → Immediate）；关闭窗口结束并在终端打印会话汇总"},
			gxscene.Op{Code: gxscene.OpClose},
			gxscene.Op{Code: gxscene.OpClose},
		}, N: 3},
			func() any { return fpsSig() },
			func() any { return statsSig() },
			func() any { return vsyncSig() },
		)
	}

	app, err := handle.Render(build, gxapp.WindowConfig{Title: "GX FPS Bench", Width: 760, Height: 420})
	if err != nil {
		panic(err)
	}
	defer handle.Close() // Handle.Close 级联：窗口 Close + 后端 Shutdown

	win := handle.WindowOf(app).(*rb.Window)

	// vsync 模式名表 + 当前模式（初始 Fifo）。
	names := []string{"Fifo（vsync 对齐屏幕刷新率）", "Mailbox（三缓冲低延迟）", "Immediate（关 vsync，可能撕裂）"}
	cur := rb.VSyncFifo

	// 初始 vsync 模式（渲染器首泵时由 resumed 应用；不支持的自动回退 Fifo）。
	switch strings.ToLower(os.Getenv("GX_VSYNC")) {
	case "mailbox":
		_ = win.SetVSync(rb.VSyncMailbox)
		cur = rb.VSyncMailbox
		b.setMode(names[1])
		vsyncSig(names[1])
	case "immediate":
		_ = win.SetVSync(rb.VSyncImmediate)
		cur = rb.VSyncImmediate
		b.setMode(names[2])
		vsyncSig(names[2])
	default:
		b.setMode(names[0])
	}

	// V 键循环切换 vsync 模式：unsupported（如 X11 无 Mailbox）自动跳到下一个。
	win.SetDispatcher(func(ev rb.InputEvent) {
		if ev.Kind != "key" || !strings.EqualFold(ev.Text, "v") || ev.Event != "down" {
			return
		}
		for i := 1; i <= 3; i++ {
			next := rb.VSyncMode((int(cur) + i) % 3)
			if werr := win.SetVSync(next); werr == nil {
				cur = next
				b.reset() // 模式切换后重开统计窗口，避免混合两种模式的样本
				b.setMode(names[int(cur)])
				vsyncSig(names[int(cur)])
				return
			}
		}
	})

	// 统一帧调度（V2 阶段 1）：signal 更新标脏 + 唤醒事件泵，动画 RAF 续帧经同一时钟。
	app.SetFrameClock(win.FrameClock())

	// GX_AUTOCLOSE_MS：到时自动请求关窗（脚本化验收：跑满 N 毫秒后打印汇总退出）。
	if v := os.Getenv("GX_AUTOCLOSE_MS"); v != "" {
		var ms int
		if cnt, perr := fmt.Sscanf(v, "%d", &ms); perr == nil && cnt > 0 && ms > 0 {
			time.AfterFunc(time.Duration(ms)*time.Millisecond, win.RequestClose)
		}
	}

	// RAF 动画连帧：帧间隔 ≈ present 节奏（Fifo 下即屏幕刷新率）。
	// 每 30 帧刷新一次直方图文案；signal 写经 FrameClock 合并为下一帧。
	var last time.Time
	var tick int
	var raf func()
	raf = func() {
		now := time.Now()
		if !last.IsZero() {
			b.record(float64(now.Sub(last).Microseconds()) / 1000.0)
		}
		last = now
		if tick++; tick%30 == 0 {
			if avg, p50, p95, p99, mx, n, ok := b.snapshot(); ok {
				fpsSig(fmt.Sprintf("%.0f fps", 1000.0/avg))
				statsSig(percentileLine(avg, p50, p95, p99, mx, n))
			}
		}
		app.RequestAnimationFrame(raf)
	}
	app.RequestAnimationFrame(raf)
	// 关键：Render 的首帧 drawNow 先于 RAF 注册，此后无人请求下一帧——
	// RAF 回调只会在「下一帧的帧首」执行。显式 Update 一次触发第二帧，
	// 之后 RAF 回调写 signal（→ RequestFrame）+ 再登记续帧，动画循环自持。
	app.Update()

	// Host 唯一主循环（P4 目标态）：全部窗口关闭后返回，随后打印会话汇总。
	if rerr := handle.Run(); rerr != nil {
		fmt.Fprintln(os.Stderr, "host run:", rerr)
	}

	total, dur := b.totals()
	fmt.Println("==== GX FPS Bench 会话汇总 ====")
	fmt.Printf("模式: %s | 总帧数: %d | 时长: %s | 整段平均: %.1f fps\n",
		b.lastMode, total, dur.Round(time.Millisecond), float64(total)/dur.Seconds())
	if avg, p50, p95, p99, mx, n, ok := b.snapshot(); ok {
		fmt.Printf("直方图（最后窗口 %d 帧）: %s\n", n, percentileLine(avg, p50, p95, p99, mx, n))
	}
}
