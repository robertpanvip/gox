package main

// GX Playground —— Android 端装配（NativeActivity + GLES2 blit）。
//
// 与桌面端 main.go 同构：唯一业务代码 ui.Playground()，来自与网页/桌面共用的
// GX 源码（ui/app.gx + ui/editor.gx）。触摸事件在 window.go 翻译成 GX 编辑
// 命令与 onClick 冒泡，编辑逻辑零条件编译。
//
// 可行性演示版限制：无软键盘（文本编辑待接 Android IME）、无滚轮路由
// （编辑器拖动滚动已覆盖）。

import (
	"fmt"
	"log"
	"time"

	"github.com/robertpanvip/gox-playground/ui"
	"github.com/robertpanvip/gox-playground/ui/interp"
	gxapp "github.com/robertpanvip/gox/runtime/app"
	"github.com/robertpanvip/gox/widgets"

	"golang.org/x/mobile/app"
	"golang.org/x/mobile/event/lifecycle"
	"golang.org/x/mobile/event/paint"
	"golang.org/x/mobile/event/size"
	"golang.org/x/mobile/event/touch"
	"golang.org/x/mobile/gl"
)

var (
	gxWin   *androidWindow
	gxApp   *gxapp.App
	setUp   bool
	glReady bool

	// frameClock 是统一帧时钟（BACKEND_ARCHITECTURE_V2 阶段 1）：paint.Event
	// （Choreographer/vsync）驱动 OnFrame，Signal 更新与 Present 解耦。
	frameClock *paintClock

	// lastSize 记录最近的窗口尺寸：size 事件可能早于 gxWin 创建（setupGX
	// 在首个 paint 事件才执行），若不暂存，画布尺寸永远是 0。
	lastSize size.Event
)

func main() {
	log.Printf("MAIN: 进入 app.Main")
	// 心跳：证明 Go runtime 与 main goroutine 是否存活（V2 诊断）。
	go func() {
		for i := 1; ; i++ {
			time.Sleep(10 * time.Second)
			log.Printf("HEARTBEAT %d alive", i)
		}
	}()
	app.Main(func(a app.App) {
		log.Printf("MAIN: app.Main 已进入")
		// 帧时钟：RequestFrame → Send paint.Event（CAS 去重），paint.Event 到达时
		// emit 派发帧回调。动画续帧由 Scheduler 经此时钟预约 vsync 帧。
		frameClock = newPaintClock(func() { a.Send(paint.Event{}) })
		var glctx gl.Context
		for e := range a.Events() {
			if lv, ok := a.Filter(e).(lifecycle.Event); ok {
				log.Printf("LIFECYCLE raw: %v cross=%v", lv, lv.Crosses(lifecycle.StageVisible))
			}
			switch e := a.Filter(e).(type) {
			case lifecycle.Event:
				switch e.Crosses(lifecycle.StageVisible) {
				case lifecycle.CrossOn:
					// EGL 上下文随可见性建立（x/mobile 内部使用；V2 的 wgpu
					// surface 由 paintNow 懒初始化，见 paintNowV2）。
					glctx, _ = e.DrawContext.(gl.Context)
					log.Printf("LIFECYCLE CrossOn glctx=%v", glctx != nil)
					if glctx != nil {
						if gxWin != nil {
							if err := gxWin.ensureGL(glctx); err != nil {
								log.Printf("GLES blit 初始化（V2 失败时的回退显示端）: %v", err)
							}
							a.Send(paint.Event{})
						}
						glReady = true
					}
				case lifecycle.CrossOff:
					glctx = nil
					glReady = false
					if gxWin != nil {
						gxWin.suspendGL()
					}
				}
				if e.Crosses(lifecycle.StageDead) == lifecycle.CrossOn {
					if gxApp != nil {
						gxApp.Close()
					}
					return
				}
			case size.Event:
				lastSize = e
				if gxWin != nil {
					gxWin.resize(e)
					// 视口建立/变化 → 响应式布局重排（四端统一断点）。
					// size 事件频率低（启动与旋转），直接重建整树。
					if gxApp != nil {
						lw, lh := gxWin.logicalSize()
						ui.SetViewport(lw, lh)
						if rerr := gxApp.Render(func() any { return ui.Playground() }); rerr != nil {
							log.Printf("viewport 重渲染失败: %v", rerr)
						}
					}
				}
			case paint.Event:
				if !setUp && glReady {
					setUp = true
					log.Printf("PAINT 首帧：开始 setupGX")
					if err := setupGX(); err != nil {
						log.Fatalf("GX 装配失败: %v", err)
					}
					// gxWin 在 setupGX 里才创建：首个 lifecycle.CrossOn 到达时
					// 它还不存在，ensureGL 被跳过——必须在首个 paint 前补做，
					// 否则 visible=false、blit=nil，paintNow 永远早退（黑屏）。
					if glctx != nil && gxWin != nil {
						if err := gxWin.ensureGL(glctx); err != nil {
							log.Fatalf("GLES blit 初始化: %v", err)
						}
					}
				}
				if gxWin == nil {
					break
				}
				gxWin.drainLoop()
				// 统一帧调度（阶段 1）：emit → FrameScheduler dirty 门控 →
				// drawNowErr（Present 标 dirty）。空闲帧不产生任何变更交付。
				if frameClock != nil {
					frameClock.emit()
				}
				gxWin.paintNow()
				if !v2Ready() {
					// 回退路径才走 x/mobile 的 EGL swap；V2 的 present 由
					// wgpu 直接写 ANativeWindow buffer queue。
					a.Publish()
				}
				// 光标 blink 等旁路动画没有 frame.Changes，paintNow 会保持
				// dirty——自发下一帧 paint 事件，动画消失后自动停帧。
				// 声明式 RAF 动画的续帧由 frameClock.RequestFrame 在 drawNowErr
				// 内预约（Send 已发生），不经过这里。
				if gxWin.hasPendingPaint() {
					a.Send(paint.Event{})
				}
			case touch.Event:
				if gxWin != nil {
					gxWin.onTouch(e)
				}
			}
		}
	})
}

// setupGX 完成 GX 整条装配，与桌面 main.go 同构。Run 解释器、主题切换、
// 示例加载全部与桌面/网页共用。
func setupGX() error {
	win := newAndroidWindow()
	// 补放启动阶段（gxWin 尚不存在时）收到的尺寸事件。
	if lastSize.WidthPx > 0 {
		win.resize(lastSize)
	}
	gxWin = win
	// GL 显示端已在 lifecycle.CrossOn 里初始化（ensureGL）。

	// 窗口逻辑尺寸：以当前已知物理尺寸/DPR 兜底（真实 size 事件会再校正）。
	lw, lh := win.logicalSize()
	gxappCfg := gxapp.WindowConfig{
		Title:  "GX Playground (Android)",
		Width:  lw,
		Height: lh,
	}
	gxApp, err := gxapp.OpenWith(gxappCfg, win)
	if err != nil {
		return fmt.Errorf("OpenWith: %w", err)
	}

	// 触屏端：启用虚拟键盘（紧凑布局底部），并按当前视口切响应式布局。
	// 必须在初始 Render 之前——首帧就要按真实视口渲染紧凑布局。
	ui.SetKeyboardEnabled(true)
	ui.SetViewport(lw, lh)

	// P2 宿主接线：UIExecutor 注入共享 Loop（paint 循环 drain）。
	gxApp.SetUIExecutor(win)

	// 统一帧调度（阶段 1）：Signal 更新只标脏 + 预约 paint.Event，同周期合并，
	// 无变化停帧；动画 RAF 续帧经同一时钟对齐 Choreographer/vsync。
	if frameClock != nil {
		gxApp.SetFrameClock(frameClock)
	}

	// Run 解释器接内置终端面板（与桌面/网页同源）。
	interp.SetOutput(ui.ConsolePrint)
	gxApp2 := gxApp
	ui.SetRunHandler(func() {
		ui.ConsoleMark("Run")
		res, err := interp.Interpret(ui.EditorText())
		if err != nil {
			ui.ConsolePrint("运行错误: " + err.Error() + "\n")
			ui.SetRunResult(nil, err.Error())
		} else {
			ui.SetRunResult(res.Instance, "")
		}
		if rerr := gxApp2.Render(func() any { return ui.Playground() }); rerr != nil {
			ui.SetRunResult(nil, "重渲染失败: "+rerr.Error())
		}
	})

	// 主题切换（与桌面同源）。
	ui.SetThemeHandler(func(light bool) {
		if light {
			widgets.SetTheme(widgets.LightTheme)
		} else {
			widgets.SetTheme(widgets.DarkTheme)
		}
		if rerr := gxApp2.Render(func() any { return ui.Playground() }); rerr != nil {
			log.Printf("主题重渲染失败: %v", rerr)
		}
	})

	// 注入实测字宽（必须在首帧 Render 前，否则点击/光标错位）。
	injectEditorCharWidth()

	// 初始化第一个示例，让编辑器有内容（Render 前写 signal，首帧即正确）。
	ui.LoadSample(0)

	// 初始渲染（与桌面端 main.go 同款）：Render 建立场景树并产出首帧。
	// 漏掉这一步会导致树永远不建立、屏幕全黑。
	if rerr := gxApp2.Render(func() any { return ui.Playground() }); rerr != nil {
		return fmt.Errorf("初始渲染: %w", rerr)
	}

	// 测试桥：周期输出 UI 状态与可点元素坐标（E2E 脚本经 logcat 消费）。
	go func() {
		for {
			time.Sleep(1500 * time.Millisecond)
			if gxApp == nil || gxWin == nil {
				continue
			}
			log.Printf("GXTEST state %s", ui.TestSnapshot())
			log.Printf("GXTEST dpr %v", gxWin.Dpr())
			log.Printf("GXTEST elems %s", gxWin.TestElements())
		}
	}()

	// gxapp.Run → win.Run：androidWindow.Run 阻塞等待生命周期退出，
	// 事件循环已由 app.Main 驱动，放独立 goroutine 即可。
	go func() {
		if rerr := gxApp2.Run(); rerr != nil {
			log.Printf("app.Run: %v", rerr)
		}
	}()
	return nil
}
