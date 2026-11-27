//go:build android

package main

// GX Playground —— Android 端装配（能力层接入）。
//
// 与桌面端 main.go 同构：唯一业务代码 ui.Playground()，来自与网页/桌面共用的
// GX 源码。渲染能力由 gx-backend-android 提供（gxapp.Backend + Window），本文件
// 只负责把 golang.org/x/mobile 的平台事件转发给能力包，业务 touch 语义通过
// TouchSink 回调实现——与桌面 inputBridge 同角色。
//
// 装配形态（与桌面一致）：
//	handle := gxapp.Use(rbandroid.NewBackend())
//	app, _ := handle.Render(func() any { return ui.Playground() }, cfg)
//	win := handle.WindowOf(app).(*rbandroid.Window)
//	app.SetFrameClock(win.FrameClock())

import (
	"fmt"
	"log"
	"time"

	rbandroid "github.com/robertpanvip/gox-backend-android"
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
	gxWin   *rbandroid.Window
	gxApp   *gxapp.App
	host    *gxapp.Handle
	setUp   bool
	glReady   bool
	glctx     gl.Context
	sendPaint func() // = func() { a.Send(paint.Event{}) }，供能力包唤醒 paint 循环

	// lastSize 暂存启动阶段（gxWin 尚未创建）收到的尺寸事件。
	lastSize size.Event

	// lastDpr 记录最近 DPR，供 size 事件转发使用。
	lastDpr float32
)

func main() {
	log.Printf("MAIN: 进入 app.Main（能力层接入）")
	go func() {
		for i := 1; ; i++ {
			time.Sleep(10 * time.Second)
			log.Printf("HEARTBEAT %d alive", i)
		}
	}()
	app.Main(func(a app.App) {
		sendPaint = func() { a.Send(paint.Event{}) }
		for e := range a.Events() {
			switch e := a.Filter(e).(type) {
			case lifecycle.Event:
				switch e.Crosses(lifecycle.StageVisible) {
				case lifecycle.CrossOn:
					glctx, _ = e.DrawContext.(gl.Context)
					glReady = glctx != nil
					if glReady && gxWin != nil {
						if err := gxWin.OnSurfaceReady(glctx); err != nil {
							log.Printf("OnSurfaceReady: %v", err)
						}
						a.Send(paint.Event{})
					}
				case lifecycle.CrossOff:
					glctx = nil
					glReady = false
					if gxWin != nil {
						gxWin.OnSurfaceLost()
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
				if e.PixelsPerPt > 0 {
					lastDpr = e.PixelsPerPt * 72 / 160 // pt(72dpi) → CSS px(160dpi)
				}
				if lastDpr <= 0 {
					lastDpr = 1
				}
				if gxWin != nil {
					gxWin.OnSize(e.WidthPx, e.HeightPx, lastDpr)
					lw, lh := gxWin.LogicalSize()
					ui.SetViewport(lw, lh)
					if gxApp != nil {
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
				}
				if gxWin == nil {
					break
				}
				// Host 装配注入的 UIExecutor 把 drawNow/RAF 任务投递到 Host 共享
				// Loop；Android 事件循环由 app.Main 驱动（不跑 Host.Run），故在
				// 每个 paint 周期手动 drain 一轮，等价于桌面 Host.Run 的 DrainTurn。
				if host != nil {
					host.Host().Loop().DrainTurn()
				}
				needPublish := gxWin.OnPaint()
				if needPublish {
					a.Publish() // GLES 回退路径：swap EGL buffer
				}
				if gxWin.HasPendingPaint() {
					a.Send(paint.Event{})
				}
			case touch.Event:
				if gxWin != nil {
					gxWin.OnTouch(e)
				}
			}
		}
	})
}

// setupGX 用统一 Backend 契约装配整条链，与桌面 main.go 同构。
func setupGX() error {
	handle := gxapp.Use(rbandroid.NewBackend())
	host = handle
	lw, lh := 1080, 720
	if lastSize.WidthPx > 0 {
		dpr := lastDpr
		if dpr <= 0 {
			dpr = 1
		}
		lw, lh = int(float32(lastSize.WidthPx)/dpr), int(float32(lastSize.HeightPx)/dpr)
	}

	gxa, err := handle.Render(func() any { return ui.Playground() }, gxapp.WindowConfig{
		Title: "GX Playground (Android)", Width: lw, Height: lh,
	})
	if err != nil {
		return fmt.Errorf("Render: %w", err)
	}
	gxApp = gxa
	win := handle.WindowOf(gxa).(*rbandroid.Window)
	gxWin = win

	// 帧时钟 + paint 唤醒：能力包 owns 帧调度，装配层注入 a.Send 与平台尺寸。
	win.SetWakePaint(sendPaint)
	gxa.SetFrameClock(win.FrameClock())
	win.SetTouchSink(&playgroundTouch{})
	if glReady && glctx != nil {
		if err := win.OnSurfaceReady(glctx); err != nil {
			log.Printf("OnSurfaceReady（setup 补做）: %v", err)
		}
	}
	if lastSize.WidthPx > 0 {
		win.OnSize(lastSize.WidthPx, lastSize.HeightPx, lastDpr)
	}

	// 触屏端布局前置：首帧按真实视口渲染紧凑布局。
	ui.SetKeyboardEnabled(true)
	ui.SetViewport(lw, lh)

	// Run 解释器接内置终端面板（与桌面/网页同源）。
	interp.SetOutput(ui.ConsolePrint)
	ui.SetRunHandler(func() {
		ui.ConsoleMark("Run")
		res, err := interp.Interpret(ui.EditorText())
		if err != nil {
			ui.ConsolePrint("运行错误: " + err.Error() + "\n")
			ui.SetRunResult(nil, err.Error())
		} else {
			ui.SetRunResult(res.Instance, "")
		}
		if rerr := gxa.Render(func() any { return ui.Playground() }); rerr != nil {
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
		if rerr := gxa.Render(func() any { return ui.Playground() }); rerr != nil {
			log.Printf("主题重渲染失败: %v", rerr)
		}
	})

	injectEditorCharWidth()
	ui.LoadSample(0)
	// Render 已建立首帧；viewport/尺寸变化后再 Render 一次确保紧凑布局生效。
	if rerr := gxa.Render(func() any { return ui.Playground() }); rerr != nil {
		return fmt.Errorf("初始重渲染: %w", rerr)
	}

	// E2E 测试桥：周期输出 UI 状态与可点元素坐标。
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
	return nil
}
