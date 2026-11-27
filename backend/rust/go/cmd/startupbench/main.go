// startupbench 测量 Rust 后端「进程启动 → 首帧可见」的分阶段耗时。
//
// 口径：
//   - 进程内 t0 取 main 最早时刻（近似 Go runtime 就绪）；
//   - OpenWith：gx_window_new（仅分配句柄，几乎瞬时）；
//   - Render：建树 + GRX1 序列化 + memcpy 到共享区（首帧"提交"）；
//   - Run：进入 winit 事件循环，真正的 wgpu 适配器/设备 + 字体系统初始化 + 首帧光栅化都在这里；
//   - 首帧可见：Rust 侧首帧 render() 完成后，若设置了 GX_STARTUP_PROBE=1，
//     会通过事件回调回传 kind="startup_first_frame" 事件，其 text 携带 Rust 内部阶段耗时。
//
// 为拿到 exec→main 的开销，外层用 Python 测总 wall time；本程序报告 main 之后的分解。
// 与真实 demo 用同一套场景构造（中等规模，节点数对启动时间影响很小，主要是 GPU/字体初始化）。
package main

import (
	"fmt"
	"os"
	"time"

	rb "github.com/robertpanvip/gox-backend-rust"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	gxscene "github.com/robertpanvip/gox/runtime/scene"
)

var procMainStart = time.Now()

func buildScene() gxscene.Node {
	ops := []gxscene.Op{
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpAttr, Key: "flex-direction", Arg: "column"},
		{Code: gxscene.OpAttr, Key: "width", Arg: "760"},
		{Code: gxscene.OpAttr, Key: "padding", Arg: "20"},
		{Code: gxscene.OpAttr, Key: "align-items", Arg: "center"},
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpAttr, Key: "color", Arg: "#e2e8f0"},
		{Code: gxscene.OpAttr, Key: "font-size", Arg: "26"},
		{Code: gxscene.OpChildText, Arg: "Startup Benchmark"},
		{Code: gxscene.OpClose},
		{Code: gxscene.OpElem, Arg: "div"},
		{Code: gxscene.OpAttr, Key: "flex-direction", Arg: "row"},
		{Code: gxscene.OpAttr, Key: "gap", Arg: "14"},
	}
	for i := 0; i < 10; i++ {
		ops = append(ops,
			gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "bg", Arg: "#141b33"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "width", Arg: "208"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "height", Arg: "96"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "border-radius", Arg: "12"},
			gxscene.Op{Code: gxscene.OpElem, Arg: "div"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "color", Arg: "#2dd4bf"},
			gxscene.Op{Code: gxscene.OpAttr, Key: "font-size", Arg: "28"},
			gxscene.Op{Code: gxscene.OpChildText, Arg: fmt.Sprintf("metric %d", i)},
			gxscene.Op{Code: gxscene.OpClose},
			gxscene.Op{Code: gxscene.OpClose},
		)
	}
	ops = append(ops, gxscene.Op{Code: gxscene.OpClose}, gxscene.Op{Code: gxscene.OpClose})
	return *gxscene.Render(gxscene.Template{Ops: ops}).Root()
}

func main() {
	ms := func(t time.Time) float64 { return float64(t.Sub(procMainStart).Microseconds()) / 1000.0 }

	// 接入层装配：gx.use(Backend) → Use(接口值) + 逐窗 Render。
	handle := gxapp.Use(rb.NewBackend())
	app, err := handle.Render(func() any { return buildScene() },
		gxapp.WindowConfig{Title: "startupbench", Width: 760, Height: 560})
	if err != nil {
		fmt.Println("ERR open:", err)
		os.Exit(1)
	}
	tOpen := time.Now()
	root := buildScene()
	_ = app.Render(func() any { return &root })
	tRender := time.Now()

	// rustbackend 专属扩展（SetDispatcher 首帧探针）经 WindowOf 取回。
	win := handle.WindowOf(app).(*rb.Window)

	got := make(chan string, 1)
	win.SetDispatcher(func(ev rb.InputEvent) {
		if ev.Kind == "startup_first_frame" {
			select {
			case got <- ev.Text:
			default:
			}
		}
	})

	done := make(chan struct{})
	go func() { _ = handle.Run(); close(done) }() // Host 唯一主循环

	select {
	case rustInfo := <-got:
		tFrame := time.Now()
		fmt.Printf("main_to_open_ms=%.3f\n", ms(tOpen))
		fmt.Printf("open_to_render_submit_ms=%.3f\n", float64(tRender.Sub(tOpen).Microseconds())/1000.0)
		fmt.Printf("render_submit_to_first_frame_ms=%.3f\n", float64(tFrame.Sub(tRender).Microseconds())/1000.0)
		fmt.Printf("main_to_first_frame_ms=%.3f\n", ms(tFrame))
		fmt.Printf("rust_stages=%s\n", rustInfo)
	case <-time.After(20 * time.Second):
		fmt.Println("ERR timeout waiting for first frame (set GX_STARTUP_PROBE=1)")
		os.Exit(2)
	}
	_ = handle.Close() // Handle.Close 级联：窗口 Close + 后端 Shutdown
	_ = done
}
