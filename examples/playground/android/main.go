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

	gxapp "github.com/robertpanvip/gox/runtime/app"
	"github.com/robertpanvip/gox-playground/ui"
	"github.com/robertpanvip/gox-playground/ui/interp"
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
)

func main() {
	app.Main(func(a app.App) {
		var glctx gl.Context
		for e := range a.Events() {
			switch e := a.Filter(e).(type) {
			case lifecycle.Event:
				switch e.Crosses(lifecycle.StageVisible) {
				case lifecycle.CrossOn:
					// EGL 上下文随可见性建立：GL 显示端在这里初始化。
					glctx, _ = e.DrawContext.(gl.Context)
					if glctx != nil {
						if gxWin != nil {
							if err := gxWin.ensureGL(glctx); err != nil {
								log.Fatalf("GLES blit 初始化: %v", err)
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
				if gxWin != nil {
					gxWin.resize(e)
				}
			case paint.Event:
				if !setUp && glReady {
					setUp = true
					if err := setupGX(); err != nil {
						log.Fatalf("GX 装配失败: %v", err)
					}
				}
				if gxWin == nil {
					break
				}
				gxWin.drainLoop()
				gxWin.paintNow()
				a.Publish()
				// 光标 blink 等旁路动画没有 frame.Changes，paintNow 会保持
				// dirty——自发下一帧 paint 事件，动画消失后自动停帧。
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

	// P2 宿主接线：UIExecutor 注入共享 Loop（paint 循环 drain）。
	gxApp.SetUIExecutor(win)

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

	// 初始化第一个示例，让编辑器有内容。
	ui.LoadSample(0)

	// gxapp.Run → win.Run：androidWindow.Run 阻塞等待生命周期退出，
	// 事件循环已由 app.Main 驱动，放独立 goroutine 即可。
	go func() {
		if rerr := gxApp2.Run(); rerr != nil {
			log.Printf("app.Run: %v", rerr)
		}
	}()
	return nil
}
